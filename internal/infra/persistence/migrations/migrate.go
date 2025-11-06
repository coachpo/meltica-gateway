// Package migrations wires golang-migrate execution for Meltica's persistence layer.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	pgxv5 "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file" // file:// migrations loader
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/infra/telemetry"
)

var (
	errNotDirectory = errors.New("migrations path must be a directory")

	migrationsCounter   metric.Int64Counter
	migrationsCounterMu sync.Once
)

// Apply ensures the migrations located at migrationsDir are applied to the Postgres
// instance reachable via dsn. A nil logger disables informational logging.
func Apply(ctx context.Context, dsn, migrationsDir string, logger *log.Logger) error {
	resolvedDir, err := resolveDir(migrationsDir)
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migrations connection: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && logger != nil {
			logger.Printf("database migrations close: %v", cerr)
		}
	}()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migrations database: %w", err)
	}

	var driverConfig pgxv5.Config
	driver, err := pgxv5.WithInstance(db, &driverConfig)
	if err != nil {
		return fmt.Errorf("initialise pgx v5 driver: %w", err)
	}

	sourceURL := fileURL(resolvedDir)
	m, err := migrate.NewWithDatabaseInstance(sourceURL, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("initialise migrate instance: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if logger == nil {
			return
		}
		if sourceErr != nil {
			logger.Printf("database migrations source close: %v", sourceErr)
		}
		if dbErr != nil {
			logger.Printf("database migrations db close: %v", dbErr)
		}
	}()

	if logger != nil {
		logger.Printf("running database migrations: path=%s", resolvedDir)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			recordMigrationMetric(ctx, "noop", resolvedDir)
			if logger != nil {
				logger.Printf("database migrations up-to-date")
			}
			return nil
		}
		recordMigrationMetric(ctx, "failed", resolvedDir)
		return fmt.Errorf("apply migrations: %w", err)
	}

	if logger != nil {
		logger.Printf("database migrations applied successfully")
	}
	recordMigrationMetric(ctx, "applied", resolvedDir)

	return nil
}

func resolveDir(dir string) (string, error) {
	clean := strings.TrimSpace(dir)
	if clean == "" {
		return "", fmt.Errorf("migrations path required")
	}

	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("resolve migrations path: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("migrations directory: %w", err)
		}
		return "", fmt.Errorf("stat migrations directory: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("migrations directory: %w", errNotDirectory)
	}

	return abs, nil
}

func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	u := new(url.URL)
	u.Scheme = "file"
	u.Path = slashed
	return u.String()
}

func recordMigrationMetric(ctx context.Context, result, path string) {
	migrationsCounterMu.Do(func() {
		meter := otel.Meter("persistence.migrations")
		counter, err := meter.Int64Counter("meltica_db_migrations_total",
			metric.WithDescription("Total migrations executed via golang-migrate"),
			metric.WithUnit("{migration}"))
		if err == nil {
			migrationsCounter = counter
		}
	})
	if migrationsCounter == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("environment", telemetry.Environment()),
		attribute.String("result", result),
	}
	if path != "" {
		attrs = append(attrs, attribute.String("migrations_path", path))
	}
	migrationsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}
