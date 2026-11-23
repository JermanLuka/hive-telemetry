package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/JermanLuka/hive-telemetry/internal/store"
	"github.com/JermanLuka/hive-telemetry/migrations"
)

// newTestStore runs the migrations in a fresh schema of the database named by
// TEST_DATABASE_URL and returns a store that uses only that schema. The
// schema is dropped when the test ends. Without TEST_DATABASE_URL the test is
// skipped, so "go test ./..." works without a database.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	// t.Context is already cancelled when cleanups run.
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO hives (id, name) VALUES ('hive-1', 'One'), ('hive-2', 'Two')`); err != nil {
		t.Fatal(err)
	}
	return store.New(pool)
}

var t0 = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

func insert(t *testing.T, s *store.Store, hiveID string, at time.Time, kg float64) {
	t.Helper()
	if _, err := s.InsertReading(t.Context(), store.NewReading{HiveID: hiveID, ObservedAt: at, WeightKg: kg}); err != nil {
		t.Fatal(err)
	}
}

func TestListHives(t *testing.T) {
	s := newTestStore(t)

	hives, err := s.ListHives(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(hives) != 2 || hives[0].ID != "hive-1" || hives[1].ID != "hive-2" {
		t.Errorf("got %+v; want hive-1, hive-2", hives)
	}
}

func TestInsertReading(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	temp := 34.5
	r := store.NewReading{HiveID: "hive-1", ObservedAt: t0, WeightKg: 42.5, TemperatureC: &temp}

	inserted, err := s.InsertReading(ctx, r)
	if err != nil || !inserted {
		t.Fatalf("first insert = %v, %v; want true, nil", inserted, err)
	}

	// The same reading again, as after an MQTT redelivery.
	inserted, err = s.InsertReading(ctx, r)
	if err != nil || inserted {
		t.Fatalf("duplicate insert = %v, %v; want false, nil", inserted, err)
	}

	r.HiveID = "hive-9"
	if _, err := s.InsertReading(ctx, r); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown hive: err = %v; want ErrNotFound", err)
	}

	got, err := s.Readings(ctx, "hive-1", t0, t0.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].WeightKg != 42.5 || got[0].TemperatureC == nil || *got[0].TemperatureC != 34.5 {
		t.Errorf("stored %+v; want one reading of 42.5 kg, 34.5 °C", got)
	}
}

func TestReadings(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	insert(t, s, "hive-1", t0.Add(2*time.Hour), 42) // inserted out of order on purpose
	insert(t, s, "hive-1", t0, 40)
	insert(t, s, "hive-1", t0.Add(time.Hour), 41)

	tests := []struct {
		name     string
		hiveID   string
		from, to time.Time
		limit    int
		wantKg   []float64
	}{
		{"range is half-open", "hive-1", t0, t0.Add(2 * time.Hour), 10, []float64{40, 41}},
		{"limit", "hive-1", t0, t0.Add(3 * time.Hour), 1, []float64{40}},
		{"empty range", "hive-1", t0.Add(-time.Hour), t0, 10, []float64{}},
		{"hive without readings", "hive-2", t0, t0.Add(3 * time.Hour), 10, []float64{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Readings(ctx, tt.hiveID, tt.from, tt.to, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.wantKg) {
				t.Fatalf("got %d readings; want %d", len(got), len(tt.wantKg))
			}
			for i, r := range got {
				if r.WeightKg != tt.wantKg[i] {
					t.Errorf("reading %d = %v kg; want %v", i, r.WeightKg, tt.wantKg[i])
				}
				if r.TemperatureC != nil {
					t.Errorf("reading %d temperature = %v; want nil", i, *r.TemperatureC)
				}
			}
		})
	}

	if _, err := s.Readings(ctx, "hive-9", t0, t0.Add(time.Hour), 10); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown hive: err = %v; want ErrNotFound", err)
	}
}

func TestSummary(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	insert(t, s, "hive-1", t0, 40)
	insert(t, s, "hive-1", t0.Add(time.Hour), 43)
	insert(t, s, "hive-1", t0.Add(2*time.Hour), 42)

	sum, err := s.Summary(ctx, "hive-1", t0, t0.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := store.WeightStats{MinKg: 40, MaxKg: 43, AvgKg: 125.0 / 3, FirstKg: 40, LastKg: 42}
	if sum.Count != 3 || sum.Weight == nil || *sum.Weight != want {
		t.Errorf("got count %d, weight %+v; want 3, %+v", sum.Count, sum.Weight, want)
	}
	if sum.Weight != nil && sum.Weight.ChangeKg() != 2 {
		t.Errorf("ChangeKg = %v; want 2", sum.Weight.ChangeKg())
	}

	empty, err := s.Summary(ctx, "hive-2", t0, t0.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Count != 0 || empty.Weight != nil {
		t.Errorf("hive without readings: got count %d, weight %+v; want 0, nil", empty.Count, empty.Weight)
	}

	if _, err := s.Summary(ctx, "hive-9", t0, t0.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown hive: err = %v; want ErrNotFound", err)
	}
}
