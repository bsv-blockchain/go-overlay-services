package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// LookupProviderDocumentationHandler is a Fiber-compatible HTTP handler that
// retrieves documentation for a specific Lookup Service Provider.
// It belongs to the ports layer and serves as the interface adapter between
// HTTP requests and the application-layer LookupDocumentationService.
type LookupProviderDocumentationHandler struct {
	service *app.LookupDocumentationService
}

// Handle processes an HTTP GET request to fetch documentation for a Lookup Service Provider.
// It extracts the `lookupService` query parameter and delegates the retrieval to
// the LookupDocumentationService.
//
// If the query parameter is missing or if the application service returns an error,
// the appropriate error response is propagated to the client.
//
// On success, it returns a 200 OK response containing the provider's documentation
// in the LookupServiceProviderDocumentationResponse format.
func (h *LookupProviderDocumentationHandler) Handle(c *fiber.Ctx, _ openapi.GetLookupServiceProviderDocumentationParams) error {
	documentation, err := h.service.GetDocumentation(c.UserContext(), c.Query("lookupService"))
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusOK).JSON(openapi.LookupServiceProviderDocumentationResponse{Documentation: documentation})
}

// NewLookupProviderDocumentationHandler constructs a new LookupProviderDocumentationHandler
// with the given LookupServiceDocumentationProvider.
//
// The provider must implement the LookupServiceDocumentationProvider interface.
// Panics if the provider is nil.
func NewLookupProviderDocumentationHandler(provider app.LookupServiceDocumentationProvider) *LookupProviderDocumentationHandler {
	if provider == nil {
		panic("LookupServiceDocumentationProvider cannot be nil")
	}
	return &LookupProviderDocumentationHandler{service: app.NewLookupDocumentationService(provider)}
}
