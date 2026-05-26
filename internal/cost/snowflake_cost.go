package cost

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	sf "github.com/snowflakedb/gosnowflake"

	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// SnowflakeCost reads ACCOUNT_USAGE.QUERY_HISTORY for actual credit consumption.
// Cost = bytes_scanned (or compilation+execution time × warehouse credit rate). Phase 1
// uses bytes_scanned × $5/TiB as a uniform proxy that matches BQ's on-demand model;
// flat-rate Snowflake accounts override via UV_SF_USD_PER_CREDIT in Phase 2.
type SnowflakeCost struct {
	db         *store.DB
	enc        *store.Encryptor
	USDPerTiB  float64
}

// sfDefaultUSDPerTiB is overridden at runtime when SnowflakeCost.USDPerTiB is non-zero.
// Customers on flat-rate / committed-spend contracts can set this via
// UV_SF_USD_PER_TIB so savings dashboards reflect real prices, not the public list.
const sfDefaultUSDPerTiB = 5.0

type sfCostCreds struct {
	Account   string `json:"account"`
	User      string `json:"user"`
	Password  string `json:"password,omitempty"`
	Role      string `json:"role,omitempty"`
	Warehouse string `json:"warehouse,omitempty"`
	Database  string `json:"database,omitempty"`
	Schema    string `json:"schema,omitempty"`
}

func (s *SnowflakeCost) LookupCost(ctx context.Context, customerID uuid.UUID, queryHash string, startedAt time.Time) (float64, error) {
	_, creds, err := s.db.GetConnectionForCustomer(ctx, s.enc, customerID, "snowflake")
	if err != nil {
		return 0, err
	}
	var c sfCostCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return 0, err
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
	dsn, err := sf.DSN(cfg)
	if err != nil {
		return 0, err
	}
	dbh, err := sql.Open("snowflake", dsn)
	if err != nil {
		return 0, err
	}
	defer dbh.Close()

	row := dbh.QueryRowContext(ctx, fmt.Sprintf(`
SELECT BYTES_SCANNED FROM SNOWFLAKE.ACCOUNT_USAGE.QUERY_HISTORY
WHERE START_TIME BETWEEN DATEADD(SECOND,-60,'%s') AND DATEADD(SECOND,60,'%s')
ORDER BY START_TIME DESC LIMIT 1`, startedAt.UTC().Format(time.RFC3339), startedAt.UTC().Format(time.RFC3339)))
	var bytesScanned int64
	if err := row.Scan(&bytesScanned); err != nil {
		return 0, err
	}
	tib := float64(bytesScanned) / (1024 * 1024 * 1024 * 1024)
	_ = queryHash
	rate := s.USDPerTiB
	if rate == 0 {
		rate = sfDefaultUSDPerTiB
	}
	return tib * rate, nil
}
