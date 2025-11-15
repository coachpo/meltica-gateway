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
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver for database/sql
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	dbmigrations "github.com/coachpo/meltica/db/migrations"
	"github.com/coachpo/meltica/internal/infra/telemetry"
)

const (
	embeddedMigrationsRoot       = "."
	embeddedMigrationsDescriptor = "embedded://db/migrations"
)

var (
	errNotDirectory = errors.New("migrations path must be a directory")

	migrationsCounter   metric.Int64Counter
	migrationsCounterMu sync.Once
)

// Apply ensures the migrations located at migrationsDir are applied to the Postgres
// instance reachable via dsn. A nil logger disables informational logging.
func Apply(ctx context.Context, dsn, migrationsDir string, logger *log.Logger) error {
	m, cleanup, resolvedDir, err := prepareMigrator(ctx, dsn, migrationsDir, logger)
	if err != nil {
		return err
	}
	defer cleanup()

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

// Rollback steps the database backwards by the requested number of migrations.
// Steps defaults to 1 when zero or negative values are supplied.
func Rollback(ctx context.Context, dsn, migrationsDir string, steps int, logger *log.Logger) error {
	if steps <= 0 {
		steps = 1
	}

	m, cleanup, resolvedDir, err := prepareMigrator(ctx, dsn, migrationsDir, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	if logger != nil {
		logger.Printf("rolling back database migrations: path=%s steps=%d", resolvedDir, steps)
	}

	if err := m.Steps(-steps); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			recordMigrationMetric(ctx, "noop", resolvedDir)
			if logger != nil {
				logger.Printf("no migrations available to roll back")
			}
			return nil
		}
		recordMigrationMetric(ctx, "failed", resolvedDir)
		return fmt.Errorf("rollback migrations: %w", err)
	}

	recordMigrationMetric(ctx, "rolled_back", resolvedDir)
	if logger != nil {
		logger.Printf("database migrations rolled back successfully")
	}
	return nil
}

func prepareMigrator(ctx context.Context, dsn, migrationsDir string, logger *log.Logger) (*migrate.Migrate, func(), string, error) {
	useEmbedded := strings.TrimSpace(migrationsDir) == ""
	var resolvedDir string
	if !useEmbedded {
		var err error
		resolvedDir, err = resolveDir(migrationsDir)
		if err != nil {
			return nil, func() {}, "", err
		}
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, func() {}, "", fmt.Errorf("open migrations connection: %w", err)
	}

	cleanup := func() {
		if cerr := db.Close(); cerr != nil && logger != nil {
			logger.Printf("database migrations close: %v", cerr)
		}
	}

	if err := db.PingContext(ctx); err != nil {
		cleanup()
		return nil, func() {}, "", fmt.Errorf("ping migrations database: %w", err)
	}

	var driverConfig pgxv5.Config
	driver, err := pgxv5.WithInstance(db, &driverConfig)
	if err != nil {
		cleanup()
		return nil, func() {}, "", fmt.Errorf("initialise pgx v5 driver: %w", err)
	}

	var (
		m           *migrate.Migrate
		resolvedRef string
	)

	if useEmbedded {
		sourceDriver, err := iofs.New(dbmigrations.Files, embeddedMigrationsRoot)
		if err != nil {
			cleanup()
			return nil, func() {}, "", fmt.Errorf("initialise embedded migrations: %w", err)
		}
		resolvedRef = embeddedMigrationsDescriptor
		m, err = migrate.NewWithInstance("iofs", sourceDriver, "pgx5", driver)
		if err != nil {
			cleanup()
			return nil, func() {}, "", fmt.Errorf("initialise migrate instance: %w", err)
		}
	} else {
		sourceURL := fileURL(resolvedDir)
		resolvedRef = resolvedDir
		m, err = migrate.NewWithDatabaseInstance(sourceURL, "pgx5", driver)
		if err != nil {
			cleanup()
			return nil, func() {}, "", fmt.Errorf("initialise migrate instance: %w", err)
		}
	}

	return m, func() {
		sourceErr, dbErr := m.Close()
		if logger != nil {
			if sourceErr != nil {
				logger.Printf("database migrations source close: %v", sourceErr)
			}
			if dbErr != nil {
				logger.Printf("database migrations db close: %v", dbErr)
			}
		}
		cleanup()
	}, resolvedRef, nil
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
