package basm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// EncodeAnchorBinary encodes a BRC-136 Topic Block Anchor for peer-to-peer
// transport. Hashes are copied in their internal Bitcoin byte order.
func EncodeAnchorBinary(anchor TopicBlockAnchor, limits Limits) ([]byte, error) {
	if err := anchor.Validate(limits); err != nil {
		return nil, err
	}

	// Two maximum-length CompactSizes, one uint32, and two hashes.
	capacity := uint64(len(anchor.Topic)) + 9 + 4 + hashSize*2 + 9
	if capacity > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("%w: anchor exceeds addressable size", ErrLimitExceeded)
	}
	result := make([]byte, 0, int(capacity))
	result = appendCompactSize(result, uint64(len(anchor.Topic)))
	result = append(result, anchor.Topic...)
	var height [4]byte
	binary.LittleEndian.PutUint32(height[:], anchor.BlockHeight)
	result = append(result, height[:]...)
	result = append(result, anchor.BlockHash[:]...)
	result = append(result, anchor.BASMRoot[:]...)
	return appendCompactSize(result, anchor.AdmittedCount), nil
}

// DecodeAnchorBinary decodes one fully consumed BRC-136 Topic Block Anchor.
// CompactSize fields must use their shortest Bitcoin representation.
func DecodeAnchorBinary(data []byte, limits Limits) (TopicBlockAnchor, error) {
	if err := limits.validate(); err != nil {
		return TopicBlockAnchor{}, err
	}
	reader := boundedWireReader{data: data}
	topicLength, err := reader.compactSize()
	if err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("topic length: %w", err)
	}
	if topicLength == 0 || topicLength > uint64(limits.MaxTopicBytes) || topicLength > uint64(reader.remaining()) { //nolint:gosec // remaining is nonnegative
		return TopicBlockAnchor{}, fmt.Errorf("%w: topic length", ErrLimitExceeded)
	}
	topicBytes, err := reader.bytes(topicLength)
	if err != nil {
		return TopicBlockAnchor{}, err
	}
	if !utf8.Valid(topicBytes) {
		return TopicBlockAnchor{}, fmt.Errorf("%w: topic must be UTF-8", ErrInvalidInput)
	}
	heightBytes, err := reader.bytes(4)
	if err != nil {
		return TopicBlockAnchor{}, err
	}
	blockHash, err := reader.bytes(hashSize)
	if err != nil {
		return TopicBlockAnchor{}, err
	}
	root, err := reader.bytes(hashSize)
	if err != nil {
		return TopicBlockAnchor{}, err
	}
	count, err := reader.compactSize()
	if err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("admitted count: %w", err)
	}
	if reader.remaining() != 0 {
		return TopicBlockAnchor{}, fmt.Errorf("%w: trailing anchor bytes", ErrInvalidInput)
	}

	anchor := TopicBlockAnchor{
		Topic:         string(topicBytes),
		BlockHeight:   binary.LittleEndian.Uint32(heightBytes),
		AdmittedCount: count,
	}
	copy(anchor.BlockHash[:], blockHash)
	copy(anchor.BASMRoot[:], root)
	if err = anchor.Validate(limits); err != nil {
		return TopicBlockAnchor{}, err
	}
	return anchor, nil
}

// ParseBoundedMerklePath accepts one canonical BRC-74 BUMP with no trailing
// data. The SDK parser follows this preflight only after counts, offsets, and
// every hash-bearing item have been bounded against the received bytes.
func ParseBoundedMerklePath(data []byte, maxBytes uint32) (*transaction.MerklePath, error) {
	if err := validateWireSize(data, maxBytes); err != nil {
		return nil, err
	}
	if err := preflightMerklePath(data); err != nil {
		return nil, err
	}
	path, err := transaction.NewMerklePathFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse BUMP: %w", err)
	}
	return path, nil
}

// RawTransactionID validates one standard (non-EF, non-BEEF) Bitcoin
// transaction and returns its SHA256d identity in internal byte order. The
// original bytes, rather than a parsed object, are the identity preimage.
func RawTransactionID(data []byte, maxBytes uint32) (Hash, error) {
	if err := validateWireSize(data, maxBytes); err != nil {
		return Hash{}, err
	}
	if err := preflightRawTransaction(data); err != nil {
		return Hash{}, err
	}
	tx, err := transaction.NewTransactionFromBytes(data)
	if err != nil {
		return Hash{}, fmt.Errorf("parse transaction: %w", err)
	}
	if !bytes.Equal(tx.Bytes(), data) {
		return Hash{}, fmt.Errorf("%w: transaction serialization changed during parsing", ErrInvalidInput)
	}
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:]), nil
}

func validateWireSize(data []byte, maxBytes uint32) error {
	if maxBytes == 0 {
		return fmt.Errorf("%w: maximum byte count must be positive", ErrInvalidInput)
	}
	if uint64(len(data)) > uint64(maxBytes) {
		return fmt.Errorf("%w: byte count", ErrLimitExceeded)
	}
	return nil
}

func preflightMerklePath(data []byte) error {
	reader := boundedWireReader{data: data}
	treeHeight, err := readBUMPTreeHeight(&reader)
	if err != nil {
		return err
	}
	for level := uint64(0); level < treeHeight; level++ {
		if err = preflightMerkleLevel(&reader, level); err != nil {
			return err
		}
	}
	if reader.remaining() != 0 {
		return fmt.Errorf("%w: trailing BUMP bytes", ErrInvalidInput)
	}
	return nil
}

func readBUMPTreeHeight(reader *boundedWireReader) (uint64, error) {
	blockHeight, err := reader.compactSize()
	if err != nil {
		return 0, fmt.Errorf("BUMP block height: %w", err)
	}
	if blockHeight > math.MaxUint32 {
		return 0, fmt.Errorf("%w: BUMP block height overflows uint32", ErrInvalidInput)
	}
	treeHeightByte, err := reader.byte()
	if err != nil {
		return 0, fmt.Errorf("BUMP tree height: %w", err)
	}
	treeHeight := uint64(treeHeightByte)
	if treeHeight == 0 || treeHeight > 64 || treeHeight > uint64(reader.remaining()) { //nolint:gosec // remaining is nonnegative
		return 0, fmt.Errorf("%w: unsafe BUMP tree height", ErrInvalidInput)
	}
	return treeHeight, nil
}

func preflightMerkleLevel(reader *boundedWireReader, level uint64) error {
	count, err := reader.compactSize()
	if err != nil {
		return fmt.Errorf("BUMP level %d count: %w", level, err)
	}
	// Every item has at least a CompactSize offset and flags byte.
	// Compound paths can omit internal nodes derived from lower levels.
	// The leaf-level hash requirement below still excludes an empty proof.
	if count > uint64(reader.remaining()/2) { //nolint:gosec // remaining is nonnegative
		return fmt.Errorf("%w: unsafe BUMP item count at level %d", ErrInvalidInput, level)
	}
	seenOffsets := make(map[uint64]struct{}, count)
	levelHasHash := false
	for item := uint64(0); item < count; item++ {
		hasHash, itemErr := preflightMerkleItem(reader, level, item, seenOffsets)
		if itemErr != nil {
			return itemErr
		}
		if hasHash {
			levelHasHash = true
		}
	}
	if level == 0 && !levelHasHash {
		return fmt.Errorf("%w: BUMP leaf level has no hash", ErrInvalidInput)
	}
	return nil
}

func preflightMerkleItem(reader *boundedWireReader, level, item uint64, seenOffsets map[uint64]struct{}) (bool, error) {
	offset, err := reader.compactSize()
	if err != nil {
		return false, fmt.Errorf("BUMP level %d item %d offset: %w", level, item, err)
	}
	if !validBUMPPosition(offset, level) {
		return false, fmt.Errorf("%w: BUMP offset outside level %d", ErrInvalidInput, level)
	}
	if _, exists := seenOffsets[offset]; exists {
		return false, fmt.Errorf("%w: duplicate BUMP offset at level %d", ErrInvalidInput, level)
	}
	seenOffsets[offset] = struct{}{}
	flags, err := reader.byte()
	if err != nil {
		return false, fmt.Errorf("BUMP level %d item %d flags: %w", level, item, err)
	}
	if flags&^byte(3) != 0 || flags == 3 {
		return false, fmt.Errorf("%w: invalid BUMP flags", ErrInvalidInput)
	}
	if flags&1 != 0 && offset%2 == 0 {
		return false, fmt.Errorf("%w: BUMP duplicate must occupy a right-hand offset", ErrInvalidInput)
	}
	if flags&1 != 0 {
		return false, nil
	}
	if _, err = reader.bytes(hashSize); err != nil {
		return false, fmt.Errorf("BUMP level %d item %d hash: %w", level, item, err)
	}
	return true, nil
}

func validBUMPPosition(offset, level uint64) bool {
	if level >= 64 {
		return offset == 0
	}
	return offset <= math.MaxUint64>>level
}

func preflightRawTransaction(data []byte) error {
	reader := boundedWireReader{data: data}
	if err := skipRawTransactionInputs(&reader); err != nil {
		return err
	}
	if err := skipRawTransactionOutputs(&reader); err != nil {
		return err
	}
	if _, err := reader.bytes(4); err != nil {
		return fmt.Errorf("transaction locktime: %w", err)
	}
	if reader.remaining() != 0 {
		return fmt.Errorf("%w: trailing transaction bytes", ErrInvalidInput)
	}
	return nil
}

func skipRawTransactionInputs(reader *boundedWireReader) error {
	if _, err := reader.bytes(4); err != nil {
		return fmt.Errorf("transaction version: %w", err)
	}
	inputCount, err := reader.compactSize()
	if err != nil {
		return fmt.Errorf("transaction input count: %w", err)
	}
	// Standard transactions have at least one input. This also excludes the
	// SDK's EF marker (0-input, 0-output, locktime 0xEF) before SDK parsing.
	if inputCount == 0 || inputCount > uint64(reader.remaining()/41) { //nolint:gosec // remaining is nonnegative
		return fmt.Errorf("%w: unsafe transaction input count", ErrInvalidInput)
	}
	for input := uint64(0); input < inputCount; input++ {
		if err = skipRawTransactionInput(reader, input); err != nil {
			return err
		}
	}
	return nil
}

func skipRawTransactionInput(reader *boundedWireReader, input uint64) error {
	if _, err := reader.bytes(36); err != nil {
		return fmt.Errorf("transaction input %d outpoint: %w", input, err)
	}
	if err := skipWireScript(reader); err != nil {
		return fmt.Errorf("transaction input %d script: %w", input, err)
	}
	if _, err := reader.bytes(4); err != nil {
		return fmt.Errorf("transaction input %d sequence: %w", input, err)
	}
	return nil
}

func skipRawTransactionOutputs(reader *boundedWireReader) error {
	outputCount, err := reader.compactSize()
	if err != nil {
		return fmt.Errorf("transaction output count: %w", err)
	}
	if outputCount == 0 || outputCount > uint64(reader.remaining()/9) { //nolint:gosec // remaining is nonnegative
		return fmt.Errorf("%w: unsafe transaction output count", ErrInvalidInput)
	}
	for output := uint64(0); output < outputCount; output++ {
		if _, err = reader.bytes(8); err != nil {
			return fmt.Errorf("transaction output %d value: %w", output, err)
		}
		if err = skipWireScript(reader); err != nil {
			return fmt.Errorf("transaction output %d script: %w", output, err)
		}
	}
	return nil
}

func skipWireScript(reader *boundedWireReader) error {
	length, err := reader.compactSize()
	if err != nil {
		return err
	}
	if length > uint64(reader.remaining()) { //nolint:gosec // remaining is nonnegative
		return fmt.Errorf("%w: script length exceeds remaining bytes", ErrInvalidInput)
	}
	_, err = reader.bytes(length)
	return err
}

type boundedWireReader struct {
	data []byte
	pos  int
}

func (r *boundedWireReader) remaining() int { return len(r.data) - r.pos }

func (r *boundedWireReader) byte() (byte, error) {
	if r.remaining() < 1 {
		return 0, fmt.Errorf("%w: truncated data", ErrInvalidInput)
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *boundedWireReader) bytes(length uint64) ([]byte, error) {
	if length > uint64(r.remaining()) { //nolint:gosec // remaining is nonnegative
		return nil, fmt.Errorf("%w: truncated data", ErrInvalidInput)
	}
	end := r.pos + int(length) //nolint:gosec // length is bounded by remaining int above.
	value := r.data[r.pos:end]
	r.pos = end
	return value, nil
}

func (r *boundedWireReader) compactSize() (uint64, error) {
	prefix, err := r.byte()
	if err != nil {
		return 0, err
	}
	var width int
	var minimum uint64
	switch prefix {
	case 0xfd:
		width, minimum = 2, 0xfd
	case 0xfe:
		width, minimum = 4, 0x10000
	case 0xff:
		width, minimum = 8, 0x100000000
	default:
		return uint64(prefix), nil
	}
	encoded, err := r.bytes(uint64(width))
	if err != nil {
		return 0, err
	}
	var value uint64
	switch width {
	case 2:
		value = uint64(binary.LittleEndian.Uint16(encoded))
	case 4:
		value = uint64(binary.LittleEndian.Uint32(encoded))
	case 8:
		value = binary.LittleEndian.Uint64(encoded)
	}
	if value < minimum {
		return 0, fmt.Errorf("%w: noncanonical CompactSize", ErrInvalidInput)
	}
	return value, nil
}

func appendCompactSize(dst []byte, value uint64) []byte {
	switch {
	case value < 0xfd:
		return append(dst, byte(value))
	case value <= math.MaxUint16:
		return append(dst, 0xfd, byte(value), byte(value>>8)) //nolint:gosec // branch bounds value to uint16
	case value <= math.MaxUint32:
		return append(dst, 0xfe, byte(value), byte(value>>8), byte(value>>16), byte(value>>24)) //nolint:gosec // branch bounds value to uint32
	default:
		return append(dst, 0xff, byte(value), byte(value>>8), byte(value>>16), byte(value>>24), byte(value>>32), byte(value>>40), byte(value>>48), byte(value>>56)) //nolint:gosec // each conversion selects one serialized byte
	}
}
