package engine

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
)

// BASMReadService validates bounded historical read projections from a storage
// snapshot against independently configured canonical headers. It does not
// mutate admissions, compute recovery progress, or establish peer agreement.
type BASMReadService struct {
	storage BASMReadOpener
	headers BASMHeaderResolver
	limits  basm.ReadLimits
}

// NewBASMReadService configures optional BASM serving without changing Engine or
// Storage defaults. A nil header resolver permits raw reads only; confirmed
// anchor/list/proof reads explicitly return ErrBASMNotReady.
func NewBASMReadService(storage BASMReadOpener, headers BASMHeaderResolver, limits basm.ReadLimits) (*BASMReadService, error) {
	if nilBASMCapability(storage) {
		return nil, ErrBASMUnsupported
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if nilBASMCapability(headers) {
		headers = nil
	}
	return &BASMReadService{storage: storage, headers: headers, limits: limits}, nil
}

// ProvideTopicAnchorTip returns the stored contiguous tip or the TS empty-tip
// sentinel for a supported initialized topic with no anchors.
func (s *BASMReadService) ProvideTopicAnchorTip(ctx context.Context, topic string) (basm.TopicAnchorTip, error) {
	return readBASM(ctx, s, topic, true, func(ctx context.Context, v BASMReadView) (basm.TopicAnchorTip, []BASMCanonicalHeader, error) {
		if err := s.checkResponseSize(400+uint64(len(topic))*6, 0, 0); err != nil {
			return basm.TopicAnchorTip{}, nil, err
		}
		anchor, err := v.Tip(ctx)
		if err != nil {
			return basm.TopicAnchorTip{}, nil, err
		}
		if anchor == nil {
			return basm.TopicAnchorTip{Topic: topic, BlockHeight: -1}, nil, nil
		}
		header, err := s.validateAnchor(ctx, topic, *anchor)
		if err != nil {
			return basm.TopicAnchorTip{}, nil, err
		}
		return basm.TopicAnchorTip{Topic: topic, BlockHeight: int64(anchor.BlockHeight), BlockHash: &anchor.BlockHash, BASMRoot: &anchor.BASMRoot, AdmittedCount: &anchor.AdmittedCount, TAC: anchor.TAC}, []BASMCanonicalHeader{header}, nil
	})
}

// ProvideTopicAnchorRange returns every requested height in order. Missing,
// reordered, or truncated storage pages are not reported as successful ranges.
func (s *BASMReadService) ProvideTopicAnchorRange(ctx context.Context, topic string, from, to uint32) (basm.TopicAnchorRange, error) {
	if s == nil {
		return basm.TopicAnchorRange{}, ErrBASMUnsupported
	}
	count, err := basm.ValidateRange(from, to, s.limits.MaxRange)
	if err != nil {
		return basm.TopicAnchorRange{}, err
	}
	return readBASM(ctx, s, topic, true, func(ctx context.Context, v BASMReadView) (basm.TopicAnchorRange, []BASMCanonicalHeader, error) {
		return s.collectAnchorRange(ctx, v, topic, from, to, count)
	})
}

func (s *BASMReadService) collectAnchorRange(ctx context.Context, v BASMReadView, topic string, from, to, count uint32) (basm.TopicAnchorRange, []BASMCanonicalHeader, error) {
	if err := s.checkResponseSize(64+uint64(len(topic))*6, 400+uint64(len(topic))*6, uint64(count)); err != nil {
		return basm.TopicAnchorRange{}, nil, err
	}
	anchors, err := v.Anchors(ctx, from, to, count)
	if err != nil {
		return basm.TopicAnchorRange{}, nil, err
	}
	if uint64(len(anchors)) != uint64(count) {
		return basm.TopicAnchorRange{}, nil, ErrBASMNotReady
	}
	headers, err := s.validateAnchorRange(ctx, topic, from, anchors)
	if err != nil {
		return basm.TopicAnchorRange{}, nil, err
	}
	return basm.TopicAnchorRange{Topic: topic, Anchors: slices.Clone(anchors)}, headers, nil
}

func (s *BASMReadService) validateAnchorRange(ctx context.Context, topic string, from uint32, anchors []basm.Anchor) ([]BASMCanonicalHeader, error) {
	headers := make([]BASMCanonicalHeader, len(anchors))
	for i, anchor := range anchors {
		if uint64(anchor.BlockHeight) != uint64(from)+uint64(i) {
			return nil, ErrBASMInvalidData
		}
		header, err := s.validateAnchor(ctx, topic, anchor)
		if err != nil {
			return nil, err
		}
		headers[i] = header
		if i > 0 && anchor.TAC != basm.HashTACStep(anchors[i-1].TAC, anchor.BlockHash, anchor.BASMRoot) {
			return nil, ErrBASMInvalidData
		}
	}
	return headers, nil
}

// ProvideAdmittedList returns a complete bounded admitted subset. A supplied
// blockHash must still be canonical; historical/orphaned revisions are not
// silently substituted. Position SPV is checked by compound-proof serving.
func (s *BASMReadService) ProvideAdmittedList(ctx context.Context, topic string, height uint32, hash *basm.Hash) (basm.AdmittedList, error) {
	return readBASM(ctx, s, topic, true, func(ctx context.Context, v BASMReadView) (basm.AdmittedList, []BASMCanonicalHeader, error) {
		anchor, admitted, header, err := s.readBlock(ctx, v, topic, height)
		if err != nil {
			return basm.AdmittedList{}, nil, err
		}
		if hash != nil && *hash != anchor.BlockHash {
			return basm.AdmittedList{}, nil, ErrBASMNotReady
		}
		if err = s.checkResponseSize(192+uint64(len(topic))*6, 128, uint64(len(admitted))); err != nil {
			return basm.AdmittedList{}, nil, err
		}
		blockHash := anchor.BlockHash
		return basm.AdmittedList{Topic: topic, BlockHeight: height, BlockHash: &blockHash, Admitted: admitted}, []BASMCanonicalHeader{header}, nil
	})
}

// ProvideRawTransactions returns identity-bound standard raw hex, preserving
// request order and explicitly listing missing entries. It does not verify
// scripts, block membership, unspentness, or local topic policy.
func (s *BASMReadService) ProvideRawTransactions(ctx context.Context, txids []basm.Hash) (basm.RawTransactions, error) {
	if err := s.validateTxids(txids, false); err != nil {
		return basm.RawTransactions{}, err
	}
	return readBASM(ctx, s, "", false, func(ctx context.Context, v BASMReadView) (basm.RawTransactions, []BASMCanonicalHeader, error) {
		return s.collectRawTransactions(ctx, v, txids)
	})
}

func (s *BASMReadService) collectRawTransactions(ctx context.Context, v BASMReadView, txids []basm.Hash) (basm.RawTransactions, []BASMCanonicalHeader, error) {
	response := basm.RawTransactions{Transactions: make([]basm.RawTransactionRecord, 0, len(txids)), Missing: make([]basm.Hash, 0)}
	var encodedBytes uint64
	for _, txid := range txids {
		next, err := s.appendRawTransaction(ctx, v, txid, &response, encodedBytes)
		if err != nil {
			return basm.RawTransactions{}, nil, err
		}
		encodedBytes = next
	}
	if encodedBytes+128 > uint64(s.limits.MaxResponseBytes) {
		return basm.RawTransactions{}, nil, basm.ErrLimitExceeded
	}
	return response, nil, nil
}

func (s *BASMReadService) appendRawTransaction(ctx context.Context, v BASMReadView, txid basm.Hash, response *basm.RawTransactions, encodedBytes uint64) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return encodedBytes, err
	}
	raw, err := v.RawTx(ctx, txid, s.limits.MaxRawTxBytes)
	if errors.Is(err, ErrBASMNotFound) {
		response.Missing = append(response.Missing, txid)
		return encodedBytes + 67, nil
	}
	if err != nil {
		return encodedBytes, err
	}
	if uint64(len(raw)) > uint64(s.limits.MaxRawTxBytes) {
		return encodedBytes, basm.ErrLimitExceeded
	}
	// Account for hex and conservative metadata before allocating the hex copy.
	encodedBytes += uint64(len(raw))*2 + 128
	if encodedBytes+128 > uint64(s.limits.MaxResponseBytes) {
		return encodedBytes, basm.ErrLimitExceeded
	}
	raw = bytes.Clone(raw)
	derived, err := basm.RawTransactionID(raw, s.limits.MaxRawTxBytes)
	if err != nil || derived != txid {
		return encodedBytes, ErrBASMInvalidData
	}
	response.Transactions = append(response.Transactions, basm.RawTransactionRecord{TxID: txid, RawTx: hex.EncodeToString(raw)})
	return encodedBytes, nil
}

// Bound conservative JSON sizes without overflowing multiplication. Topic
// lengths have already passed validation, and six bytes cover JSON escaping.
func (s *BASMReadService) checkResponseSize(base, perItem, count uint64) error {
	limit := uint64(s.limits.MaxResponseBytes)
	if base > limit || (perItem != 0 && count > (limit-base)/perItem) {
		return basm.ErrLimitExceeded
	}
	return nil
}

func (s *BASMReadService) validateTxids(txids []basm.Hash, required bool) error {
	if s == nil {
		return ErrBASMUnsupported
	}
	if uint64(len(txids)) > uint64(s.limits.MaxRequestedTxIDs) {
		return basm.ErrLimitExceeded
	}
	if required && len(txids) == 0 {
		return basm.ErrInvalidInput
	}
	seen := make(map[basm.Hash]bool, len(txids))
	for _, txid := range txids {
		if seen[txid] {
			return basm.ErrInvalidInput
		}
		seen[txid] = true
	}
	return nil
}

func (s *BASMReadService) validateAnchor(ctx context.Context, topic string, anchor basm.Anchor) (BASMCanonicalHeader, error) {
	if err := ctx.Err(); err != nil {
		return BASMCanonicalHeader{}, err
	}
	if anchor.Topic != topic {
		return BASMCanonicalHeader{}, ErrBASMInvalidData
	}
	if err := anchor.Validate(s.limits.Limits); err != nil {
		return BASMCanonicalHeader{}, fmt.Errorf("%w: %w", ErrBASMInvalidData, err)
	}
	header, err := s.headers.CanonicalBASMHeader(ctx, anchor.BlockHeight)
	if err != nil {
		return BASMCanonicalHeader{}, err
	}
	if header.Height != anchor.BlockHeight || header.BlockHash != anchor.BlockHash || header.TransactionCount == 0 || header.TransactionCount > basm.MaxSafeJSONInteger {
		return BASMCanonicalHeader{}, ErrBASMNotReady
	}
	if anchor.AdmittedCount > header.TransactionCount {
		return BASMCanonicalHeader{}, ErrBASMInvalidData
	}
	return header, nil
}

func (s *BASMReadService) readBlock(ctx context.Context, v BASMReadView, topic string, height uint32) (basm.Anchor, []basm.AdmittedTxRef, BASMCanonicalHeader, error) {
	anchors, err := v.Anchors(ctx, height, height, 1)
	if err != nil {
		return basm.Anchor{}, nil, BASMCanonicalHeader{}, err
	}
	if len(anchors) != 1 || anchors[0].BlockHeight != height {
		return basm.Anchor{}, nil, BASMCanonicalHeader{}, ErrBASMNotReady
	}
	anchor := anchors[0]
	header, err := s.validateAnchor(ctx, topic, anchor)
	if err != nil {
		return basm.Anchor{}, nil, BASMCanonicalHeader{}, err
	}
	admitted, err := v.Admitted(ctx, height, s.limits.MaxAdmitted)
	if err != nil {
		return basm.Anchor{}, nil, BASMCanonicalHeader{}, err
	}
	if err = basm.ValidateAnchorList(ctx, anchor.TopicBlockAnchor, admitted, header.TransactionCount, s.limits.Limits); err != nil {
		return basm.Anchor{}, nil, BASMCanonicalHeader{}, fmt.Errorf("%w: %w", ErrBASMInvalidData, err)
	}
	return anchor, append([]basm.AdmittedTxRef{}, admitted...), header, nil
}

// readBASM owns the context and storage view. It rechecks both storage history
// currency and canonical header identity before returning any response. A view
// must cooperate with cancellation; no orphan goroutine is used for timeouts.
func readBASM[T any](ctx context.Context, s *BASMReadService, topic string, needsHeaders bool, read func(context.Context, BASMReadView) (T, []BASMCanonicalHeader, error)) (output T, err error) {
	var zero T
	if s == nil || nilBASMCapability(s.storage) {
		return zero, ErrBASMUnsupported
	}
	if ctx == nil {
		return zero, basm.ErrInvalidInput
	}
	if err = requireBASMHeaders(s, topic, needsHeaders); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.limits.RequestTimeout)
	defer cancel()
	view, err := openBASMReadView(ctx, s, topic)
	if err != nil {
		return zero, err
	}
	defer closeBASMView(view, &output, &err, zero, ctx)
	result, headers, readErr := read(ctx, view)
	if err = completeBASMRead(ctx, s.headers, view, readErr, headers); err != nil {
		return zero, err
	}
	return result, nil
}

func requireBASMHeaders(s *BASMReadService, topic string, needsHeaders bool) error {
	if !needsHeaders {
		return nil
	}
	if err := (basm.TopicBlockAnchor{Topic: topic}).Validate(s.limits.Limits); err != nil {
		return err
	}
	if s.headers == nil {
		return ErrBASMNotReady
	}
	return nil
}

func openBASMReadView(ctx context.Context, s *BASMReadService, topic string) (BASMReadView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	view, err := s.storage.OpenBASMRead(ctx, topic, s.limits)
	if err != nil {
		return nil, err
	}
	if nilBASMCapability(view) {
		return nil, ErrBASMNotReady
	}
	return view, nil
}

func closeBASMView[T any](view BASMReadView, output *T, err *error, zero T, ctx context.Context) {
	closeErr := view.Close()
	if *err == nil {
		*err = closeErr
		if *err == nil {
			*err = ctx.Err()
		}
	}
	if *err != nil {
		*output = zero
	}
}

func completeBASMRead(ctx context.Context, resolver BASMHeaderResolver, view BASMReadView, readErr error, headers []BASMCanonicalHeader) error {
	if readErr != nil {
		return readErr
	}
	if err := recheckCanonicalHeaders(ctx, resolver, headers); err != nil {
		return err
	}
	return view.CheckCurrent(ctx)
}

func recheckCanonicalHeaders(ctx context.Context, resolver BASMHeaderResolver, headers []BASMCanonicalHeader) error {
	for _, header := range headers {
		current, err := resolver.CanonicalBASMHeader(ctx, header.Height)
		if err != nil {
			return err
		}
		if current != header {
			return ErrBASMNotReady
		}
	}
	return nil
}
