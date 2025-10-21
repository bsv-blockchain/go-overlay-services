package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/stretchr/testify/require"
)

var errStartGASPSyncTestError = errors.New("internal start GASP sync service test error")

func TestStartGASPSyncService_InvalidCase(t *testing.T) {
	// given:
	providerError := errStartGASPSyncTestError
	expectations := testabilities.StartGASPSyncProviderMockExpectations{
		StartGASPSyncCall: true,
		Error:             providerError,
	}
	expectedErr := app.NewStartGASPSyncProviderError(providerError)
	mock := testabilities.NewStartGASPSyncProviderMock(t, expectations)
	service := app.NewStartGASPSyncService(mock)

	// when:
	err := service.StartGASPSync(context.Background())

	// then:
	var actualErr app.Error
	require.ErrorAs(t, err, &actualErr)
	require.Equal(t, expectedErr, actualErr)
	mock.AssertCalled()
}

func TestStartGASPSyncService_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.StartGASPSyncProviderMockExpectations{
		StartGASPSyncCall: true,
		Error:             nil,
	}

	mock := testabilities.NewStartGASPSyncProviderMock(t, expectations)
	service := app.NewStartGASPSyncService(mock)

	// when:
	err := service.StartGASPSync(context.Background())

	// then:
	require.NoError(t, err)
	mock.AssertCalled()
}
