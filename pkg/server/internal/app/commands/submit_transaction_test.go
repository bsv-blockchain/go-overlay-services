package commands_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands/testutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitTransactionHandler_Handle_SuccessfulSubmission(t *testing.T) {
	// Given:
	mock := testutil.NewSubmitTransactionProviderAlwaysSuccess(overlay.Steak{
		"test": &overlay.AdmittanceInstructions{
			OutputsToAdmit: []uint32{1},
		},
	})

	handler, err := commands.NewSubmitTransactionCommandHandler(mock)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	requestBody := []byte("test transaction body")

	// Using comma-separated topics
	topics := "topic1,topic2"

	req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBuffer(requestBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(commands.XTopicsHeader, topics)

	// When:
	res, err := ts.Client().Do(req)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var actual commands.SubmitTransactionHandlerResponse
	require.NoError(t, jsonutil.DecodeResponseBody(res, &actual))
	require.Equal(t, mock.ExpectedSteak, actual.Steak)
}

func TestSubmitTransactionHandler_Handle_InvalidMethod(t *testing.T) {
	// Given:
	mock := testutil.NewSubmitTransactionProviderAlwaysSuccess(overlay.Steak{
		"test": &overlay.AdmittanceInstructions{
			OutputsToAdmit: []uint32{1},
		},
	})

	handler, err := commands.NewSubmitTransactionCommandHandler(mock)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// When:
	res, err := ts.Client().Do(req)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, commands.ErrInvalidHTTPMethod.Error()+"\n", string(body))
}

func TestSubmitTransactionHandler_Handle_MissingTopicsHeader(t *testing.T) {
	// Given:
	mock := testutil.NewSubmitTransactionProviderAlwaysSuccess(overlay.Steak{
		"test": &overlay.AdmittanceInstructions{
			OutputsToAdmit: []uint32{1},
		},
	})

	handler, err := commands.NewSubmitTransactionCommandHandler(mock)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader("test body"))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	// When:
	res, err := ts.Client().Do(req)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	require.Equal(t, commands.ErrMissingXTopicsHeader.Error()+"\n", string(body))
}

func TestSubmitTransactionHandler_Handle_InvalidTopicsFormat(t *testing.T) {
	// Given:
	mock := testutil.NewSubmitTransactionProviderAlwaysSuccess(overlay.Steak{
		"test": &overlay.AdmittanceInstructions{
			OutputsToAdmit: []uint32{1},
		},
	})

	handler, err := commands.NewSubmitTransactionCommandHandler(mock)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader("test body"))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	// Empty topic results in invalid format
	req.Header.Set(commands.XTopicsHeader, "  ,  ,")

	// When:
	res, err := ts.Client().Do(req)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusBadRequest, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, commands.ErrInvalidXTopicsHeaderFormat.Error()+"\n", string(body))
}

func TestSubmitTransactionHandler_Handle_ProviderError(t *testing.T) {
	// Given:
	mock := testutil.NewSubmitTransactionProviderAlwaysFailure(errors.New("internal"))
	handler, err := commands.NewSubmitTransactionCommandHandler(mock)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	requestBody := []byte("test transaction body")
	topics := "topic1,topic2"

	req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBuffer(requestBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(commands.XTopicsHeader, topics)

	// When:
	res, err := ts.Client().Do(req)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()

	var actual string
	require.NoError(t, jsonutil.DecodeResponseBody(res, &actual))
	actualErr := errors.New(actual)

	require.Equal(t, http.StatusInternalServerError, res.StatusCode)
	require.Equal(t, mock.ExpectedErr, actualErr)
}

func TestSubmitTransactionHandler_Handle_RequestTooLarge(t *testing.T) {
	tests := []struct {
		name string

		requestBodyLimit       int64
		requestBody            string
		expectedHTTPStatusCode int
		expectedErr            error
	}{
		{
			name:                   "request with body size greater than server limit",
			requestBodyLimit:       10,
			requestBody:            "abcdefghijklmnoprst",
			expectedHTTPStatusCode: http.StatusRequestEntityTooLarge,
			expectedErr:            commands.ErrRequestBodyTooLarge,
		},
		{
			name:                   "request with body size less than server limit",
			requestBodyLimit:       10,
			requestBody:            "abcdef",
			expectedHTTPStatusCode: http.StatusOK,
		},
		{
			name:                   "request with body size equal than server limit",
			requestBodyLimit:       4,
			requestBody:            "abcd",
			expectedHTTPStatusCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// given:
			mock := testutil.NewSubmitTransactionProviderAlwaysSuccess(overlay.Steak{
				"test": &overlay.AdmittanceInstructions{
					OutputsToAdmit: []uint32{1},
				},
			})

			opts := []commands.SubmitTransactionHandlerOption{
				commands.WithRequestBodyLimit(tc.requestBodyLimit),
				commands.WithResponseTime(1 * time.Second),
			}

			handler, err := commands.NewSubmitTransactionCommandHandler(mock, opts...)
			require.NoError(t, err)
			require.NotNil(t, handler)

			ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
			defer ts.Close()

			req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(tc.requestBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(commands.XTopicsHeader, "topic1")

			// when:
			res, err := ts.Client().Do(req)

			// then:
			require.NoError(t, err)
			defer res.Body.Close()

			require.Equal(t, tc.expectedHTTPStatusCode, res.StatusCode)

			if res.StatusCode != http.StatusOK {
				bb, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				require.NotEmpty(t, bb)
				require.Equal(t, tc.expectedErr, errors.New(string(bb[:len(bb)-1])))
			}
		})
	}
}

func TestSubmitTransactionHandler_Handle_Timeout(t *testing.T) {
	// Given:
	handler, err := commands.NewSubmitTransactionCommandHandler(
		&testutil.SubmitTransactionProviderNeverCallback{},
		commands.WithResponseTime(2*time.Second),
	)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	requestBody := []byte("test transaction body")
	topics := "topic1,topic2"

	req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBuffer(requestBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(commands.XTopicsHeader, topics)

	// When:
	res, err := ts.Client().Do(req)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusRequestTimeout, res.StatusCode)
}

func TestNewSubmitTransactionCommandHandler_WithNilProvider(t *testing.T) {
	// When:
	handler, err := commands.NewSubmitTransactionCommandHandler(nil)

	// Then:
	assert.Nil(t, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "submit transaction provider is nil")
}
