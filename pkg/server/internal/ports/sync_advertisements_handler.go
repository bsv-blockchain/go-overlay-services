package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// SyncAdvertisementsHandler is a Fiber-compatible HTTP handler that orchestrates
// the processing of synchronize advertisements requests.
// It acts as the adapter between incoming HTTP requests and the
// application-layer AdvertisementsSyncService.
type SyncAdvertisementsHandler struct {
	service *app.AdvertisementsSyncService
}

// Handle processes an HTTP request to synchronize advertisements.
// It delegates the synchronization logic to the underlying service.
// On success, it returns HTTP 200 OK with a confirmation message.
// If an error occurs during synchronization, it returns the appropriate application error.
func (s *SyncAdvertisementsHandler) Handle(c *fiber.Ctx) error {
	err := s.service.SyncAdvertisements(c.Context())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusOK).JSON(NewSyncAdvertisementsSuccessResponse())
}

// NewSyncAdvertisementsHandler creates a new SyncAdvertisementsHandler
// wired with the given SyncAdvertisementsProvider implementation.
// It panics if the provider is nil.
func NewSyncAdvertisementsHandler(provider app.SyncAdvertisementsProvider) *SyncAdvertisementsHandler {
	return &SyncAdvertisementsHandler{service: app.NewAdvertisementsSyncService(provider)}
}

// NewSyncAdvertisementsSuccessResponse constructs a success response
// confirming that the advertisement synchronization request was delegated successfully.
func NewSyncAdvertisementsSuccessResponse() openapi.AdvertisementsSync {
	return openapi.AdvertisementsSync{
		Message: "Advertisement sync request successfully delegated to overlay engine.",
	}
}
