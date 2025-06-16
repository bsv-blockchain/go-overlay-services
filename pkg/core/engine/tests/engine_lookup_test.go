package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

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
	expectedErr := errors.New("internal error")

	sut := &engine.Engine{
		LookupServices: map[string]engine.LookupService{
			"test": fakeLookupService{
				lookupFunc: func(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
					return nil, expectedErr
				},
			},
		},
	}

	// when
	actualAnswer, err := sut.Lookup(context.Background(), &lookup.LookupQuestion{Service: "test"})

	// then
	require.ErrorIs(t, err, expectedErr)
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
				lookupFunc: func(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
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
				lookupFunc: func(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
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
				lookupFunc: func(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
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
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
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
