package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// StartGASPSyncHandler is a Fiber-compatible HTTP handler that processes
// requests to initiate a GASP synchronization routine.
// It acts as the adapter between HTTP requests and the application-layer
// StartGASPSyncService, coordinating the sync trigger and formatting the response.
type StartGASPSyncHandler struct {
	service *app.StartGASPSyncService
}

// Handle initiates the GASP sync and returns the appropriate status.
func (h *StartGASPSyncHandler) Handle(c *fiber.Ctx) error {
	if err := h.service.StartGASPSync(c.UserContext()); err != nil {
		return err
	}
	return c.Status(fiber.StatusOK).JSON(NewStartGASPSyncResponse())
}

// NewStartGASPSyncHandler creates a new StartGASPSyncHandler with the given provider.
// If the provider is nil, it panics.
func NewStartGASPSyncHandler(provider app.StartGASPSyncProvider) *StartGASPSyncHandler {
	return &StartGASPSyncHandler{service: app.NewStartGASPSyncService(provider)}
}

// NewStartGASPSyncResponse returns a new StartGASPSync response.
func NewStartGASPSyncResponse() openapi.StartGASPSync {
	return openapi.StartGASPSync{
		Message: "OK",
	}
}
