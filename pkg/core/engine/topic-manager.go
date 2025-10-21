package engine

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// TopicManager defines the interface for managing topic-specific admission rules and documentation.
type TopicManager interface {
	IdentifyAdmissibleOutputs(ctx context.Context, beef []byte, previousCoins map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error)
	IdentifyNeededInputs(ctx context.Context, beef []byte) ([]*transaction.Outpoint, error)
	GetDocumentation() string
	GetMetaData() *overlay.MetaData
}
