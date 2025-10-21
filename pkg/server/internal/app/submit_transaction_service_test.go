package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

var errSubmitTransactionTestError = errors.New("internal submit transaction service test error")

func TestSubmitTransactionService_InvalidCase_ContextCancellation(t *testing.T) {
	expectations := testabilities.SubmitTransactionProviderMockExpectations{
		SubmitCall:           true,
		TriggerCallbackAfter: 3 * time.Second,
		STEAK:                nil,
	}

	// given:
	topics := app.TransactionTopics{"topic1", "topic2"}
	txBytes := testabilities.DummyTxBEEF(t)

	mock := testabilities.NewSubmitTransactionProviderMock(t, expectations)
	service := app.NewSubmitTransactionService(mock)
	expectedErr := app.NewContextCancellationError()

	// when:
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	steak, err := service.SubmitTransaction(ctx, topics, txBytes...)

	// then:
	var actualErr app.Error
	require.ErrorAs(t, err, &actualErr)
	require.Equal(t, expectedErr, actualErr)

	require.Nil(t, steak)
	mock.AssertCalled()
}

func TestSubmitTransactionService_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectations  testabilities.SubmitTransactionProviderMockExpectations
		topics        app.TransactionTopics
		timeout       time.Duration
		txBytes       []byte
		expectedError app.Error
	}{
		"Submit transaction service fails to handle the transaction submission - internal error": {
			topics:  app.TransactionTopics{"topic1", "topic2"},
			txBytes: testabilities.DummyTxBEEF(t),
			expectations: testabilities.SubmitTransactionProviderMockExpectations{
				SubmitCall: true,
				STEAK:      nil,
				Error:      errSubmitTransactionTestError,
			},
			expectedError: app.NewSubmitTransactionProviderError(errSubmitTransactionTestError),
		},
		"Submit transaction service fails to handle the transaction submission - empty topics": {
			txBytes: testabilities.DummyTxBEEF(t),
			expectations: testabilities.SubmitTransactionProviderMockExpectations{
				SubmitCall: false,
			},
			expectedError: app.NewEmptyTransactionTopicsError(),
		},
		"Submit transaction service fails to handle the transaction submission - second topic (at idx: 1) is an empty string": {
			txBytes: testabilities.DummyTxBEEF(t),
			topics:  app.TransactionTopics{"topic1", " "},
			expectations: testabilities.SubmitTransactionProviderMockExpectations{
				SubmitCall: false,
			},
			expectedError: app.NewErrInvalidTopicFormatError(1),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testabilities.NewSubmitTransactionProviderMock(t, tc.expectations)
			service := app.NewSubmitTransactionService(mock)

			// when:
			steak, err := service.SubmitTransaction(context.Background(), tc.topics, tc.txBytes...)

			// then:
			var actualErr app.Error
			require.ErrorAs(t, err, &actualErr)
			require.Equal(t, tc.expectedError, actualErr)

			require.Nil(t, steak)
			mock.AssertCalled()
		})
	}
}

func TestSubmitTransactionService_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.SubmitTransactionProviderMockExpectations{
		STEAK: &overlay.Steak{
			"test_response": &overlay.AdmittanceInstructions{
				OutputsToAdmit: []uint32{1},
				CoinsToRetain:  []uint32{1},
				CoinsRemoved:   []uint32{1},
			},
		},
		Error:      nil,
		SubmitCall: true,
	}

	topics := app.TransactionTopics{"topic1", "topic2"}
	mock := testabilities.NewSubmitTransactionProviderMock(t, expectations)
	service := app.NewSubmitTransactionService(mock)

	// when:
	actualSTEAK, err := service.SubmitTransaction(context.Background(), topics)

	// then:
	require.NoError(t, err)
	require.Equal(t, expectations.STEAK, actualSTEAK)
	mock.AssertCalled()
}
