package ports_test

import (
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

var errSubmitTxHandlerTestError = errors.New("internal submit transaction provider error during submit transaction handler unit test")

func TestSubmitTransactionHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedStatusCode int
		headers            map[string]string
		body               string
		expectedResponse   openapi.Error
		expectations       testabilities.SubmitTransactionProviderMockExpectations
	}{
		"Submit transaction service fails to handle the transaction submission request - internal error": {
			expectedStatusCode: fiber.StatusInternalServerError,
			body:               "test transaction body",
			headers: map[string]string{
				fiber.HeaderContentType: fiber.MIMEOctetStream,
				ports.XTopicsHeader:     "topics1,topics2",
			},
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t,
				app.NewSubmitTransactionProviderError(
					errSubmitTxHandlerTestError,
				),
			),
			expectations: testabilities.SubmitTransactionProviderMockExpectations{
				Error:      errSubmitTxHandlerTestError,
				SubmitCall: true,
			},
		},
		"Missing x-topics header in the HTTP request": {
			expectedStatusCode: fiber.StatusBadRequest,
			body:               "test transaction body",
			headers: map[string]string{
				fiber.HeaderContentType: fiber.MIMEOctetStream,
			},
			expectedResponse: openapi.Error{
				Message: "The submitted request does not include required header: x-topics.",
			},
			expectations: testabilities.SubmitTransactionProviderMockExpectations{
				SubmitCall: false,
			},
		},
		"Empty topics in the x-topics header in the HTTP request": {
			expectedStatusCode: fiber.StatusBadRequest,
			body:               "test transaction body",
			headers: map[string]string{
				fiber.HeaderContentType: fiber.MIMEOctetStream,
				ports.XTopicsHeader:     "",
			},
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t, app.NewErrInvalidTopicFormatError(0)),
			expectations: testabilities.SubmitTransactionProviderMockExpectations{
				SubmitCall: false,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithSubmitTransactionProvider(testabilities.NewSubmitTransactionProviderMock(t, tc.expectations)))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.BadRequestResponse

			res, _ := fixture.Client().
				R().
				SetHeaders(tc.headers).
				SetBody(tc.body).
				SetError(&actualResponse).
				Post("/api/v1/submit")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, &tc.expectedResponse, &actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func TestSubmitTransactionHandler_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.SubmitTransactionProviderMockExpectations{
		SubmitCall: true,
		STEAK: &overlay.Steak{
			"test": &overlay.AdmittanceInstructions{
				OutputsToAdmit: []uint32{1},
			},
		},
	}

	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithSubmitTransactionProvider(testabilities.NewSubmitTransactionProviderMock(t, expectations)))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	headers := map[string]string{
		fiber.HeaderContentType: fiber.MIMEOctetStream,
		ports.XTopicsHeader:     "topic1,topic2",
	}

	// when:
	var actualResponse openapi.SubmitTransactionResponse

	res, _ := fixture.Client().
		R().
		SetHeaders(headers).
		SetBody("test transaction body").
		SetResult(&actualResponse).
		Post("/api/v1/submit")

	// then:
	expectedResponse := ports.NewSubmitTransactionSuccessResponse(expectations.STEAK)

	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, expectedResponse, &actualResponse)
	stub.AssertProvidersState()
}
