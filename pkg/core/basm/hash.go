package basm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	hashSize             = 32
	contextCheckInterval = 1024
)

// Hash is a 32-byte hash stored in internal byte order, as used by Bitcoin
// Merkle-tree hashing. Its text representation uses reversed display order.
type Hash [hashSize]byte

// ParseHash parses an exactly 64-character lowercase hexadecimal hash in
// display order and stores it in internal byte order.
func ParseHash(s string) (Hash, error) {
	if len(s) != hashSize*2 {
		return Hash{}, fmt.Errorf("%w: hash must be exactly 64 lowercase hexadecimal characters", ErrInvalidHash)
	}

	for i := range s {
		if !isLowerHex(s[i]) {
			return Hash{}, fmt.Errorf("%w: hash must be exactly 64 lowercase hexadecimal characters", ErrInvalidHash)
		}
	}

	var display [hashSize]byte
	if _, err := hex.Decode(display[:], []byte(s)); err != nil {
		return Hash{}, err
	}

	var hash Hash
	for i := range hash {
		hash[i] = display[hashSize-1-i]
	}

	return hash, nil
}

// String returns the hash as lowercase hexadecimal in display order.
func (h Hash) String() string {
	var display [hashSize]byte
	for i := range h {
		display[hashSize-1-i] = h[i]
	}

	return hex.EncodeToString(display[:])
}

// MarshalText returns the lowercase display-order hash representation.
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

// UnmarshalText parses a lowercase display-order hash representation.
func (h *Hash) UnmarshalText(text []byte) error {
	if h == nil {
		return fmt.Errorf("%w: cannot unmarshal hash into nil receiver", ErrInvalidInput)
	}

	parsed, err := ParseHash(string(text))
	if err != nil {
		return err
	}

	*h = parsed
	return nil
}

// Root returns the BRC-0136 BASM root of leaves in canonical block order.
// maxLeaves must be positive and bounds the number of leaves accepted.
// ctx controls cancellation. Input order is preserved and leaves are not
// mutated. Duplicate leaves are permitted for primitive conformance vectors;
// use ValidateAdmittedList to reject duplicates in real admission claims.
func Root(ctx context.Context, leaves []Hash, maxLeaves uint32) (Hash, error) {
	if maxLeaves == 0 {
		return Hash{}, fmt.Errorf("%w: max leaves must be greater than zero", ErrInvalidInput)
	}
	if uint64(len(leaves)) > uint64(maxLeaves) {
		return Hash{}, fmt.Errorf("%w: leaf count exceeds maximum", ErrLimitExceeded)
	}
	if ctx == nil {
		return Hash{}, fmt.Errorf("%w: context must not be nil", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return Hash{}, err
	}

	switch len(leaves) {
	case 0:
		return Hash{}, nil
	case 1:
		return leaves[0], nil
	}

	// Each new layer is allocated separately, so caller-owned leaves remain
	// unchanged while their order is retained.
	layer := leaves
	for len(layer) > 1 {
		if err := ctx.Err(); err != nil {
			return Hash{}, err
		}

		next := make([]Hash, len(layer)/2+len(layer)%2)
		for left, parent := 0, 0; left < len(layer); left, parent = left+2, parent+1 {
			if left%contextCheckInterval == 0 {
				if err := ctx.Err(); err != nil {
					return Hash{}, err
				}
			}

			right := left + 1
			if right == len(layer) {
				right = left
			}
			next[parent] = hashPair(layer[left], layer[right])
		}

		layer = next
	}

	return layer[0], nil
}

// HashTACStep computes SHA256d(previous || blockHash || root) using internal
// byte-order hashes. It is low-level arithmetic only: it does not verify
// heights, genesis rules, continuity, or any chain state.
func HashTACStep(previous, blockHash, root Hash) Hash {
	var input [hashSize * 3]byte
	copy(input[:hashSize], previous[:])
	copy(input[hashSize:hashSize*2], blockHash[:])
	copy(input[hashSize*2:], root[:])

	return doubleSHA256(input[:])
}

func hashPair(left, right Hash) Hash {
	var input [hashSize * 2]byte
	copy(input[:hashSize], left[:])
	copy(input[hashSize:], right[:])

	return doubleSHA256(input[:])
}

func doubleSHA256(input []byte) Hash {
	first := sha256.Sum256(input)
	return sha256.Sum256(first[:])
}

func isLowerHex(b byte) bool {
	return ('0' <= b && b <= '9') || ('a' <= b && b <= 'f')
}
