package engine

import (
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// Output represents a transaction output with its metadata, history, and BEEF data.
type Output struct {
	Outpoint        transaction.Outpoint
	Topic           string
	Script          *script.Script
	Satoshis        uint64
	Spent           bool
	OutputsConsumed []*transaction.Outpoint
	ConsumedBy      []*transaction.Outpoint
	BlockHeight     uint32
	BlockIdx        uint64
	Score           float64 // sort score for outputs. Usage is up to Storage implementation.
	Beef            []byte
	AncillaryTxids  []*chainhash.Hash
	AncillaryBeef   []byte
}
