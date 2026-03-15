package gasp

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// Binary wire format serialization for GASP types.
// These are transport-agnostic and can be used over libp2p, WebSocket, or raw TCP.

// SerializeInitialRequest encodes an InitialRequest into binary wire format.
//
//	[uint32 version][float64 since][uint32 limit]
func (r *InitialRequest) Serialize() []byte {
	buf := make([]byte, 0, 16)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(r.Version))
	buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(r.Since))
	buf = binary.LittleEndian.AppendUint32(buf, r.Limit)
	return buf
}

// DeserializeInitialRequest decodes binary wire format into an InitialRequest.
func DeserializeInitialRequest(data []byte) (*InitialRequest, error) {
	if len(data) != 16 {
		return nil, fmt.Errorf("InitialRequest: want 16 bytes, got %d", len(data))
	}
	return &InitialRequest{
		Version: int(binary.LittleEndian.Uint32(data[0:4])),
		Since:   math.Float64frombits(binary.LittleEndian.Uint64(data[4:12])),
		Limit:   binary.LittleEndian.Uint32(data[12:16]),
	}, nil
}

// SerializeInitialResponse encodes an InitialResponse into binary wire format.
//
//	[float64 since][varint count][32 bytes txid, uint32 index, float64 score]...
func (r *InitialResponse) Serialize() []byte {
	size := 8 + varintSize(len(r.UTXOList)) + len(r.UTXOList)*44
	buf := make([]byte, 0, size)
	buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(r.Since))
	buf = appendOutputList(buf, r.UTXOList)
	return buf
}

// DeserializeInitialResponse decodes binary wire format into an InitialResponse.
func DeserializeInitialResponse(data []byte) (*InitialResponse, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("InitialResponse too short: got %d bytes", len(data))
	}
	r := &InitialResponse{
		Since: math.Float64frombits(binary.LittleEndian.Uint64(data[0:8])),
	}
	var err error
	r.UTXOList, _, err = readOutputList(data, 8)
	if err != nil {
		return nil, fmt.Errorf("InitialResponse: %w", err)
	}
	return r, nil
}

// SerializeInitialReply encodes an InitialReply into binary wire format.
//
//	[varint count][32 bytes txid, uint32 index, float64 score]...
func (r *InitialReply) Serialize() []byte {
	size := varintSize(len(r.UTXOList)) + len(r.UTXOList)*44
	buf := make([]byte, 0, size)
	buf = appendOutputList(buf, r.UTXOList)
	return buf
}

// DeserializeInitialReply decodes binary wire format into an InitialReply.
func DeserializeInitialReply(data []byte) (*InitialReply, error) {
	r := &InitialReply{}
	var err error
	r.UTXOList, _, err = readOutputList(data, 0)
	if err != nil {
		return nil, fmt.Errorf("InitialReply: %w", err)
	}
	return r, nil
}

// SerializeNodeRequest encodes a NodeRequest into binary wire format.
//
//	[32 bytes graphID txid][uint32 graphID index]
//	[32 bytes txid][uint32 outputIndex]
//	[1 byte metadata]
func (r *NodeRequest) Serialize() []byte {
	buf := make([]byte, 0, 73)
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
	if len(data) != 73 {
		return nil, fmt.Errorf("NodeRequest: want 73 bytes, got %d", len(data))
	}
	r := &NodeRequest{}
	graphID := &transaction.Outpoint{}
	copy(graphID.Txid[:], data[0:32])
	graphID.Index = binary.LittleEndian.Uint32(data[32:36])
	r.GraphID = graphID

	txid := &chainhash.Hash{}
	copy(txid[:], data[36:68])
	r.Txid = txid

	r.OutputIndex = binary.LittleEndian.Uint32(data[68:72])
	r.Metadata = data[72] != 0
	return r, nil
}

// SerializeNode encodes a Node into binary wire format.
//
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

	size := 36 + 4 // graphID + outputIndex
	size += varintSize(len(rawTxBytes)) + len(rawTxBytes)
	size += varintSize(len(proofBytes)) + len(proofBytes)
	size += varintSize(len(txMeta)) + len(txMeta)
	size += varintSize(len(outMeta)) + len(outMeta)
	size += varintSize(len(n.Inputs))
	for hash := range n.Inputs {
		size += varintSize(len(hash)) + len(hash)
	}

	buf := make([]byte, 0, size)
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
	if len(data) < 40 {
		return nil, fmt.Errorf("Node too short: got %d bytes", len(data))
	}

	n := &Node{}
	offset := 0

	graphID := &transaction.Outpoint{}
	copy(graphID.Txid[:], data[offset:offset+32])
	offset += 32
	graphID.Index = binary.LittleEndian.Uint32(data[offset:])
	offset += 4
	n.GraphID = graphID

	n.OutputIndex = binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	var err error
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

	inputCount, nn := binary.Uvarint(data[offset:])
	if nn <= 0 {
		return nil, fmt.Errorf("invalid input count varint at offset %d", offset)
	}
	offset += nn

	if inputCount > 0 {
		n.Inputs = make(map[string]*Input, inputCount)
		for i := 0; i < int(inputCount); i++ {
			var hashBytes []byte
			hashBytes, offset, err = readByteField(data, offset)
			if err != nil {
				return nil, fmt.Errorf("input[%d]: %w", i, err)
			}
			n.Inputs[string(hashBytes)] = &Input{Hash: string(hashBytes)}
		}
	}

	if offset != len(data) {
		return nil, fmt.Errorf("trailing bytes: consumed %d of %d", offset, len(data))
	}
	return n, nil
}

// SerializeNodeResponse encodes a NodeResponse into binary wire format.
//
//	[varint count][32 bytes txid, uint32 index, 1 byte metadata]...
func (r *NodeResponse) Serialize() []byte {
	size := varintSize(len(r.RequestedInputs)) + len(r.RequestedInputs)*37
	buf := make([]byte, 0, size)
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
	r := &NodeResponse{}
	offset := 0

	count, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, fmt.Errorf("invalid count varint at offset %d", offset)
	}
	offset += n

	if offset+int(count)*37 > len(data) {
		return nil, fmt.Errorf("need %d bytes for %d inputs, got %d", count*37, count, len(data)-offset)
	}

	r.RequestedInputs = make(map[transaction.Outpoint]*NodeResponseData, count)
	for i := 0; i < int(count); i++ {
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
		return nil, fmt.Errorf("trailing bytes: consumed %d of %d", offset, len(data))
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
		return nil, offset, fmt.Errorf("invalid length varint at offset %d", offset)
	}
	offset += n
	if offset+int(length) > len(data) {
		return nil, offset, fmt.Errorf("need %d bytes, got %d", length, len(data)-offset)
	}
	if length == 0 {
		return nil, offset, nil
	}
	result := make([]byte, length)
	copy(result, data[offset:offset+int(length)])
	return result, offset + int(length), nil
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

func readOutputList(data []byte, offset int) ([]*Output, int, error) {
	count, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, offset, fmt.Errorf("invalid count varint at offset %d", offset)
	}
	offset += n

	needed := int(count) * 44
	if offset+needed > len(data) {
		return nil, offset, fmt.Errorf("need %d bytes for %d outputs, got %d", needed, count, len(data)-offset)
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
	return len(binary.AppendUvarint(nil, uint64(n)))
}
