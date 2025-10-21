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

var errStartGASPSyncHandlerTestError = errors.New("internal start GASP sync provider error during start GASP sync handler unit test")

func TestStartGASPSyncHandler_InvalidCase(t *testing.T) {
	// given:
	providerError := errStartGASPSyncHandlerTestError
	expectations := testabilities.StartGASPSyncProviderMockExpectations{
		StartGASPSyncCall: true,
		Error:             providerError,
	}

	const token = "22222222-2222-2222-2222-222222222222"
	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithStartGASPSyncProvider(testabilities.NewStartGASPSyncProviderMock(t, expectations)))
	fixture := server.NewTestFixture(t, server.WithEngine(stub), server.WithAdminBearerToken(token))
	expectedResponse := testabilities.NewTestOpenapiErrorResponse(t, app.NewStartGASPSyncProviderError(providerError))

	// when:
	var actualResponse openapi.Error
	res, _ := fixture.Client().
		R().
		SetHeader(fiber.HeaderAuthorization, "Bearer "+token).
		SetError(&actualResponse).
		Post("/api/v1/admin/startGASPSync")

	// then:
	require.Equal(t, fiber.StatusInternalServerError, res.StatusCode())
	require.Equal(t, expectedResponse, actualResponse)
	stub.AssertProvidersState()
}

func TestStartGASPSyncHandler_ValidCase(t *testing.T) {
	// given:
	const token = "22222222-2222-2222-2222-222222222222"
	expectations := testabilities.StartGASPSyncProviderMockExpectations{
		StartGASPSyncCall: true,
		Error:             nil,
	}

	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithStartGASPSyncProvider(testabilities.NewStartGASPSyncProviderMock(t, expectations)))
	fixture := server.NewTestFixture(t, server.WithEngine(stub), server.WithAdminBearerToken(token))

	// when:
	var actualResponse openapi.StartGASPSync
	res, _ := fixture.Client().
		R().
		SetHeader(fiber.HeaderAuthorization, "Bearer "+token).
		SetResult(&actualResponse).
		Post("/api/v1/admin/startGASPSync")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, ports.NewStartGASPSyncResponse(), actualResponse)
	stub.AssertProvidersState()
}
