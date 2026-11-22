// Command simulator pretends to be a set of beehives: each hive publishes a
// reading to the MQTT broker at a fixed interval until the process is stopped.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/JermanLuka/hive-telemetry/internal/config"
	"github.com/JermanLuka/hive-telemetry/internal/sim"
	"github.com/JermanLuka/hive-telemetry/internal/telemetry"
)

func main() {
	interval := flag.Duration("interval", 5*time.Second, "time between readings of one hive")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, *interval); err != nil {
		logger.Error("simulator stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("interval must be positive, got %s", interval)
	}
	cfg := config.Load()

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTTURL).
		SetClientID("hive-simulator").
		SetAutoReconnect(true).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			logger.Warn("mqtt connection lost", "err", err)
		})
	client := mqtt.NewClient(opts)

	if err := wait(ctx, client.Connect(), 5*time.Second); err != nil {
		return fmt.Errorf("connect to mqtt broker %s: %w", cfg.MQTTURL, err)
	}
	// Disconnect waits up to 250 ms for messages still being sent.
	defer client.Disconnect(250)
	logger.Info("connected to mqtt broker", "url", cfg.MQTTURL)

	// The IDs match deploy/seed.sql, so the hives exist in the database.
	hives := []*sim.Hive{
		sim.NewHive("hive-1", 43.0, true, 1),
		sim.NewHive("hive-2", 37.5, false, 2),
	}

	// One goroutine per hive. Each returns when ctx is cancelled, and
	// wg.Wait blocks until all of them have returned.
	var wg sync.WaitGroup
	for _, h := range hives {
		wg.Go(func() {
			simulate(ctx, client, h, interval, logger)
		})
	}
	logger.Info("simulating", "hives", len(hives), "interval", interval.String())

	wg.Wait()
	logger.Info("stopped")
	return nil
}

// simulate publishes a reading for h on every tick until ctx is cancelled.
func simulate(ctx context.Context, client mqtt.Client, h *sim.Hive, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			r := h.Next(t, interval)
			if err := publish(ctx, client, h.ID, r); err != nil {
				// A lost reading is not fatal: log it and try the next one.
				logger.Warn("publish failed", "hive_id", h.ID, "err", err)
				continue
			}
			logger.Info("published", "hive_id", h.ID, "weight_kg", r.WeightKg)
		}
	}
}

func publish(ctx context.Context, client mqtt.Client, hiveID string, r telemetry.Reading) error {
	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode reading: %w", err)
	}
	tok := client.Publish(telemetry.ReadingTopic(hiveID), telemetry.QoS, false, payload)
	return wait(ctx, tok, 5*time.Second)
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
