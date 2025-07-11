package app_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

func TestMetadataService_ValidCases(t *testing.T) {
	t.Run("Metadata service should return the metadata DTO from the topic managers metadata service", func(t *testing.T) {
		// given:
		provider1 := testabilities.NewTopicManagersListProviderMock(t, testabilities.TopicManagersListProviderMockExpectations{
			ListTopicManagersCall: true,
			Metadata: map[string]*overlay.MetaData{
				"topic_managers_metadata_service": {
					Name:        "name",
					Description: "desc",
					Icon:        "icon",
					Version:     "version",
					InfoUrl:     "info",
				},
			},
		})
		provider2 := testabilities.NewLookupListProviderMock(t, testabilities.LookupListProviderMockExpectations{
			ListLookupServiceProvidersCall: false,
		})

		expectedDTO := app.MetadataDTO{
			"topic_managers_metadata_service": app.ServiceMetadataDTO{
				Name:        "name",
				Description: "desc",
				IconURL:     "icon",
				Version:     "version",
				InfoURL:     "info",
			},
		}
		service := app.NewMetadataService(
			app.NewTopicManagersMetadataService(provider1),
			app.NewLookupListService(provider2),
		)

		// when:
		actualDTO, err := service.GetMetadata(app.TopicManagersServiceMetadataType)

		// then:
		require.Nil(t, err)
		require.Equal(t, expectedDTO, actualDTO)

		provider1.AssertCalled()
		provider2.AssertCalled()
	})

	t.Run("Metadata service should return the metadata DTO from the lookups metadata service", func(t *testing.T) {
		// given:
		provider1 := testabilities.NewLookupListProviderMock(t, testabilities.LookupListProviderMockExpectations{
			ListLookupServiceProvidersCall: true,
			Metadata: map[string]*overlay.MetaData{
				"lookups_metadata_service": {
					Name:        "name",
					Description: "desc",
					Icon:        "icon",
					Version:     "version",
					InfoUrl:     "info",
				},
			},
		})
		provider2 := testabilities.NewTopicManagersListProviderMock(t, testabilities.TopicManagersListProviderMockExpectations{
			ListTopicManagersCall: false,
		})

		expectedDTO := app.MetadataDTO{
			"lookups_metadata_service": app.ServiceMetadataDTO{
				Name:        "name",
				Description: "desc",
				IconURL:     "icon",
				Version:     "version",
				InfoURL:     "info",
			},
		}
		service := app.NewMetadataService(
			app.NewLookupListService(provider1),
			app.NewTopicManagersMetadataService(provider2),
		)

		// when:
		actualDTO, err := service.GetMetadata(app.LookupsMetadataServiceMetadataType)

		// then:
		require.Nil(t, err)
		require.Equal(t, expectedDTO, actualDTO)

		provider1.AssertCalled()
		provider2.AssertCalled()
	})
}

func TestMetadataService_InvalidCase(t *testing.T) {
	// given:
	provider1 := testabilities.NewTopicManagersListProviderMock(t, testabilities.TopicManagersListProviderMockExpectations{
		ListTopicManagersCall: false,
		Metadata: map[string]*overlay.MetaData{
			"topic_managers_metadata_service": {
				Name:        "name",
				Description: "desc",
				Icon:        "icon",
				Version:     "version",
				InfoUrl:     "info",
			},
		},
	})

	service := app.NewMetadataService(app.NewTopicManagersMetadataService(provider1))

	// when:
	actualDTO, err := service.GetMetadata(app.MetadataType{})

	// then:
	var actualErr app.Error
	require.ErrorAs(t, err, &actualErr)
	require.Equal(t, app.NewUnrecognizedMetadataType(app.MetadataType{}), actualErr)

	require.Nil(t, actualDTO)
	provider1.AssertCalled()
}
