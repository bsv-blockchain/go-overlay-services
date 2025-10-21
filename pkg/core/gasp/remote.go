package gasp

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// Remote defines the interface for remote GASP node communication operations.
type Remote interface {
	GetInitialResponse(ctx context.Context, request *InitialRequest) (*InitialResponse, error)
	GetInitialReply(ctx context.Context, response *InitialResponse) (*InitialReply, error)
	RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*Node, error)
	SubmitNode(ctx context.Context, node *Node) (*NodeResponse, error)
}
