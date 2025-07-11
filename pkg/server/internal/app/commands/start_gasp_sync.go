package commands

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
)

// StartGASPSyncProvider defines the contract for triggering GASP sync.
type StartGASPSyncProvider interface {
	StartGASPSync(ctx context.Context) error
}

// StartGASPSyncHandler handles the /admin/start-gasp-sync endpoint.
type StartGASPSyncHandler struct {
	provider StartGASPSyncProvider
}

// ResponseStartGASPNodeHandler is the standard response body format.
type ResponseStartGASPNodeHandler struct {
	Message string `json:"message"`
}

// Handle initiates the sync and returns appropriate status.
func (h *StartGASPSyncHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.provider.StartGASPSync(r.Context()); err != nil {
		jsonutil.SendHTTPResponse(w, http.StatusInternalServerError, ResponseStartGASPNodeHandler{Message: "FAILED"})
		return
	}

	jsonutil.SendHTTPResponse(w, http.StatusOK, ResponseStartGASPNodeHandler{Message: "OK"})
}

// NewStartGASPSyncHandler constructs the handler.
func NewStartGASPSyncHandler(provider StartGASPSyncProvider) (*StartGASPSyncHandler, error) {
	if provider == nil {
		return nil, fmt.Errorf("StartGASPSyncProvider is nil")
	}
	return &StartGASPSyncHandler{provider: provider}, nil
}
