package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/advertiser"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/require"
)

var errInternalQueryCallFailure = errors.New("internal query call failure")

func TestEngine_StartGASPSync_CallsSyncSuccessfully(t *testing.T) {
	// given:
	resolver := LookupResolverMock{
		ExpectQueryCall:       true,
		ExpectSetTrackersCall: true,
		ExpectedAnswer: &lookup.LookupAnswer{
			Type: lookup.AnswerTypeOutputList,
			Outputs: []*lookup.OutputListItem{
				{
					Beef:        createDummyBEEF(t),
					OutputIndex: 0,
				},
			},
		},
	}
	advertiser := fakeAdvertiser{
		parseAdvertisement: func(_ *script.Script) (*advertiser.Advertisement, error) {
			return &advertiser.Advertisement{Protocol: "SHIP"}, nil
		},
	}

	mockStorage := &fakeStorage{
		getLastInteractionFunc: func(_ context.Context, _, _ string) (float64, error) {
			return 0, nil
		},
		findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
			return []*engine.Output{}, nil
		},
		updateLastInteractionFunc: func(_ context.Context, _, _ string, _ float64) error {
			return nil
		},
	}

	sut := engine.NewEngine(engine.Engine{
		SyncConfiguration: map[string]engine.SyncConfiguration{"test-topic": {Type: engine.SyncConfigurationSHIP}},
		Advertiser:        &advertiser,
		HostingURL:        "http://localhost",
		SHIPTrackers:      []string{"http://localhost"},
		LookupResolver:    &resolver,
		Storage:           mockStorage,
	})

	// when:
	err := sut.StartGASPSync(context.Background())

	// then:
	require.NoError(t, err)

	resolver.AssertCalled(t)
}

func TestEngine_StartGASPSync_ResolverQueryFails(t *testing.T) {
	// given:
	// Use the static error variable
	resolver := LookupResolverMock{
		ExpectQueryCall:       true,
		ExpectSetTrackersCall: true,
		ExpectedError:         errInternalQueryCallFailure,
		ExpectedAnswer: &lookup.LookupAnswer{
			Type: lookup.AnswerTypeOutputList,
			Outputs: []*lookup.OutputListItem{
				{
					Beef:        createDummyBEEF(t),
					OutputIndex: 0,
				},
			},
		},
	}

	advertiser := fakeAdvertiser{
		parseAdvertisement: func(_ *script.Script) (*advertiser.Advertisement, error) {
			return &advertiser.Advertisement{Protocol: "SHIP"}, nil
		},
	}

	mockStorage := &fakeStorage{
		getLastInteractionFunc: func(_ context.Context, _, _ string) (float64, error) {
			return 0, nil
		},
		findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
			return []*engine.Output{}, nil
		},
		updateLastInteractionFunc: func(_ context.Context, _, _ string, _ float64) error {
			return nil
		},
	}

	sut := engine.NewEngine(engine.Engine{
		SyncConfiguration: map[string]engine.SyncConfiguration{"test-topic": {Type: engine.SyncConfigurationSHIP}},
		Advertiser:        &advertiser,
		HostingURL:        "http://localhost",
		SHIPTrackers:      []string{"http://localhost"},
		LookupResolver:    &resolver,
		Storage:           mockStorage,
	})

	// when:
	err := sut.StartGASPSync(context.Background())

	// then:
	require.ErrorIs(t, err, errInternalQueryCallFailure)

	resolver.AssertCalled(t)
}

func TestEngine_StartGASPSync_GaspSyncFails(t *testing.T) {
	// given:
	resolver := LookupResolverMock{
		ExpectQueryCall:       true,
		ExpectSetTrackersCall: true,
		ExpectedAnswer: &lookup.LookupAnswer{
			Type: lookup.AnswerTypeOutputList,
			Outputs: []*lookup.OutputListItem{
				{
					Beef:        createDummyBEEF(t),
					OutputIndex: 0,
				},
			},
		},
	}

	advertiser := fakeAdvertiser{
		parseAdvertisement: func(_ *script.Script) (*advertiser.Advertisement, error) {
			return &advertiser.Advertisement{Protocol: "SHIP"}, nil
		},
	}

	mockStorage := &fakeStorage{
		getLastInteractionFunc: func(_ context.Context, _, _ string) (float64, error) {
			return 0, nil
		},
		findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
			return []*engine.Output{}, nil
		},
		updateLastInteractionFunc: func(_ context.Context, _, _ string, _ float64) error {
			return nil
		},
	}

	sut := engine.NewEngine(engine.Engine{
		SyncConfiguration: map[string]engine.SyncConfiguration{"test-topic": {Type: engine.SyncConfigurationSHIP}},
		Advertiser:        &advertiser,
		HostingURL:        "http://localhost",
		SHIPTrackers:      []string{"http://localhost"},
		LookupResolver:    &resolver,
		Storage:           mockStorage,
	})

	// when:
	err := sut.StartGASPSync(context.Background())

	// then:
	require.NoError(t, err)

	resolver.AssertCalled(t)
}

// GASPMock is a test double for a GASP implementation.
// It allows simulating the behavior of the Sync method
// and verifying whether it was called during testing.
type GASPMock struct {
	// ExpectedErr is the error to return when Sync is called.
	ExpectedErr error

	// ExpectedCall indicates whether Sync is expected to be called.
	ExpectSyncCall bool

	// SyncWasCalled is true if Sync was called during the test.
	SyncWasCalled bool
}

// Sync simulates the synchronization process.
// It returns the predefined ExpectedErr if set,
// and marks the method as called for test verification.
func (g *GASPMock) Sync(_ context.Context) error {
	g.SyncWasCalled = true

	if g.ExpectedErr != nil {
		return g.ExpectedErr
	}
	return nil
}

// AssertCalled verifies that the Sync method was called.
// It should be used in tests to ensure expectations were met.
func (g *GASPMock) AssertCalled(t *testing.T) {
	t.Helper()
	require.Equal(t, g.ExpectSyncCall, g.SyncWasCalled, "Sync call mismatch")
}

// LookupResolverMock is a test double for the LookupResolver interface.
// It simulates resolver behavior and captures input/output for assertions.
type LookupResolverMock struct {
	// Expected outputs
	ExpectedAnswer *lookup.LookupAnswer
	ExpectedError  error

	// Expectations
	ExpectQueryCall       bool
	ExpectSetTrackersCall bool
	ExpectTrackersAccess  bool
	ExpectedTrackers      []string

	// Captured state
	QueryCalled       bool
	SetTrackersCalled bool
	TrackersCalled    bool
	ReceivedTrackers  []string
	ReceivedQuestion  *lookup.LookupQuestion
}

// SLAPTrackers simulates retrieving trackers and captures the call.
func (m *LookupResolverMock) SLAPTrackers() []string {
	m.TrackersCalled = true
	return m.ReceivedTrackers
}

// SetSLAPTrackers captures the trackers provided in the test.
func (m *LookupResolverMock) SetSLAPTrackers(trackers []string) {
	m.SetTrackersCalled = true
	m.ReceivedTrackers = trackers
}

// Query simulates a resolver query and captures the input question.
func (m *LookupResolverMock) Query(_ context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	m.QueryCalled = true
	m.ReceivedQuestion = question

	if m.ExpectedError != nil {
		return nil, m.ExpectedError
	}
	return m.ExpectedAnswer, nil
}

// AssertCalled verifies that the mock was used correctly in the test.
// It checks for expected method calls and captured input values.
func (m *LookupResolverMock) AssertCalled(t *testing.T) {
	t.Helper()

	require.Equal(t, m.ExpectQueryCall, m.QueryCalled, "Query call mismatch")
	require.Equal(t, m.ExpectSetTrackersCall, m.SetTrackersCalled, "SetSLAPTrackers call mismatch")
	require.Equal(t, m.ExpectTrackersAccess, m.TrackersCalled, "SLAPTrackers access mismatch")

	require.NotNil(t, m.ReceivedQuestion, "expected non-nil LookupQuestion")
	require.Equal(t, m.ExpectedTrackers, m.ReceivedTrackers, "unexpected SLAP trackers")
}
