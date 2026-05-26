package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// BigQueryCost reads INFORMATION_SCHEMA.JOBS_BY_PROJECT to attribute actual cost.
// Pricing: $5 per TiB scanned (on-demand pricing). For flat-rate slots we fall back
// to the same TiB metric × the customer-configured rate (Phase 2).
type BigQueryCost struct {
	db  *store.DB
	enc *store.Encryptor
}

const bqOnDemandUSDPerTiB = 5.0

func (b *BigQueryCost) LookupCost(ctx context.Context, customerID uuid.UUID, queryHash string, startedAt time.Time) (float64, error) {
	_, creds, err := b.db.GetConnectionForCustomer(ctx, b.enc, customerID, "bigquery")
	if err != nil {
		return 0, err
	}
	var c struct {
		ProjectID         string          `json:"project_id"`
		ServiceAccountKey json.RawMessage `json:"service_account_key"`
	}
	if err := json.Unmarshal(creds, &c); err != nil {
		return 0, err
	}
	var opts []option.ClientOption
	if len(c.ServiceAccountKey) > 0 && string(c.ServiceAccountKey) != "null" {
		opts = append(opts, option.WithCredentialsJSON(c.ServiceAccountKey))
	}
	client, err := bigquery.NewClient(ctx, c.ProjectID, opts...)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	// Look up the latest job in the same minute as startedAt with matching query_hash label.
	// Phase 1 simplification: match by total_bytes_billed window (±60s) since we don't
	// inject a job label yet. Real implementation labels every BQ job with query_hash.
	q := client.Query(fmt.Sprintf(`
SELECT total_bytes_billed
FROM `+"`%s.region-us.INFORMATION_SCHEMA.JOBS_BY_PROJECT`"+`
WHERE creation_time BETWEEN TIMESTAMP_SUB(@ts, INTERVAL 60 SECOND) AND TIMESTAMP_ADD(@ts, INTERVAL 60 SECOND)
  AND statement_type IS NOT NULL
ORDER BY creation_time DESC LIMIT 1`, c.ProjectID))
	q.Parameters = []bigquery.QueryParameter{{Name: "ts", Value: startedAt}}
	q.UseLegacySQL = false

	job, err := q.Run(ctx)
	if err != nil {
		return 0, err
	}
	it, err := job.Read(ctx)
	if err != nil {
		return 0, err
	}
	var row []bigquery.Value
	if err := it.Next(&row); err != nil {
		if err == iterator.Done {
			return 0, nil
		}
		return 0, err
	}
	bytesBilled, _ := row[0].(int64)
	tib := float64(bytesBilled) / (1024 * 1024 * 1024 * 1024)
	_ = queryHash
	return tib * bqOnDemandUSDPerTiB, nil
}
