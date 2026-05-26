// Package semantic compiles a YAML semantic model (Cube/MetricFlow-flavored)
// into SQL fragments the dashboarding layer can stitch into queries.
package semantic

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

// Model = one semantic model file. Tables → measures + dimensions → joins.
type Model struct {
	Name       string      `yaml:"name"`
	Source     string      `yaml:"source"` // FQN
	Dimensions []Dimension `yaml:"dimensions"`
	Measures   []Measure   `yaml:"measures"`
	Joins      []Join      `yaml:"joins"`
}

type Dimension struct {
	Name string `yaml:"name"`
	Expr string `yaml:"expr"`
	Type string `yaml:"type"` // string|number|timestamp
}

type Measure struct {
	Name string `yaml:"name"`
	Expr string `yaml:"expr"`
	Type string `yaml:"type"` // sum|count|avg|min|max|count_distinct
}

type Join struct {
	Name string `yaml:"name"`
	On   string `yaml:"on"`
}

func Parse(src string) (*Model, error) {
	var m Model
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		return nil, fmt.Errorf("parse semantic yaml: %w", err)
	}
	if m.Name == "" || m.Source == "" {
		return nil, fmt.Errorf("semantic model requires name + source")
	}
	return &m, nil
}

// Compile a query: chosen dimensions + measures + filters → SQL.
type Query struct {
	Dimensions []string
	Measures   []string
	Filters    []string // raw SQL predicates; the API enforces RLS injection upstream
	Limit      int
}

func (m *Model) Compile(q Query) (string, error) {
	var selects []string
	for _, d := range q.Dimensions {
		dim := m.findDim(d)
		if dim == nil {
			return "", fmt.Errorf("unknown dimension %q", d)
		}
		selects = append(selects, fmt.Sprintf("%s AS %s", dim.Expr, d))
	}
	for _, ms := range q.Measures {
		me := m.findMeasure(ms)
		if me == nil {
			return "", fmt.Errorf("unknown measure %q", ms)
		}
		selects = append(selects, fmt.Sprintf("%s AS %s", aggregate(me), ms))
	}
	if len(selects) == 0 {
		return "", fmt.Errorf("query needs at least one dimension or measure")
	}
	sql := "SELECT " + strings.Join(selects, ", ") + " FROM " + m.Source
	if len(q.Filters) > 0 {
		sql += " WHERE " + strings.Join(q.Filters, " AND ")
	}
	if len(q.Dimensions) > 0 {
		sql += " GROUP BY " + strings.Join(q.Dimensions, ", ")
	}
	if q.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", q.Limit)
	}
	return sql, nil
}

func aggregate(m *Measure) string {
	switch strings.ToLower(m.Type) {
	case "count":
		return fmt.Sprintf("COUNT(%s)", m.Expr)
	case "count_distinct":
		return fmt.Sprintf("COUNT(DISTINCT %s)", m.Expr)
	case "avg":
		return fmt.Sprintf("AVG(%s)", m.Expr)
	case "min":
		return fmt.Sprintf("MIN(%s)", m.Expr)
	case "max":
		return fmt.Sprintf("MAX(%s)", m.Expr)
	default: // sum default
		return fmt.Sprintf("SUM(%s)", m.Expr)
	}
}

func (m *Model) findDim(name string) *Dimension {
	for i := range m.Dimensions {
		if m.Dimensions[i].Name == name {
			return &m.Dimensions[i]
		}
	}
	return nil
}

func (m *Model) findMeasure(name string) *Measure {
	for i := range m.Measures {
		if m.Measures[i].Name == name {
			return &m.Measures[i]
		}
	}
	return nil
}

// CompileWithParams parses a YAML semantic model, compiles the supplied
// query, then applies parameter overrides via simple `:param_name` → value
// substitution on the resulting SQL. Used by the what-if endpoint (bu-8) so
// callers can preview the effect of changing a model parameter without
// mutating the stored YAML.
//
// Caller is responsible for re-validating that the substituted SQL still
// parses (the api layer does this with pg_query_go). We do NOT execute.
func CompileWithParams(yamlSrc string, q Query, params map[string]string) (string, error) {
	m, err := Parse(yamlSrc)
	if err != nil {
		return "", err
	}
	sql, err := m.Compile(q)
	if err != nil {
		return "", err
	}
	for k, v := range params {
		if k == "" {
			continue
		}
		// Bare-token substitution. Replaces ":name" verbatim with the override
		// value. Quoting is the caller's responsibility (e.g. send "'2024-01-01'"
		// rather than "2024-01-01" for a date literal).
		sql = strings.ReplaceAll(sql, ":"+k, v)
	}
	return sql, nil
}

// Store persists semantic_model rows.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Upsert(ctx context.Context, customerID uuid.UUID, name, src, compiled string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO semantic_model (customer_id, name, yaml_source, compiled_sql)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (customer_id, name) DO UPDATE
		SET yaml_source = EXCLUDED.yaml_source, compiled_sql = EXCLUDED.compiled_sql,
		    version = semantic_model.version + 1, updated_at = now()`,
		customerID, name, src, compiled)
	return err
}

func (s *Store) List(ctx context.Context, customerID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM semantic_model WHERE customer_id = $1 ORDER BY name`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
