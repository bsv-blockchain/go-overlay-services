package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

var errStorageFailed = errors.New("storage failed")

func TestEngine_ProvideForeignSyncResponse_ShouldReturnUTXOList(t *testing.T) {
	// given
	expectedOutpoint := &transaction.Outpoint{
		Txid:  fakeTxID(t),
		Index: 1,
	}
	expectedResponse := &gasp.InitialResponse{
		UTXOList: []*gasp.Output{{
			Txid:        expectedOutpoint.Txid,
			OutputIndex: expectedOutpoint.Index,
			Score:       0,
		}},
		Since: 0,
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
				return []*engine.Output{
					{Outpoint: *expectedOutpoint},
				}, nil
			},
		},
	}

	// when
	actualResponse, actualErr := sut.ProvideForeignSyncResponse(context.Background(), &gasp.InitialRequest{Since: 0}, "test-topic")

	// then
	require.NoError(t, actualErr)
	require.Equal(t, expectedResponse, actualResponse)
}

func TestEngine_ProvideForeignSyncResponse_ShouldReturnError_WhenStorageFails(t *testing.T) {
	// given
	sut := &engine.Engine{
		Storage: fakeStorage{
			findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
				return nil, errStorageFailed
			},
		},
	}

	// when
	resp, err := sut.ProvideForeignSyncResponse(context.Background(), &gasp.InitialRequest{Since: 0}, "test-topic")

	// then
	require.Error(t, err)
	require.Nil(t, resp)
	require.Equal(t, errStorageFailed, err)
}
