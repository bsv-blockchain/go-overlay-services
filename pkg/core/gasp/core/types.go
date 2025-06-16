package core

import (
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

type GASPInitialRequest struct {
	Version int    `json:"version"`
	Since   uint32 `json:"since"`
}

type GASPInitialResponse struct {
	UTXOList []*transaction.Outpoint `json:"utxo_list"`
	Since    uint32                  `json:"since"`
}

type GASPInitialReply struct {
	UTXOList []*transaction.Outpoint `json:"utxo_list"`
}

type GASPInput struct {
	Hash string `json:"hash"`
}

type GASPNode struct {
	GraphID        *transaction.Outpoint `json:"graphID"`
	RawTx          string                `json:"rawTx"`
	OutputIndex    uint32                `json:"outputIndex"`
	Proof          *string               `json:"proof"`
	TxMetadata     string                `json:"txMetadata"`
	OutputMetadata string                `json:"outputMetadata"`
	Inputs         map[string]*GASPInput `json:"inputs"`
	AncillaryBeef  []byte                `json:"ancillaryBeef"`
}

type GASPNodeResponseData struct {
	Metadata bool `json:"metadata"`
}

type GASPNodeResponse struct {
	RequestedInputs map[string]*GASPNodeResponseData `json:"requestedInputs"`
}

type GASPVersionMismatchError struct {
	Message        string `json:"message"`
	Code           string `json:"code"`
	CurrentVersion int    `json:"currentVersion"`
	ForeignVersion int    `json:"foreignVersion"`
}

func (e *GASPVersionMismatchError) Error() string {
	return e.Message
}

func NewGASPVersionMismatchError(currentVersion int, foreignVersion int) *GASPVersionMismatchError {
	return &GASPVersionMismatchError{
		Message:        fmt.Sprintf("GASP version mismatch. Current version: %d, foreign version: %d", currentVersion, foreignVersion),
		Code:           "ERR_GASP_VERSION_MISMATCH",
		CurrentVersion: currentVersion,
		ForeignVersion: foreignVersion,
	}
}
