package ports

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
)

// LookupQuestionHandler is a Fiber-compatible HTTP handler that processes
// lookup requests for a specific question against a provider-defined lookup service.
//
// It belongs to the ports layer and acts as the interface adapter between
// HTTP requests and the application-layer LookupQuestionService.
type LookupQuestionHandler struct {
	service *app.LookupQuestionService
}

// Handle processes an HTTP POST request to perform a lookup on a question.
// It expects a JSON body matching the LookupQuestionBody OpenAPI definition.
//
// The handler parses and validates the request body, then delegates the lookup
// operation to the LookupQuestionService. The response is formatted according
// to the OpenAPI LookupAnswer schema.
//
// On success, it returns a 200 OK response with the lookup results.
// On failure, it returns either a request parsing error or a service-level error.
func (h *LookupQuestionHandler) Handle(c *fiber.Ctx) error {
	var body openapi.LookupQuestionBody

	err := c.BodyParser(&body)
	if err != nil {
		return NewRequestBodyParserError(err)
	}

	dto, err := h.service.LookupQuestion(c.UserContext(), body.Service, body.Query)
	if err != nil {
		return err
	}

	res, err := NewLookupQuestionSuccessResponse(dto)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusOK).JSON(res)
}

// NewLookupQuestionHandler constructs a new LookupQuestionHandler using the given
// LookupQuestionProvider to initialize the underlying LookupQuestionService.
//
// The provider must implement the LookupQuestionProvider interface.
// This function bridges the infrastructure (provider) with the application logic.
// Panics if the provider is nil.
func NewLookupQuestionHandler(provider app.LookupQuestionProvider) *LookupQuestionHandler {
	if provider == nil {
		panic("LookupQuestionProvider cannot be nil")
	}
	return &LookupQuestionHandler{service: app.NewLookupQuestionService(provider)}
}

// NewLookupQuestionSuccessResponse transforms a LookupAnswerDTO into an OpenAPI-compatible
// LookupAnswer response structure.
//
// It marshals the output items and result string into the format expected by the client.
// Returns an error if the transformation fails.
func NewLookupQuestionSuccessResponse(dto *app.LookupAnswerDTO) (*openapi.LookupAnswer, error) {
	var outputs []openapi.OutputListItem

	if len(dto.Outputs) > 0 {
		outputs = make([]openapi.OutputListItem, len(dto.Outputs))
		for i, output := range dto.Outputs {
			outputs[i] = openapi.OutputListItem{
				Beef:        brc24Bytes(output.BEEF),
				OutputIndex: output.OutputIndex,
			}
		}
	}

	return &openapi.LookupAnswer{
		Outputs: outputs,
		Result:  dto.Result,
		Type:    dto.Type,
	}, nil
}

// brc24Bytes converts Go's base64-marshaled []byte form into BRC-24's
// portable JSON byte array. This conversion is confined to the JSON lookup
// response; aggregated binary lookup responses are unchanged.
func brc24Bytes(bytes []byte) []int32 {
	if bytes == nil {
		return nil
	}
	result := make([]int32, len(bytes))
	for i, value := range bytes {
		result[i] = int32(value)
	}
	return result
}
