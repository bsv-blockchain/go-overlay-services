package engine

import (
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// MerkleState represents the validation state of an output's merkle proof
type MerkleState uint8

// MerkleState values for output validation states.
const (
	MerkleStateUnmined MerkleState = iota
	MerkleStateValidated
	MerkleStateInvalidated
	MerkleStateImmutable
)

// String returns the string representation of the MerkleState
func (m MerkleState) String() string {
	switch m {
	case MerkleStateUnmined:
		return "Unmined"
	case MerkleStateValidated:
		return "Validated"
	case MerkleStateInvalidated:
		return "Invalidated"
	case MerkleStateImmutable:
		return "Immutable"
	default:
		return "Unknown"
	}
}

// Output represents a transaction output with its metadata, history, and BEEF data.
type Output struct {
	Outpoint        transaction.Outpoint
	Topic           string
	Spent           bool
	OutputsConsumed []*transaction.Outpoint
	ConsumedBy      []*transaction.Outpoint
	BlockHeight     uint32
	BlockIdx        uint64
	Score           float64 // sort score for outputs. Usage is up to Storage implementation.
	Beef            *transaction.Beef
	AncillaryTxids  []*chainhash.Hash
	MerkleRoot      *chainhash.Hash // Merkle root extracted from the merkle path
	MerkleState     MerkleState     // Validation state of the merkle proof
}
