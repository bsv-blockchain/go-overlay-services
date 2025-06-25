package gasp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

type fakeGASPStorage struct {
	findKnownUTXOsFunc func(ctx context.Context, since uint32) ([]*transaction.Outpoint, error)
}

func (f fakeGASPStorage) FindKnownUTXOs(ctx context.Context, since uint32) ([]*transaction.Outpoint, error) {
	return f.findKnownUTXOsFunc(ctx, since)
}

func (f fakeGASPStorage) HydrateGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*core.GASPNode, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) FindNeededInputs(ctx context.Context, tx *core.GASPNode) (*core.GASPNodeResponse, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) AppendToGraph(ctx context.Context, tx *core.GASPNode, spentBy *transaction.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) DiscardGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	panic("not implemented")
}

func TestGASP_GetInitialResponse_Success(t *testing.T) {
	// given:
	ctx := context.Background()
	request := &core.GASPInitialRequest{
		Version: 1,
		Since:   10,
	}

	expectedResponse := &core.GASPInitialResponse{
		UTXOList: []*transaction.Outpoint{
			{Index: 1},
			{Index: 2},
		},
		Since: 0,
	}

	sut := core.NewGASP(core.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(ctx context.Context, since uint32) ([]*transaction.Outpoint, error) {
				return expectedResponse.UTXOList, nil
			},
		},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.NoError(t, err)
	require.Equal(t, expectedResponse, actualResp)
}

func TestGASP_GetInitialResponse_VersionMismatch_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	request := &core.GASPInitialRequest{
		Version: 99, // wrong version
		Since:   0,
	}
	sut := core.NewGASP(core.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.IsType(t, &core.GASPVersionMismatchError{}, err)
	require.Nil(t, actualResp)
}

func TestGASP_GetInitialResponse_StorageFailure_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	request := &core.GASPInitialRequest{
		Version: 1,
		Since:   0,
	}

	expectedErr := errors.New("forced storage error")
	sut := core.NewGASP(core.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(ctx context.Context, since uint32) ([]*transaction.Outpoint, error) {
				return nil, expectedErr
			},
		},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, actualResp)
}

func ptr(i int) *int {
	return &i
}
