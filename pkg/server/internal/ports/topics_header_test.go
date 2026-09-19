package ports_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
)

const (
	testTopicShip = "tm_ship"
	testTopicSlap = "tm_slap"
)

var errTopicsHeaderTestError = errors.New("x-topics header decoding error during topics header unit test")

func TestParseTopicsHeader_ValidCases(t *testing.T) {
	tests := map[string]struct {
		value    string
		expected app.TransactionTopics
	}{
		"simple form - single topic": {
			value:    testTopicShip,
			expected: app.TransactionTopics{testTopicShip},
		},
		"simple form - multiple topics": {
			value:    "tm_ship,tm_slap",
			expected: app.TransactionTopics{testTopicShip, testTopicSlap},
		},
		"simple form - whitespace around topics is trimmed": {
			value:    "  tm_ship , tm_slap  ",
			expected: app.TransactionTopics{testTopicShip, testTopicSlap},
		},
		"simple form - empty entries are dropped": {
			value:    "tm_ship,,tm_slap, ,",
			expected: app.TransactionTopics{testTopicShip, testTopicSlap},
		},
		"simple form - duplicates are removed preserving order": {
			value:    "tm_slap,tm_ship,tm_slap,tm_ship",
			expected: app.TransactionTopics{testTopicSlap, testTopicShip},
		},
		"JSON form - single topic as sent by @bsv/sdk SHIPBroadcaster": {
			value:    `["tm_ship"]`,
			expected: app.TransactionTopics{testTopicShip},
		},
		"JSON form - multiple topics": {
			value:    `["tm_ship","tm_slap"]`,
			expected: app.TransactionTopics{testTopicShip, testTopicSlap},
		},
		"JSON form - surrounding and inner whitespace is tolerated": {
			value:    `  [ " tm_ship " , "tm_slap" ]  `,
			expected: app.TransactionTopics{testTopicShip, testTopicSlap},
		},
		"JSON form - empty strings are dropped": {
			value:    `["", "tm_ship", " "]`,
			expected: app.TransactionTopics{testTopicShip},
		},
		"JSON form - duplicates are removed preserving order": {
			value:    `["tm_slap","tm_ship","tm_slap"]`,
			expected: app.TransactionTopics{testTopicSlap, testTopicShip},
		},
		"JSON form - topic containing a comma is not split": {
			value:    `["tm_a,b"]`,
			expected: app.TransactionTopics{"tm_a,b"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// when:
			actual, err := ports.ParseTopicsHeader(tc.value)

			// then:
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestParseTopicsHeader_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		value        string
		expectedSlug string
	}{
		"empty value": {
			value:        "",
			expectedSlug: app.NewEmptyTransactionTopicsError().Slug(),
		},
		"whitespace only": {
			value:        "   ",
			expectedSlug: app.NewEmptyTransactionTopicsError().Slug(),
		},
		"commas only": {
			value:        ", ,,",
			expectedSlug: app.NewEmptyTransactionTopicsError().Slug(),
		},
		"JSON form - empty array": {
			value:        "[]",
			expectedSlug: app.NewEmptyTransactionTopicsError().Slug(),
		},
		"JSON form - only blank strings": {
			value:        `["", " "]`,
			expectedSlug: app.NewEmptyTransactionTopicsError().Slug(),
		},
		"JSON form - malformed JSON": {
			value:        `["tm_ship"`,
			expectedSlug: ports.NewTopicsHeaderParserError(errTopicsHeaderTestError).Slug(),
		},
		"JSON form - unquoted element": {
			value:        `[tm_ship]`,
			expectedSlug: ports.NewTopicsHeaderParserError(errTopicsHeaderTestError).Slug(),
		},
		"JSON form - non-string element": {
			value:        `["tm_ship", 1]`,
			expectedSlug: ports.NewTopicsHeaderParserError(errTopicsHeaderTestError).Slug(),
		},
		"JSON form - nested array element": {
			value:        `[["tm_ship"]]`,
			expectedSlug: ports.NewTopicsHeaderParserError(errTopicsHeaderTestError).Slug(),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// when:
			actual, err := ports.ParseTopicsHeader(tc.value)

			// then:
			require.Error(t, err)
			var appErr app.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, app.ErrorTypeIncorrectInput, appErr.ErrorType())
			assert.Equal(t, tc.expectedSlug, appErr.Slug())
			assert.Nil(t, actual)
		})
	}
}
