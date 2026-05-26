// Package pii auto-tags columns with sensitivity classifications (pii.email,
// pii.ssn, pii.phone, financial, hipaa). Two-pass detection:
//
//  1. Regex against column NAMES (cheap, runs on every introspect).
//  2. Sample-value check (runs from a nightly cron, samples N rows per column,
//     applies regex against actual data).
//
// Stored in column_tag with confidence + source. The semantic layer + rbac
// matrix consult these tags to enforce field-level access policies.
package pii

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Tag string

const (
	TagEmail     Tag = "pii.email"
	TagSSN       Tag = "pii.ssn"
	TagPhone     Tag = "pii.phone"
	TagCreditCard Tag = "pii.credit_card"
	TagDOB       Tag = "pii.dob"
	TagIP        Tag = "pii.ip"
	TagFinancial Tag = "financial"
	TagHIPAA     Tag = "hipaa"
)

type rule struct {
	Tag         Tag
	NamePattern *regexp.Regexp
	ValuePattern *regexp.Regexp
	NameWeight  float64 // confidence when name matches
	ValueWeight float64 // confidence when value matches
}

var rules = []rule{
	{TagEmail, regexp.MustCompile(`(?i)\b(email|e_mail|mail|email_address)\b`), regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`), 0.7, 0.95},
	{TagSSN, regexp.MustCompile(`(?i)\b(ssn|social|tax_id|tin)\b`), regexp.MustCompile(`^\d{3}-?\d{2}-?\d{4}$`), 0.9, 0.99},
	{TagPhone, regexp.MustCompile(`(?i)\b(phone|mobile|cell|telephone|tel)\b`), regexp.MustCompile(`^\+?\d[\d\s\-().]{6,}\d$`), 0.7, 0.85},
	{TagCreditCard, regexp.MustCompile(`(?i)\b(card|cc|credit_card|pan)\b`), regexp.MustCompile(`^\d{4}[ -]?\d{4}[ -]?\d{4}[ -]?\d{4}$`), 0.8, 0.99},
	{TagDOB, regexp.MustCompile(`(?i)\b(dob|date_of_birth|birthdate|birthday)\b`), nil, 0.85, 0},
	{TagIP, regexp.MustCompile(`(?i)\b(ip|ip_address|client_ip|remote_ip)\b`), regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`), 0.7, 0.95},
}

// ScoreName returns (tag, confidence) for a column name; returns ("", 0) if no match.
func ScoreName(colName string) (Tag, float64) {
	for _, r := range rules {
		if r.NamePattern != nil && r.NamePattern.MatchString(colName) {
			return r.Tag, r.NameWeight
		}
	}
	return "", 0
}

// ScoreValue inspects a sample value (call once per row sample).
func ScoreValue(v string) (Tag, float64) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", 0
	}
	for _, r := range rules {
		if r.ValuePattern != nil && r.ValuePattern.MatchString(v) {
			return r.Tag, r.ValueWeight
		}
	}
	return "", 0
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Upsert(ctx context.Context, customerID uuid.UUID, tableFQN, columnName string, tag Tag, confidence float64, source string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO column_tag (customer_id, table_fqn, column_name, tag, confidence, source)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (customer_id, table_fqn, column_name, tag) DO UPDATE SET
			confidence = GREATEST(column_tag.confidence, EXCLUDED.confidence),
			source = CASE WHEN EXCLUDED.confidence > column_tag.confidence THEN EXCLUDED.source ELSE column_tag.source END`,
		customerID, tableFQN, columnName, tag, confidence, source)
	return err
}

func (s *Store) ForTable(ctx context.Context, customerID uuid.UUID, tableFQN string) (map[string][]Tag, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT column_name, tag FROM column_tag WHERE customer_id = $1 AND table_fqn = $2`,
		customerID, tableFQN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Tag{}
	for rows.Next() {
		var col, t string
		if err := rows.Scan(&col, &t); err != nil {
			return nil, err
		}
		out[col] = append(out[col], Tag(t))
	}
	return out, rows.Err()
}
