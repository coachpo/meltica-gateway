package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/infra/telemetry"
)

// ObservePoolMetrics registers observable gauges that report pgx pool health.
// Gauges emit total, idle, acquired, and constructing connection counts.
func ObservePoolMetrics(pool *pgxpool.Pool, poolName string) {
	if pool == nil {
		return
	}
	normalized := strings.TrimSpace(poolName)
	if normalized == "" {
		normalized = "primary"
	}
	attrs := []attribute.KeyValue{
		attribute.String("environment", telemetry.Environment()),
		attribute.String("db_pool", normalized),
	}

	meter := otel.Meter("postgres.pool")
	if _, err := meter.Int64ObservableGauge("meltica_db_pool_connections_total",
		metric.WithDescription("Total connections (idle + acquired + constructing)"),
		metric.WithUnit("{connection}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			stat := pool.Stat()
			observer.Observe(int64(stat.TotalConns()), metric.WithAttributes(attrs...))
			return nil
		}),
	); err != nil {
		return
	}
	if _, err := meter.Int64ObservableGauge("meltica_db_pool_connections_idle",
		metric.WithDescription("Idle connections ready for checkout"),
		metric.WithUnit("{connection}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			stat := pool.Stat()
			observer.Observe(int64(stat.IdleConns()), metric.WithAttributes(attrs...))
			return nil
		}),
	); err != nil {
		return
	}
	if _, err := meter.Int64ObservableGauge("meltica_db_pool_connections_acquired",
		metric.WithDescription("Connections currently acquired by callers"),
		metric.WithUnit("{connection}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			stat := pool.Stat()
			observer.Observe(int64(stat.AcquiredConns()), metric.WithAttributes(attrs...))
			return nil
		}),
	); err != nil {
		return
	}
	if _, err := meter.Int64ObservableGauge("meltica_db_pool_connections_constructing",
		metric.WithDescription("Connections currently being constructed"),
		metric.WithUnit("{connection}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			stat := pool.Stat()
			observer.Observe(int64(stat.ConstructingConns()), metric.WithAttributes(attrs...))
			return nil
		}),
	); err != nil {
		return
	}
}
