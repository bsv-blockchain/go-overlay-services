package engine

import (
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

type Output struct {
	Outpoint        transaction.Outpoint    `json:"-"`
	Topic           string                  `json:"topic"`
	Script          *script.Script          `json:"-"`
	Satoshis        uint64                  `json:"satoshis"`
	Spent           bool                    `json:"spent"`
	OutputsConsumed []*transaction.Outpoint `json:""`
	ConsumedBy      []*transaction.Outpoint
	BlockHeight     uint32
	BlockIdx        uint64
	Beef            []byte
	AncillaryTxids  []*chainhash.Hash
	AncillaryBeef   []byte
}
