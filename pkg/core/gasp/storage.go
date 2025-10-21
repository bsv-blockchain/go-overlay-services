package gasp

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// Storage defines the interface for GASP storage operations including UTXO management and graph operations.
type Storage interface {
	FindKnownUTXOs(ctx context.Context, since float64, limit uint32) ([]*Output, error)
	HydrateGASPNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*Node, error)
	FindNeededInputs(ctx context.Context, tx *Node) (*NodeResponse, error)
	AppendToGraph(ctx context.Context, tx *Node, spentBy *transaction.Outpoint) error
	ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error
	DiscardGraph(ctx context.Context, graphID *transaction.Outpoint) error
	FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error
}
