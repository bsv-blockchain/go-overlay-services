package app_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

func TestTopicManagersMetadataService_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewTopicManagersListProviderMock(t, testabilities.TopicManagersListProviderMockExpectations{
		ListTopicManagersCall: true,
		Metadata: map[string]*overlay.MetaData{
			"service1": {
				Name:        "name",
				Description: "desc",
				Icon:        "icon",
				Version:     "version",
				InfoUrl:     "infoURL",
			},
		},
	})
	expectedDTO := app.MetadataDTO{
		"service1": app.ServiceMetadataDTO{
			Name:        "name",
			Description: "desc",
			IconURL:     "icon",
			Version:     "version",
			InfoURL:     "infoURL",
		},
	}

	service := app.NewTopicManagersMetadataService(mock)

	// when:
	actualDTO := service.GetMetadata()

	// then:
	require.Equal(t, expectedDTO, actualDTO)
	mock.AssertCalled()
}
