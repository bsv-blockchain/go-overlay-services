package core

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

type GASPStorage interface {
	FindKnownUTXOs(ctx context.Context, since uint32) ([]*transaction.Outpoint, error)
	HydrateGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*GASPNode, error)
	FindNeededInputs(ctx context.Context, tx *GASPNode) (*GASPNodeResponse, error)
	AppendToGraph(ctx context.Context, tx *GASPNode, spentBy *transaction.Outpoint) error
	ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error
	DiscardGraph(ctx context.Context, graphID *transaction.Outpoint) error
	FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error
}
