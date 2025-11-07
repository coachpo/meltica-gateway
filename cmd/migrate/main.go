package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/infra/persistence/migrations"
)

const (
	defaultMigrationsPath = "db/migrations"
	defaultTimeout        = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dsn     = flag.String("database", "", "PostgreSQL DSN (e.g. postgresql://user:pass@host:5432/db)")
		dir     = flag.String("path", defaultMigrationsPath, "Directory containing SQL migrations")
		timeout = flag.Duration("timeout", defaultTimeout, "Maximum time to wait for database connectivity")
		quiet   = flag.Bool("quiet", false, "Suppress informational logs")
	)
	flag.Parse()

	if strings.TrimSpace(*dsn) == "" {
		return errors.New("-database flag is required")
	}
	if strings.TrimSpace(*dir) == "" {
		return errors.New("-path flag is required")
	}

	args := flag.Args()
	if len(args) == 0 {
		return errors.New("command required (up|down)")
	}

	var logger *log.Logger
	if !*quiet {
		logger = log.New(os.Stdout, "meltica-migrate ", log.LstdFlags)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch args[0] {
	case "up":
		if err := migrations.Apply(ctx, *dsn, *dir, logger); err != nil {
			return err
		}
	case "down":
		steps := 1
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid down steps %q: %w", args[1], err)
			}
			steps = n
		}
		if err := migrations.Rollback(ctx, *dsn, *dir, steps, logger); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q (expected up or down)", args[0])
	}

	return nil
}
