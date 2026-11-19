// Command api serves the hive-telemetry HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JermanLuka/hive-telemetry/internal/config"
	"github.com/JermanLuka/hive-telemetry/internal/httpapi"
	"github.com/JermanLuka/hive-telemetry/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
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

	handler, err := httpapi.New(store.New(pool), pool, logger)
	if err != nil {
		return err
	}

	// Timeouts protect the server from slow or stuck clients. Without them
	// a client that sends its request one byte per minute holds a
	// connection (and a goroutine) forever.
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.Info("listening", "addr", cfg.HTTPAddr)
	return serve(ctx, srv, logger)
}

// serve runs srv until ctx is cancelled, then shuts it down gracefully:
// stop accepting new connections, wait for requests in progress to finish
// (but not forever), then return.
func serve(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	// ListenAndServe blocks, so it runs in its own goroutine and reports
	// how it ended through errCh. The buffer of 1 lets that goroutine
	// exit even if nobody is reading anymore.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		// The server stopped on its own, e.g. the port is already in use.
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// ctx is already cancelled, so the shutdown deadline needs a fresh one.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	// After Shutdown, ListenAndServe returns http.ErrServerClosed, which
	// means a clean stop.
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("stopped")
	return nil
}

// shutdownTimeout is how long requests in progress get to finish.
// Kubernetes waits 30 seconds after SIGTERM before killing the pod,
// so this must stay below that.
const shutdownTimeout = 10 * time.Second
