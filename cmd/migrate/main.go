// Command migrate applies or rolls back the database schema.
//
// Usage:
//
//	migrate          apply all pending migrations (same as "up")
//	migrate up       apply all pending migrations
//	migrate down     roll back the most recent migration
//	migrate status   list migrations and whether they are applied
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for database/sql
	"github.com/pressly/goose/v3"

	"github.com/JermanLuka/hive-telemetry/internal/config"
	"github.com/JermanLuka/hive-telemetry/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	if err := run(context.Background(), logger, command); err != nil {
		logger.Error("migrate failed", "command", command, "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, command string) error {
	cfg := config.Load()

	// goose works with database/sql, so we open the database through
	// pgx's database/sql adapter instead of pgxpool.
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	switch command {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if len(results) == 0 {
			logger.Info("database is up to date")
		}
		for _, r := range results {
			logger.Info("applied", "version", r.Source.Version, "file", r.Source.Path, "duration_ms", r.Duration.Milliseconds())
		}

	case "down":
		r, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("roll back migration: %w", err)
		}
		logger.Info("rolled back", "version", r.Source.Version, "file", r.Source.Path, "duration_ms", r.Duration.Milliseconds())

	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read migration status: %w", err)
		}
		for _, s := range statuses {
			logger.Info("migration", "version", s.Source.Version, "file", s.Source.Path, "state", s.State)
		}

	default:
		return fmt.Errorf("unknown command %q (want up, down or status)", command)
	}
	return nil
}
