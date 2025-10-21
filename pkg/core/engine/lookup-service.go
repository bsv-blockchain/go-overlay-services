package engine

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// OutputAdmittedByTopic contains information about an output that has been admitted by a topic manager.
type OutputAdmittedByTopic struct {
	Topic         string
	Outpoint      *transaction.Outpoint
	Satoshis      uint64
	LockingScript *script.Script
	AtomicBEEF    []byte
}

// OutputSpent contains information about an output that has been spent.
type OutputSpent struct {
	Outpoint           *transaction.Outpoint
	Topic              string
	SpendingTxid       *chainhash.Hash
	InputIndex         uint32
	UnlockingScript    *script.Script
	SequenceNumber     uint32
	SpendingAtomicBEEF []byte
}

// LookupService defines the interface for managing and querying outputs in a lookup service.
type LookupService interface {
	/**
	 * Invoked when a Topic Manager admits a new UTXO.
	 * The payload shape depends on this.admissionMode.
	 */
	OutputAdmittedByTopic(ctx context.Context, payload *OutputAdmittedByTopic) error

	/**
	 * Invoked when a previously-admitted UTXO is spent.
	 * The payload shape depends on this.spendNotificationMode.
	 */
	OutputSpent(ctx context.Context, payload *OutputSpent) error

	/**
	 * Called when a Topic Manager decides that **historical retention** of the
	 * specified UTXO is no longer required.
	 */
	OutputNoLongerRetainedInHistory(ctx context.Context, outpoint *transaction.Outpoint, topic string) error

	/**
	 * LEGAL EVICTION:
	 * Permanently remove the referenced UTXO from all indices maintained by the
	 * Lookup Service.  After eviction the service MUST NOT reference the output
	 * in any future lookup answer.
	 */
	OutputEvicted(ctx context.Context, outpoint *transaction.Outpoint) error
	OutputBlockHeightUpdated(ctx context.Context, txid *chainhash.Hash, blockHeight uint32, blockIndex uint64) error
	Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error)
	GetDocumentation() string
	GetMetaData() *overlay.MetaData
}
