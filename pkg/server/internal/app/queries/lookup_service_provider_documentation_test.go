package queries_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/queries"
	"github.com/stretchr/testify/require"
)

type LookupDocumentationProviderAlwaysFailure struct{}

func (*LookupDocumentationProviderAlwaysFailure) GetDocumentationForLookupServiceProvider(lookupService string) (string, error) {
	return "", errors.New("documentation not found")
}

type LookupDocumentationProviderAlwaysSuccess struct{}

func (*LookupDocumentationProviderAlwaysSuccess) GetDocumentationForLookupServiceProvider(lookupService string) (string, error) {
	return "# Test Documentation\nThis is a test markdown document.", nil
}

func TestLookupServiceDocumentationHandler_Handle_SuccessfulRetrieval(t *testing.T) {
	// Given:
	handler, err := queries.NewLookupServiceDocumentationHandler(&LookupDocumentationProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	res, err := ts.Client().Get(ts.URL + "?lookupService=example")

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "application/json", res.Header.Get("Content-Type"))

	var actual queries.LookupServiceDocumentationHandlerResponse
	const expected = "# Test Documentation\nThis is a test markdown document."

	require.NoError(t, jsonutil.DecodeResponseBody(res, &actual))
	require.Equal(t, expected, actual.Documentation)
}

func TestLookupDocumentationHandler_Handle_ProviderError(t *testing.T) {
	// Given:
	handler, err := queries.NewLookupServiceDocumentationHandler(&LookupDocumentationProviderAlwaysFailure{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	res, err := ts.Client().Get(ts.URL + "?lookupService=example")

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestLookupDocumentationHandler_Handle_EmptyLookupServiceParameter(t *testing.T) {
	// Given:
	handler, err := queries.NewLookupServiceDocumentationHandler(&LookupDocumentationProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	res, err := ts.Client().Get(ts.URL)

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	require.Equal(t, "text/plain; charset=utf-8", res.Header.Get("Content-Type"))

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, "lookupService query parameter is required\n", string(body))
}

func TestNewLookupServiceDocumentationHandler_WithNilProvider(t *testing.T) {
	// Given:
	// When:
	handler, err := queries.NewLookupServiceDocumentationHandler(nil)
	require.Error(t, err)

	// Then:
	require.Nil(t, handler)
}
