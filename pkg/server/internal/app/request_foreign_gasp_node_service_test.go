package app_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/stretchr/testify/require"
)

func TestRequestForeignGASPNodeService_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		dto               app.RequestForeignGASPNodeDTO
		expectations      testabilities.RequestForeignGASPNodeProviderMockExpectations
		expectedErrorType app.ErrorType
	}{
		"Request foreign GASP node service fails to due to an invalid transaction ID format": {
			dto: app.RequestForeignGASPNodeDTO{
				GraphID:     testabilities.DefaultValidGraphID,
				TxID:        testabilities.DefaultInvalidTxID,
				OutputIndex: testabilities.DefaultValidOutputIndex,
				Topic:       testabilities.DefaultValidTopic,
			},
			expectations: testabilities.RequestForeignGASPNodeProviderMockExpectations{
				ProvideForeignGASPNodeCall: false,
			},
			expectedErrorType: app.ErrorTypeRawDataProcessing,
		},
		"Request foreign GASP node service fails due to an invalid graph ID format": {
			dto: app.RequestForeignGASPNodeDTO{
				GraphID:     testabilities.DefaultInvalidGraphID,
				TxID:        testabilities.DefaultValidTxID,
				OutputIndex: testabilities.DefaultValidOutputIndex,
				Topic:       testabilities.DefaultValidTopic,
			},
			expectations: testabilities.RequestForeignGASPNodeProviderMockExpectations{
				ProvideForeignGASPNodeCall: false,
			},
			expectedErrorType: app.ErrorTypeRawDataProcessing,
		},
		"Request foreign GASP node service fails due to an internal provider failure": {
			dto: testabilities.ForeignGASPNodeDefaultDTO,
			expectations: testabilities.RequestForeignGASPNodeProviderMockExpectations{
				ProvideForeignGASPNodeCall: true,
				Error:                      testabilities.ErrTestNoopOpFailure,
			},
			expectedErrorType: app.ErrorTypeProviderFailure,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testabilities.NewRequestForeignGASPNodeProviderMock(t, tc.expectations)
			service := app.NewRequestForeignGASPNodeService(mock)

			// when:
			node, err := service.RequestForeignGASPNode(t.Context(), tc.dto)

			// then:
			var actualErr app.Error
			require.ErrorAs(t, err, &actualErr)
			require.Equal(t, tc.expectedErrorType, actualErr.ErrorType())

			require.Nil(t, node)
			mock.AssertCalled()
		})
	}
}

func TestRequestForeignGASPNodeService_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewRequestForeignGASPNodeProviderMock(t, testabilities.DefaultRequestForeignGASPNodeProviderMockExpectations)
	service := app.NewRequestForeignGASPNodeService(mock)

	// when:
	node, err := service.RequestForeignGASPNode(t.Context(), testabilities.ForeignGASPNodeDefaultDTO)

	// then:
	require.NoError(t, err)
	require.Equal(t, testabilities.DefaultRequestForeignGASPNodeProviderMockExpectations.Node, node)
	mock.AssertCalled()
}
