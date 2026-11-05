package engine

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// TopicManager defines the interface for managing topic-specific admission rules and documentation.
type TopicManager interface {
	// IdentifyAdmissibleOutputs reports which outputs of the transaction selected by txid
	// are admissible for this topic.
	//
	// offChainValues are the opaque trailing bytes from the original POST /submit when
	// x-includes-off-chain-values was exactly true. nil means that submit had no off-chain
	// values. Historical and GASP calls that do not have the original submit bytes pass nil.
	// Implementations must not assume the bytes are JSON or UTF-8.
	IdentifyAdmissibleOutputs(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32, offChainValues []byte) (overlay.AdmittanceInstructions, error)
	IdentifyNeededInputs(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash) ([]*transaction.Outpoint, error)
	GetDocumentation() string
	GetMetaData() *overlay.MetaData
}
