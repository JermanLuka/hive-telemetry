package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JermanLuka/hive-telemetry/internal/gen/api"
	"github.com/JermanLuka/hive-telemetry/internal/httpapi"
	"github.com/JermanLuka/hive-telemetry/internal/store"
)

// fakeStore returns canned data and records the arguments it was called with.
type fakeStore struct {
	hives    []store.Hive
	readings []store.Reading
	summary  store.Summary
	err      error

	gotFrom, gotTo time.Time
	gotLimit       int
}

func (s *fakeStore) ListHives(context.Context) ([]store.Hive, error) {
	return s.hives, s.err
}

func (s *fakeStore) Readings(_ context.Context, _ string, from, to time.Time, limit int) ([]store.Reading, error) {
	s.gotFrom, s.gotTo, s.gotLimit = from, to, limit
	return s.readings, s.err
}

func (s *fakeStore) Summary(_ context.Context, _ string, from, to time.Time) (store.Summary, error) {
	s.gotFrom, s.gotTo = from, to
	return s.summary, s.err
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

// newServer starts the real HTTP handler on a local test server.
func newServer(t *testing.T, st httpapi.Store, db httpapi.Pinger) *httptest.Server {
	t.Helper()
	h, err := httpapi.New(st, db, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// newClient returns the ogen-generated client for srv.
func newClient(t *testing.T, srv *httptest.Server) *api.Client {
	t.Helper()
	c, err := api.NewClient(srv.URL, api.WithClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListHives(t *testing.T) {
	berlin := time.FixedZone("CEST", 2*60*60)
	st := &fakeStore{hives: []store.Hive{
		{ID: "hive-1", Name: "Garden, left", CreatedAt: time.Date(2026, 5, 1, 14, 0, 0, 0, berlin)},
	}}
	c := newClient(t, newServer(t, st, fakePinger{}))

	got, err := c.ListHives(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].ID != "hive-1" || got[0].Name != "Garden, left" {
		t.Fatalf("got %+v; want hive-1 / Garden, left", got)
	}
	if want := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC); !got[0].CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v; want %v", got[0].CreatedAt, want)
	}
}

func TestListHiveReadingsDefaults(t *testing.T) {
	temp := 34.6
	st := &fakeStore{readings: []store.Reading{
		{HiveID: "hive-1", ObservedAt: time.Now(), ReceivedAt: time.Now(), WeightKg: 42.5, TemperatureC: &temp},
		{HiveID: "hive-1", ObservedAt: time.Now(), ReceivedAt: time.Now(), WeightKg: 42.6},
	}}
	c := newClient(t, newServer(t, st, fakePinger{}))

	got, err := c.ListHiveReadings(t.Context(), api.ListHiveReadingsParams{HiveId: "hive-1"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d readings; want 2", len(got))
	}
	if v, ok := got[0].TemperatureC.Get(); !ok || v != 34.6 {
		t.Errorf("first temperature = %v, %v; want 34.6, true", v, ok)
	}
	if got[1].TemperatureC.IsSet() {
		t.Errorf("second temperature is set; want it omitted")
	}
	if st.gotLimit != 1000 {
		t.Errorf("limit = %d; want default 1000", st.gotLimit)
	}
	if d := st.gotTo.Sub(st.gotFrom); d != 24*time.Hour {
		t.Errorf("range = %v; want default 24h", d)
	}
	if d := time.Since(st.gotTo); d < 0 || d > time.Minute {
		t.Errorf("to = %v; want about now", st.gotTo)
	}
}

// Postgres stores microseconds; the API must not round them away, or two
// readings within the same second would look identical.
func TestReadingTimesKeepMicroseconds(t *testing.T) {
	observed := time.Date(2026, 5, 1, 12, 0, 0, 123456000, time.UTC)
	st := &fakeStore{readings: []store.Reading{
		{HiveID: "hive-1", ObservedAt: observed, ReceivedAt: observed.Add(1500 * time.Microsecond), WeightKg: 42.5},
	}}
	c := newClient(t, newServer(t, st, fakePinger{}))

	got, err := c.ListHiveReadings(t.Context(), api.ListHiveReadingsParams{HiveId: "hive-1"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || !got[0].ObservedAt.Equal(observed) || !got[0].ReceivedAt.Equal(observed.Add(1500*time.Microsecond)) {
		t.Errorf("got %+v; want observed_at %v and received_at 1.5 ms later", got, observed)
	}
}

func TestGetHiveSummary(t *testing.T) {
	t.Run("with readings", func(t *testing.T) {
		st := &fakeStore{summary: store.Summary{
			HiveID: "hive-1",
			Count:  3,
			Weight: &store.WeightStats{MinKg: 40, MaxKg: 42.5, AvgKg: 41.123456, FirstKg: 41.1, LastKg: 41.99},
		}}
		c := newClient(t, newServer(t, st, fakePinger{}))

		got, err := c.GetHiveSummary(t.Context(), api.GetHiveSummaryParams{HiveId: "hive-1"})
		if err != nil {
			t.Fatal(err)
		}

		w, ok := got.Weight.Get()
		if !ok {
			t.Fatal("weight is not set; want stats")
		}
		// 41.99 - 41.1 is 0.8900000000000006 in float64; the API rounds to grams.
		if w.AvgKg != 41.123 || w.ChangeKg != 0.89 {
			t.Errorf("avg, change = %v, %v; want 41.123, 0.89", w.AvgKg, w.ChangeKg)
		}
	})

	t.Run("without readings", func(t *testing.T) {
		st := &fakeStore{summary: store.Summary{HiveID: "hive-1"}}
		c := newClient(t, newServer(t, st, fakePinger{}))

		got, err := c.GetHiveSummary(t.Context(), api.GetHiveSummaryParams{HiveId: "hive-1"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Count != 0 || got.Weight.IsSet() {
			t.Errorf("got count %d, weight set %v; want 0, false", got.Count, got.Weight.IsSet())
		}
	})
}

// TestErrors sends raw requests, so it also covers what the generated client
// would refuse to send, and checks the {"message": ...} body of each error.
func TestErrors(t *testing.T) {
	notFound := fmt.Errorf("hive %q: %w", "hive-9", store.ErrNotFound)
	tests := []struct {
		name        string
		storeErr    error
		path        string
		wantStatus  int
		wantMessage string // empty: any non-empty message
	}{
		{"unknown hive", notFound, "/hives/hive-9/readings", 404, `hive "hive-9": not found`},
		{"unknown hive summary", notFound, "/hives/hive-9/summary", 404, `hive "hive-9": not found`},
		{"store failure hides details", errors.New("dial tcp 10.0.0.5:5432: refused"), "/hives", 500, "internal error"},
		{"from after to", nil, "/hives/hive-1/readings?from=2026-05-02T00:00:00Z&to=2026-05-01T00:00:00Z", 400, "from must be before to"},
		{"limit too small", nil, "/hives/hive-1/readings?limit=0", 400, ""},
		{"limit too large", nil, "/hives/hive-1/readings?limit=10001", 400, ""},
		{"bad date", nil, "/hives/hive-1/summary?from=yesterday", 400, ""},
		{"unknown path", nil, "/nope", 404, "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, &fakeStore{err: tt.storeErr}, fakePinger{})

			resp, err := srv.Client().Get(srv.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d; want %d", resp.StatusCode, tt.wantStatus)
			}
			var body api.Error
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Message == "" || (tt.wantMessage != "" && body.Message != tt.wantMessage) {
				t.Errorf("message = %q; want %q", body.Message, tt.wantMessage)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
	}{
		{"database up", nil, http.StatusOK},
		{"database down", errors.New("connection refused"), http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, &fakeStore{}, fakePinger{err: tt.pingErr})

			resp, err := srv.Client().Get(srv.URL + "/healthz")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d; want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
