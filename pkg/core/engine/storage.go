package engine

import (
	"context"
	"errors"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// ErrNotFound is returned when a requested item is not found in storage.
var ErrNotFound = errors.New("not-found")

// Storage defines the interface for persisting and retrieving overlay transaction data.
type Storage interface {
	// Add a transaction's outputs to storage.
	InsertOutputs(ctx context.Context, topic string, txid *chainhash.Hash, outputs []uint32, outpointsConsumed []*transaction.Outpoint, beef *transaction.Beef, ancillaryTxids []*chainhash.Hash) error

	// Finds an output from storage
	FindOutput(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*Output, error)

	FindOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*Output, error)

	// Finds outputs with a matching transaction ID from storage
	FindOutputsForTransaction(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*Output, error)

	// Finds current UTXOs that have been admitted into a given topic
	FindUTXOsForTopic(ctx context.Context, topic string, since float64, limit uint32, includeBEEF bool) ([]*Output, error)

	// Deletes an output from storage
	DeleteOutput(ctx context.Context, outpoint *transaction.Outpoint, topic string) error

	// Updates UTXOs as spent
	MarkUTXOsAsSpent(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error

	// Updates which outputs are consumed by this output
	UpdateConsumedBy(ctx context.Context, outpoint *transaction.Outpoint, topic string, consumedBy []*transaction.Outpoint) error

	// Updates the beef data for a transaction
	UpdateTransactionBEEF(ctx context.Context, txid *chainhash.Hash, beef *transaction.Beef) error

	// Updates the block height on an output
	UpdateOutputBlockHeight(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIndex uint64) error

	// Inserts record of the applied transaction
	InsertAppliedTransaction(ctx context.Context, tx *overlay.AppliedTransaction) error

	// Checks if a duplicate transaction exists
	DoesAppliedTransactionExist(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error)

	// Updates the last interaction score for a given host and topic
	UpdateLastInteraction(ctx context.Context, host, topic string, since float64) error

	// Retrieves the last interaction score for a given host and topic
	// Returns 0 if no record exists
	GetLastInteraction(ctx context.Context, host, topic string) (float64, error)

	// Finds outpoints with a specific merkle validation state
	// Returns only the outpoints (not full output data) for efficiency
	FindOutpointsByMerkleState(ctx context.Context, topic string, state MerkleState, limit uint32) ([]*transaction.Outpoint, error)

	// Reconciles validation state for all outputs at a given block height
	// Compares outputs' merkle roots with the authoritative root and updates states:
	// - Matching roots become Validated (or Immutable if old enough)
	// - Non-matching roots become Invalidated
	// - Null roots remain Unmined
	ReconcileMerkleRoot(ctx context.Context, topic string, blockHeight uint32, merkleRoot *chainhash.Hash) error

	// LoadAncillaryBeef merges an output's AncillaryTxids into its Beef field.
	// This is used when the full BEEF with all ancillary transactions is needed.
	LoadAncillaryBeef(ctx context.Context, output *Output) error
}
