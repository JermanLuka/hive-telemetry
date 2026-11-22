// Package ingest turns MQTT messages from hives into stored readings.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/JermanLuka/hive-telemetry/internal/store"
	"github.com/JermanLuka/hive-telemetry/internal/telemetry"
)

// ErrInvalid marks a message that can never be stored: a bad topic, a
// malformed payload, values out of range or an unknown hive. Retrying such a
// message does not help, so callers drop it.
var ErrInvalid = errors.New("invalid reading")

// Limits for accepted readings.
const (
	maxHiveIDLen = 64 // same limit as the HTTP API
	maxWeightKg  = 250
	minTempC     = -40
	maxTempC     = 70
	// Hive clocks drift; readings slightly ahead of server time are accepted.
	maxClockSkew = 5 * time.Minute
)

// Store is the part of the store the ingestor needs.
type Store interface {
	InsertReading(ctx context.Context, r store.NewReading) (bool, error)
}

// Ingestor validates readings and writes them to the store.
type Ingestor struct {
	store Store
	now   func() time.Time
}

// New returns an Ingestor that writes to st.
func New(st Store) *Ingestor {
	return &Ingestor{store: st, now: time.Now}
}

// Handle validates one message and stores the reading in it. It reports
// whether the reading was new; false means it had been stored before.
//
// Errors that wrap ErrInvalid are permanent. Any other error comes from the
// store and may succeed on retry.
func (i *Ingestor) Handle(ctx context.Context, topic string, payload []byte) (bool, error) {
	hiveID, ok := telemetry.HiveIDFromTopic(topic)
	if !ok || len(hiveID) > maxHiveIDLen {
		return false, fmt.Errorf("%w: unexpected topic %q", ErrInvalid, topic)
	}

	var msg telemetry.Reading
	if err := json.Unmarshal(payload, &msg); err != nil {
		return false, fmt.Errorf("%w: decode payload: %w", ErrInvalid, err)
	}
	if err := i.validate(msg); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	inserted, err := i.store.InsertReading(ctx, store.NewReading{
		HiveID:       hiveID,
		ObservedAt:   msg.ObservedAt,
		WeightKg:     msg.WeightKg,
		TemperatureC: msg.TemperatureC,
	})
	if errors.Is(err, store.ErrNotFound) {
		return false, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func (i *Ingestor) validate(r telemetry.Reading) error {
	switch {
	case r.ObservedAt.IsZero():
		return errors.New("observed_at is missing")
	case r.ObservedAt.After(i.now().Add(maxClockSkew)):
		return fmt.Errorf("observed_at %s is in the future", r.ObservedAt.Format(time.RFC3339))
	case r.WeightKg <= 0 || r.WeightKg > maxWeightKg:
		// A missing weight_kg decodes as 0 and is rejected here too.
		return fmt.Errorf("weight_kg %v is outside (0, %d]", r.WeightKg, maxWeightKg)
	case r.TemperatureC != nil && (*r.TemperatureC < minTempC || *r.TemperatureC > maxTempC):
		return fmt.Errorf("temperature_c %v is outside [%d, %d]", *r.TemperatureC, minTempC, maxTempC)
	}
	return nil
}
