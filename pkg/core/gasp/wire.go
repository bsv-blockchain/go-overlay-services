package gasp

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// Wire format error sentinels.
var (
	ErrWireEmptyData      = errors.New("empty data")
	ErrWireUnsupportedVer = errors.New("unsupported wire version")
	ErrWireTooShort       = errors.New("insufficient data")
	ErrWireInvalidVarint  = errors.New("invalid varint")
	ErrWireTrailingBytes  = errors.New("trailing bytes")
)

// fmtVarintAtOffset formats a "invalid varint at offset" error message.
const fmtVarintAtOffset = "%w at offset %d"

// Binary wire format serialization for GASP types.
// These are transport-agnostic and can be used over libp2p, WebSocket, or raw TCP.
// Every serialized message starts with a version byte for forward compatibility.

// WireVersion is the current binary wire format version byte.
const WireVersion byte = 0

func writeVersion(buf []byte) []byte {
	return append(buf, WireVersion)
}

func checkVersion(data []byte) (int, error) {
	if len(data) < 1 {
		return 0, ErrWireEmptyData
	}
	if data[0] != WireVersion {
		return 0, fmt.Errorf("%w: %d", ErrWireUnsupportedVer, data[0])
	}
	return 1, nil
}

// Serialize encodes an InitialRequest into binary wire format.
//
//	[1 byte version]
//	[uint32 version][float64 since][uint32 limit]
func (r *InitialRequest) Serialize() []byte {
	buf := make([]byte, 0, 17)
	buf = writeVersion(buf)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(r.Version)) //nolint:gosec // version is always small
	buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(r.Since))
	buf = binary.LittleEndian.AppendUint32(buf, r.Limit)
	return buf
}

// DeserializeInitialRequest decodes binary wire format into an InitialRequest.
func DeserializeInitialRequest(data []byte) (*InitialRequest, error) {
	offset, err := checkVersion(data)
	if err != nil {
		return nil, fmt.Errorf("InitialRequest: %w", err)
	}
	if len(data)-offset != 16 {
		return nil, fmt.Errorf("InitialRequest: %w: want 16 bytes, got %d", ErrWireTooShort, len(data)-offset)
	}
	return &InitialRequest{
		Version: int(binary.LittleEndian.Uint32(data[offset : offset+4])),
		Since:   math.Float64frombits(binary.LittleEndian.Uint64(data[offset+4 : offset+12])),
		Limit:   binary.LittleEndian.Uint32(data[offset+12 : offset+16]),
	}, nil
}

// Serialize encodes an InitialResponse into binary wire format.
//
//	[1 byte version]
//	[float64 since][varint count][32 bytes txid, uint32 index, float64 score]...
func (r *InitialResponse) Serialize() []byte {
	size := 1 + 8 + varintSize(len(r.UTXOList)) + len(r.UTXOList)*44
	buf := make([]byte, 0, size)
	buf = writeVersion(buf)
	buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(r.Since))
	buf = appendOutputList(buf, r.UTXOList)
	return buf
}

// DeserializeInitialResponse decodes binary wire format into an InitialResponse.
func DeserializeInitialResponse(data []byte) (*InitialResponse, error) {
	offset, err := checkVersion(data)
	if err != nil {
		return nil, fmt.Errorf("InitialResponse: %w", err)
	}
	if len(data)-offset < 8 {
		return nil, fmt.Errorf("InitialResponse: %w: got %d bytes", ErrWireTooShort, len(data)-offset)
	}
	r := &InitialResponse{
		Since: math.Float64frombits(binary.LittleEndian.Uint64(data[offset : offset+8])),
	}
	r.UTXOList, _, err = readOutputList(data, offset+8)
	if err != nil {
		return nil, fmt.Errorf("InitialResponse: %w", err)
	}
	return r, nil
}

// Serialize encodes an InitialReply into binary wire format.
//
//	[1 byte version]
//	[varint count][32 bytes txid, uint32 index, float64 score]...
func (r *InitialReply) Serialize() []byte {
	size := 1 + varintSize(len(r.UTXOList)) + len(r.UTXOList)*44
	buf := make([]byte, 0, size)
	buf = writeVersion(buf)
	buf = appendOutputList(buf, r.UTXOList)
	return buf
}

// DeserializeInitialReply decodes binary wire format into an InitialReply.
func DeserializeInitialReply(data []byte) (*InitialReply, error) {
	offset, err := checkVersion(data)
	if err != nil {
		return nil, fmt.Errorf("InitialReply: %w", err)
	}
	r := &InitialReply{}
	r.UTXOList, _, err = readOutputList(data, offset)
	if err != nil {
		return nil, fmt.Errorf("InitialReply: %w", err)
	}
	return r, nil
}

// Serialize encodes a NodeRequest into binary wire format.
//
//	[1 byte version]
//	[32 bytes graphID txid][uint32 graphID index]
//	[32 bytes txid][uint32 outputIndex]
//	[1 byte metadata]
func (r *NodeRequest) Serialize() []byte {
	buf := make([]byte, 0, 74)
	buf = writeVersion(buf)
	if r.GraphID != nil {
		buf = append(buf, r.GraphID.Txid[:]...)
		buf = binary.LittleEndian.AppendUint32(buf, r.GraphID.Index)
	} else {
		buf = append(buf, make([]byte, 36)...)
	}
	if r.Txid != nil {
		buf = append(buf, r.Txid[:]...)
	} else {
		buf = append(buf, make([]byte, 32)...)
	}
	buf = binary.LittleEndian.AppendUint32(buf, r.OutputIndex)
	if r.Metadata {
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	return buf
}

// DeserializeNodeRequest decodes binary wire format into a NodeRequest.
func DeserializeNodeRequest(data []byte) (*NodeRequest, error) {
	offset, err := checkVersion(data)
	if err != nil {
		return nil, fmt.Errorf("NodeRequest: %w", err)
	}
	if len(data)-offset != 73 {
		return nil, fmt.Errorf("NodeRequest: %w: want 73 bytes, got %d", ErrWireTooShort, len(data)-offset)
	}
	r := &NodeRequest{}
	graphID := &transaction.Outpoint{}
	copy(graphID.Txid[:], data[offset:offset+32])
	graphID.Index = binary.LittleEndian.Uint32(data[offset+32 : offset+36])
	r.GraphID = graphID

	txid := &chainhash.Hash{}
	copy(txid[:], data[offset+36:offset+68])
	r.Txid = txid

	r.OutputIndex = binary.LittleEndian.Uint32(data[offset+68 : offset+72])
	r.Metadata = data[offset+72] != 0
	return r, nil
}

// Serialize encodes a Node into binary wire format.
//
//	[1 byte version]
//	[32 bytes graphID txid][uint32 graphID index]
//	[uint32 outputIndex]
//	[varint len][rawTx bytes]
//	[varint len][proof bytes]
//	[varint len][txMetadata bytes]
//	[varint len][outputMetadata bytes]
//	[varint input count][varint len, hash bytes]...
func (n *Node) Serialize() ([]byte, error) {
	rawTxBytes, err := hex.DecodeString(n.RawTx)
	if err != nil {
		return nil, fmt.Errorf("decode rawTx hex: %w", err)
	}

	var proofBytes []byte
	if n.Proof != nil {
		proofBytes, err = hex.DecodeString(*n.Proof)
		if err != nil {
			return nil, fmt.Errorf("decode proof hex: %w", err)
		}
	}

	txMeta := []byte(n.TxMetadata)
	outMeta := []byte(n.OutputMetadata)

	size := 1 + 36 + 4 // version + graphID + outputIndex
	size += varintSize(len(rawTxBytes)) + len(rawTxBytes)
	size += varintSize(len(proofBytes)) + len(proofBytes)
	size += varintSize(len(txMeta)) + len(txMeta)
	size += varintSize(len(outMeta)) + len(outMeta)
	size += varintSize(len(n.Inputs))
	for hash := range n.Inputs {
		size += varintSize(len(hash)) + len(hash)
	}

	buf := make([]byte, 0, size)
	buf = writeVersion(buf)
	if n.GraphID != nil {
		buf = append(buf, n.GraphID.Txid[:]...)
		buf = binary.LittleEndian.AppendUint32(buf, n.GraphID.Index)
	} else {
		buf = append(buf, make([]byte, 36)...)
	}
	buf = binary.LittleEndian.AppendUint32(buf, n.OutputIndex)
	buf = appendByteField(buf, rawTxBytes)
	buf = appendByteField(buf, proofBytes)
	buf = appendByteField(buf, txMeta)
	buf = appendByteField(buf, outMeta)

	buf = binary.AppendUvarint(buf, uint64(len(n.Inputs)))
	for hash := range n.Inputs {
		buf = appendByteField(buf, []byte(hash))
	}

	return buf, nil
}

// DeserializeNode decodes binary wire format into a Node.
func DeserializeNode(data []byte) (*Node, error) {
	offset, err := checkVersion(data)
	if err != nil {
		return nil, fmt.Errorf("Node: %w", err)
	}
	if len(data)-offset < 40 {
		return nil, fmt.Errorf("Node: %w: got %d bytes", ErrWireTooShort, len(data)-offset)
	}

	n := &Node{}

	graphID := &transaction.Outpoint{}
	copy(graphID.Txid[:], data[offset:offset+32])
	offset += 32
	graphID.Index = binary.LittleEndian.Uint32(data[offset:])
	offset += 4
	n.GraphID = graphID

	n.OutputIndex = binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	var rawTxBytes, proofBytes, txMeta, outMeta []byte

	rawTxBytes, offset, err = readByteField(data, offset)
	if err != nil {
		return nil, fmt.Errorf("rawTx: %w", err)
	}
	n.RawTx = hex.EncodeToString(rawTxBytes)

	proofBytes, offset, err = readByteField(data, offset)
	if err != nil {
		return nil, fmt.Errorf("proof: %w", err)
	}
	if len(proofBytes) > 0 {
		proofHex := hex.EncodeToString(proofBytes)
		n.Proof = &proofHex
	}

	txMeta, offset, err = readByteField(data, offset)
	if err != nil {
		return nil, fmt.Errorf("txMetadata: %w", err)
	}
	n.TxMetadata = string(txMeta)

	outMeta, offset, err = readByteField(data, offset)
	if err != nil {
		return nil, fmt.Errorf("outputMetadata: %w", err)
	}
	n.OutputMetadata = string(outMeta)

	n.Inputs, offset, err = deserializeNodeInputs(data, offset)
	if err != nil {
		return nil, err
	}

	if offset != len(data) {
		return nil, fmt.Errorf("%w: consumed %d of %d", ErrWireTrailingBytes, offset, len(data))
	}
	return n, nil
}

// deserializeNodeInputs reads the input map from binary wire format.
func deserializeNodeInputs(data []byte, offset int) (map[string]*Input, int, error) {
	inputCount, nn := binary.Uvarint(data[offset:])
	if nn <= 0 {
		return nil, offset, fmt.Errorf(fmtVarintAtOffset, ErrWireInvalidVarint, offset)
	}
	offset += nn

	if inputCount > math.MaxUint32 {
		return nil, offset, fmt.Errorf("%w: input count %d exceeds uint32", ErrWireTooShort, inputCount)
	}
	if inputCount == 0 {
		return nil, offset, nil
	}
	inputs := make(map[string]*Input, inputCount)
	for i := range int(inputCount) {
		hashBytes, newOffset, err := readByteField(data, offset)
		if err != nil {
			return nil, offset, fmt.Errorf("input[%d]: %w", i, err)
		}
		offset = newOffset
		inputs[string(hashBytes)] = &Input{Hash: string(hashBytes)}
	}
	return inputs, offset, nil
}

// Serialize encodes a NodeResponse into binary wire format.
//
//	[1 byte version]
//	[varint count][32 bytes txid, uint32 index, 1 byte metadata]...
func (r *NodeResponse) Serialize() []byte {
	size := 1 + varintSize(len(r.RequestedInputs)) + len(r.RequestedInputs)*37
	buf := make([]byte, 0, size)
	buf = writeVersion(buf)
	buf = binary.AppendUvarint(buf, uint64(len(r.RequestedInputs)))
	for outpoint, data := range r.RequestedInputs {
		buf = append(buf, outpoint.Txid[:]...)
		buf = binary.LittleEndian.AppendUint32(buf, outpoint.Index)
		if data != nil && data.Metadata {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
	}
	return buf
}

// DeserializeNodeResponse decodes binary wire format into a NodeResponse.
func DeserializeNodeResponse(data []byte) (*NodeResponse, error) {
	offset, err := checkVersion(data)
	if err != nil {
		return nil, fmt.Errorf("NodeResponse: %w", err)
	}

	r := &NodeResponse{}

	count, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, fmt.Errorf(fmtVarintAtOffset, ErrWireInvalidVarint, offset)
	}
	offset += n

	if offset+int(count)*37 > len(data) { //nolint:gosec // count is bounded by data length
		return nil, fmt.Errorf("%w: need %d bytes for %d inputs, got %d", ErrWireTooShort, count*37, count, len(data)-offset)
	}

	r.RequestedInputs = make(map[transaction.Outpoint]*NodeResponseData, count)
	for i := 0; i < int(count); i++ { //nolint:gosec // count bounded above
		outpoint := transaction.Outpoint{}
		copy(outpoint.Txid[:], data[offset:offset+32])
		offset += 32
		outpoint.Index = binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		r.RequestedInputs[outpoint] = &NodeResponseData{
			Metadata: data[offset] != 0,
		}
		offset++
	}

	if offset != len(data) {
		return nil, fmt.Errorf("%w: consumed %d of %d", ErrWireTrailingBytes, offset, len(data))
	}
	return r, nil
}

// helpers

func appendByteField(buf, data []byte) []byte {
	buf = binary.AppendUvarint(buf, uint64(len(data)))
	return append(buf, data...)
}

func readByteField(data []byte, offset int) ([]byte, int, error) {
	length, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, offset, fmt.Errorf(fmtVarintAtOffset, ErrWireInvalidVarint, offset)
	}
	offset += n
	if offset+int(length) > len(data) { //nolint:gosec // length bounded by data size
		return nil, offset, fmt.Errorf("%w: need %d bytes, got %d", ErrWireTooShort, length, len(data)-offset)
	}
	if length == 0 {
		return nil, offset, nil
	}
	result := make([]byte, length)
	copy(result, data[offset:offset+int(length)]) //nolint:gosec // length bounded by data size
	return result, offset + int(length), nil      //nolint:gosec // length bounded by data size
}

func appendOutputList(buf []byte, outputs []*Output) []byte {
	buf = binary.AppendUvarint(buf, uint64(len(outputs)))
	for _, o := range outputs {
		buf = append(buf, o.Txid[:]...)
		buf = binary.LittleEndian.AppendUint32(buf, o.OutputIndex)
		buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(o.Score))
	}
	return buf
}

func readOutputList(data []byte, offset int) ([]*Output, int, error) { //nolint:unparam // offset return kept for consistency with readByteField
	count, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, offset, fmt.Errorf(fmtVarintAtOffset, ErrWireInvalidVarint, offset)
	}
	offset += n

	needed := int(count) * 44 //nolint:gosec // count bounded by data length
	if offset+needed > len(data) {
		return nil, offset, fmt.Errorf("%w: need %d bytes for %d outputs, got %d", ErrWireTooShort, needed, count, len(data)-offset)
	}

	outputs := make([]*Output, count)
	for i := range outputs {
		o := &Output{}
		copy(o.Txid[:], data[offset:offset+32])
		offset += 32
		o.OutputIndex = binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		o.Score = math.Float64frombits(binary.LittleEndian.Uint64(data[offset:]))
		offset += 8
		outputs[i] = o
	}
	return outputs, offset, nil
}

func varintSize(n int) int {
	return len(binary.AppendUvarint(nil, uint64(n))) //nolint:gosec // n is always non-negative (slice length)
}
