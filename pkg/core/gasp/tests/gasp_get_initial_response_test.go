package gasp_test

import (
	"context"
	"errors"
	"testing"

	gasp "github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

type fakeGASPStorage struct {
	findKnownUTXOsFunc func(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error)
}

func (f fakeGASPStorage) FindKnownUTXOs(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
	return f.findKnownUTXOsFunc(ctx, since, limit)
}

func (f fakeGASPStorage) HydrateGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) FindNeededInputs(ctx context.Context, tx *gasp.Node) (*gasp.NodeResponse, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) AppendToGraph(ctx context.Context, tx *gasp.Node, spentBy *transaction.Outpoint) error {
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
	request := &gasp.InitialRequest{
		Version: 1,
		Since:   10,
	}

	// Create a dummy hash for testing
	dummyHash, _ := chainhash.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	utxoList := []*gasp.Output{
		{Txid: *dummyHash, OutputIndex: 1, Score: 100},
		{Txid: *dummyHash, OutputIndex: 2, Score: 200},
	}

	expectedResponse := &gasp.InitialResponse{
		UTXOList: utxoList,
		Since:    0,
	}

	sut := gasp.NewGASP(gasp.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
				return utxoList, nil
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
	request := &gasp.InitialRequest{
		Version: 99, // wrong version
		Since:   0,
	}
	sut := gasp.NewGASP(gasp.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.IsType(t, &gasp.VersionMismatchError{}, err)
	require.Nil(t, actualResp)
}

func TestGASP_GetInitialResponse_StorageFailure_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	request := &gasp.InitialRequest{
		Version: 1,
		Since:   0,
	}

	expectedErr := errors.New("forced storage error")
	sut := gasp.NewGASP(gasp.GASPParams{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
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

func TestGASP_GetInitialResponse_WithLimit_Success(t *testing.T) {
	// given:
	ctx := context.Background()
	limit := uint32(50)
	request := &gasp.InitialRequest{
		Version: 1,
		Since:   10,
		Limit:   limit,
	}

	// Create a dummy hash for testing
	dummyHash, _ := chainhash.NewHashFromHex("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	utxoList := []*gasp.Output{
		{Txid: *dummyHash, OutputIndex: 1, Score: 100},
		{Txid: *dummyHash, OutputIndex: 2, Score: 200},
	}

	expectedResponse := &gasp.InitialResponse{
		UTXOList: utxoList,
		Since:    0,
	}

	sut := gasp.NewGASP(gasp.GASPParams{
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
				require.Equal(t, uint32(50), limit)
				return utxoList, nil
			},
		},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.NoError(t, err)
	require.Equal(t, expectedResponse, actualResp)
}

func ptr(i int) *int {
	return &i
}
