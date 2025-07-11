package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/stretchr/testify/require"
)

// SubmitTransactionProviderAlwaysSuccess mocks a transaction provider that always succeeds
type SubmitTransactionProviderAlwaysSuccess struct{ ExpectedSteak overlay.Steak }

// NewSubmitTransactionProviderAlwaysSuccess creates a new instance of SubmitTransactionProviderAlwaysSuccess
func NewSubmitTransactionProviderAlwaysSuccess(steak overlay.Steak) *SubmitTransactionProviderAlwaysSuccess {
	return &SubmitTransactionProviderAlwaysSuccess{ExpectedSteak: steak}
}

// Submit implements the transaction submission interface
func (s SubmitTransactionProviderAlwaysSuccess) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode engine.SumbitMode, onSteakReady engine.OnSteakReady) (overlay.Steak, error) {
	// Call the onSteakReady callback to simulate async completion
	onSteakReady(&s.ExpectedSteak)
	return nil, nil
}

// SubmitTransactionProviderAlwaysFailure mocks a transaction provider that always fails
type SubmitTransactionProviderAlwaysFailure struct{ ExpectedErr error }

// Submit implements the transaction submission interface
func (s SubmitTransactionProviderAlwaysFailure) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode engine.SumbitMode, onSteakReady engine.OnSteakReady) (overlay.Steak, error) {
	return nil, s.ExpectedErr
}

// NewSubmitTransactionProviderAlwaysFailure creates a new instance of SubmitTransactionProviderAlwaysFailure
func NewSubmitTransactionProviderAlwaysFailure(err error) *SubmitTransactionProviderAlwaysFailure {
	return &SubmitTransactionProviderAlwaysFailure{ExpectedErr: err}
}

// SubmitTransactionProviderNeverCallback mocks a transaction provider that never calls the callback
type SubmitTransactionProviderNeverCallback struct{}

// Submit implements the transaction submission interface
func (s SubmitTransactionProviderNeverCallback) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode engine.SumbitMode, onSteakReady engine.OnSteakReady) (overlay.Steak, error) {
	// Never call the callback which then should trigger the timeout
	return overlay.Steak{}, nil
}

// LookupQuestionProviderAlwaysSucceeds implements the LookupQuestionProvider interface for successful test cases
type LookupQuestionProviderAlwaysSucceeds struct {
	ExpectedLookupAnswer *lookup.LookupAnswer
}

// Lookup implements the LookupQuestionProvider interface
func (s *LookupQuestionProviderAlwaysSucceeds) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	return s.ExpectedLookupAnswer, nil
}

// LookupQuestionProviderAlwaysFails implements the LookupQuestionProvider interface for failure test cases
type LookupQuestionProviderAlwaysFails struct {
	ExpectedErr error
}

// Lookup implements the LookupQuestionProvider interface
func (l *LookupQuestionProviderAlwaysFails) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	return nil, l.ExpectedErr
}

// ParseToError reads the HTTP response body and returns an error created from its content.
// The response body is expected to contain an error message that can be converted to an error object.
// It ensures that the response body is properly read and closed, and it trims any trailing whitespace.
func ParseToError(t *testing.T, r io.ReadCloser) error {
	t.Helper()

	defer func() {
		require.NoError(t, r.Close(), "failed to close response body")
	}()

	body, err := io.ReadAll(r)
	require.NoError(t, err, "failed to read response body")
	require.NotNil(t, body, "response body is nil")

	return errors.New(strings.TrimSpace(string(body)))
}

// RequestBody serializes the provided value into a JSON-encoded byte slice and returns it as an io.Reader.
// This is typically used in tests to create a request body for HTTP requests.
// The function ensures that marshaling succeeds; otherwise, it stops the test execution with an error.
func RequestBody(t *testing.T, v any) io.Reader {
	t.Helper()
	bb, err := json.Marshal(v)
	require.NoError(t, err, "failed to marshal request body")

	return bytes.NewReader(bb)
}
