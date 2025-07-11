package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/stretchr/testify/require"
)

func TestAdvertisementsSyncService_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewSyncAdvertisementsProviderMock(t, testabilities.SyncAdvertisementsProviderMockExpectations{
		SyncAdvertisementsCall: true,
		Err:                    nil,
	})
	service := app.NewAdvertisementsSyncService(mock)

	// when:
	err := service.SyncAdvertisements(context.Background())

	// then:
	require.NoError(t, err)
	mock.AssertCalled()
}

func TestAdvertisementsSyncService_InvalidCase(t *testing.T) {
	// given:
	expectations := testabilities.SyncAdvertisementsProviderMockExpectations{
		SyncAdvertisementsCall: true,
		Err:                    errors.New("internal test error"),
	}
	mock := testabilities.NewSyncAdvertisementsProviderMock(t, expectations)
	service := app.NewAdvertisementsSyncService(mock)
	expectedErr := app.NewSyncAdvertisementsProviderError(expectations.Err)

	// when:
	err := service.SyncAdvertisements(context.Background())

	// then:
	var actualErr app.Error
	require.ErrorAs(t, err, &actualErr)
	require.Equal(t, expectedErr, actualErr)

	mock.AssertCalled()
}
