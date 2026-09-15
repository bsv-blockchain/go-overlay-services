package basm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Anchor is the current TS JSON anchor projection, including its cumulative TAC.
// It is not a persistence revision or evidence of peer agreement.
type Anchor struct {
	TopicBlockAnchor

	TAC Hash `json:"tac"`
}

// TopicAnchorTip matches the TS tip response. An initialized topic without an
// anchor uses blockHeight -1 and zero TAC; absent capability is an error instead.
type TopicAnchorTip struct {
	Topic         string  `json:"topic"`
	BlockHeight   int64   `json:"blockHeight"`
	BlockHash     *Hash   `json:"blockHash,omitempty"`
	BASMRoot      *Hash   `json:"basmRoot,omitempty"`
	AdmittedCount *uint64 `json:"admittedCount,omitempty"`
	TAC           Hash    `json:"tac"`
}

// TopicAnchorRange is an entire requested inclusive interval, never a silently
// shortened page. Clients choose smaller intervals when local limits reject it.
type TopicAnchorRange struct {
	Topic   string   `json:"topic"`
	Anchors []Anchor `json:"anchors"`
}

// AdmittedList contains original block indices in canonical block order.
type AdmittedList struct {
	Topic       string          `json:"topic"`
	BlockHeight uint32          `json:"blockHeight"`
	BlockHash   *Hash           `json:"blockHash,omitempty"`
	Admitted    []AdmittedTxRef `json:"admitted"`
}

// CompoundMerklePath carries standard BUMP hex, not a BASM membership proof.
type CompoundMerklePath struct {
	Topic       string `json:"topic"`
	BlockHeight uint32 `json:"blockHeight"`
	TxIDs       []Hash `json:"txids"`
	MerklePath  string `json:"merklePath"`
}

// RawTransactionRecord binds display txid to standard raw transaction hex.
type RawTransactionRecord struct {
	TxID  Hash   `json:"txid"`
	RawTx string `json:"rawTx"`
}

// RawTransactions explicitly lists absent requested transactions. Resource
// exhaustion is an error, never mislabeled as missing data.
type RawTransactions struct {
	Transactions []RawTransactionRecord `json:"transactions"`
	Missing      []Hash                 `json:"missing"`
}

// ReadLimits bounds a single server request and its response. Providers must
// honor byte/count budgets before allocation and cooperate with cancellation.
type ReadLimits struct {
	Limits

	MaxRequestedTxIDs uint32
	MaxRequestBytes   uint32
	MaxProofBytes     uint32
	MaxRawTxBytes     uint32
	MaxResponseBytes  uint32
	RequestTimeout    time.Duration
}

// DefaultReadLimits proposes finite limits for explicitly configured BASM read
// providers. Raw transactions can exceed 16 MiB; responses remain bounded even
// after hex encoding. There is no unlimited sentinel.
func DefaultReadLimits() ReadLimits {
	return ReadLimits{Limits: DefaultLimits(), MaxRequestedTxIDs: 1000, MaxRequestBytes: 1 << 20, MaxProofBytes: 8 << 20, MaxRawTxBytes: 32 << 20, MaxResponseBytes: 96 << 20, RequestTimeout: 10 * time.Second}
}

// Validate rejects zero/invalid local budgets; it does not activate any service.
func (l ReadLimits) Validate() error {
	if err := l.validate(); err != nil {
		return err
	}
	if l.MaxRequestedTxIDs == 0 || l.MaxRequestBytes == 0 || l.MaxProofBytes == 0 || l.MaxRawTxBytes == 0 || l.MaxResponseBytes == 0 || l.RequestTimeout <= 0 {
		return fmt.Errorf("%w: BASM read limits must be positive", ErrInvalidInput)
	}
	return nil
}

// ReadRequest is the validated input to one of the five TS BASM read routes.
type ReadRequest struct {
	FromHeight  uint32
	ToHeight    uint32
	BlockHeight uint32
	BlockHash   *Hash
	TxIDs       []Hash
}

// DecodeReadRequest validates the selected TS route's fields within limits.
// kind is tip, range, list, proof, or raw. Unknown fields remain additive;
// recognized integers and hashes use strict encodings without coercion.
func DecodeReadRequest(data []byte, kind string, limits ReadLimits) (ReadRequest, error) {
	if err := limits.Validate(); err != nil {
		return ReadRequest{}, err
	}
	bound := limits.Limits
	bound.MaxJSONBytes = limits.MaxRequestBytes
	dec, err := boundedDecoder(data, bound)
	if err != nil {
		return ReadRequest{}, err
	}
	fields, err := readObject(dec)
	if err != nil {
		return ReadRequest{}, err
	}
	if err = requireEOF(dec); err != nil {
		return ReadRequest{}, err
	}
	var request ReadRequest
	switch kind {
	case "tip":
		return request, nil
	case "range":
		request.FromHeight, request.ToHeight, err = DecodeRangeJSON(data, bound)
		return request, err
	case "list", "proof":
		height, parseErr := parseUint(fields["blockHeight"], 32)
		if parseErr != nil {
			return ReadRequest{}, parseErr
		}
		request.BlockHeight = uint32(height) //nolint:gosec // parsed as uint32
		if kind == "list" {
			if raw, exists := fields["blockHash"]; exists {
				parsed, hashErr := parseJSONHash(raw)
				if hashErr != nil {
					return ReadRequest{}, hashErr
				}
				request.BlockHash = &parsed
			}
			return request, nil
		}
	case "raw":
	default:
		return ReadRequest{}, fmt.Errorf("%w: unknown BASM request kind", ErrInvalidInput)
	}
	request.TxIDs, err = decodeRequestedTxIDs(fields["txids"], limits.MaxRequestedTxIDs, kind == "proof")
	return request, err
}

func decodeRequestedTxIDs(raw json.RawMessage, maxCount uint32, required bool) ([]Hash, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('[') {
		return nil, fmt.Errorf("%w: txids must be an array", ErrInvalidInput)
	}
	txids := make([]Hash, 0)
	seen := make(map[Hash]bool)
	for dec.More() {
		if uint64(len(txids)) >= uint64(maxCount) {
			return nil, fmt.Errorf("%w: requested txids", ErrLimitExceeded)
		}
		var item json.RawMessage
		if err = dec.Decode(&item); err != nil {
			return nil, err
		}
		hash, hashErr := parseJSONHash(item)
		if hashErr != nil {
			return nil, hashErr
		}
		if seen[hash] {
			return nil, fmt.Errorf("%w: duplicate requested txid", ErrInvalidInput)
		}
		seen[hash] = true
		txids = append(txids, hash)
	}
	if _, err = dec.Token(); err != nil {
		return nil, err
	}
	if err = requireEOF(dec); err != nil {
		return nil, err
	}
	if required && len(txids) == 0 {
		return nil, fmt.Errorf("%w: at least one proof txid is required", ErrInvalidInput)
	}
	return txids, nil
}
