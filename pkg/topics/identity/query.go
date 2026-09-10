package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// QueryKind values identify the selected legacy lookup path.
const (
	// MaxQueryBytes bounds the complete JSON query before decoding.
	MaxQueryBytes = 64 * 1024
	// MaxQueryStringBytes bounds each unescaped JSON string accepted by a query.
	MaxQueryStringBytes = 4 * 1024
	// MaxQueryAttributes bounds the number of attribute fields in one query.
	MaxQueryAttributes = 64
	// MaxQueryArrayValues bounds certificate type and certifier arrays.
	MaxQueryArrayValues = 128

	// MaxQueryResults is the hard maximum for records returned by a lookup.
	MaxQueryResults = 10_000
	// MaxQueryOffset bounds scans that a projection may be asked to skip.
	MaxQueryOffset = 100_000
)

var (
	// ErrInvalidQuery reports a JSON query whose shape or values do not satisfy
	// the identity lookup contract.
	ErrInvalidQuery = errors.New("invalid identity query")
	// ErrInvalidQueryPolicy reports a policy outside the hard operational bounds.
	ErrInvalidQueryPolicy = errors.New("invalid identity query policy")
)

// QueryPolicy sets the resource limits for a compiled identity query.
// MaxResults and MaxOffset must both be positive and no larger than their
// respective hard package limits.
type QueryPolicy struct {
	MaxResults int
	MaxOffset  int
}

// DefaultQueryPolicy returns the default operational query budget. MaxOffset
// is a scan bound, not a promise that every projection can serve that offset cheaply.
func DefaultQueryPolicy() QueryPolicy {
	return QueryPolicy{
		MaxResults: MaxQueryResults,
		MaxOffset:  MaxQueryOffset,
	}
}

// AttributeMatch identifies how a projection must evaluate an attribute
// predicate without coupling the query contract to a database implementation.
type AttributeMatch uint8

const (
	// AttributeMatchExact compares Value exactly and case-sensitively.
	AttributeMatchExact AttributeMatch = iota + 1
	// AttributeMatchFuzzy evaluates Value as an escaped, case-insensitive regex.
	AttributeMatchFuzzy
	// AttributeMatchText performs native full-text search using Value. Field is
	// empty because the projection must use its searchable-attributes index.
	AttributeMatchText
)

// QueryKind identifies the selected legacy lookup path. Adapters must use Kind
// rather than zero-value filter fields to preserve selectors whose string value
// is empty, such as identityKey: "".
type QueryKind uint8

// Query kinds preserve the branch selected by the legacy identity service.
const (
	QueryKindSerialNumber QueryKind = iota + 1
	QueryKindAttributes
	QueryKindIdentityKeyAndTypes
	QueryKindIdentityKey
	QueryKindCertifiers
)

// AttributePredicate is one portable attribute predicate. Fuzzy Value is an
// escaped regular expression whose normalized whitespace tokens are joined by
// `.*`; adapters must apply it case-insensitively. Field is empty only for an
// attributes.any predicate, which targets Record.SearchableAttributes; this
// avoids colliding with a certificate field literally named searchableAttributes.
// Text predicates must use a native full-text capability and must not be
// approximated with fuzzy matching.
type AttributePredicate struct {
	Field string
	Value string
	Match AttributeMatch
}

// Query is a compiled, backend-neutral identity lookup request. Filter slices
// have exact-match semantics. AttributePredicates are combined with the exact
// filters and each other. No result ordering is guaranteed.
//
// An omitted or zero requested limit sets Unbounded and gives Limit one extra
// record beyond ResultLimit. The caller can then reject an over-budget result
// without silently truncating it. Empty means the selected lookup path is valid
// but cannot match any record, so projections may return immediately.
type Query struct {
	Kind                QueryKind
	SerialNumber        string
	IdentityKey         string
	CertificateTypes    []string
	Certifiers          []string
	AttributePredicates []AttributePredicate

	Limit       int
	Offset      int
	ResultLimit int
	Unbounded   bool
	Empty       bool
}

// CompileQuery parses raw identity lookup JSON into a portable query under
// policy. It preserves the TypeScript selector precedence: serialNumber,
// attributes, identityKey with certificateTypes, identityKey, then certifiers.
// Unknown top-level properties are ignored for compatibility, while malformed
// known properties are rejected.
func CompileQuery(raw json.RawMessage, policy QueryPolicy) (Query, error) {
	if err := policy.validate(); err != nil {
		return Query{}, err
	}
	if len(raw) == 0 || len(raw) > MaxQueryBytes {
		return Query{}, fmt.Errorf("%w: query must be between 1 and %d bytes", ErrInvalidQuery, MaxQueryBytes)
	}

	var values map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return Query{}, fmt.Errorf("%w: decode JSON: %w", ErrInvalidQuery, err)
	}
	if values == nil || decoder.More() {
		return Query{}, fmt.Errorf("%w: query must be an object", ErrInvalidQuery)
	}
	if err := ensureEOF(decoder); err != nil {
		return Query{}, err
	}

	query, err := compilePagination(values, policy)
	if err != nil {
		return Query{}, err
	}

	if rawSerialNumber, ok := values["serialNumber"]; ok {
		serialNumber, serialErr := decodeString(rawSerialNumber, "serialNumber")
		if serialErr != nil {
			return Query{}, serialErr
		}
		query.SerialNumber = serialNumber
		query.Kind = QueryKindSerialNumber
		query.Empty = serialNumber == ""
		return query, nil
	}

	if rawAttributes, ok := values["attributes"]; ok {
		predicates, empty, attributesErr := compileAttributes(rawAttributes)
		if attributesErr != nil {
			return Query{}, attributesErr
		}
		query.AttributePredicates = predicates
		query.Kind = QueryKindAttributes
		query.Empty = empty
		certifiers, certifiersErr := decodeOptionalStringArray(values, "certifiers")
		if certifiersErr != nil {
			return Query{}, certifiersErr
		}
		query.Certifiers = certifiers
		return query, nil
	}

	identityKey, hasIdentityKey, err := decodeOptionalString(values, "identityKey")
	if err != nil {
		return Query{}, err
	}
	certificateTypes, hasCertificateTypes, err := decodeOptionalStringArrayWithPresence(values, "certificateTypes")
	if err != nil {
		return Query{}, err
	}
	certifiers, hasCertifiers, err := decodeOptionalStringArrayWithPresence(values, "certifiers")
	if err != nil {
		return Query{}, err
	}

	if hasIdentityKey && hasCertificateTypes {
		query.Kind = QueryKindIdentityKeyAndTypes
		query.IdentityKey = identityKey
		query.CertificateTypes = certificateTypes
		query.Certifiers = certifiers
		query.Empty = len(certificateTypes) == 0
		return query, nil
	}
	if hasIdentityKey {
		query.Kind = QueryKindIdentityKey
		query.IdentityKey = identityKey
		query.Certifiers = certifiers
		return query, nil
	}
	if hasCertifiers {
		query.Kind = QueryKindCertifiers
		query.Certifiers = certifiers
		query.Empty = len(certifiers) == 0
		return query, nil
	}

	return Query{}, fmt.Errorf("%w: one of attributes, identityKey, certifiers, or certificateTypes is required", ErrInvalidQuery)
}

func (policy QueryPolicy) validate() error {
	if policy.MaxResults <= 0 || policy.MaxResults > MaxQueryResults {
		return fmt.Errorf("%w: MaxResults must be between 1 and %d", ErrInvalidQueryPolicy, MaxQueryResults)
	}
	if policy.MaxOffset <= 0 || policy.MaxOffset > MaxQueryOffset {
		return fmt.Errorf("%w: MaxOffset must be between 1 and %d", ErrInvalidQueryPolicy, MaxQueryOffset)
	}
	return nil
}

func compilePagination(values map[string]json.RawMessage, policy QueryPolicy) (Query, error) {
	query := Query{Offset: 0, ResultLimit: policy.MaxResults}
	limit, hasLimit, err := decodeOptionalInteger(values, "limit")
	if err != nil {
		return Query{}, err
	}
	if !hasLimit || limit == 0 {
		query.Unbounded = true
		query.Limit = policy.MaxResults + 1
	} else {
		if limit < 0 || limit > policy.MaxResults {
			return Query{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidQuery, policy.MaxResults)
		}
		query.Limit = limit
	}

	offset, hasOffset, err := decodeOptionalInteger(values, "offset")
	if err != nil {
		return Query{}, err
	}
	if hasOffset {
		if offset < 0 || offset > policy.MaxOffset {
			return Query{}, fmt.Errorf("%w: offset must be between 0 and %d", ErrInvalidQuery, policy.MaxOffset)
		}
		query.Offset = offset
	}
	return query, nil
}

func compileAttributes(raw json.RawMessage) ([]AttributePredicate, bool, error) {
	decoded, keys, err := decodeAttributeMap(raw)
	if err != nil {
		return nil, false, err
	}
	if value, ok := decoded["any"]; ok {
		predicates, empty := compileAnyAttribute(value)
		return predicates, empty, nil
	}
	predicates, empty := compileFieldAttributes(decoded, keys)
	return predicates, empty, nil
}

func decodeAttributeMap(raw json.RawMessage) (map[string]string, []string, error) {
	var attributes map[string]json.RawMessage
	if err := json.Unmarshal(raw, &attributes); err != nil {
		return nil, nil, fmt.Errorf("%w: attributes must be an object: %w", ErrInvalidQuery, err)
	}
	if attributes == nil {
		return nil, nil, fmt.Errorf("%w: attributes must be an object", ErrInvalidQuery)
	}
	if len(attributes) > MaxQueryAttributes {
		return nil, nil, fmt.Errorf("%w: attributes may contain at most %d fields", ErrInvalidQuery, MaxQueryAttributes)
	}

	keys := make([]string, 0, len(attributes))
	decoded := make(map[string]string, len(attributes))
	for key := range attributes {
		value, err := decodeAttributeEntry(key, attributes[key])
		if err != nil {
			return nil, nil, err
		}
		decoded[key] = value
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return decoded, keys, nil
}

func decodeAttributeEntry(key string, raw json.RawMessage) (string, error) {
	if len(key) > MaxQueryStringBytes {
		return "", fmt.Errorf("%w: attribute name exceeds %d bytes", ErrInvalidQuery, MaxQueryStringBytes)
	}
	if key != "any" && !safeAttributePath(key) {
		return "", fmt.Errorf("%w: unsafe attribute path %q", ErrInvalidQuery, key)
	}
	return decodeString(raw, "attributes."+key)
}

func compileAnyAttribute(value string) ([]AttributePredicate, bool) {
	normalized := normalizeSearchInput(value)
	if utf16Length(normalized) <= 1 {
		return nil, true
	}
	if utf16Length(normalized) == 2 {
		return []AttributePredicate{{
			Field: "", Value: fuzzyPattern(normalized), Match: AttributeMatchFuzzy,
		}}, false
	}
	return []AttributePredicate{{Field: "", Value: normalized, Match: AttributeMatchText}}, false
}

func compileFieldAttributes(decoded map[string]string, keys []string) ([]AttributePredicate, bool) {
	predicates := make([]AttributePredicate, 0, len(keys))
	for _, key := range keys {
		normalized := normalizeSearchInput(decoded[key])
		if normalized == "" {
			continue
		}
		predicate := AttributePredicate{Field: key, Match: AttributeMatchFuzzy, Value: fuzzyPattern(normalized)}
		if key == "userName" {
			predicate.Match = AttributeMatchExact
			predicate.Value = normalized
		}
		predicates = append(predicates, predicate)
	}
	return predicates, len(predicates) == 0
}

func decodeOptionalString(values map[string]json.RawMessage, name string) (string, bool, error) {
	raw, ok := values[name]
	if !ok {
		return "", false, nil
	}
	value, err := decodeString(raw, name)
	return value, true, err
}

func decodeString(raw json.RawMessage, name string) (string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("%w: %s must be a string", ErrInvalidQuery, name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%w: %s must be a string", ErrInvalidQuery, name)
	}
	if len(value) > MaxQueryStringBytes {
		return "", fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidQuery, name, MaxQueryStringBytes)
	}
	return value, nil
}

func decodeOptionalStringArray(values map[string]json.RawMessage, name string) ([]string, error) {
	array, _, err := decodeOptionalStringArrayWithPresence(values, name)
	return array, err
}

func decodeOptionalStringArrayWithPresence(values map[string]json.RawMessage, name string) ([]string, bool, error) {
	raw, ok := values[name]
	if !ok {
		return nil, false, nil
	}
	var array []json.RawMessage
	if err := json.Unmarshal(raw, &array); err != nil || array == nil {
		if err != nil {
			return nil, true, fmt.Errorf("%w: %s must be an array of strings", ErrInvalidQuery, name)
		}
		return nil, true, fmt.Errorf("%w: %s must be an array of strings", ErrInvalidQuery, name)
	}
	if len(array) > MaxQueryArrayValues {
		return nil, true, fmt.Errorf("%w: %s may contain at most %d values", ErrInvalidQuery, name, MaxQueryArrayValues)
	}
	result := make([]string, len(array))
	for index, rawValue := range array {
		value, err := decodeString(rawValue, name+"["+strconv.Itoa(index)+"]")
		if err != nil {
			return nil, true, err
		}
		result[index] = value
	}
	return result, true, nil
}

func decodeOptionalInteger(values map[string]json.RawMessage, name string) (int, bool, error) {
	raw, ok := values[name]
	if !ok {
		return 0, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || (trimmed[0] != '-' && (trimmed[0] < '0' || trimmed[0] > '9')) {
		return 0, true, fmt.Errorf("%w: %s must be an integer", ErrInvalidQuery, name)
	}
	var number float64
	if err := json.Unmarshal(trimmed, &number); err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, true, fmt.Errorf("%w: %s must be an integer", ErrInvalidQuery, name)
	}
	if math.Trunc(number) != number || number > 1<<53-1 || number < -(1<<53-1) {
		return 0, true, fmt.Errorf("%w: %s must be an integer", ErrInvalidQuery, name)
	}
	if number > float64(math.MaxInt) || number < float64(math.MinInt) {
		return 0, true, fmt.Errorf("%w: %s exceeds int bounds", ErrInvalidQuery, name)
	}
	return int(number), true, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: trailing JSON value", ErrInvalidQuery)
		}
		return fmt.Errorf("%w: trailing JSON data", ErrInvalidQuery)
	}
	return nil
}

func normalizeSearchInput(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	spacePending := false
	written := false
	for _, runeValue := range input {
		if isECMAScriptWhitespace(runeValue) {
			if written {
				spacePending = true
			}
			continue
		}
		if spacePending {
			builder.WriteByte(' ')
			spacePending = false
		}
		builder.WriteRune(runeValue)
		written = true
	}
	return builder.String()
}

func isECMAScriptWhitespace(runeValue rune) bool {
	switch runeValue {
	case '\u0009', '\u000A', '\u000B', '\u000C', '\u000D', '\u2028', '\u2029', '\uFEFF':
		return true
	}
	return runeValue == '\u0020' || runeValue == '\u00A0' || runeValue == '\u1680' ||
		(runeValue >= '\u2000' && runeValue <= '\u200A') || runeValue == '\u202F' ||
		runeValue == '\u205F' || runeValue == '\u3000'
}

func utf16Length(input string) int {
	return len(utf16.Encode([]rune(input)))
}

func fuzzyPattern(input string) string {
	tokens := strings.FieldsFunc(input, isECMAScriptWhitespace)
	for index := range tokens {
		tokens[index] = regexp.QuoteMeta(tokens[index])
	}
	return strings.Join(tokens, ".*")
}

func safeAttributePath(path string) bool {
	return path != "" && !strings.ContainsAny(path, ".$\x00")
}
