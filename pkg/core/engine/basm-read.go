package engine

import (
	"context"
	"errors"
	"reflect"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
)

// BASM read failures distinguish missing capability, incomplete local state,
// unknown records, and inconsistent provider data. None expresses agreement.
var (
	ErrBASMUnsupported = errors.New("BASM read capability unsupported")
	ErrBASMNotReady    = errors.New("BASM read state not ready")
	ErrBASMNotFound    = errors.New("BASM record not found")
	ErrBASMInvalidData = errors.New("BASM provider data inconsistent")
)

// BASMProvider is an optional read capability, separate from OverlayEngineProvider
// and Storage. Legacy providers need not implement it. Implementations must
// provide bounded, consistent responses and honor the request context.
type BASMProvider interface {
	ProvideTopicAnchorTip(ctx context.Context, topic string) (basm.TopicAnchorTip, error)
	ProvideTopicAnchorRange(ctx context.Context, topic string, from, to uint32) (basm.TopicAnchorRange, error)
	ProvideAdmittedList(ctx context.Context, topic string, height uint32, blockHash *basm.Hash) (basm.AdmittedList, error)
	ProvideCompoundMerklePath(ctx context.Context, topic string, height uint32, txids []basm.Hash) (basm.CompoundMerklePath, error)
	ProvideRawTransactions(ctx context.Context, txids []basm.Hash) (basm.RawTransactions, error)
}

// IsBASMProviderAvailable reports whether an optional provider has a non-nil
// implementation. It treats typed-nil implementations as absent capabilities.
func IsBASMProviderAvailable(provider BASMProvider) bool {
	return !nilBASMCapability(provider)
}

func nilBASMCapability(capability any) bool {
	if capability == nil {
		return true
	}
	value := reflect.ValueOf(capability)
	switch value.Kind() { //nolint:exhaustive // Only nilable kinds support IsNil; all other implementations are present.
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// BASMReadStorage opens an immutable per-request read view. topic is empty only
// for the current protocol's global raw-transaction lookup. Unsupported topics
// return ErrBASMNotFound; unavailable/incomplete snapshots return ErrBASMNotReady.
// Opening a view must not mutate admissions, evict history, or advance recovery.
type BASMReadStorage interface {
	OpenBASMRead(ctx context.Context, topic string, limits basm.ReadLimits) (BASMReadView, error)
}

// BASMReadView provides historical admissions, including spent/banned entries.
// A storage adapter must pin a coherent immutable history revision and verify
// BOTH chainEpoch and topicHistoryGeneration in CheckCurrent. These refer to
// S01 HistoryFence semantics; this interface defines no competing fence fields.
// Reads must enforce passed limits before allocation, never silently truncate;
// unavailable requested intervals return ErrBASMNotReady. Close releases all
// resources even after cancellation. RawTx returns ErrBASMNotFound for absence.
type BASMReadView interface {
	Tip(ctx context.Context) (*basm.Anchor, error)
	Anchors(ctx context.Context, from, to, maxCount uint32) ([]basm.Anchor, error)
	Admitted(ctx context.Context, height, maxCount uint32) ([]basm.AdmittedTxRef, error)
	MerklePath(ctx context.Context, txid basm.Hash, maxBytes uint32) ([]byte, error)
	RawTx(ctx context.Context, txid basm.Hash, maxBytes uint32) ([]byte, error)
	CheckCurrent(ctx context.Context) error
	Close() error
}

// BASMCanonicalHeader is independently sourced chain information, not a header
// supplied by a peer or the admission store. TransactionCount bounds positions.
type BASMCanonicalHeader struct {
	Height           uint32
	BlockHash        basm.Hash
	MerkleRoot       basm.Hash
	TransactionCount uint64
}

// BASMHeaderResolver resolves the current canonical header at a height. A
// caller must configure a trusted chain source; no permissive fallback exists.
type BASMHeaderResolver interface {
	CanonicalBASMHeader(ctx context.Context, height uint32) (BASMCanonicalHeader, error)
}
