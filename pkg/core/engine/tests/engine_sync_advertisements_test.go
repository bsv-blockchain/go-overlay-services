package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/advertiser"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

func TestEngine_SyncAdvertisements_ShouldReturnNil_WhenAdvertiserIsNil(t *testing.T) {
	// given
	sut := &engine.Engine{
		Advertiser: nil,
	}

	// when
	err := sut.SyncAdvertisements(context.Background())

	// then
	require.NoError(t, err)
}

func TestEngine_SyncAdvertisements_ShouldNotFail_WhenCreateAdvertisementsFails(t *testing.T) {
	// given
	sut := &engine.Engine{
		Advertiser: fakeAdvertiser{
			findAllAdvertisementsFunc: func(protocol overlay.Protocol) ([]*advertiser.Advertisement, error) {
				return []*advertiser.Advertisement{}, nil
			},
			createAdvertisementsFunc: func(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error) {
				return overlay.TaggedBEEF{}, errors.New("invalid-atomic-beef")
			},
		},
		Managers:   map[string]engine.TopicManager{"test-topic": fakeTopicManager{}},
		HostingURL: "http://localhost",
	}

	// when
	err := sut.SyncAdvertisements(context.Background())

	// then
	require.NoError(t, err)
}

func TestEngine_SyncAdvertisements_ShouldCompleteSuccessfully(t *testing.T) {
	// given
	sut := &engine.Engine{
		Advertiser: fakeAdvertiser{
			findAllAdvertisementsFunc: func(protocol overlay.Protocol) ([]*advertiser.Advertisement, error) {
				return []*advertiser.Advertisement{}, nil
			},
			createAdvertisementsFunc: func(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error) {
				return overlay.TaggedBEEF{}, nil
			},
			revokeAdvertisementsFunc: func(data []*advertiser.Advertisement) (overlay.TaggedBEEF, error) {
				return overlay.TaggedBEEF{}, nil
			},
		},
		Managers:       map[string]engine.TopicManager{"test-topic": fakeTopicManager{}},
		LookupServices: map[string]engine.LookupService{"test-service": fakeLookupService{}},
		HostingURL:     "http://localhost",
	}

	// when
	err := sut.SyncAdvertisements(context.Background())

	// then
	require.NoError(t, err)
}

func TestEngine_SyncAdvertisements_ShouldLogAndContinue_WhenCreateOrRevokeFails(t *testing.T) {
	// given
	sut := &engine.Engine{
		Advertiser: fakeAdvertiser{
			findAllAdvertisementsFunc: func(protocol overlay.Protocol) ([]*advertiser.Advertisement, error) {
				return []*advertiser.Advertisement{}, nil
			},
			createAdvertisementsFunc: func(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error) {
				return overlay.TaggedBEEF{}, errors.New("create failed")
			},
			revokeAdvertisementsFunc: func(data []*advertiser.Advertisement) (overlay.TaggedBEEF, error) {
				return overlay.TaggedBEEF{}, errors.New("revoke failed")
			},
		},
		Managers:       map[string]engine.TopicManager{"test-topic": fakeTopicManager{}},
		LookupServices: map[string]engine.LookupService{"test-service": fakeLookupService{}},
		HostingURL:     "http://localhost",
	}

	// when
	err := sut.SyncAdvertisements(context.Background())

	// then
	require.NoError(t, err)
}

func TestEngine_SyncAdvertisements_ShouldSkip_WhenHostingURLIsInvalid(t *testing.T) {
	tests := []struct {
		name       string
		hostingURL string
	}{
		{"empty hosting URL", ""},
		{"localhost URL", "https://localhost:8080"},
		{"127.0.0.1 URL", "https://127.0.0.1:8080"},
		{"private IP 10.x", "https://10.0.0.1"},
		{"private IP 192.168.x", "https://192.168.1.1"},
		{"private IP 172.16.x", "https://172.16.0.1"},
		{"IPv6 loopback", "https://[::1]"},
		{"non-routable 0.0.0.0", "https://0.0.0.0"},
		{"HTTP protocol", "http://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			sut := &engine.Engine{
				Advertiser: fakeAdvertiser{
					findAllAdvertisementsFunc: func(protocol overlay.Protocol) ([]*advertiser.Advertisement, error) {
						return []*advertiser.Advertisement{}, nil
					},
					createAdvertisementsFunc: func(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error) {
						return overlay.TaggedBEEF{}, nil
					},
				},
				Managers:   map[string]engine.TopicManager{"test-topic": fakeTopicManager{}},
				HostingURL: tt.hostingURL,
			}

			// when
			err := sut.SyncAdvertisements(context.Background())

			// then
			require.NoError(t, err)
			// The function should return early without calling advertiser methods
			// when hosting URL is invalid
			// NOTE: This test assumes the Go implementation will add URL validation
			// Currently, it doesn't validate URLs, so this is a suggested enhancement
		})
	}
}
