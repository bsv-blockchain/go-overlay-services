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

// TopicManagerDocumentationProviderAlwaysFailure is an implementation that always returns an error
type TopicManagerDocumentationProviderAlwaysFailure struct{}

func (*TopicManagerDocumentationProviderAlwaysFailure) GetDocumentationForTopicManager(topicManager string) (string, error) {
	return "", errors.New("documentation not found")
}

// TopicManagerDocumentationProviderAlwaysSuccess extends NoopEngineProvider to return custom documentation
type TopicManagerDocumentationProviderAlwaysSuccess struct{}

func (*TopicManagerDocumentationProviderAlwaysSuccess) GetDocumentationForTopicManager(topicManager string) (string, error) {
	return "# Test Documentation\nThis is a test markdown document.", nil
}

func TestTopicManagerDocumentationHandler_Handle_SuccessfulRetrieval(t *testing.T) {
	// Given:
	handler, err := queries.NewTopicManagerDocumentationHandler(&TopicManagerDocumentationProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	res, err := ts.Client().Get(ts.URL + "?topicManager=example")

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "application/json", res.Header.Get("Content-Type"))

	var actual queries.TopicManagerDocumentationHandlerResponse
	expected := "# Test Documentation\nThis is a test markdown document."

	require.NoError(t, jsonutil.DecodeResponseBody(res, &actual))
	require.Equal(t, expected, actual.Documentation)
}

func TestTopicManagerDocumentationHandler_Handle_ProviderError(t *testing.T) {
	// Given:
	handler, err := queries.NewTopicManagerDocumentationHandler(&TopicManagerDocumentationProviderAlwaysFailure{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	res, err := ts.Client().Get(ts.URL + "?topicManager=example")

	// Then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestTopicManagerDocumentationHandler_Handle_EmptyTopicManagerParameter(t *testing.T) {
	// Given:
	// Create a handler with a custom provider that implements only the required interface
	handler, err := queries.NewTopicManagerDocumentationHandler(&TopicManagerDocumentationProviderAlwaysSuccess{})
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
	require.Equal(t, "topicManager query parameter is required\n", string(body))
}

func TestNewTopicManagerDocumentationHandler_WithNilProvider(t *testing.T) {
	// Given:
	var provider queries.TopicManagerDocumentationProvider = nil

	// When:
	handler, err := queries.NewTopicManagerDocumentationHandler(provider)
	require.Error(t, err)

	// Then:
	require.Nil(t, handler)
}
