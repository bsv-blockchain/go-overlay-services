package commands_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/stretchr/testify/assert"
)

// This is an example describing how the handler unit tests
// should be structured and tested based on the HTTP standard package.
func TestSyncAdvertisementsHandler_Handle(t *testing.T) {
	// given:
	app, err := commands.NewSyncAdvertisementsCommandHandler(syncAdvertisementsProviderAlwaysOK{})
	assert.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(app.Handle))
	defer ts.Close()

	// when:
	res, err := ts.Client().Post(ts.URL, "application/json", nil)

	// then:
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	var actual commands.SyncAdvertisementsHandlerResponse
	expected := commands.SyncAdvertisementsHandlerResponse{Message: "OK"}

	assert.NoError(t, jsonutil.DecodeResponseBody(res, &actual))
	assert.Equal(t, expected, actual)
}

type syncAdvertisementsProviderAlwaysOK struct{}

func (syncAdvertisementsProviderAlwaysOK) SyncAdvertisements(ctx context.Context) error {
	return nil
}
