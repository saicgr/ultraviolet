package connectors

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	sf "github.com/snowflakedb/gosnowflake"

	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

type Snowflake struct {
	connID uuid.UUID
	db     *sql.DB
	log    zerolog.Logger
}

// sfCreds is the JSON shape stored in connections.credentials_ciphertext for Snowflake.
// Either Password OR PrivateKeyPEM must be supplied.
type sfCreds struct {
	Account       string `json:"account"`
	User          string `json:"user"`
	Password      string `json:"password,omitempty"`
	PrivateKeyPEM string `json:"private_key_pem,omitempty"`
	Role          string `json:"role,omitempty"`
	Warehouse     string `json:"warehouse,omitempty"`
	Database      string `json:"database,omitempty"`
	Schema        string `json:"schema,omitempty"`
}

func NewSnowflake(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (*Snowflake, error) {
	var c sfCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return nil, fmt.Errorf("sf creds parse: %w", err)
	}
	if c.Account == "" || c.User == "" {
		return nil, fmt.Errorf("sf creds missing account/user")
	}
	cfg := &sf.Config{
		Account:   c.Account,
		User:      c.User,
		Password:  c.Password,
		Role:      c.Role,
		Warehouse: c.Warehouse,
		Database:  c.Database,
		Schema:    c.Schema,
	}
	if c.PrivateKeyPEM != "" {
		key, err := parseRSAPrivateKey([]byte(c.PrivateKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("sf private key: %w", err)
		}
		cfg.PrivateKey = key
		cfg.Authenticator = sf.AuthTypeJwt
	}
	dsn, err := sf.DSN(cfg)
	if err != nil {
		return nil, fmt.Errorf("sf dsn: %w", err)
	}
	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("sf open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sf ping: %w", err)
	}
	return &Snowflake{connID: conn.ID, db: db, log: log}, nil
}

func (s *Snowflake) ConnectionID() uuid.UUID { return s.connID }
func (s *Snowflake) WarehouseType() string   { return "snowflake" }
func (s *Snowflake) Close() error            { return s.db.Close() }

func (s *Snowflake) ExecuteStreaming(ctx context.Context, sqlText string, params [][]byte, w *pgwire.Writer) (int64, error) {
	rows, err := s.db.QueryContext(ctx, sqlText)
	if err != nil {
		return 0, fmt.Errorf("sf query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return 0, err
	}
	fields := make([]pgwire.Field, len(cols))
	for i, t := range types {
		oid, size := MapSnowflakeType(strings.ToUpper(t.DatabaseTypeName()))
		fields[i] = pgwire.Field{Name: cols[i], TypeOID: oid, TypeSize: size, Format: 0}
	}
	if err := w.WriteRowDescription(fields); err != nil {
		return 0, err
	}

	scanDest := make([]interface{}, len(cols))
	scanPtrs := make([]interface{}, len(cols))
	for i := range scanDest {
		scanPtrs[i] = &scanDest[i]
	}
	var n int64
	for rows.Next() {
		if err := rows.Scan(scanPtrs...); err != nil {
			return n, fmt.Errorf("sf scan: %w", err)
		}
		vals := make([][]byte, len(cols))
		for i, v := range scanDest {
			vals[i] = encodeSFValue(v)
		}
		if err := w.WriteDataRow(vals); err != nil {
			return n, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	return n, w.WriteCommandComplete(commandTag(sqlText, n))
}

// parseRSAPrivateKey accepts PKCS8 (BEGIN PRIVATE KEY) or PKCS1 (BEGIN RSA PRIVATE KEY) PEM.
// Snowflake JWT auth requires an RSA key; we reject non-RSA keys explicitly.
func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	any, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := any.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return rsaKey, nil
}

func encodeSFValue(v interface{}) []byte {
	if v == nil {
		return nil
	}
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
		return []byte(x.UTC().Format("2006-01-02 15:04:05.000000-07"))
	default:
		rv := reflect.ValueOf(x)
		return []byte(fmt.Sprintf("%v", rv.Interface()))
	}
}
