package app

import (
	"context"
	"errors"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// ARCIngestProvider defines an interface for handling the ingestion of Merkle proofs
// for a given transaction. It is typically implemented by a domain service or adapter
// responsible for storing or processing Merkle proofs.
type ARCIngestProvider interface {
	HandleNewMerkleProof(ctx context.Context, txid *chainhash.Hash, proof *transaction.MerklePath) error
}

// ARCIngestService coordinates the ingestion of Merkle proofs in the application layer.
// It acts as an orchestrator that validates inputs, constructs domain data, and delegates
// execution to a configured ARCIngestProvider implementation.
type ARCIngestService struct {
	provider ARCIngestProvider
}

// ProcessIngest receives transaction and Merkle path data in string form,
// performs input validation and parsing, sets the block height, and delegates
// the actual proof handling to the ARCIngestProvider.
func (a *ARCIngestService) ProcessIngest(ctx context.Context, txID, merklePath string, blockHeight uint32) error {
	hash, err := chainhash.NewHashFromHex(txID)
	if err != nil {
		return NewInvalidTxIDFormatError(err)
	}

	path, err := transaction.NewMerklePathFromHex(merklePath)
	if err != nil {
		return NewInvalidMerklePathFormatError(err)
	}

	if blockHeight == 0 {
		return NewInvalidBlockHeightError(errors.New("block height must be a positive integer (greater than 0)"))
	}

	path.BlockHeight = blockHeight

	err = a.provider.HandleNewMerkleProof(ctx, hash, path)
	if err != nil {
		return NewArcIngestProviderError(err)
	}

	return nil
}

// NewARCIngestService constructs a new ARCIngestService with the given provider.
// It panics if the provider is nil, enforcing correct application configuration.
func NewARCIngestService(provider ARCIngestProvider) *ARCIngestService {
	if provider == nil {
		panic("ARC ingest service provider is nil")
	}

	return &ARCIngestService{provider: provider}
}

// NewInvalidMerklePathFormatError returns an error indicating that the provided Merkle path
// is in an invalid format. This typically happens when the input string is malformed or does not
// follow the expected hex-encoded Merkle path structure.
func NewInvalidMerklePathFormatError(err error) Error {
	return NewIncorrectInputError(
		err.Error(),
		"Unable to process Merkle path argument due to an invalid data format. Please verify the content, try again later or contact the support team.",
	)
}

// NewInvalidTxIDFormatError returns an error indicating that the provided transaction ID
// is not a valid hexadecimal-encoded string. This usually means the client submitted malformed input.
func NewInvalidTxIDFormatError(err error) Error {
	return NewIncorrectInputError(
		err.Error(),
		"Unable to process transaction ID due to an invalid data format. Please verify the content, try again later or contact the support team.",
	)
}

// NewArcIngestProviderError returns an error indicating that the underlying ARCIngestProvider
// failed to process the Merkle proof. This is typically a system-level failure.
func NewArcIngestProviderError(err error) Error {
	return NewProviderFailureError(
		err.Error(),
		"Unable to process Merkle proof due to an internal error. Please try again later or contact the support team.",
	)
}

// NewInvalidBlockHeightError returns an error indicating that the provided block height
// is invalid. This typically happens when the block height is zero or otherwise invalid.
func NewInvalidBlockHeightError(err error) Error {
	return NewIncorrectInputError(
		err.Error(),
		"Unable to process block height due to an invalid value. Please verify the block height and try again.",
	)
}
