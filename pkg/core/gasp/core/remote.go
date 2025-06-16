package core

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

type GASPRemote interface {
	GetInitialResponse(ctx context.Context, request *GASPInitialRequest) (*GASPInitialResponse, error)
	GetInitialReply(ctx context.Context, response *GASPInitialResponse) (*GASPInitialReply, error)
	RequestNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*GASPNode, error)
	SubmitNode(ctx context.Context, node *GASPNode) (*GASPNodeResponse, error)
}
