package users

import (
	"context"
	"database/sql"

	"github.com/hzbay/chain-bridge/internal/models"
	"github.com/hzbay/chain-bridge/internal/util"
)

type DatabaseMetricsCollector struct {
	db *sql.DB
}

func NewDatabaseMetricsCollector(db *sql.DB) *DatabaseMetricsCollector {
	return &DatabaseMetricsCollector{db: db}
}

func (c DatabaseMetricsCollector) GetTotalUsersCount(ctx context.Context) float64 {
	log := util.LogFromContext(ctx)

	count, err := models.Users().Count(ctx, c.db)
	if err != nil {
		log.Error().Err(err).Msg("Failed to total user count")
		return 0
	}

	return float64(count)
}
