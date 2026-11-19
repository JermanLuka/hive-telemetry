package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/JermanLuka/hive-telemetry/internal/gen/api"
)

// Pinger checks that the database is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// New returns the HTTP handler for the whole service: the generated API
// routes plus GET /healthz.
func New(st Store, db Pinger, logger *slog.Logger) (http.Handler, error) {
	h := &Handler{store: st, logger: logger, now: time.Now}

	apiServer, err := api.NewServer(h,
		api.WithErrorHandler(handleRequestError),
		api.WithNotFound(handleNotFound),
	)
	if err != nil {
		return nil, fmt.Errorf("create api server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(db))
	mux.Handle("/", apiServer)
	return mux, nil
}

// healthz reports whether the service can reach its database. Kubernetes
// and load balancers call it to decide whether to send traffic here.
func healthz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unreachable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	}
}
