package app

import (
	"context"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
)

// RequestForeignGASPNodeDTO represents the data transfer object used to request a foreign GASP node.
// It includes all necessary identifiers and metadata for locating and categorizing the node.
type RequestForeignGASPNodeDTO struct {
	GraphID     string // GraphID is a string representation of the graph's outpoint.
	TxID        string // TxID is the hexadecimal transaction ID that produced the desired output.
	OutputIndex uint32 // OutputIndex specifies the index of the output within the transaction.
	Topic       string // Topic is a metadata string for categorizing or filtering the request.
}

// RequestForeignGASPNodeProvider defines the interface that must be implemented to fulfill a foreign GASP node request.
type RequestForeignGASPNodeProvider interface {
	// ProvideForeignGASPNode resolves the foreign GASP node using the given graphID, outpoint, and topic.
	// Returns a pointer to a GASP node or an error if retrieval fails.
	ProvideForeignGASPNode(ctx context.Context, graphID, outpoint *overlay.Outpoint, topic string) (*core.GASPNode, error)
}

// RequestForeignGASPNodeService coordinates and orchestrates the process of requesting a foreign GASP node.
// It uses the injected provider to perform the actual node retrieval based on validated input.
type RequestForeignGASPNodeService struct {
	provider RequestForeignGASPNodeProvider
}

// RequestForeignGASPNode validates and converts input DTO fields and delegates the request to the provider.
// It parses the transaction ID into a chain hash, constructs a new outpoint using the parsed chain hash
// and the output index, and creates a graph outpoint from the GraphID string.
// All validated data is then passed to the configured provider.
// Returns the GASP node on success, or a detailed error if processing fails.
func (s *RequestForeignGASPNodeService) RequestForeignGASPNode(ctx context.Context, dto RequestForeignGASPNodeDTO) (*core.GASPNode, error) {
	txID, err := chainhash.NewHashFromHex(dto.TxID)
	if err != nil {
		return nil, NewRawDataProcessingWithFieldError(err, "TransactionID")
	}

	graphID, err := overlay.NewOutpointFromString(dto.GraphID)
	if err != nil {
		return nil, NewRawDataProcessingWithFieldError(err, "GraphID")
	}

	node, err := s.provider.ProvideForeignGASPNode(ctx, graphID, &overlay.Outpoint{
		OutputIndex: dto.OutputIndex,
		Txid:        *txID,
	}, dto.Topic)
	if err != nil {
		return nil, NewForeignGASPNodeProviderError(err)
	}
	return node, nil
}

// NewRequestForeignGASPNodeService constructs and returns a new instance of RequestForeignGASPNodeService.
// Panics if the given provider is nil, as a valid provider is required for service operation.
func NewRequestForeignGASPNodeService(provider RequestForeignGASPNodeProvider) *RequestForeignGASPNodeService {
	if provider == nil {
		panic("request foreign GASP node service provider is nil")
	}

	return &RequestForeignGASPNodeService{provider: provider}
}

// NewForeignGASPNodeProviderError wraps a lower-level provider error in a user-facing error with guidance.
// Used when the provider fails to supply the requested foreign GASP node.
func NewForeignGASPNodeProviderError(err error) Error {
	return NewProviderFailureError(
		err.Error(),
		"Unable to process foreign gasp node request due to an internal error. Please try again later or contact the support team.",
	)
}
