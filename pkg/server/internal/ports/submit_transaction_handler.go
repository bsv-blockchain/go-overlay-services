package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/gofiber/fiber/v2"
)

// XTopicsHeader defines the HTTP header key used to specify transaction topics.
const XTopicsHeader = "x-topics"

// SubmitTransactionHandler is a Fiber-compatible HTTP handler that processes
// incoming transaction submission requests.
// It validates the request body and headers, delegates transaction submission to the service layer,
// and returns a response formatted according to the OpenAPI specification.
type SubmitTransactionHandler struct {
	service *app.SubmitTransactionService
}

// Handle processes an HTTP request to submit a transaction.
// It expects the `x-topics` header to be present and valid.
// On success, it returns HTTP 200 OK with a STEAK response (openapi.SubmitTransactionResponse).
// If an error occurs during transaction submission, it returns the corresponding application error.
func (s *SubmitTransactionHandler) Handle(c *fiber.Ctx, params openapi.SubmitTransactionParams) error {
	steak, err := s.service.SubmitTransaction(c.UserContext(), params.XTopics, c.Body()...)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusOK).JSON(NewSubmitTransactionSuccessResponse(steak))
}

// NewSubmitTransactionHandler creates a new SubmitTransactionHandler with the given provider.
// It panics if the provider is nil.
func NewSubmitTransactionHandler(provider app.SubmitTransactionProvider) *SubmitTransactionHandler {
	return &SubmitTransactionHandler{service: app.NewSubmitTransactionService(provider)}
}

// NewSubmitTransactionSuccessResponse converts the internal STEAK data structure
// into an OpenAPI-compatible SubmitTransactionResponse.
func NewSubmitTransactionSuccessResponse(steak *overlay.Steak) *openapi.SubmitTransactionResponse {
	if steak == nil {
		return &openapi.SubmitTransactionResponse{
			STEAK: make(openapi.STEAK),
		}
	}

	response := openapi.SubmitTransactionResponse{
		STEAK: make(openapi.STEAK, len(*steak)),
	}

	for key, instructions := range *steak {
		ancillaryIDs := make([]string, 0, len(instructions.AncillaryTxids))
		for _, id := range instructions.AncillaryTxids {
			ancillaryIDs = append(ancillaryIDs, id.String())
		}

		response.STEAK[key] = openapi.AdmittanceInstructions{
			AncillaryTxIDs: ancillaryIDs,
			CoinsRemoved:   instructions.CoinsRemoved,
			CoinsToRetain:  instructions.CoinsToRetain,
			OutputsToAdmit: instructions.OutputsToAdmit,
		}
	}
	return &response
}
