package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// RequestSyncResponseHandler is a Fiber-compatible HTTP handler that processes
// requests to retrieve synchronization response data from a remote topic.
// It acts as the adapter between HTTP requests and the application-layer
// RequestSyncResponseService.
type RequestSyncResponseHandler struct {
	service *app.RequestSyncResponseService
}

// Handle processes an HTTP POST request to fetch sync response data.
// It expects a JSON request body matching the RequestSyncResponseJSONRequestBody OpenAPI schema,
// and requires the topic to be passed as an X-BSV-Topic header parameter.
//
// It transforms request values into domain models and delegates processing
// to the application service. The response is returned in OpenAPI-compatible format.
//
// On success, returns 200 OK with a list of UTXOs and a since marker.
// On failure, returns a request parsing or application error.
func (h *RequestSyncResponseHandler) Handle(c *fiber.Ctx, params openapi.RequestSyncResponseParams) error {
	var body openapi.RequestSyncResponseJSONRequestBody

	err := c.BodyParser(&body)
	if err != nil {
		return NewRequestBodyParserError(err)
	}

	dto, err := h.service.RequestSyncResponse(
		c.Context(),
		app.NewTopic(params.XBSVTopic),
		app.Version(body.Version),
		app.Since(body.Since),
	)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusOK).JSON(NewRequestSyncResponseSuccessResponse(dto))
}

// NewRequestSyncResponseHandler constructs a new RequestSyncResponseHandler
// with the provided application-level RequestSyncResponseProvider.
// It connects the infrastructure provider to the business logic service.
//
// Panics if the provider is nil.
func NewRequestSyncResponseHandler(provider app.RequestSyncResponseProvider) *RequestSyncResponseHandler {
	return &RequestSyncResponseHandler{service: app.NewRequestSyncResponseService(provider)}
}

// NewRequestSyncResponseSuccessResponse converts a RequestSyncResponseDTO into a
// RequestSyncResResponse object compatible with the OpenAPI specification.
//
// This includes mapping a list of UTXO items and the latest "since" value used for pagination.
func NewRequestSyncResponseSuccessResponse(response *app.RequestSyncResponseDTO) *openapi.RequestSyncResResponse {
	if response == nil {
		return &openapi.RequestSyncResResponse{
			UTXOList: []openapi.UTXOItem{},
			Since:    0,
		}
	}

	utxos := make([]openapi.UTXOItem, 0, len(response.UTXOList))
	for _, utxo := range response.UTXOList {
		utxos = append(utxos, openapi.UTXOItem{
			Txid:        utxo.TxID,
			OutputIndex: int(utxo.OutputIndex),
			Score:       utxo.Score,
		})
	}

	return &openapi.RequestSyncResResponse{
		UTXOList: utxos,
		Since:    response.Since,
	}
}
