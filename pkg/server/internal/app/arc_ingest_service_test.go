package app_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/stretchr/testify/require"
)

func TestARCIngestService_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		txID            string
		merklePath      string
		blockHeight     uint32
		expectedErrType app.ErrorType
		expectations    testabilities.ARCIngestProviderMockExpectations
	}{
		"ARC ingest service returns error for invalid transaction ID format": {
			expectedErrType: app.ErrorTypeIncorrectInput,
			txID:            "INVALID-HEX-STR",
			merklePath:      testabilities.NewTestMerklePath(t),
			blockHeight:     testabilities.DefaultBlockHeight,
			expectations: testabilities.ARCIngestProviderMockExpectations{
				HandleNewMerkleProofCall: false,
			},
		},
		"ARC ingest service returns error for invalid Merkle path format": {
			expectedErrType: app.ErrorTypeIncorrectInput,
			txID:            testabilities.NewTxID(t),
			merklePath:      "INVALID-HEX-STR",
			blockHeight:     testabilities.DefaultBlockHeight,
			expectations: testabilities.ARCIngestProviderMockExpectations{
				HandleNewMerkleProofCall: false,
			},
		},
		"ARC ingest service returns error for invalid block height (zero)": {
			expectedErrType: app.ErrorTypeIncorrectInput,
			txID:            testabilities.NewTxID(t),
			merklePath:      testabilities.NewTestMerklePath(t),
			blockHeight:     0,
			expectations: testabilities.ARCIngestProviderMockExpectations{
				HandleNewMerkleProofCall: false,
			},
		},
		"ARC ingest service returns error due to internal provider failure": {
			txID:            testabilities.NewTxID(t),
			merklePath:      testabilities.NewTestMerklePath(t),
			blockHeight:     testabilities.DefaultBlockHeight,
			expectedErrType: app.ErrorTypeProviderFailure,
			expectations: testabilities.ARCIngestProviderMockExpectations{
				HandleNewMerkleProofCall: true,
				Error:                    testabilities.ErrTestNoopOpFailure,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testabilities.NewARCIngestProviderMock(t, tc.expectations)
			service := app.NewARCIngestService(mock)

			// when:
			err := service.ProcessIngest(
				t.Context(),
				tc.txID,
				tc.merklePath,
				tc.blockHeight,
			)

			// then:
			var actualErr app.Error
			require.ErrorAs(t, err, &actualErr)
			require.Equal(t, tc.expectedErrType, actualErr.ErrorType())

			mock.AssertCalled()
		})
	}
}

func TestARCIngestService_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewARCIngestProviderMock(t, testabilities.ARCIngestProviderMockExpectations{
		Error:                    nil,
		HandleNewMerkleProofCall: true,
	})

	service := app.NewARCIngestService(mock)

	// when:
	err := service.ProcessIngest(
		t.Context(),
		testabilities.NewTxID(t),
		testabilities.NewTestMerklePath(t),
		testabilities.DefaultBlockHeight,
	)

	// then:
	require.NoError(t, err)
	mock.AssertCalled()
}
