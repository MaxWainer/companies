package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/companies/internal/platform/health"
)

func HealthCheck(pool *pgxpool.Pool) health.Check {
	return health.Check{
		Name:     "postgres",
		Critical: true,
		Run: func(ctx context.Context) (map[string]string, error) {
			st := pool.Stat()
			details := map[string]string{
				"total_conns":    strconv.Itoa(int(st.TotalConns())),
				"idle_conns":     strconv.Itoa(int(st.IdleConns())),
				"acquired_conns": strconv.Itoa(int(st.AcquiredConns())),
				"max_conns":      strconv.Itoa(int(st.MaxConns())),
			}
			if err := pool.Ping(ctx); err != nil {
				return details, fmt.Errorf("ping: %w", err)
			}
			return details, nil
		},
	}
}
