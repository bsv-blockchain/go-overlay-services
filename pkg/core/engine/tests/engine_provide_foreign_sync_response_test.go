package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

func TestEngine_ProvideForeignSyncResponse_ShouldReturnUTXOList(t *testing.T) {
	// given
	expectedOutpoint := &transaction.Outpoint{
		Txid:  fakeTxID(t),
		Index: 1,
	}
	expectedResponse := &core.GASPInitialResponse{
		UTXOList: []*transaction.Outpoint{expectedOutpoint},
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findUTXOsForTopicFunc: func(ctx context.Context, topic string, since uint32, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{
					{Outpoint: *expectedOutpoint},
				}, nil
			},
		},
	}

	// when
	actualResponse, actualErr := sut.ProvideForeignSyncResponse(context.Background(), &core.GASPInitialRequest{Since: 0}, "test-topic")

	// then
	require.NoError(t, actualErr)
	require.Equal(t, expectedResponse, actualResponse)
}

func TestEngine_ProvideForeignSyncResponse_ShouldReturnError_WhenStorageFails(t *testing.T) {
	// given
	expectedError := errors.New("storage failed")
	sut := &engine.Engine{
		Storage: fakeStorage{
			findUTXOsForTopicFunc: func(ctx context.Context, topic string, since uint32, includeBEEF bool) ([]*engine.Output, error) {
				return nil, expectedError
			},
		},
	}

	// when
	resp, err := sut.ProvideForeignSyncResponse(context.Background(), &core.GASPInitialRequest{Since: 0}, "test-topic")

	// then
	require.Error(t, err)
	require.Nil(t, resp)
	require.Equal(t, expectedError, err)
}
