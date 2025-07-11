package ports

import (
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// RequestForeignGASPNodeHandler is a Fiber-compatible HTTP handler that processes
// foreign GASP node requests. It belongs to the ports layer and acts as the interface
// adapter between HTTP input and application-layer logic provided by RequestForeignGASPNodeService.
type RequestForeignGASPNodeHandler struct {
	service *app.RequestForeignGASPNodeService
}

// Handle processes an HTTP POST request for requesting a foreign GASP node.
// It expects a JSON body conforming to the RequestForeignGASPNodeJSONBody OpenAPI definition,
// along with an X-BSV-Topic header passed via params.
//
// The request is parsed and validated before being forwarded to the application layer.
// The response is formatted as a GASPNode object in OpenAPI-compatible JSON format.
//
// On success, returns a 200 OK response with the GASP node data.
// On failure, returns a request parsing or service-level error.
func (h *RequestForeignGASPNodeHandler) Handle(c *fiber.Ctx, params openapi.RequestForeignGASPNodeParams) error {
	var body openapi.RequestForeignGASPNodeJSONBody

	err := c.BodyParser(&body)
	if err != nil {
		return NewRequestBodyParserError(err)
	}

	node, err := h.service.RequestForeignGASPNode(c.Context(), app.RequestForeignGASPNodeDTO{
		GraphID:     body.GraphID,
		TxID:        body.TxID,
		OutputIndex: body.OutputIndex,
		Topic:       params.XBSVTopic,
	})
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusOK).JSON(NewRequestForeignGASPNodeSuccessResponse(node))
}

// NewRequestForeignGASPNodeHandler constructs a new RequestForeignGASPNodeHandler
// using the given RequestForeignGASPNodeProvider to instantiate the underlying service.
//
// The provider must implement the RequestForeignGASPNodeProvider interface.
// This function bridges infrastructure (provider) with application-layer logic.
// Panics if the provider is nil.
func NewRequestForeignGASPNodeHandler(provider app.RequestForeignGASPNodeProvider) *RequestForeignGASPNodeHandler {
	return &RequestForeignGASPNodeHandler{service: app.NewRequestForeignGASPNodeService(provider)}
}

// NewRequestForeignGASPNodeSuccessResponse converts a gasp.Node into a
// GASPNode object compatible with the OpenAPI specification.
//
// It ensures proper mapping of fields including inputs, optional graph ID and proof,
// and transaction/output metadata.
func NewRequestForeignGASPNodeSuccessResponse(node *gasp.Node) openapi.GASPNode {
	var inputs map[string]any
	if len(node.Inputs) > 0 {
		inputs = make(map[string]any, len(node.Inputs))
		for k, v := range node.Inputs {
			inputs[k] = v
		}
	}

	var graphID string
	if node.GraphID != nil {
		graphID = node.GraphID.String()
	}

	var proof string
	if node.Proof != nil {
		proof = *node.Proof
	}

	return openapi.GASPNode{
		GraphID:        graphID,
		RawTx:          node.RawTx,
		OutputIndex:    node.OutputIndex,
		Proof:          proof,
		TxMetadata:     node.TxMetadata,
		OutputMetadata: node.OutputMetadata,
		Inputs:         inputs,
		AncillaryBeef:  node.AncillaryBeef,
	}
}
