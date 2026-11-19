// Package store reads and writes hive data in PostgreSQL.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when the requested hive does not exist.
// Callers check it with errors.Is(err, store.ErrNotFound).
var ErrNotFound = errors.New("not found")

// Hive is a monitored beehive.
type Hive struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Reading is one measurement from a hive scale.
type Reading struct {
	HiveID       string
	ObservedAt   time.Time // sensor time
	ReceivedAt   time.Time // server time
	WeightKg     float64
	TemperatureC *float64 // nil when the scale has no temperature sensor
}

// Summary describes a hive's readings over a time range.
type Summary struct {
	HiveID string
	From   time.Time
	To     time.Time
	Count  int
	Weight *WeightStats // nil when there are no readings in the range
}

// WeightStats are weight statistics over a time range.
type WeightStats struct {
	MinKg   float64
	MaxKg   float64
	AvgKg   float64
	FirstKg float64 // earliest reading in the range
	LastKg  float64 // latest reading in the range
}

// ChangeKg is the weight gained (positive) or lost (negative) over the
// range. A rising hive is usually bringing in nectar.
func (w WeightStats) ChangeKg() float64 {
	return w.LastKg - w.FirstKg
}

// Store gives access to hive data. It is safe for concurrent use,
// because the underlying pool is.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store that uses the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ListHives returns all hives, ordered by ID.
func (s *Store) ListHives(ctx context.Context) ([]Hive, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at FROM hives ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query hives: %w", err)
	}
	// CollectRows reads every row, closes rows and reports any error.
	hives, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Hive, error) {
		var h Hive
		err := row.Scan(&h.ID, &h.Name, &h.CreatedAt)
		return h, err
	})
	if err != nil {
		return nil, fmt.Errorf("read hives: %w", err)
	}
	return hives, nil
}

// Readings returns up to limit readings of one hive with
// from <= observed_at < to, oldest first. It returns ErrNotFound if the
// hive does not exist, and an empty slice if it has no readings in range.
func (s *Store) Readings(ctx context.Context, hiveID string, from, to time.Time, limit int) ([]Reading, error) {
	if err := s.ensureHive(ctx, hiveID); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT hive_id, observed_at, received_at, weight_kg, temperature_c
		FROM hive_readings
		WHERE hive_id = $1 AND observed_at >= $2 AND observed_at < $3
		ORDER BY observed_at
		LIMIT $4`,
		hiveID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query readings: %w", err)
	}
	readings, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reading, error) {
		var r Reading
		err := row.Scan(&r.HiveID, &r.ObservedAt, &r.ReceivedAt, &r.WeightKg, &r.TemperatureC)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("read readings: %w", err)
	}
	return readings, nil
}

// Summary returns statistics for one hive with from <= observed_at < to.
// It returns ErrNotFound if the hive does not exist.
func (s *Store) Summary(ctx context.Context, hiveID string, from, to time.Time) (Summary, error) {
	if err := s.ensureHive(ctx, hiveID); err != nil {
		return Summary{}, err
	}

	// With no matching rows, count is 0 and every aggregate is NULL,
	// so the aggregates are scanned into pointers.
	var (
		count               int
		minKg, maxKg, avgKg *float64
		firstKg, lastKg     *float64
	)
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       min(weight_kg),
		       max(weight_kg),
		       avg(weight_kg),
		       (array_agg(weight_kg ORDER BY observed_at ASC))[1],
		       (array_agg(weight_kg ORDER BY observed_at DESC))[1]
		FROM hive_readings
		WHERE hive_id = $1 AND observed_at >= $2 AND observed_at < $3`,
		hiveID, from, to,
	).Scan(&count, &minKg, &maxKg, &avgKg, &firstKg, &lastKg)
	if err != nil {
		return Summary{}, fmt.Errorf("query summary: %w", err)
	}

	sum := Summary{HiveID: hiveID, From: from, To: to, Count: count}
	if count > 0 {
		sum.Weight = &WeightStats{
			MinKg:   *minKg,
			MaxKg:   *maxKg,
			AvgKg:   *avgKg,
			FirstKg: *firstKg,
			LastKg:  *lastKg,
		}
	}
	return sum, nil
}

// ensureHive returns ErrNotFound if no hive has the given ID. It lets
// callers tell "unknown hive" (404) apart from "no readings yet" (empty).
func (s *Store) ensureHive(ctx context.Context, hiveID string) error {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hives WHERE id = $1)`, hiveID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check hive %q: %w", hiveID, err)
	}
	if !exists {
		return fmt.Errorf("hive %q: %w", hiveID, ErrNotFound)
	}
	return nil
}
