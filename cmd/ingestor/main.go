// Command ingestor subscribes to hive readings on the MQTT broker, validates
// them and stores them in PostgreSQL.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JermanLuka/hive-telemetry/internal/config"
	"github.com/JermanLuka/hive-telemetry/internal/ingest"
	"github.com/JermanLuka/hive-telemetry/internal/store"
	"github.com/JermanLuka/hive-telemetry/internal/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		logger.Error("ingestor stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg := config.Load()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create db pool: %w", err)
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	logger.Info("connected to postgres")

	ing := ingest.New(store.New(pool))

	// The MQTT client calls the message handler on its own goroutine. msgs
	// hands each message to the loop at the end of run, which processes them
	// one at a time. While that loop is busy the handler waits, and the
	// broker holds further messages back.
	msgs := make(chan mqtt.Message)

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTTURL).
		SetClientID("hive-ingestor").
		// Persistent session: while the ingestor is down, the broker keeps
		// QoS 1 messages for it and delivers them when it reconnects.
		SetCleanSession(false).
		// A message is acked only once it is stored (or rejected). If the
		// ingestor stops before that, the broker sends it again.
		SetAutoAckDisabled(true).
		SetAutoReconnect(true).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			logger.Warn("mqtt connection lost", "err", err)
		}).
		// Messages the broker kept for this session can arrive before
		// Subscribe returns, so all messages go through the default handler.
		SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
			select {
			case msgs <- m:
			case <-ctx.Done():
				// Shutting down: the message stays unacked and is redelivered.
			}
		})
	client := mqtt.NewClient(opts)

	if err := wait(ctx, client.Connect(), 5*time.Second); err != nil {
		return fmt.Errorf("connect to mqtt broker %s: %w", cfg.MQTTURL, err)
	}
	// Deferred after pool.Close, so it runs first: stop receiving, then
	// close the database.
	defer client.Disconnect(250)

	if err := wait(ctx, client.Subscribe(telemetry.ReadingSubscription, telemetry.QoS, nil), 5*time.Second); err != nil {
		return fmt.Errorf("subscribe to %s: %w", telemetry.ReadingSubscription, err)
	}
	logger.Info("subscribed", "topic", telemetry.ReadingSubscription)

	for {
		select {
		case <-ctx.Done():
			logger.Info("stopped")
			return nil
		case m := <-msgs:
			process(ctx, ing, m, logger)
		}
	}
}

// process stores one message, retrying store errors (such as the database
// being down) until it succeeds or ctx is cancelled. The message is acked
// once it is stored or known to be invalid.
func process(ctx context.Context, ing *ingest.Ingestor, m mqtt.Message, logger *slog.Logger) {
	for attempt := 1; ; attempt++ {
		inserted, err := handle(ctx, ing, m)
		switch {
		case err == nil:
			m.Ack()
			logger.Info("reading stored", "topic", m.Topic(), "duplicate", !inserted)
			return
		case errors.Is(err, ingest.ErrInvalid):
			// Retrying will not fix a bad message, so it is dropped.
			m.Ack()
			logger.Warn("reading rejected", "topic", m.Topic(), "err", err)
			return
		}

		delay := retryDelay(attempt)
		logger.Error("store reading failed", "topic", m.Topic(), "attempt", attempt, "retry_in", delay.String(), "err", err)
		select {
		case <-ctx.Done():
			return // not acked, so the broker redelivers it after a restart
		case <-time.After(delay):
		}
	}
}

func handle(ctx context.Context, ing *ingest.Ingestor, m mqtt.Message) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return ing.Handle(ctx, m.Topic(), m.Payload())
}

// retryDelay doubles with each attempt (1s, 2s, 4s, ...) up to 30s.
func retryDelay(attempt int) time.Duration {
	return min(time.Second<<min(attempt-1, 5), 30*time.Second)
}

var errTimeout = errors.New("timed out")

// wait blocks until the MQTT operation behind tok completes, the timeout
// passes, or ctx is cancelled, whichever comes first.
func wait(ctx context.Context, tok mqtt.Token, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-tok.Done():
		return tok.Error()
	case <-timer.C:
		return errTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}
