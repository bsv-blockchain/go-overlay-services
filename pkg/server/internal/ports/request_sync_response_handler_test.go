package ports_test

import (
	"math"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
)

func TestRequestSyncResponseHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		payload            any
		headers            map[string]string
		expectations       testabilities.RequestSyncResponseProviderMockExpectations
		expectedStatusCode int
		expectedResponse   openapi.Error
	}{
		"Request sync response handler fails due to missing topic header": {
			payload: testabilities.NewDefaultRequestSyncResponseBody(),
			headers: map[string]string{
				"Content-Type": "application/json",
			},
			expectedStatusCode: fiber.StatusBadRequest,
			expectations: testabilities.RequestSyncResponseProviderMockExpectations{
				ProvideForeignSyncResponseCall: false,
			},
			expectedResponse: openapi.Error{Message: "The submitted request does not include required header: X-BSV-Topic."},
		},
		"Request sync response handler fails due to invalid JSON": {
			payload: "INVALID_JSON",
			headers: map[string]string{
				"Content-Type": "application/json",
				"X-BSV-Topic":  testabilities.DefaultTopic,
			},
			expectedStatusCode: fiber.StatusInternalServerError,
			expectations: testabilities.RequestSyncResponseProviderMockExpectations{
				ProvideForeignSyncResponseCall: false,
			},
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t, ports.NewRequestBodyParserError(testabilities.ErrTestNoopOpFailure)),
		},
		"Request sync response handler fails due to provider error": {
			payload: testabilities.NewDefaultRequestSyncResponseBody(),
			headers: map[string]string{
				"Content-Type": "application/json",
				"X-BSV-Topic":  testabilities.DefaultTopic,
			},
			expectations: testabilities.RequestSyncResponseProviderMockExpectations{
				Error:                          testabilities.ErrTestNoopOpFailure,
				ProvideForeignSyncResponseCall: true,
				InitialRequest: &gasp.InitialRequest{
					Version: testabilities.DefaultVersion,
					Since:   testabilities.DefaultSince,
					Limit:   0,
				},
				Topic: testabilities.DefaultTopic,
			},
			expectedStatusCode: fiber.StatusInternalServerError,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, app.NewRequestSyncResponseProviderError(testabilities.ErrTestNoopOpFailure)),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithRequestSyncResponseProvider(
				testabilities.NewRequestSyncResponseProviderMock(t, tc.expectations),
			))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.Error

			res, _ := fixture.Client().
				R().
				SetHeaders(tc.headers).
				SetBody(tc.payload).
				SetError(&actualResponse).
				Post("/api/v1/requestSyncResponse")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, &tc.expectedResponse, &actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func TestRequestSyncResponseHandler_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.RequestSyncResponseProviderMockExpectations{
		ProvideForeignSyncResponseCall: true,
		InitialRequest: &gasp.InitialRequest{
			Version: testabilities.DefaultVersion,
			Since:   testabilities.DefaultSince,
			Limit:   0,
		},
		Topic: testabilities.DefaultTopic,
		Response: &gasp.InitialResponse{
			Since: testabilities.DefaultSince,
			UTXOList: []*gasp.Output{
				{
					Txid:        *testabilities.DummyTxHash(t, "03895fb984362a4196bc9931629318fcbb2aeba7c6293638119ea653fa31d119"),
					OutputIndex: 0,
					Score:       0,
				},
				{
					Txid:        *testabilities.DummyTxHash(t, "27c8f37851aabc468d3dbb6bf0789dc398a602dcb897ca04e7815d939d621595"),
					OutputIndex: 1,
					Score:       0,
				},
				{
					Txid:        *testabilities.DummyTxHash(t, "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"),
					OutputIndex: 2,
					Score:       0,
				},
			},
		},
	}

	expectedDTO := app.NewRequestSyncResponseDTO(expectations.Response)
	expectedResponse, err := ports.NewRequestSyncResponseSuccessResponse(expectedDTO)
	require.NoError(t, err)
	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithRequestSyncResponseProvider(testabilities.NewRequestSyncResponseProviderMock(t, expectations)))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	headers := map[string]string{
		"X-BSV-Topic":  testabilities.DefaultTopic,
		"Content-Type": fiber.MIMEApplicationJSON,
	}

	// when:
	var actualResponse openapi.RequestSyncResResponse

	res, _ := fixture.Client().
		R().
		SetHeaders(headers).
		SetBody(testabilities.NewDefaultRequestSyncResponseBody()).
		SetResult(&actualResponse).
		Post("/api/v1/requestSyncResponse")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, expectedResponse, &actualResponse)
	stub.AssertProvidersState()
}

func TestNewRequestSyncResponseSuccessResponse_OutputIndexBounds(t *testing.T) {
	// An int is 32 bits wide on some targets, so the largest wire index only converts
	// where the platform int can hold it.
	maxIndexFits := uint64(math.MaxUint32) <= uint64(math.MaxInt)

	tests := map[string]struct {
		outputIndex uint32
		expectError bool
	}{
		"zero index converts":                  {outputIndex: 0},
		"largest signed 32-bit index converts": {outputIndex: math.MaxInt32},
		"largest wire index converts only when the platform int can hold it": {
			outputIndex: math.MaxUint32,
			expectError: !maxIndexFits,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			dto := &app.RequestSyncResponseDTO{
				UTXOList: []app.OutpointDTO{{TxID: "03895fb984362a4196bc9931629318fcbb2aeba7c6293638119ea653fa31d119", OutputIndex: tc.outputIndex}},
			}

			// when:
			response, err := ports.NewRequestSyncResponseSuccessResponse(dto)

			// then:
			if tc.expectError {
				require.Error(t, err)
				require.Nil(t, response)
				return
			}
			require.NoError(t, err)
			require.Len(t, response.UTXOList, 1)
			require.EqualValues(t, tc.outputIndex, response.UTXOList[0].OutputIndex)
		})
	}
}
