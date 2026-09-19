package basm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopicBlockAnchorValidateRejectsZeroLimitsAndInvalidShape(t *testing.T) {
	valid := TopicBlockAnchor{Topic: "topic", BlockHeight: 7}
	limits := DefaultLimits()

	tests := []struct {
		name   string
		limits Limits
		anchor TopicBlockAnchor
	}{
		{
			name:   "zero maximum admissions",
			limits: Limits{MaxRange: 1, MaxJSONBytes: 1, MaxTopicBytes: 1},
			anchor: valid,
		},
		{
			name:   "zero maximum range",
			limits: Limits{MaxAdmitted: 1, MaxJSONBytes: 1, MaxTopicBytes: 1},
			anchor: valid,
		},
		{
			name:   "zero maximum JSON bytes",
			limits: Limits{MaxAdmitted: 1, MaxRange: 1, MaxTopicBytes: 1},
			anchor: valid,
		},
		{
			name:   "zero maximum topic bytes",
			limits: Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 1},
			anchor: valid,
		},
		{
			name:   "empty topic",
			limits: limits,
			anchor: TopicBlockAnchor{},
		},
		{
			name:   "invalid UTF-8 topic",
			limits: limits,
			anchor: TopicBlockAnchor{Topic: string([]byte{0xff})},
		},
		{
			name:   "topic over byte limit",
			limits: Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 1, MaxTopicBytes: 3},
			anchor: TopicBlockAnchor{Topic: "🌍"},
		},
		{
			name:   "admitted count over local limit",
			limits: limits,
			anchor: TopicBlockAnchor{Topic: "topic", AdmittedCount: uint64(limits.MaxAdmitted) + 1},
		},
		{
			name:   "nonzero root for empty admission list",
			limits: limits,
			anchor: TopicBlockAnchor{Topic: "topic", BASMRoot: Hash{1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.anchor.Validate(tt.limits))
		})
	}

	validMultibyte := TopicBlockAnchor{Topic: "café🌍", BlockHeight: 7}
	require.NoError(t, validMultibyte.Validate(limits))
}

func TestValidateAdmittedListAcceptsCoinbaseAndOriginalIndexGapsWithoutMutation(t *testing.T) {
	admitted := []AdmittedTxRef{
		{TxID: Hash{1}, BlockIndex: 0},
		{TxID: Hash{2}, BlockIndex: 2},
		{TxID: Hash{3}, BlockIndex: 5},
	}
	original := append([]AdmittedTxRef(nil), admitted...)

	_, err := ValidateAdmittedList(context.Background(), admitted, 6, DefaultLimits())

	require.NoError(t, err)
	assert.Equal(t, original, admitted)
}

func TestValidateAdmittedListRejectsInvalidReferences(t *testing.T) {
	limits := DefaultLimits()
	tests := []struct {
		name         string
		admitted     []AdmittedTxRef
		blockTxCount uint64
		limits       Limits
	}{
		{
			name: "duplicate txid",
			admitted: []AdmittedTxRef{
				{TxID: Hash{1}, BlockIndex: 0},
				{TxID: Hash{1}, BlockIndex: 1},
			},
			blockTxCount: 2,
			limits:       limits,
		},
		{
			name: "duplicate index",
			admitted: []AdmittedTxRef{
				{TxID: Hash{1}, BlockIndex: 0},
				{TxID: Hash{2}, BlockIndex: 0},
			},
			blockTxCount: 2,
			limits:       limits,
		},
		{
			name: "reversed original order",
			admitted: []AdmittedTxRef{
				{TxID: Hash{1}, BlockIndex: 2},
				{TxID: Hash{2}, BlockIndex: 1},
			},
			blockTxCount: 3,
			limits:       limits,
		},
		{
			name:         "index at block transaction count",
			admitted:     []AdmittedTxRef{{TxID: Hash{1}, BlockIndex: 2}},
			blockTxCount: 2,
			limits:       limits,
		},
		{
			name:         "empty block transaction count",
			admitted:     nil,
			blockTxCount: 0,
			limits:       limits,
		},
		{
			name:         "unsafe block transaction count",
			admitted:     nil,
			blockTxCount: MaxSafeJSONInteger + 1,
			limits:       limits,
		},
		{
			name: "admitted list over local limit",
			admitted: []AdmittedTxRef{
				{TxID: Hash{1}, BlockIndex: 0},
				{TxID: Hash{2}, BlockIndex: 1},
			},
			blockTxCount: 2,
			limits:       Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 1, MaxTopicBytes: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateAdmittedList(context.Background(), tt.admitted, tt.blockTxCount, tt.limits)
			require.Error(t, err)
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ValidateAdmittedList(ctx, nil, 1, limits)
	require.ErrorIs(t, err, context.Canceled)

	var nilContext context.Context
	_, err = ValidateAdmittedList(nilContext, nil, 1, limits)
	require.Error(t, err)
}

func TestValidateAnchorListChecksEmptyAndClaimedRootAndCount(t *testing.T) {
	limits := DefaultLimits()
	emptyAnchor := TopicBlockAnchor{Topic: "topic", BlockHeight: 4}

	require.NoError(t, ValidateAnchorList(context.Background(), emptyAnchor, nil, 1, limits))

	countMismatch := emptyAnchor
	countMismatch.AdmittedCount = 1
	require.Error(t, ValidateAnchorList(context.Background(), countMismatch, nil, 1, limits))

	rootMismatch := TopicBlockAnchor{Topic: "topic", BlockHeight: 4, BASMRoot: Hash{2}, AdmittedCount: 1}
	require.Error(t, ValidateAnchorList(context.Background(), rootMismatch, []AdmittedTxRef{{TxID: Hash{1}, BlockIndex: 0}}, 1, limits))
}

func TestValidateRangePreventsUint32Overflow(t *testing.T) {
	maxUint32 := ^uint32(0)
	tests := []struct {
		name    string
		from    uint32
		to      uint32
		max     uint32
		want    uint32
		wantErr bool
	}{
		{
			name: "proposed page boundary",
			from: 850000,
			to:   851023,
			max:  DefaultLimits().MaxRange,
			want: 1024,
		},
		{
			name:    "one height beyond proposed page boundary",
			from:    850000,
			to:      851024,
			max:     DefaultLimits().MaxRange,
			wantErr: true,
		},
		{
			name: "maximum singleton",
			from: maxUint32,
			to:   maxUint32,
			max:  1,
			want: 1,
		},
		{
			name: "maximum bounded range",
			from: 1,
			to:   maxUint32,
			max:  maxUint32,
			want: maxUint32,
		},
		{
			name:    "full uint32 range exceeds uint32 limit",
			from:    0,
			to:      maxUint32,
			max:     maxUint32,
			wantErr: true,
		},
		{
			name:    "inverted range",
			from:    2,
			to:      1,
			max:     2,
			wantErr: true,
		},
		{
			name:    "zero maximum range",
			from:    1,
			to:      1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateRange(tt.from, tt.to, tt.max)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
