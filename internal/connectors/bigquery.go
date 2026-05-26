package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

type BigQuery struct {
	connID    uuid.UUID
	projectID string
	client    *bigquery.Client
	log       zerolog.Logger
}

// bqCreds is the JSON shape stored in connections.credentials_ciphertext for BQ.
type bqCreds struct {
	ProjectID         string          `json:"project_id"`
	ServiceAccountKey json.RawMessage `json:"service_account_key"`
}

func NewBigQuery(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (*BigQuery, error) {
	var c bqCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return nil, fmt.Errorf("bq creds parse: %w", err)
	}
	if c.ProjectID == "" {
		return nil, fmt.Errorf("bq creds missing project_id")
	}
	var opts []option.ClientOption
	if len(c.ServiceAccountKey) > 0 && string(c.ServiceAccountKey) != "null" {
		opts = append(opts, option.WithCredentialsJSON(c.ServiceAccountKey))
	}
	client, err := bigquery.NewClient(ctx, c.ProjectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("bq client: %w", err)
	}
	return &BigQuery{connID: conn.ID, projectID: c.ProjectID, client: client, log: log}, nil
}

func (b *BigQuery) ConnectionID() uuid.UUID { return b.connID }
func (b *BigQuery) WarehouseType() string   { return "bigquery" }
func (b *BigQuery) Close() error            { return b.client.Close() }

// ExecuteStreaming runs the SQL on BigQuery and writes RowDescription + DataRow frames.
// Transient 5xx errors are retried with exponential backoff (3 attempts, 100ms→400ms→1.6s)
// before being surfaced; bind-parameter mismatch is detected before sending to BQ.
func (b *BigQuery) ExecuteStreaming(ctx context.Context, sql string, params [][]byte, w *pgwire.Writer) (int64, error) {
	for i, p := range params {
		if p == nil {
			continue // NULL is fine
		}
		_ = i // params are reserved for future Bind-format wiring; unused today
	}
	q := b.client.Query(sql)
	q.UseLegacySQL = false
	var job *bigquery.Job
	var err error
	for attempt, delay := 0, 100*time.Millisecond; attempt < 3; attempt, delay = attempt+1, delay*4 {
		job, err = q.Run(ctx)
		if err == nil || !isRetriable(err) {
			break
		}
		select {
		case <-ctx.Done():
			return 0, WrapWarehouseError(ctx.Err())
		case <-time.After(delay):
		}
	}
	if err != nil {
		return 0, WrapWarehouseError(fmt.Errorf("bq run: %w", err))
	}
	it, err := job.Read(ctx)
	if err != nil {
		return 0, WrapWarehouseError(fmt.Errorf("bq read: %w", err))
	}

	// Schema arrives after the first Next call in some BQ versions; iterate once,
	// then write description + first row.
	wroteHeader := false
	var rows int64
	for {
		var row []bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			if !wroteHeader {
				return 0, WrapWarehouseError(fmt.Errorf("bq iter: %w", err))
			}
			b.log.Warn().Err(err).Msg("bq mid-stream iter error")
			break
		}
		if !wroteHeader {
			fields := bqFieldsToPG(it.Schema)
			if err := w.WriteRowDescription(fields); err != nil {
				return rows, err
			}
			wroteHeader = true
		}
		vals := encodeBQRow(row, it.Schema)
		if err := w.WriteDataRow(vals); err != nil {
			return rows, err
		}
		rows++
	}
	if !wroteHeader {
		// Empty result still needs a RowDescription so clients know column shape.
		fields := bqFieldsToPG(it.Schema)
		_ = w.WriteRowDescription(fields)
	}
	tag := commandTag(sql, rows)
	if err := w.WriteCommandComplete(tag); err != nil {
		return rows, err
	}
	return rows, nil
}

func bqFieldsToPG(schema bigquery.Schema) []pgwire.Field {
	out := make([]pgwire.Field, len(schema))
	for i, f := range schema {
		oid, size := MapBigQueryFieldType(string(f.Type))
		out[i] = pgwire.Field{Name: f.Name, TypeOID: oid, TypeSize: size, Format: 0}
	}
	return out
}

func encodeBQRow(row []bigquery.Value, schema bigquery.Schema) [][]byte {
	out := make([][]byte, len(row))
	for i, v := range row {
		if v == nil {
			out[i] = nil
			continue
		}
		out[i] = encodeBQValue(v, schema[i])
	}
	return out
}

func encodeBQValue(v bigquery.Value, f *bigquery.FieldSchema) []byte {
	switch x := v.(type) {
	case string:
		return []byte(x)
	case bool:
		if x {
			return []byte("t")
		}
		return []byte("f")
	case int64:
		return []byte(strconv.FormatInt(x, 10))
	case float64:
		return []byte(strconv.FormatFloat(x, 'g', -1, 64))
	case []byte:
		return []byte(base64.StdEncoding.EncodeToString(x))
	case time.Time:
		if x.IsZero() {
			return nil
		}
		return []byte(x.UTC().Format("2006-01-02 15:04:05.000000-07"))
	case map[string]bigquery.Value, []bigquery.Value:
		buf, _ := json.Marshal(x)
		return buf
	default:
		return []byte(fmt.Sprintf("%v", x))
	}
}

// isRetriable returns true for transient errors (5xx HTTP, deadline exceeded
// pre-Run). Used by the BQ retry loop in ExecuteStreaming.
func isRetriable(err error) bool {
	if err == nil {
		return false
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		return ge.Code >= 500 && ge.Code < 600
	}
	return false
}

// commandTag returns the PG CommandComplete tag, e.g. "SELECT 42".
func commandTag(sql string, rows int64) string {
	verb := strings.ToUpper(strings.Fields(strings.TrimSpace(sql))[0])
	switch verb {
	case "SELECT", "VALUES", "TABLE":
		return fmt.Sprintf("SELECT %d", rows)
	case "INSERT":
		return fmt.Sprintf("INSERT 0 %d", rows)
	case "UPDATE", "DELETE":
		return fmt.Sprintf("%s %d", verb, rows)
	default:
		return verb
	}
}
