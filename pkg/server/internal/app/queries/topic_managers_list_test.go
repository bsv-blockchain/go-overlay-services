package queries_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/queries"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

type TopicManagerListProviderAlwaysEmpty struct{}

func (*TopicManagerListProviderAlwaysEmpty) ListTopicManagers() map[string]*overlay.MetaData {
	return map[string]*overlay.MetaData{}
}

type TopicManagerListProviderAlwaysSuccess struct{}

func (*TopicManagerListProviderAlwaysSuccess) ListTopicManagers() map[string]*overlay.MetaData {
	return map[string]*overlay.MetaData{
		"manager1": {
			Description: "Description 1",
			Icon:        "https://example.com/icon.png",
			Version:     "1.0.0",
			InfoUrl:     "https://example.com/info",
			Name:        "Manager 1",
		},
		"manager2": {
			Description: "Description 2",
			Icon:        "https://example.com/icon2.png",
			Version:     "1.0.0",
			InfoUrl:     "https://example.com/info",
			Name:        "Manager 2",
		},
	}
}

func TestTopicManagersListHandler_Handle_EmptyList(t *testing.T) {
	// given:
	handler, err := queries.NewTopicManagersListHandler(&TopicManagerListProviderAlwaysEmpty{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// when:
	res, err := ts.Client().Get(ts.URL)

	// then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "application/json", res.Header.Get("Content-Type"))

	var actual queries.TopicManagersListHandlerResponse
	require.NoError(t, jsonutil.DecodeResponseBody(res, &actual))
	require.Empty(t, actual)
}

func TestTopicManagersListHandler_Handle_WithManagers(t *testing.T) {
	// given:
	handler, err := queries.NewTopicManagersListHandler(&TopicManagerListProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// when:
	res, err := ts.Client().Get(ts.URL)

	// then:
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "application/json", res.Header.Get("Content-Type"))

	var actual queries.TopicManagersListHandlerResponse
	require.NoError(t, jsonutil.DecodeResponseBody(res, &actual))

	expected := queries.TopicManagersListHandlerResponse{
		"manager1": queries.TopicManagerMetadata{
			Name:           "manager1",
			Description:    "Description 1",
			IconURL:        ptr.To("https://example.com/icon.png"),
			Version:        ptr.To("1.0.0"),
			InformationURL: ptr.To("https://example.com/info"),
		},
		"manager2": queries.TopicManagerMetadata{
			Name:           "manager2",
			Description:    "Description 2",
			IconURL:        ptr.To("https://example.com/icon2.png"),
			Version:        ptr.To("1.0.0"),
			InformationURL: ptr.To("https://example.com/info"),
		},
	}

	require.EqualValues(t, expected, actual)
}

func TestNewTopicManagersListHandler_WithNilProvider(t *testing.T) {
	// when:
	handler, err := queries.NewTopicManagersListHandler(nil)
	require.Error(t, err)

	// then:
	require.Nil(t, handler)
}
