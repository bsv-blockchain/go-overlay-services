package ports_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestMetadataHandler_ValidCases(t *testing.T) {
	tests := map[string]struct {
		endpoint               string
		expectedResponse       openapi.MetadataResponse
		expectedStatusCode     int
		expectedProviderToCall testabilities.TestOverlayEngineStubOption
	}{
		"Metadata handler should return metadata for configured topic managers": {
			endpoint: "/api/v1/listTopicManagers",
			expectedResponse: openapi.MetadataResponse{
				"topic_managers_metadata_service": {
					IconURL:          "topic_managers_icon_url",
					InformationURL:   "topic_managers_info_url",
					Name:             "topic_managers_name",
					ShortDescription: "topic_managers_short_desc",
					Version:          "topic_managers_version",
				},
			},
			expectedStatusCode: fiber.StatusOK,
			expectedProviderToCall: testabilities.WithTopicManagersListProvider(testabilities.NewTopicManagersListProviderMock(t, testabilities.TopicManagersListProviderMockExpectations{
				Metadata: map[string]*overlay.MetaData{
					"topic_managers_metadata_service": {
						Icon:        "topic_managers_icon_url",
						InfoUrl:     "topic_managers_info_url",
						Name:        "topic_managers_name",
						Description: "topic_managers_short_desc",
						Version:     "topic_managers_version",
					},
				},
				ListTopicManagersCall: true,
			})),
		},
		"Metadata handler should return metadata for configured lookup services": {
			endpoint: "/api/v1/listLookupServiceProviders",
			expectedResponse: openapi.MetadataResponse{
				"lookup_metadata_service": {
					IconURL:          "lookup_metadata_service_icon",
					InformationURL:   "lookup_metadata_service_info",
					Name:             "lookup_metadata_service_name",
					ShortDescription: "lookup_metadata_service_short_desc",
					Version:          "lookup_metadata_service_version",
				},
			},
			expectedStatusCode: fiber.StatusOK,
			expectedProviderToCall: testabilities.WithLookupListProvider(testabilities.NewLookupListProviderMock(t, testabilities.LookupListProviderMockExpectations{
				Metadata: map[string]*overlay.MetaData{
					"lookup_metadata_service": {
						Icon:        "lookup_metadata_service_icon",
						InfoUrl:     "lookup_metadata_service_info",
						Name:        "lookup_metadata_service_name",
						Description: "lookup_metadata_service_short_desc",
						Version:     "lookup_metadata_service_version",
					},
				},
				ListLookupServiceProvidersCall: true,
			})),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, tc.expectedProviderToCall)
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.MetadataResponse

			res, _ := fixture.Client().
				R().
				SetResult(&actualResponse).
				Execute(fiber.MethodGet, tc.endpoint)

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, tc.expectedResponse, actualResponse)
			stub.AssertProvidersState()
		})
	}
}
