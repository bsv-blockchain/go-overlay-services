package gasp

import (
	"fmt"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

type InitialRequest struct {
	Version int     `json:"version"`
	Since   float64 `json:"since"`
	Limit   uint32  `json:"limit,omitempty"`
}

type Output struct {
	Txid        chainhash.Hash `json:"txid"`
	OutputIndex uint32         `json:"outputIndex"`
	Score       float64        `json:"score"`
}

type InitialResponse struct {
	UTXOList []*Output `json:"UTXOList"`
	Since    float64   `json:"since"`
}

func (g *Output) Outpoint() *transaction.Outpoint {
	return &transaction.Outpoint{
		Txid:  g.Txid,
		Index: g.OutputIndex,
	}
}

func (g *Output) OutpointString() string {
	return (&transaction.Outpoint{Txid: g.Txid, Index: g.OutputIndex}).String()
}

type InitialReply struct {
	UTXOList []*Output `json:"UTXOList"`
}

type Input struct {
	Hash string `json:"hash"`
}

type Node struct {
	GraphID        *transaction.Outpoint `json:"graphID"`
	RawTx          string                `json:"rawTx"`
	OutputIndex    uint32                `json:"outputIndex"`
	Proof          *string               `json:"proof"`
	TxMetadata     string                `json:"txMetadata"`
	OutputMetadata string                `json:"outputMetadata"`
	Inputs         map[string]*Input     `json:"inputs"`
	AncillaryBeef  []byte                `json:"ancillaryBeef"`
}

type NodeResponseData struct {
	Metadata bool `json:"metadata"`
}

type NodeResponse struct {
	RequestedInputs map[string]*NodeResponseData `json:"requestedInputs"`
}

type VersionMismatchError struct {
	Message        string `json:"message"`
	Code           string `json:"code"`
	CurrentVersion int    `json:"currentVersion"`
	ForeignVersion int    `json:"foreignVersion"`
}

func (e *VersionMismatchError) Error() string {
	return e.Message
}

func NewVersionMismatchError(currentVersion, foreignVersion int) *VersionMismatchError {
	return &VersionMismatchError{
		Message:        fmt.Sprintf("GASP version mismatch. Current version: %d, foreign version: %d", currentVersion, foreignVersion),
		Code:           "ERR_GASP_VERSION_MISMATCH",
		CurrentVersion: currentVersion,
		ForeignVersion: foreignVersion,
	}
}
