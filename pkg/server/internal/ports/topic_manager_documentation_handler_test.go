package ports_test

import (
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

var errTopicManagerDocHandlerTestError = errors.New("test error")

func TestTopicManagerDocumentationHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedStatusCode int
		queryParams        map[string]string
		expectedResponse   openapi.Error
		expectations       testabilities.TopicManagerDocumentationProviderMockExpectations
	}{
		"Topic manager documentation service fails to handle request - empty topic manager name": {
			expectedStatusCode: fiber.StatusBadRequest,
			queryParams:        map[string]string{"topicManager": ""},
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, app.NewEmptyTopicManagerNameError()),
			expectations: testabilities.TopicManagerDocumentationProviderMockExpectations{
				DocumentationCall: false,
			},
		},
		"Topic manager documentation service fails to handle request - internal error": {
			expectedStatusCode: fiber.StatusInternalServerError,
			queryParams:        map[string]string{"topicManager": "testProvider"},
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, app.NewTopicManagerDocumentationProviderError(errTopicManagerDocHandlerTestError)),
			expectations: testabilities.TopicManagerDocumentationProviderMockExpectations{
				DocumentationCall: true,
				Error:             app.NewTopicManagerDocumentationProviderError(errTopicManagerDocHandlerTestError),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithTopicManagerDocumentationProvider(testabilities.NewTopicManagerDocumentationProviderMock(t, tc.expectations)))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.BadRequestResponse

			res, _ := fixture.Client().
				R().
				SetQueryParams(tc.queryParams).
				SetError(&actualResponse).
				Get("/api/v1/getDocumentationForTopicManager")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, &tc.expectedResponse, &actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func TestTopicManagerDocumentationHandler_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewTopicManagerDocumentationProviderMock(t, testabilities.TopicManagerDocumentationProviderMockExpectations{
		DocumentationCall: true,
		Documentation:     testabilities.DefaultTopicManagerDocumentationProviderMockExpectations.Documentation,
	})
	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithTopicManagerDocumentationProvider(mock))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	// when:
	var actualResponse openapi.TopicManagerDocumentationResponse
	res, _ := fixture.Client().
		R().
		SetResult(&actualResponse).
		Get("/api/v1/getDocumentationForTopicManager?topicManager=testProvider")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, testabilities.DefaultTopicManagerDocumentationProviderMockExpectations.Documentation, actualResponse.Documentation)
	mock.AssertCalled()
}
