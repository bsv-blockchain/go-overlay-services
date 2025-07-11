package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// MetadataServiceProvider defines the contract for retrieving metadata
// by delegating to the application layer's metadata service.
type MetadataServiceProvider interface {
	GetMetadata(t app.MetadataType) (app.MetadataDTO, error)
}

// MetadataHandler handles incoming HTTP requests related to metadata,
// acting as the adapter between the transport (e.g., Fiber) and the application layer.
type MetadataHandler struct {
	service MetadataServiceProvider
}

// Handle processes an HTTP request for metadata of a specific type.
// It delegates the call to the application service and serializes the response.
// Returns an HTTP 200 response with metadata on success, or an error otherwise.
func (h *MetadataHandler) Handle(c *fiber.Ctx, t app.MetadataType) error {
	metadata, err := h.service.GetMetadata(t)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusOK).JSON(NewSuccessMetadataResponse(metadata))
}

// NewMetadataHandler creates a new instance of MetadataHandler.
// Panics if the provided MetadataServiceProvider is nil.
func NewMetadataHandler(s MetadataServiceProvider) *MetadataHandler {
	if s == nil {
		panic("metadata service provider cannot be nil")
	}
	return &MetadataHandler{service: s}
}

// NewSuccessMetadataResponse converts the internal application-layer MetadataDTO
// into the format expected by the OpenAPI layer (i.e., MetadataResponse).
func NewSuccessMetadataResponse(dto app.MetadataDTO) openapi.MetadataResponse {
	response := make(openapi.MetadataResponse, len(dto))
	for name, metadata := range dto {
		response[name] = openapi.ServiceMetadata{
			Name:             metadata.Name,
			ShortDescription: metadata.Description,
			IconURL:          metadata.IconURL,
			Version:          metadata.Version,
			InformationURL:   metadata.InfoURL,
		}
	}
	return response
}
