package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JermanLuka/hive-telemetry/internal/store"
)

// fakeStore keeps readings in memory and behaves like the real store for
// duplicates and unknown hives.
type fakeStore struct {
	hives map[string]bool
	seen  map[string]bool
	err   error // returned by every call when set
}

func newFakeStore(hiveIDs ...string) *fakeStore {
	s := &fakeStore{hives: map[string]bool{}, seen: map[string]bool{}}
	for _, id := range hiveIDs {
		s.hives[id] = true
	}
	return s
}

func (s *fakeStore) InsertReading(_ context.Context, r store.NewReading) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if !s.hives[r.HiveID] {
		return false, fmt.Errorf("hive %q: %w", r.HiveID, store.ErrNotFound)
	}
	key := r.HiveID + "@" + r.ObservedAt.String()
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

var now = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

func newTestIngestor(st Store) *Ingestor {
	i := New(st)
	i.now = func() time.Time { return now }
	return i
}

func TestHandle(t *testing.T) {
	const topic = "hives/hive-1/readings"
	tests := []struct {
		name         string
		topic        string
		payload      string
		wantInserted bool
		wantInvalid  bool
	}{
		{"valid", topic, `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5,"temperature_c":34.6}`, true, false},
		{"without temperature", topic, `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5}`, true, false},
		{"slightly in the future", topic, `{"observed_at":"2026-05-01T12:04:00Z","weight_kg":42.5}`, true, false},
		{"unknown hive", "hives/hive-9/readings", `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5}`, false, true},
		{"bad topic", "hives/hive-1/status", `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5}`, false, true},
		{"hive id too long", "hives/" + strings.Repeat("x", 65) + "/readings", `{}`, false, true},
		{"not json", topic, `not json`, false, true},
		{"missing observed_at", topic, `{"weight_kg":42.5}`, false, true},
		{"far in the future", topic, `{"observed_at":"2026-05-01T13:00:00Z","weight_kg":42.5}`, false, true},
		{"missing weight", topic, `{"observed_at":"2026-05-01T11:59:00Z"}`, false, true},
		{"negative weight", topic, `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":-1}`, false, true},
		{"weight too high", topic, `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":251}`, false, true},
		{"temperature too low", topic, `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5,"temperature_c":-41}`, false, true},
		{"temperature too high", topic, `{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5,"temperature_c":71}`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := newTestIngestor(newFakeStore("hive-1"))

			inserted, err := ing.Handle(t.Context(), tt.topic, []byte(tt.payload))

			if inserted != tt.wantInserted {
				t.Errorf("inserted = %v; want %v", inserted, tt.wantInserted)
			}
			if got := errors.Is(err, ErrInvalid); got != tt.wantInvalid {
				t.Errorf("errors.Is(err, ErrInvalid) = %v (err: %v); want %v", got, err, tt.wantInvalid)
			}
			if !tt.wantInvalid && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestHandleDuplicate(t *testing.T) {
	ing := newTestIngestor(newFakeStore("hive-1"))
	payload := []byte(`{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5}`)

	first, err := ing.Handle(t.Context(), "hives/hive-1/readings", payload)
	if err != nil || !first {
		t.Fatalf("first Handle = %v, %v; want true, nil", first, err)
	}
	second, err := ing.Handle(t.Context(), "hives/hive-1/readings", payload)
	if err != nil || second {
		t.Fatalf("second Handle = %v, %v; want false, nil", second, err)
	}
}

// A store failure is not the message's fault: it must not be reported as
// invalid, or the caller would drop a good reading instead of retrying.
func TestHandleStoreErrorIsNotInvalid(t *testing.T) {
	st := newFakeStore("hive-1")
	st.err = errors.New("connection refused")
	ing := newTestIngestor(st)

	_, err := ing.Handle(t.Context(), "hives/hive-1/readings", []byte(`{"observed_at":"2026-05-01T11:59:00Z","weight_kg":42.5}`))

	if err == nil {
		t.Fatal("err = nil; want the store error")
	}
	if errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v; want an error that does not wrap ErrInvalid", err)
	}
}
