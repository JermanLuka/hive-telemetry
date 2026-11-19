package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/JermanLuka/hive-telemetry/internal/gen/api"
	"github.com/JermanLuka/hive-telemetry/internal/store"
)

// NewError turns an error returned by a handler method into a response.
// ogen calls it for every error that is not already an *api.ErrorStatusCode.
func (h *Handler) NewError(ctx context.Context, err error) *api.ErrorStatusCode {
	if errors.Is(err, store.ErrNotFound) {
		return &api.ErrorStatusCode{
			StatusCode: http.StatusNotFound,
			Response:   api.Error{Message: err.Error()},
		}
	}

	// Anything else is our fault. Log the details, but don't send them to
	// the client: they can contain SQL or connection information.
	h.logger.ErrorContext(ctx, "request failed", "err", err)
	return &api.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   api.Error{Message: "internal error"},
	}
}

// handleRequestError answers errors that ogen detects before a handler
// method runs, such as an invalid query parameter, in the API's own
// {"message": ...} shape instead of ogen's default one.
func handleRequestError(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	code := ogenerrors.ErrorCode(err)
	msg := err.Error()
	if code >= http.StatusInternalServerError {
		msg = http.StatusText(code)
	}
	writeError(w, code, msg)
}

// handleNotFound answers requests for paths the API does not have.
func handleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(api.Error{Message: msg})
}
