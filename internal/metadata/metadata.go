// Package metadata pulls catalog enrichment from external systems and merges it
// into the lineage graph. Each provider returns []TableMeta; the enricher
// upserts to table_metadata.
package metadata

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TableMeta struct {
	FQN         string
	Description string
	Owner       string
	SLAMinutes  int
	Tags        []string
	Source      string // dbt|tableau|hex|looker|bq|metaplane|atlan|manual
	Payload     map[string]any
}

// Provider abstracts an external metadata source.
type Provider interface {
	Name() string
	Pull(ctx context.Context) ([]TableMeta, error)
}

type Enricher struct{ pool *pgxpool.Pool }

func NewEnricher(pool *pgxpool.Pool) *Enricher { return &Enricher{pool: pool} }

// Apply upserts a batch from one provider into table_metadata.
func (e *Enricher) Apply(ctx context.Context, customerID uuid.UUID, batch []TableMeta) error {
	for _, t := range batch {
		var payload []byte
		if t.Payload != nil {
			payload, _ = json.Marshal(t.Payload)
		}
		_, err := e.pool.Exec(ctx, `
			INSERT INTO table_metadata (customer_id, fqn, description, owner, sla_minutes, tags, source, payload)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (customer_id, fqn, source) DO UPDATE SET
				description = EXCLUDED.description,
				owner = EXCLUDED.owner,
				sla_minutes = EXCLUDED.sla_minutes,
				tags = EXCLUDED.tags,
				payload = EXCLUDED.payload,
				updated_at = now()`,
			customerID, t.FQN, nilIfEmpty(t.Description), nilIfEmpty(t.Owner),
			nilIfZero(t.SLAMinutes), t.Tags, t.Source, payload)
		if err != nil {
			return err
		}
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nilIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
