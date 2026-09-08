package basm

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// MaxSafeJSONInteger is the largest exact integer in the existing TS JSON
// protocol. Heights additionally fit BRC-136's uint32 binary representation.
const MaxSafeJSONInteger uint64 = 1<<53 - 1

// Limits bounds local parsing and calculation work. All fields must be positive.
// These are operational limits, not consensus rules or protocol maxima.
type Limits struct {
	MaxAdmitted   uint32
	MaxRange      uint32
	MaxJSONBytes  uint32
	MaxTopicBytes uint32
}

// DefaultLimits returns proposed per-call limits for this inactive foundation:
// 100,000 admissions, 1,024 heights, 16 MiB JSON, and 256 UTF-8 topic bytes.
// Callers must select limits against their workload before endpoint activation.
func DefaultLimits() Limits {
	return Limits{MaxAdmitted: 100_000, MaxRange: 1024, MaxJSONBytes: 16 << 20, MaxTopicBytes: 256}
}

func (l Limits) validate() error {
	if l.MaxAdmitted == 0 || l.MaxRange == 0 || l.MaxJSONBytes == 0 || l.MaxTopicBytes == 0 {
		return fmt.Errorf("%w: BASM limits must all be positive", ErrInvalidInput)
	}
	return nil
}

// TopicBlockAnchor is the BRC-136 anchor value. Hashes are internal-order values;
// JSON encoding uses lowercase display-order hex. TAC is a separate cumulative
// calculation, not a field of the normative anchor. Use DecodeAnchorJSON for
// untrusted JSON: ordinary json.Unmarshal does not enforce required fields.
type TopicBlockAnchor struct {
	Topic         string `json:"topic"`
	BlockHeight   uint32 `json:"blockHeight"`
	BlockHash     Hash   `json:"blockHash"`
	BASMRoot      Hash   `json:"basmRoot"`
	AdmittedCount uint64 `json:"admittedCount"`
}

// Validate checks topic encoding, count limits, and the empty-root rule against
// limits. It does not authenticate the block, root, or admission history.
func (a TopicBlockAnchor) Validate(limits Limits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if err := validateTopic(a.Topic, limits.MaxTopicBytes); err != nil {
		return err
	}
	if a.AdmittedCount > uint64(limits.MaxAdmitted) {
		return fmt.Errorf("%w: admitted count exceeds local limit", ErrLimitExceeded)
	}
	if a.AdmittedCount == 0 && a.BASMRoot != (Hash{}) {
		return fmt.Errorf("%w: empty admission list requires zero BASM root", ErrInconsistent)
	}
	return nil
}

func validateTopic(topic string, maxBytes uint32) error {
	if len(topic) == 0 || uint64(len(topic)) > uint64(maxBytes) || !utf8.ValidString(topic) {
		return fmt.Errorf("%w: topic must be nonempty bounded UTF-8", ErrInvalidInput)
	}
	return nil
}

// AdmittedTxRef retains the original in-block index; gaps between indices are
// valid. Neither list validation nor root calculation sorts or renumbers it.
type AdmittedTxRef struct {
	TxID       Hash   `json:"txid"`
	BlockIndex uint64 `json:"blockIndex"`
}

// ValidateAdmittedList checks list bounds, unique txids, strictly increasing
// original indices, and index range against blockTxCount, then returns its root.
// ctx controls calculation cancellation. blockTxCount must be independently
// obtained by the caller; this range check does not prove block membership or
// position. No inputs are mutated. Duplicate primitive leaves are allowed by
// Root, but duplicate entries in an actual admitted list are rejected here.
func ValidateAdmittedList(ctx context.Context, admitted []AdmittedTxRef, blockTxCount uint64, limits Limits) (Hash, error) {
	if err := limits.validate(); err != nil {
		return Hash{}, err
	}
	if ctx == nil {
		return Hash{}, fmt.Errorf("%w: context must not be nil", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return Hash{}, err
	}
	if blockTxCount == 0 || blockTxCount > MaxSafeJSONInteger {
		return Hash{}, fmt.Errorf("%w: block transaction count must be a positive safe JSON integer", ErrInvalidInput)
	}
	if uint64(len(admitted)) > uint64(limits.MaxAdmitted) {
		return Hash{}, fmt.Errorf("%w: admitted list exceeds local limit", ErrLimitExceeded)
	}
	seen := make(map[Hash]struct{}, len(admitted))
	leaves := make([]Hash, len(admitted))
	var previousIndex uint64
	for i, ref := range admitted {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return Hash{}, err
			}
		}
		if ref.BlockIndex >= blockTxCount {
			return Hash{}, fmt.Errorf("%w: admitted entry %d: block index out of range", ErrInconsistent, i)
		}
		if i > 0 && ref.BlockIndex <= previousIndex {
			return Hash{}, fmt.Errorf("%w: admitted entry %d: block indices must strictly increase", ErrInconsistent, i)
		}
		if _, exists := seen[ref.TxID]; exists {
			return Hash{}, fmt.Errorf("%w: admitted entry %d: duplicate txid", ErrInconsistent, i)
		}
		seen[ref.TxID] = struct{}{}
		leaves[i] = ref.TxID
		previousIndex = ref.BlockIndex
	}
	return Root(ctx, leaves, limits.MaxAdmitted)
}

// ValidateAnchorList additionally checks the admitted list against the anchor's
// claimed count and root. Success is structural consistency only, not SPV.
func ValidateAnchorList(ctx context.Context, anchor TopicBlockAnchor, admitted []AdmittedTxRef, blockTxCount uint64, limits Limits) error {
	if err := anchor.Validate(limits); err != nil {
		return err
	}
	if anchor.AdmittedCount != uint64(len(admitted)) {
		return fmt.Errorf("%w: anchor count does not match admitted list", ErrInconsistent)
	}
	root, err := ValidateAdmittedList(ctx, admitted, blockTxCount, limits)
	if err != nil {
		return err
	}
	if root != anchor.BASMRoot {
		return fmt.Errorf("%w: anchor root does not match admitted list", ErrInconsistent)
	}
	return nil
}

// ValidateRange returns the inclusive height count for fromHeight..toHeight,
// rejecting inverted ranges and counts above maxHeights (which must be positive).
// Arithmetic widens before subtraction/addition to prevent uint32 overflow.
func ValidateRange(fromHeight, toHeight, maxHeights uint32) (uint32, error) {
	if maxHeights == 0 || fromHeight > toHeight {
		return 0, fmt.Errorf("%w: invalid height range or zero limit", ErrInvalidInput)
	}
	count := uint64(toHeight) - uint64(fromHeight) + 1
	if count > uint64(maxHeights) {
		return 0, fmt.Errorf("%w: height range exceeds local limit", ErrLimitExceeded)
	}
	return uint32(count), nil //nolint:gosec // bounded by uint32 maxHeights above
}
