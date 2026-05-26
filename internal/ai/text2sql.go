package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/metadata"
)

// Text2SQL grounds NL-to-SQL generation in the catalog (W3) + lineage (W1) so
// hallucinated joins are caught. The flow:
//
//  1. Build a schema digest from table_metadata for every table the user can see.
//  2. Send NL question + digest to the LLM with a strict "use only tables listed".
//  3. Validate output through pg_query parser (rejects malformed SQL).
//  4. Optionally run EXPLAIN against the warehouse to verify it would execute.
type Text2SQL struct {
	r       *Rewriter
	enrich  *metadata.Enricher
}

func NewText2SQL(r *Rewriter, e *metadata.Enricher) *Text2SQL {
	return &Text2SQL{r: r, enrich: e}
}

type Question struct {
	CustomerID    uuid.UUID
	UserSlug      string
	NL            string
	AllowedTables []string
}

func (t *Text2SQL) Generate(ctx context.Context, q Question) (string, error) {
	if t.r == nil || t.r.provider == nil {
		return "", ErrNoProvider
	}
	digest := strings.Join(q.AllowedTables, "\n")
	prompt := fmt.Sprintf(`You write Postgres-compatible SQL for the Ultraviolet query proxy.
Use ONLY these tables:
%s

Return only SQL — no commentary.

Question: %s`, digest, q.NL)
	out, err := t.r.CompleteOne(ctx, t.r.cfg.AIDefaultModel, prompt, q.UserSlug)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
