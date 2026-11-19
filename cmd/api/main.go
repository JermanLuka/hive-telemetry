// Command api serves the hive-telemetry HTTP API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JermanLuka/hive-telemetry/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(context.Background(), logger); err != nil {
		logger.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

// run holds the real startup logic. Returning an error instead of calling
// os.Exit lets deferred cleanup (pool.Close) run before the process exits.
func run(ctx context.Context, logger *slog.Logger) error {
	cfg := config.Load()

	// pgxpool.New only parses the URL; it does not connect yet.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create db pool: %w", err)
	}
	defer pool.Close()

	// Ping opens a real connection. The timeout stops it from hanging
	// when Postgres is down or unreachable.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	logger.Info("connected to postgres")
	return nil
}
