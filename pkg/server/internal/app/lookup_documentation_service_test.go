package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/stretchr/testify/require"
)

func TestLookupDocumentationService_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedError app.Error
		expectations  testabilities.LookupServiceDocumentationProviderMockExpectations
		lookupService string
	}{
		"Lookup documentation service fails to handle request - empty lookup service name": {
			lookupService: "",
			expectations: testabilities.LookupServiceDocumentationProviderMockExpectations{
				DocumentationCall: false,
			},
			expectedError: app.NewEmptyLookupServiceNameError(),
		},
		"Lookup manager documentation service fails to handle request - internal error": {
			lookupService: "test-lookup-service",
			expectations: testabilities.LookupServiceDocumentationProviderMockExpectations{
				DocumentationCall: true,
				Error:             errors.New("internal lookup service documentation provider test error"),
			},
			expectedError: app.NewLookupServiceProviderDocumentationError(errors.New("internal lookup service documentation provider test error")),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testabilities.NewLookupServiceDocumentationProviderMock(t, tc.expectations)
			service := app.NewLookupDocumentationService(mock)

			// when:
			document, err := service.GetDocumentation(context.Background(), tc.lookupService)

			// then:
			var actualErr app.Error
			require.ErrorAs(t, err, &actualErr)
			require.Equal(t, tc.expectedError, actualErr)

			require.Empty(t, document)
			mock.AssertCalled()
		})
	}
}

func TestGetLookupServiceProviderDocumentation_Success(t *testing.T) {
	// given:
	mock := testabilities.NewLookupServiceDocumentationProviderMock(t, testabilities.DefaultLookupServiceDocumentationProviderMockExpectations)
	service := app.NewLookupDocumentationService(mock)

	// when:
	documentation, err := service.GetDocumentation(context.Background(), "test-service")

	// then:
	require.NoError(t, err)
	require.Equal(t, testabilities.DefaultLookupServiceDocumentationProviderMockExpectations.Documentation, documentation)
	mock.AssertCalled()
}
