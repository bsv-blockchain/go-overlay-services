package ports

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
)

// XTopicsHeader defines the HTTP header key used to specify transaction topics.
const XTopicsHeader = "x-topics"

// ParseTopicsHeader decodes the value of the x-topics header into a list of topics.
//
// Two encodings are accepted, because overlay clients disagree on which one to send:
//
//   - A JSON array of strings (e.g. `["tm_ship","tm_slap"]`), which is what the
//     TypeScript @bsv/sdk SHIPBroadcaster and the go-sdk HTTPSOverlayBroadcastFacilitator emit.
//   - A comma-separated list (OpenAPI "simple" style, e.g. `tm_ship,tm_slap`).
//
// A value whose first non-blank character is '[' is parsed as JSON and rejected if it is
// not a JSON array of strings; any other value is split on commas. In both cases whitespace
// around each topic is trimmed, empty entries are dropped, and duplicates are removed while
// preserving first-seen order. An error is returned when no topic remains.
func ParseTopicsHeader(value string) (app.TransactionTopics, error) {
	value = strings.TrimSpace(value)

	var topics []string
	if strings.HasPrefix(value, "[") {
		if err := json.Unmarshal([]byte(value), &topics); err != nil {
			return nil, NewTopicsHeaderParserError(fmt.Errorf("decoding %s header as JSON array of strings: %w", XTopicsHeader, err))
		}
	} else {
		topics = strings.Split(value, ",")
	}

	return normalizeTopics(topics)
}

// normalizeTopics trims each topic, drops empty entries, removes duplicates
// (keeping the first occurrence), and rejects an empty result.
func normalizeTopics(topics []string) (app.TransactionTopics, error) {
	seen := make(map[string]struct{}, len(topics))
	result := make(app.TransactionTopics, 0, len(topics))

	for _, topic := range topics {
		topic = strings.TrimSpace(topic)
		if topic == "" {
			continue
		}
		if _, ok := seen[topic]; ok {
			continue
		}
		seen[topic] = struct{}{}
		result = append(result, topic)
	}

	if len(result) == 0 {
		return nil, app.NewEmptyTransactionTopicsError()
	}

	return result, nil
}

// NewTopicsHeaderParserError wraps an x-topics header decoding failure into a user-friendly
// application error, indicating that the header is neither a JSON array of strings nor a
// comma-separated list of topics.
func NewTopicsHeaderParserError(err error) app.Error {
	return app.NewIncorrectInputError(
		err.Error(),
		"The x-topics header must be a JSON array of strings or a comma-separated list of topics.",
	)
}
