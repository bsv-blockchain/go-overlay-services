package engine

import (
	"context"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

var ErrNotFound = fmt.Errorf("not-found")

type Storage interface {
	// Adds a new output to storage
	InsertOutput(ctx context.Context, utxo *Output) error

	// Finds an output from storage
	FindOutput(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*Output, error)

	FindOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*Output, error)

	// Finds outputs with a matching transaction ID from storage
	FindOutputsForTransaction(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*Output, error)

	// Finds current UTXOs that have been admitted into a given topic
	FindUTXOsForTopic(ctx context.Context, topic string, since uint32, includeBEEF bool) ([]*Output, error)

	// Deletes an output from storage
	DeleteOutput(ctx context.Context, outpoint *transaction.Outpoint, topic string) error

	// Updates UTXOs as spent
	MarkUTXOsAsSpent(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error

	// Updates which outputs are consumed by this output
	UpdateConsumedBy(ctx context.Context, outpoint *transaction.Outpoint, topic string, consumedBy []*transaction.Outpoint) error

	// Updates the beef data for a transaction
	UpdateTransactionBEEF(ctx context.Context, txid *chainhash.Hash, beef []byte) error

	// Updates the block height on an output
	UpdateOutputBlockHeight(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIndex uint64, ancillaryBeef []byte) error

	// Inserts record of the applied transaction
	InsertAppliedTransaction(ctx context.Context, tx *overlay.AppliedTransaction) error

	// Checks if a duplicate transaction exists
	DoesAppliedTransactionExist(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error)
}
