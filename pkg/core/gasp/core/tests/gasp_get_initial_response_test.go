package gasp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

type fakeGASPStorage struct {
	findKnownUTXOsFunc func(ctx context.Context, since uint32) ([]*overlay.Outpoint, error)
}

func (f fakeGASPStorage) FindKnownUTXOs(ctx context.Context, since uint32) ([]*overlay.Outpoint, error) {
	return f.findKnownUTXOsFunc(ctx, since)
}

func (f fakeGASPStorage) HydrateGASPNode(ctx context.Context, graphID *overlay.Outpoint, outpoint *overlay.Outpoint, metadata bool) (*core.GASPNode, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) FindNeededInputs(ctx context.Context, tx *core.GASPNode) (*core.GASPNodeResponse, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) AppendToGraph(ctx context.Context, tx *core.GASPNode, spentBy *overlay.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *overlay.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) DiscardGraph(ctx context.Context, graphID *overlay.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) FinalizeGraph(ctx context.Context, graphID *overlay.Outpoint) error {
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
		UTXOList: []*overlay.Outpoint{
			{OutputIndex: 1},
			{OutputIndex: 2},
		},
		Since: 0,
	}

	sut := core.NewGASP(core.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(ctx context.Context, since uint32) ([]*overlay.Outpoint, error) {
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
			findKnownUTXOsFunc: func(ctx context.Context, since uint32) ([]*overlay.Outpoint, error) {
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
