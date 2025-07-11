package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// TopicManagerDocumentationHandler is a Fiber-compatible HTTP handler that
// processes requests to retrieve documentation for topic managers.
// It acts as the adapter between HTTP requests and the application-layer
// TopicManagerDocumentationService.
type TopicManagerDocumentationHandler struct {
	service *app.TopicManagerDocumentationService
}

// Handle processes an HTTP request to retrieve documentation for a specific topic manager.
// It extracts the `topicManager` query parameter, invokes the service to fetch the documentation,
// and returns it as a JSON response.
// On success, it returns HTTP 200 OK with the documentation content.
// Returns an appropriate error if the service fails.
func (h *TopicManagerDocumentationHandler) Handle(c *fiber.Ctx, params openapi.GetTopicManagerDocumentationParams) error {
	documentation, err := h.service.GetDocumentation(c.UserContext(), c.Query("topicManager"))
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusOK).JSON(openapi.TopicManagerDocumentationResponse{Documentation: documentation})
}

// NewTopicManagerDocumentationHandler creates a new TopicManagerDocumentationHandler
// wired with the given TopicManagerDocumentationProvider.
// It panics if the provider is nil.
func NewTopicManagerDocumentationHandler(provider app.TopicManagerDocumentationProvider) *TopicManagerDocumentationHandler {
	return &TopicManagerDocumentationHandler{service: app.NewTopicManagerDocumentationService(provider)}
}
