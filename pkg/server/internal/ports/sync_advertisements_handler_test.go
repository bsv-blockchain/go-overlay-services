package ports_test

import (
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

var errSyncAdvertisementsHandlerTestError = errors.New("internal SyncAdvertisements service test error")

func TestSyncAdvertisementsHandler_InvalidCase(t *testing.T) {
	// given:
	const token = "22222222-2222-2222-2222-222222222222"
	providerInternalErr := errSyncAdvertisementsHandlerTestError
	expectedResponse := testabilities.NewTestOpenapiErrorResponse(t, app.NewSyncAdvertisementsProviderError(providerInternalErr))
	stub := testabilities.NewTestOverlayEngineStub(t,
		testabilities.WithSyncAdvertisementsProvider(
			testabilities.NewSyncAdvertisementsProviderMock(t, testabilities.SyncAdvertisementsProviderMockExpectations{
				Err:                    providerInternalErr,
				SyncAdvertisementsCall: true,
			}),
		),
	)
	fixture := server.NewTestFixture(t, server.WithEngine(stub), server.WithAdminBearerToken(token))

	// when:
	var actualResponse openapi.Error

	res, _ := fixture.Client().
		R().
		SetHeader(fiber.HeaderAuthorization, "Bearer "+token).
		SetError(&actualResponse).
		Post("api/v1/admin/syncAdvertisements")

	// then:

	require.Equal(t, fiber.StatusInternalServerError, res.StatusCode())
	require.Equal(t, expectedResponse, actualResponse)
	stub.AssertProvidersState()
}

func TestSyncAdvertisementsHandler_ValidCase(t *testing.T) {
	// given:
	const token = "22222222-2222-2222-2222-222222222222"

	stub := testabilities.NewTestOverlayEngineStub(t,
		testabilities.WithSyncAdvertisementsProvider(testabilities.NewSyncAdvertisementsProviderMock(t,
			testabilities.SyncAdvertisementsProviderMockExpectations{
				SyncAdvertisementsCall: true,
			}),
		),
	)
	fixture := server.NewTestFixture(t, server.WithEngine(stub), server.WithAdminBearerToken(token))

	// when:
	var actualResponse openapi.AdvertisementsSyncResponse

	res, _ := fixture.Client().
		R().
		SetHeader(fiber.HeaderAuthorization, "Bearer "+token).
		SetResult(&actualResponse).
		Post("api/v1/admin/syncAdvertisements")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, ports.NewSyncAdvertisementsSuccessResponse(), actualResponse)
	stub.AssertProvidersState()
}
