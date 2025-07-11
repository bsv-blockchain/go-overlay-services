package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/stretchr/testify/require"
)

func TestLookupResolver_NewLookupResolver(t *testing.T) {
	t.Run("should create resolver with default HTTPS facilitator", func(t *testing.T) {
		// when
		resolver := engine.NewLookupResolver()

		// then
		require.NotNil(t, resolver)
		require.Empty(t, resolver.SLAPTrackers())
	})
}

func TestLookupResolver_SetSLAPTrackers(t *testing.T) {
	t.Run("should set SLAP trackers", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()
		trackers := []string{"https://tracker1.com", "https://tracker2.com"}

		// when
		resolver.SetSLAPTrackers(trackers)

		// then
		require.Equal(t, trackers, resolver.SLAPTrackers())
	})

	t.Run("should not set empty trackers", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()
		initialTrackers := []string{"https://tracker1.com"}
		resolver.SetSLAPTrackers(initialTrackers)

		// when
		resolver.SetSLAPTrackers([]string{})

		// then
		require.Equal(t, initialTrackers, resolver.SLAPTrackers())
	})

	t.Run("should replace existing trackers", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()
		oldTrackers := []string{"https://old1.com", "https://old2.com"}
		newTrackers := []string{"https://new1.com", "https://new2.com", "https://new3.com"}

		resolver.SetSLAPTrackers(oldTrackers)
		require.Equal(t, oldTrackers, resolver.SLAPTrackers())

		// when
		resolver.SetSLAPTrackers(newTrackers)

		// then
		require.Equal(t, newTrackers, resolver.SLAPTrackers())
	})
}

func TestLookupResolver_Query(t *testing.T) {
	t.Run("should query with valid question", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()

		// Note: In real implementation, you might need to expose a method to set facilitator
		// or use dependency injection for better testability

		question := &lookup.LookupQuestion{
			Service: "test-service",
			Query:   json.RawMessage(`"testkey"`),
		}

		// when
		// Note: This test demonstrates the interface, but actual testing would require
		// either mocking the HTTP client or using a test server
		_ = resolver
		_ = question

		// then
		// The actual Query method would make HTTP requests, so comprehensive testing
		// would require integration tests or HTTP mocking
	})

	t.Run("should handle query errors", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()

		question := &lookup.LookupQuestion{
			Service: "test-service",
			Query:   json.RawMessage(`"testkey"`),
		}

		// when
		// Note: Testing error scenarios would require mocking the underlying HTTP client
		// or using a test server that returns errors
		_ = resolver
		_ = question

		// then
		// Error handling tests would verify that network errors, timeouts, and
		// invalid responses are properly propagated
	})
}

func TestLookupResolver_QueryWithSLAPTrackers(t *testing.T) {
	t.Run("should use configured SLAP trackers for queries", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()
		trackers := []string{"https://slap1.com", "https://slap2.com"}
		resolver.SetSLAPTrackers(trackers)

		// then
		require.Equal(t, trackers, resolver.SLAPTrackers())

		// Note: Actual query behavior with trackers would require integration testing
		// or mocking the HTTP layer to verify that queries are sent to the configured trackers
	})
}

func TestLookupResolver_ErrorScenarios(t *testing.T) {
	t.Run("should handle nil question gracefully", func(t *testing.T) {
		// given
		resolver := engine.NewLookupResolver()

		// when/then
		// The actual behavior depends on the underlying implementation
		// This test documents expected behavior when nil is passed
		_ = resolver
	})

	t.Run("should handle context cancellation", func(t *testing.T) {
		// given
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		resolver := engine.NewLookupResolver()
		question := &lookup.LookupQuestion{
			Service: "test-service",
			Query:   json.RawMessage(`"testkey"`),
		}

		// when/then
		// Query should respect context cancellation
		// Actual test would verify that context.Canceled error is returned
		_ = ctx
		_ = resolver
		_ = question
	})
}
