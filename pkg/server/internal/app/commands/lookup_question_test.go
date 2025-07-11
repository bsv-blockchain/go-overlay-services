package commands_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands/testutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/stretchr/testify/require"
)

func TestLookupQuestionHandler_Handle_ShouldReturnOKStatusAfterProcessingRequest(t *testing.T) {
	// given:
	stub := &testutil.LookupQuestionProviderAlwaysSucceeds{
		ExpectedLookupAnswer: &lookup.LookupAnswer{
			Type:   lookup.AnswerTypeFreeform,
			Result: map[string]any{"data": "test data"},
		},
	}

	handler, err := commands.NewLookupQuestionHandler(stub)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	jsonData, err := json.Marshal(lookup.LookupQuestion{
		Service: "test-service",
		Query:   json.RawMessage(`{"test":"query"}`),
	})
	require.NoError(t, err)

	// when:
	resp, err := ts.Client().Post(ts.URL, "application/json", bytes.NewBuffer(jsonData))

	// then:
	require.NoError(t, err)
	require.NotNil(t, resp)

	defer resp.Body.Close()
	var actual commands.LookupQuestionHandlerResponse
	expected := commands.LookupQuestionHandlerResponse{LookupAnswer: stub.ExpectedLookupAnswer}

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, jsonutil.DecodeResponseBody(resp, &actual))
	require.Equal(t, expected, actual)
}

func TestLookupQuestionHandler_Handle_ShouldReturnErrorResponseForInvalidRequest(t *testing.T) {
	tests := []struct {
		name           string
		body           io.Reader
		stub           commands.LookupQuestionProvider
		expectedStatus int
		expectedErr    error
	}{
		{
			name:           "should return HTTP status code 400 when handling an invalid request body",
			expectedErr:    commands.ErrInvalidRequestBody,
			expectedStatus: http.StatusBadRequest,
			body:           testutil.RequestBody(t, "{}"),
			stub:           &testutil.LookupQuestionProviderAlwaysSucceeds{ExpectedLookupAnswer: &lookup.LookupAnswer{}},
		},
		{
			name:           "should return HTTP status code 400 when handling an empty request body",
			expectedErr:    commands.ErrInvalidRequestBody,
			expectedStatus: http.StatusBadRequest,
			body:           testutil.RequestBody(t, ""),
			stub:           &testutil.LookupQuestionProviderAlwaysSucceeds{ExpectedLookupAnswer: &lookup.LookupAnswer{}},
		},
		{
			name:           "should return HTTP status code 400 when handling a request body with a missing service field.",
			body:           testutil.RequestBody(t, commands.LookupQuestionHandlerRequest{Query: json.RawMessage(`{"field":"value"}`)}),
			stub:           &testutil.LookupQuestionProviderAlwaysSucceeds{ExpectedLookupAnswer: &lookup.LookupAnswer{}},
			expectedStatus: http.StatusBadRequest,
			expectedErr:    commands.ErrMissingServiceField,
		},
		{
			name: "should return HTTP status code 500 when the lookup question provider fails",
			body: testutil.RequestBody(t, commands.LookupQuestionHandlerRequest{
				Service: "example",
				Query:   json.RawMessage(`{"field":"value"}`),
			}),
			stub:           &testutil.LookupQuestionProviderAlwaysFails{ExpectedErr: errors.New("failure")},
			expectedStatus: http.StatusInternalServerError,
			expectedErr:    errors.New("failure"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// given:
			handler, err := commands.NewLookupQuestionHandler(tc.stub)
			require.NoError(t, err)

			ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
			defer ts.Close()

			// when:
			res, err := ts.Client().Post(ts.URL, "application/json", tc.body)

			// then:
			require.NoError(t, err)
			require.NotNil(t, res)

			require.Equal(t, tc.expectedStatus, res.StatusCode)
			actualErr := testutil.ParseToError(t, res.Body)
			require.Equal(t, tc.expectedErr, actualErr)
		})
	}
}
