package ports

import (
	"fmt"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// ARCIngestHandler is a Fiber-compatible HTTP handler that accepts incoming
// Merkle proof ingestion requests and delegates processing to the ARCIngestService.
// It belongs to the ports layer and acts as the interface adapter between
// HTTP requests and application-layer logic.
type ARCIngestHandler struct {
	service *app.ARCIngestService
}

// Handle processes an HTTP POST request for ingesting a Merkle proof.
// It expects a JSON body matching the ArcIngestBody OpenAPI definition.
//
// Request validation errors (e.g. malformed JSON or invalid fields)
// will return a request parsing error.
// Application-level validation and processing are delegated to ARCIngestService.
//
// On success, it returns a 200 OK response with a success message.
func (h *ARCIngestHandler) Handle(c *fiber.Ctx) error {
	var body openapi.ArcIngestBody

	err := c.BodyParser(&body)
	if err != nil {
		return NewRequestBodyParserError(err)
	}

	err = h.service.ProcessIngest(c.Context(), body.Txid, body.MerklePath, body.BlockHeight)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusOK).JSON(NewARCIngestSuccessResponse(body.Txid))
}

// NewARCIngestHandler creates a new ARCIngestHandler using the given
// OverlayEngineProvider as the underlying provider for the ARCIngestService.
//
// The provider must implement ARCIngestProvider.
// This function bridges the infrastructure (engine) with the application logic.
func NewARCIngestHandler(provider engine.OverlayEngineProvider) *ARCIngestHandler {
	return &ARCIngestHandler{service: app.NewARCIngestService(provider)}
}

// NewARCIngestSuccessResponse returns a standardized success response
// when a Merkle proof is successfully ingested.
//
// The response includes a "success" status and a message with the transaction ID.
func NewARCIngestSuccessResponse(txID string) *openapi.ArcIngestResponse {
	return &openapi.ArcIngestResponse{
		Status:  "success",
		Message: fmt.Sprintf("Transaction with ID:%s successfully ingested.", txID),
	}
}
