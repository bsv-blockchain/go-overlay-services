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

var errForcedStorageError = errors.New("forced storage error")

type fakeGASPStorage struct {
	findKnownUTXOsFunc func(_ context.Context, since float64, limit uint32) ([]*gasp.Output, error)
}

func (f fakeGASPStorage) FindKnownUTXOs(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
	return f.findKnownUTXOsFunc(ctx, since, limit)
}

func (f fakeGASPStorage) HydrateGASPNode(_ context.Context, _, _ *transaction.Outpoint, _ bool) (*gasp.Node, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) FindNeededInputs(_ context.Context, _ *gasp.Node) (*gasp.NodeResponse, error) {
	panic("not implemented")
}

func (f fakeGASPStorage) AppendToGraph(_ context.Context, _ *gasp.Node, _ *transaction.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) ValidateGraphAnchor(_ context.Context, _ *transaction.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) DiscardGraph(_ context.Context, _ *transaction.Outpoint) error {
	panic("not implemented")
}

func (f fakeGASPStorage) FinalizeGraph(_ context.Context, _ *transaction.Outpoint) error {
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

	sut := gasp.NewGASP(gasp.Params{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(_ context.Context, _ float64, _ uint32) ([]*gasp.Output, error) {
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
	sut := gasp.NewGASP(gasp.Params{
		Version: ptr(1),
		Storage: fakeGASPStorage{},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.ErrorIs(t, err, &gasp.VersionMismatchError{})
	require.Nil(t, actualResp)
}

func TestGASP_GetInitialResponse_StorageFailure_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	request := &gasp.InitialRequest{
		Version: 1,
		Since:   0,
	}

	// Use the static error variable
	sut := gasp.NewGASP(gasp.Params{
		Version: ptr(1),
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(_ context.Context, _ float64, _ uint32) ([]*gasp.Output, error) {
				return nil, errForcedStorageError
			},
		},
	})

	// when:
	actualResp, err := sut.GetInitialResponse(ctx, request)

	// then:
	require.ErrorIs(t, err, errForcedStorageError)
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

	sut := gasp.NewGASP(gasp.Params{
		Storage: fakeGASPStorage{
			findKnownUTXOsFunc: func(_ context.Context, _ float64, _ uint32) ([]*gasp.Output, error) {
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
