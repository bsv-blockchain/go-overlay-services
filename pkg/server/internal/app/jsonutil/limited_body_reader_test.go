package jsonutil_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/stretchr/testify/require"
)

func TestLimitedBodyReader_NegativeScenarios(t *testing.T) {
	tests := map[string]struct {
		name        string
		body        io.Reader
		readLimit   int64
		expectError error
	}{
		"body exceeds limit": {
			body:        strings.NewReader(strings.Repeat("A", 1025)),
			readLimit:   1024,
			expectError: jsonutil.ErrRequestBodyTooLarge,
		},
		"error during body read": {
			body:        &readerAlwaysFailureStub{},
			readLimit:   1024,
			expectError: jsonutil.ErrBodyReaderFailure,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			reader := &jsonutil.LimitedBodyReader{
				Body:      tc.body,
				ReadLimit: tc.readLimit,
			}

			// when:
			data, err := reader.Read()

			// then:
			require.ErrorIs(t, err, tc.expectError)
			require.Nil(t, data)
		})
	}
}

func TestLimitedBodyReader_PositiveScenarios(t *testing.T) {
	tests := map[string]struct {
		name         string
		body         io.Reader
		readLimit    int64
		expectedData []byte
	}{
		"valid small body": {
			body:         strings.NewReader("hello world"),
			readLimit:    1024,
			expectedData: []byte("hello world"),
		},
		"empty body": {
			body:         strings.NewReader(""),
			readLimit:    1024,
			expectedData: nil,
		},
		"body exactly at limit": {
			body:         strings.NewReader(strings.Repeat("A", 1024)),
			readLimit:    1024,
			expectedData: []byte(strings.Repeat("A", 1024)),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			reader := &jsonutil.LimitedBodyReader{
				Body:      tc.body,
				ReadLimit: tc.readLimit,
			}

			// when:
			data, err := reader.Read()

			// then:
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedData, data)
		})
	}
}

type readerAlwaysFailureStub struct{}

func (*readerAlwaysFailureStub) Read(p []byte) (n int, err error) { return 0, errors.New("read error") }
