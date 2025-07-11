package commands_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands/testutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/stretchr/testify/require"
)

func Test_ArcIngestHandler_ShouldRespondsWith200AndCallsProvider(t *testing.T) {
	// given:
	payload := commands.ArcIngestRequest{
		TxID:        testutil.ValidTxId,
		MerklePath:  testutil.NewValidTestMerklePath(t),
		BlockHeight: 848372,
	}

	mock := testutil.NewMerkleProofProviderMock(nil, payload.BlockHeight)
	handler, err := commands.NewArcIngestHandler(mock)

	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL, testutil.RequestBody(t, payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// when:
	res, err := ts.Client().Do(req)

	// then:
	require.NoError(t, err)
	defer res.Body.Close()

	require.NotNil(t, res)
	require.Equal(t, http.StatusOK, res.StatusCode)

	var actualResponse commands.ArcIngestHandlerResponse
	require.NoError(t, jsonutil.DecodeResponseBody(res, &actualResponse))

	expectedResponse := commands.NewSuccessArcIngestHandlerResponse()
	require.Equal(t, expectedResponse, actualResponse)

	mock.AssertCalled(t)
}

func Test_ArcIngestHandler_ValidationTests(t *testing.T) {
	tests := map[string]struct {
		method             string
		payload            commands.ArcIngestRequest
		setupRequest       func(*http.Request)
		mockError          error
		expectedResponse   commands.ArcIngestHandlerResponse
		expectedHTTPStatus int
	}{
		"should fail with 405 when HTTP method is GET": {
			method: http.MethodGet,
			payload: commands.ArcIngestRequest{
				TxID:        testutil.ValidTxId,
				MerklePath:  testutil.NewValidTestMerklePath(t),
				BlockHeight: 848372,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrInvalidHTTPMethod.Error()),
			expectedHTTPStatus: http.StatusMethodNotAllowed,
		},
		"should fail with 400 when all required fields are missing": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        "",
				MerklePath:  "",
				BlockHeight: 0,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrMissingRequiredRequestFieldsDefinition.Error()),
			expectedHTTPStatus: http.StatusBadRequest,
		},
		"should fail with 400 when TxID field is missing": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        "",
				MerklePath:  testutil.NewValidTestMerklePath(t),
				BlockHeight: 848372,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrMissingRequiredTxIDFieldDefinition.Error()),
			expectedHTTPStatus: http.StatusBadRequest,
		},
		"should fail with 400 when MerklePath field is missing": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        testutil.ValidTxId,
				MerklePath:  "",
				BlockHeight: 848372,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrMissingRequiredMerklePathFieldDefinition.Error()),
			expectedHTTPStatus: http.StatusBadRequest,
		},
		"should fail with 400 when TxID format is invalid": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        "invalid-hex-string",
				MerklePath:  testutil.NewValidTestMerklePath(t),
				BlockHeight: 848372,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrInvalidTxIDFormat.Error()),
			expectedHTTPStatus: http.StatusBadRequest,
		},
		"should fail with 400 when TxID length is invalid": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        "1234",
				MerklePath:  testutil.NewValidTestMerklePath(t),
				BlockHeight: 848372,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrInvalidTxIDLength.Error()),
			expectedHTTPStatus: http.StatusBadRequest,
		},
		"should fail with 400 when MerklePath format is invalid": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        testutil.ValidTxId,
				MerklePath:  "invalid-merkle-path",
				BlockHeight: 848372,
			},
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrInvalidMerklePathFormat.Error()),
			expectedHTTPStatus: http.StatusBadRequest,
		},
		"should fail with 504 when context deadline exceeded": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        testutil.ValidTxId,
				MerklePath:  testutil.NewValidTestMerklePath(t),
				BlockHeight: 848372,
			},
			mockError:          context.DeadlineExceeded,
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrMerkleProofProcessingTimeout.Error()),
			expectedHTTPStatus: http.StatusGatewayTimeout,
		},
		"should fail with 408 when context canceled": {
			method: http.MethodPost,
			payload: commands.ArcIngestRequest{
				TxID:        testutil.ValidTxId,
				MerklePath:  testutil.NewValidTestMerklePath(t),
				BlockHeight: 848372,
			},
			mockError:          context.Canceled,
			expectedResponse:   commands.NewFailureArcIngestHandlerResponse(commands.ErrMerkleProofProcessingCanceled.Error()),
			expectedHTTPStatus: http.StatusRequestTimeout,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testutil.NewMerkleProofProviderMock(tc.mockError, tc.payload.BlockHeight)
			handler, err := commands.NewArcIngestHandler(mock)
			require.NoError(t, err)

			ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
			defer ts.Close()

			req, err := http.NewRequest(tc.method, ts.URL, testutil.RequestBody(t, tc.payload))
			require.NoError(t, err)

			// when:
			res, err := ts.Client().Do(req)
			require.NoError(t, err)
			defer res.Body.Close()

			// then:
			require.Equal(t, tc.expectedHTTPStatus, res.StatusCode)

			var actualResponse commands.ArcIngestHandlerResponse
			err = jsonutil.DecodeResponseBody(res, &actualResponse)
			require.NoError(t, err)

			require.Equal(t, tc.expectedResponse, actualResponse)
		})
	}
}
