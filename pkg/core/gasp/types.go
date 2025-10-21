package gasp

import (
	"fmt"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// InitialRequest represents the initial GASP synchronization request containing version and timestamp information.
type InitialRequest struct {
	Version int     `json:"version"`
	Since   float64 `json:"since"`
	Limit   uint32  `json:"limit,omitempty"`
}

// Output represents a UTXO output in the GASP protocol with its transaction ID, index, and score.
type Output struct {
	Txid        chainhash.Hash `json:"txid"`
	OutputIndex uint32         `json:"outputIndex"`
	Score       float64        `json:"score"`
}

// InitialResponse represents the response to an initial GASP request containing a list of UTXOs and timestamp.
type InitialResponse struct {
	UTXOList []*Output `json:"UTXOList"`
	Since    float64   `json:"since"`
}

// Outpoint converts the GASP Output to a transaction Outpoint.
func (g *Output) Outpoint() *transaction.Outpoint {
	return &transaction.Outpoint{
		Txid:  g.Txid,
		Index: g.OutputIndex,
	}
}

// OutpointString returns the string representation of the GASP Output's outpoint.
func (g *Output) OutpointString() string {
	return (&transaction.Outpoint{Txid: g.Txid, Index: g.OutputIndex}).String()
}

// InitialReply represents a reply to an initial GASP response containing additional UTXOs not in the original response.
type InitialReply struct {
	UTXOList []*Output `json:"UTXOList"`
}

// Input represents an input to a GASP node identified by its hash.
type Input struct {
	Hash string `json:"hash"`
}

// Node represents a node in the GASP graph containing transaction data, metadata, and ancillary information.
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

// NodeResponseData contains metadata flags for a node response.
type NodeResponseData struct {
	Metadata bool `json:"metadata"`
}

// NodeResponse represents the response when submitting a node, indicating which inputs are needed.
type NodeResponse struct {
	RequestedInputs map[string]*NodeResponseData `json:"requestedInputs"`
}

// VersionMismatchError represents an error that occurs when GASP versions do not match between nodes.
type VersionMismatchError struct {
	Message        string `json:"message"`
	Code           string `json:"code"`
	CurrentVersion int    `json:"currentVersion"`
	ForeignVersion int    `json:"foreignVersion"`
}

func (e *VersionMismatchError) Error() string {
	return e.Message
}

// Is implements error matching for errors.Is
func (e *VersionMismatchError) Is(target error) bool {
	_, ok := target.(*VersionMismatchError)
	return ok
}

// NewVersionMismatchError creates a new VersionMismatchError with the specified versions.
func NewVersionMismatchError(currentVersion, foreignVersion int) *VersionMismatchError {
	return &VersionMismatchError{
		Message:        fmt.Sprintf("GASP version mismatch. Current version: %d, foreign version: %d", currentVersion, foreignVersion),
		Code:           "ERR_GASP_VERSION_MISMATCH",
		CurrentVersion: currentVersion,
		ForeignVersion: foreignVersion,
	}
}
