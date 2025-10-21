package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

var errInternalError = errors.New("internal error")

func TestEngine_Lookup_ShouldReturnError_WhenServiceUnknown(t *testing.T) {
	// given
	expectedErr := engine.ErrUnknownTopic

	sut := &engine.Engine{
		LookupServices: make(map[string]engine.LookupService),
	}

	// when
	actualAnswer, actualErr := sut.Lookup(context.Background(), &lookup.LookupQuestion{Service: "non-existing"})

	// then
	require.ErrorIs(t, actualErr, expectedErr)
	require.Nil(t, actualAnswer)
}

func TestEngine_Lookup_ShouldReturnError_WhenServiceLookupFails(t *testing.T) {
	// given
	sut := &engine.Engine{
		LookupServices: map[string]engine.LookupService{
			"test": fakeLookupService{
				lookupFunc: func(_ context.Context, _ *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
					return nil, errInternalError
				},
			},
		},
	}

	// when
	actualAnswer, err := sut.Lookup(context.Background(), &lookup.LookupQuestion{Service: "test"})

	// then
	require.ErrorIs(t, err, errInternalError)
	require.Nil(t, actualAnswer)
}

func TestEngine_Lookup_ShouldReturnDirectResult_WhenAnswerTypeIsFreeform(t *testing.T) {
	// given
	expectedAnswer := &lookup.LookupAnswer{
		Type: lookup.AnswerTypeFreeform,
		Result: map[string]interface{}{
			"key": "value",
		},
	}

	sut := &engine.Engine{
		LookupServices: map[string]engine.LookupService{
			"test": fakeLookupService{
				lookupFunc: func(_ context.Context, _ *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
					return expectedAnswer, nil
				},
			},
		},
	}

	// when
	actualAnswer, err := sut.Lookup(context.Background(), &lookup.LookupQuestion{Service: "test"})

	// then
	require.NoError(t, err)
	require.Equal(t, expectedAnswer, actualAnswer)
}

func TestEngine_Lookup_ShouldReturnDirectResult_WhenAnswerTypeIsOutputList(t *testing.T) {
	// given
	expectedAnswer := &lookup.LookupAnswer{
		Type: lookup.AnswerTypeOutputList,
		Outputs: []*lookup.OutputListItem{
			{
				OutputIndex: 0,
				Beef:        []byte("test"),
			},
		},
	}

	sut := &engine.Engine{
		LookupServices: map[string]engine.LookupService{
			"test": fakeLookupService{
				lookupFunc: func(_ context.Context, _ *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
					return expectedAnswer, nil
				},
			},
		},
	}

	// when
	actualAnswer, err := sut.Lookup(context.Background(), &lookup.LookupQuestion{Service: "test"})

	// then
	require.NoError(t, err)
	require.Equal(t, expectedAnswer, actualAnswer)
}

func TestEngine_Lookup_ShouldHydrateOutputs_WhenFormulasProvided(t *testing.T) {
	// given
	ctx := context.Background()
	expectedBeef := []byte("hydrated beef")
	outpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 0}

	sut := &engine.Engine{
		LookupServices: map[string]engine.LookupService{
			"test": fakeLookupService{
				lookupFunc: func(_ context.Context, _ *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
					return &lookup.LookupAnswer{
						Type: lookup.AnswerTypeFormula,
						Formulas: []lookup.LookupFormula{
							{Outpoint: &transaction.Outpoint{Txid: fakeTxID(t), Index: 0}},
						},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: *outpoint,
					Beef:     expectedBeef,
				}, nil
			},
		},
	}

	expectedAnswer := &lookup.LookupAnswer{
		Type: lookup.AnswerTypeOutputList,
		Outputs: []*lookup.OutputListItem{
			{
				OutputIndex: outpoint.Index,
				Beef:        expectedBeef,
			},
		},
	}

	// when
	actualAnswer, err := sut.Lookup(ctx, &lookup.LookupQuestion{Service: "test"})

	// then
	require.NoError(t, err)
	require.Equal(t, expectedAnswer, actualAnswer)
}
