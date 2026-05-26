// Package budgets owns the cost-budget gauge: monthly cap × current spend ×
// soft/hard threshold breach detection × forecast. Wired into:
//   - workbench pre-flight cost check (block when over hard cap)
//   - dashboard render (warn when over soft cap)
//   - notifications inbox (push budget_warn on first crossing)
package budgets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Budget struct {
	CustomerID    uuid.UUID `json:"customer_id"`
	MonthlyCapUSD float64   `json:"monthly_cap_usd"`
	SoftPct       int       `json:"soft_pct"`
	HardPct       int       `json:"hard_pct"`
	AlertChannel  string    `json:"alert_channel"`
}

type State struct {
	Budget        Budget    `json:"budget"`
	SpendMTDUSD   float64   `json:"spend_mtd_usd"`
	UtilizationPct float64  `json:"utilization_pct"`
	Forecast      Forecast  `json:"forecast"`
	Status        string    `json:"status"` // "ok" | "soft_breach" | "hard_breach"
	AsOf          time.Time `json:"as_of"`
}

type Forecast struct {
	ProjectedMonthEndUSD float64 `json:"projected_month_end_usd"`
	BurnRatePerDayUSD    float64 `json:"burn_rate_per_day_usd"`
	DaysRemaining        int     `json:"days_remaining"`
}

type Engine struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

func (e *Engine) Upsert(ctx context.Context, b Budget) error {
	_, err := e.pool.Exec(ctx, `
		INSERT INTO cost_budget (customer_id, monthly_cap_usd, soft_pct, hard_pct, alert_channel)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (customer_id) DO UPDATE SET
			monthly_cap_usd = EXCLUDED.monthly_cap_usd,
			soft_pct = EXCLUDED.soft_pct,
			hard_pct = EXCLUDED.hard_pct,
			alert_channel = EXCLUDED.alert_channel,
			updated_at = now()`,
		b.CustomerID, b.MonthlyCapUSD, b.SoftPct, b.HardPct, nilIfEmpty(b.AlertChannel))
	return err
}

// Status reads the current month's cost_attribution + query_log actual costs and
// returns a fully-resolved State suitable for the budget gauge UI.
func (e *Engine) Status(ctx context.Context, customerID uuid.UUID) (State, error) {
	var b Budget
	row := e.pool.QueryRow(ctx,
		`SELECT customer_id, monthly_cap_usd, soft_pct, hard_pct, COALESCE(alert_channel,'')
		 FROM cost_budget WHERE customer_id = $1`, customerID)
	if err := row.Scan(&b.CustomerID, &b.MonthlyCapUSD, &b.SoftPct, &b.HardPct, &b.AlertChannel); err != nil {
		// No budget configured — return zero state, status="ok".
		return State{Budget: Budget{CustomerID: customerID}, Status: "ok", AsOf: time.Now()}, nil
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	daysIntoMonth := int(now.Sub(monthStart).Hours()/24) + 1
	daysRemaining := int(monthEnd.Sub(now).Hours() / 24)

	var spend float64
	_ = e.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(actual_cost_usd), 0) FROM query_log
		 WHERE customer_id = $1 AND started_at >= $2`, customerID, monthStart).Scan(&spend)

	burn := 0.0
	if daysIntoMonth > 0 {
		burn = spend / float64(daysIntoMonth)
	}
	projected := burn*float64(daysIntoMonth+daysRemaining)

	util := 0.0
	if b.MonthlyCapUSD > 0 {
		util = spend / b.MonthlyCapUSD * 100.0
	}
	status := "ok"
	if util >= float64(b.HardPct) {
		status = "hard_breach"
	} else if util >= float64(b.SoftPct) {
		status = "soft_breach"
	}

	return State{
		Budget:         b,
		SpendMTDUSD:    spend,
		UtilizationPct: util,
		Forecast: Forecast{
			ProjectedMonthEndUSD: projected,
			BurnRatePerDayUSD:    burn,
			DaysRemaining:        daysRemaining,
		},
		Status: status,
		AsOf:   now,
	}, nil
}

// PreflightAllow returns false when running a query that would cost `priceUSD`
// would push the customer past the hard cap.
func (e *Engine) PreflightAllow(ctx context.Context, customerID uuid.UUID, priceUSD float64) (bool, State, error) {
	st, err := e.Status(ctx, customerID)
	if err != nil {
		return true, st, err
	}
	if st.Budget.MonthlyCapUSD == 0 {
		return true, st, nil
	}
	hardLimit := st.Budget.MonthlyCapUSD * float64(st.Budget.HardPct) / 100.0
	if st.SpendMTDUSD+priceUSD > hardLimit {
		return false, st, nil
	}
	return true, st, nil
}

// Budgets is an alias for Engine retained for API symmetry with the fo-1
// hard-cap helper (callers use `*Budgets` in signatures while existing code
// continues to use `*Engine`).
type Budgets = Engine

// HardBlock is the strict fo-1 hard-cap gate: reject the query when the
// month-to-date spend plus the supplied estimate exceeds the monthly cap
// scaled by `hard_cap_factor` (defaults to 1.0 = exactly the cap). Unlike
// PreflightAllow, which uses cost_budget.hard_pct as a percentage, this is
// an absolute multiplier and is what the workbench's "hard block" mode and
// the proxy-side pre-execution gate consume.
//
// Returns allowed=true when no budget is configured (cap == 0) — callers
// are expected to treat absence of a budget as "unbounded".
func (b *Budgets) HardBlock(ctx context.Context, customerID uuid.UUID, estimatedCostUSD float64) (bool, string, error) {
	hardCapFactor := 1.0
	st, err := b.Status(ctx, customerID)
	if err != nil {
		return false, "status_error", err
	}
	if st.Budget.MonthlyCapUSD == 0 {
		return true, "no_budget", nil
	}
	limit := st.Budget.MonthlyCapUSD * hardCapFactor
	if st.SpendMTDUSD+estimatedCostUSD > limit {
		return false, "hard_cap_exceeded", nil
	}
	return true, "ok", nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
