package identity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileQueryPreservesSelectorPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		assertQuery func(*testing.T, Query)
	}{
		{
			name: "serial number wins over all other selectors",
			raw:  `{"serialNumber":"serial","attributes":{"userName":"ignored"},"identityKey":"ignored","certificateTypes":["ignored"],"certifiers":["ignored"]}`,
			assertQuery: func(t *testing.T, query Query) {
				assert.Equal(t, "serial", query.SerialNumber)
				assert.Empty(t, query.AttributePredicates)
				assert.Empty(t, query.IdentityKey)
			},
		},
		{
			name: "attributes win over identity key and certifiers",
			raw:  `{"attributes":{"userName":" Alice "},"identityKey":"ignored","certificateTypes":["ignored"],"certifiers":["certifier"]}`,
			assertQuery: func(t *testing.T, query Query) {
				require.Len(t, query.AttributePredicates, 1)
				assert.Equal(t, AttributeMatchExact, query.AttributePredicates[0].Match)
				assert.Equal(t, "Alice", query.AttributePredicates[0].Value)
				assert.Equal(t, []string{"certifier"}, query.Certifiers)
			},
		},
		{
			name: "identity key with certificate types wins over identity key alone",
			raw:  `{"identityKey":"subject","certificateTypes":["type"],"certifiers":["certifier"]}`,
			assertQuery: func(t *testing.T, query Query) {
				assert.Equal(t, "subject", query.IdentityKey)
				assert.Equal(t, []string{"type"}, query.CertificateTypes)
				assert.Equal(t, []string{"certifier"}, query.Certifiers)
			},
		},
		{
			name: "identity key wins over certifiers",
			raw:  `{"identityKey":"subject","certifiers":["certifier"]}`,
			assertQuery: func(t *testing.T, query Query) {
				assert.Equal(t, "subject", query.IdentityKey)
				assert.Empty(t, query.CertificateTypes)
				assert.Equal(t, []string{"certifier"}, query.Certifiers)
			},
		},
		{
			name: "empty identity key remains the identity key selector",
			raw:  `{"identityKey":"","certifiers":["certifier"]}`,
			assertQuery: func(t *testing.T, query Query) {
				assert.Equal(t, QueryKindIdentityKey, query.Kind)
				assert.Empty(t, query.IdentityKey)
				assert.Equal(t, []string{"certifier"}, query.Certifiers)
			},
		},
		{
			name: "certifiers are used when no stronger selector exists",
			raw:  `{"certifiers":["certifier"]}`,
			assertQuery: func(t *testing.T, query Query) {
				assert.Equal(t, []string{"certifier"}, query.Certifiers)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, err := CompileQuery(json.RawMessage(test.raw), DefaultQueryPolicy())
			require.NoError(t, err)
			test.assertQuery(t, query)
		})
	}
}

func TestCompileQueryCompilesAttributeSemantics(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		assertQuery func(*testing.T, Query)
	}{
		{
			name: "user name is an exact normalized case sensitive predicate",
			raw:  `{"attributes":{"userName":"\u00a0Ada\tLovelace\ufeff"}}`,
			assertQuery: func(t *testing.T, query Query) {
				require.Len(t, query.AttributePredicates, 1)
				assert.Equal(t, AttributePredicate{Field: "userName", Value: "Ada Lovelace", Match: AttributeMatchExact}, query.AttributePredicates[0])
			},
		},
		{
			name: "attribute fuzzy pattern escapes regex syntax and joins whitespace tokens",
			raw:  `{"attributes":{"displayName":" A.*  B[ "}}`,
			assertQuery: func(t *testing.T, query Query) {
				require.Len(t, query.AttributePredicates, 1)
				assert.Equal(t, AttributeMatchFuzzy, query.AttributePredicates[0].Match)
				assert.Equal(t, `A\.\*.*B\[`, query.AttributePredicates[0].Value)
			},
		},
		{
			name: "two UTF16 units in any use fuzzy searchable attributes",
			raw:  `{"attributes":{"any":"ab"}}`,
			assertQuery: func(t *testing.T, query Query) {
				require.Len(t, query.AttributePredicates, 1)
				assert.Equal(t, AttributeMatchFuzzy, query.AttributePredicates[0].Match)
				assert.Equal(t, "ab", query.AttributePredicates[0].Value)
			},
		},
		{
			name: "surrogate pair has two UTF16 units and uses fuzzy search",
			raw:  `{"attributes":{"any":"😀"}}`,
			assertQuery: func(t *testing.T, query Query) {
				require.Len(t, query.AttributePredicates, 1)
				assert.Equal(t, AttributeMatchFuzzy, query.AttributePredicates[0].Match)
			},
		},
		{
			name: "more than two UTF16 units requires native text search",
			raw:  `{"attributes":{"any":"ada"}}`,
			assertQuery: func(t *testing.T, query Query) {
				require.Len(t, query.AttributePredicates, 1)
				assert.Equal(t, AttributeMatchText, query.AttributePredicates[0].Match)
				assert.Equal(t, "ada", query.AttributePredicates[0].Value)
			},
		},
		{
			name: "one or fewer UTF16 units in any is empty",
			raw:  `{"attributes":{"any":" a "}}`,
			assertQuery: func(t *testing.T, query Query) {
				assert.True(t, query.Empty)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, err := CompileQuery(json.RawMessage(test.raw), DefaultQueryPolicy())
			require.NoError(t, err)
			test.assertQuery(t, query)
		})
	}
}

func TestCompileQueryPreservesLegacyEmptySelectorResults(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty serial number", raw: `{"serialNumber":""}`},
		{name: "empty attributes", raw: `{"attributes":{}}`},
		{name: "blank attribute values", raw: `{"attributes":{"name":" \t "}}`},
		{name: "empty certificate types", raw: `{"identityKey":"subject","certificateTypes":[]}`},
		{name: "certifier only empty array", raw: `{"certifiers":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, err := CompileQuery(json.RawMessage(test.raw), DefaultQueryPolicy())
			require.NoError(t, err)
			assert.True(t, query.Empty)
		})
	}
}

func TestCompileQueryUsesBoundedPagination(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		policy      QueryPolicy
		wantErr     string
		assertQuery func(*testing.T, Query)
	}{
		{
			name:   "omitted limit requests one extra result for budget detection",
			raw:    `{"identityKey":"subject"}`,
			policy: QueryPolicy{MaxResults: 3, MaxOffset: 7},
			assertQuery: func(t *testing.T, query Query) {
				assert.True(t, query.Unbounded)
				assert.Equal(t, 4, query.Limit)
				assert.Equal(t, 3, query.ResultLimit)
			},
		},
		{
			name:   "zero limit retains legacy uncapped intent",
			raw:    `{"identityKey":"subject","limit":0}`,
			policy: QueryPolicy{MaxResults: 3, MaxOffset: 7},
			assertQuery: func(t *testing.T, query Query) {
				assert.True(t, query.Unbounded)
				assert.Equal(t, 4, query.Limit)
			},
		},
		{
			name:   "positive limit fetches exactly the requested count",
			raw:    `{"identityKey":"subject","limit":2,"offset":7}`,
			policy: QueryPolicy{MaxResults: 3, MaxOffset: 7},
			assertQuery: func(t *testing.T, query Query) {
				assert.False(t, query.Unbounded)
				assert.Equal(t, 2, query.Limit)
				assert.Equal(t, 7, query.Offset)
			},
		},
		{
			name:   "integral decimal limit retains JavaScript number semantics",
			raw:    `{"identityKey":"subject","limit":1.0,"offset":1e0}`,
			policy: QueryPolicy{MaxResults: 3, MaxOffset: 7},
			assertQuery: func(t *testing.T, query Query) {
				assert.Equal(t, 1, query.Limit)
				assert.Equal(t, 1, query.Offset)
			},
		},
		{name: "limit over policy", raw: `{"identityKey":"subject","limit":4}`, policy: QueryPolicy{MaxResults: 3, MaxOffset: 7}, wantErr: "limit"},
		{name: "negative offset", raw: `{"identityKey":"subject","offset":-1}`, policy: QueryPolicy{MaxResults: 3, MaxOffset: 7}, wantErr: "offset"},
		{name: "fractional limit", raw: `{"identityKey":"subject","limit":1.5}`, policy: QueryPolicy{MaxResults: 3, MaxOffset: 7}, wantErr: "integer"},
		{name: "exponent limit over policy", raw: `{"identityKey":"subject","limit":1e2}`, policy: QueryPolicy{MaxResults: 3, MaxOffset: 7}, wantErr: "limit"},
		{name: "invalid policy", raw: `{"identityKey":"subject"}`, policy: QueryPolicy{}, wantErr: "policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, err := CompileQuery(json.RawMessage(test.raw), test.policy)
			if test.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErr)
				return
			}
			require.NoError(t, err)
			test.assertQuery(t, query)
		})
	}
}

func TestCompileQueryRejectsMalformedAndUnsafeInput(t *testing.T) {
	oversizedString := strings.Repeat("a", MaxQueryStringBytes+1)
	tests := []struct {
		name string
		raw  string
	}{
		{name: "null known property", raw: `{"identityKey":null}`},
		{name: "null limit", raw: `{"identityKey":"subject","limit":null}`},
		{name: "string offset", raw: `{"identityKey":"subject","offset":"1"}`},
		{name: "wrong attribute value type", raw: `{"attributes":{"name":1}}`},
		{name: "unsafe dot path", raw: `{"attributes":{"name.first":"Ada"}}`},
		{name: "unsafe dollar path", raw: `{"attributes":{"$where":"Ada"}}`},
		{name: "unsafe null path", raw: `{"attributes":{"name\u0000first":"Ada"}}`},
		{name: "null certifier array", raw: `{"certifiers":null}`},
		{name: "null certifier array item", raw: `{"certifiers":[null]}`},
		{name: "null certifier with empty attributes", raw: `{"attributes":{},"certifiers":null}`},
		{name: "too long string", raw: `{"identityKey":"` + oversizedString + `"}`},
		{name: "missing selector", raw: `{}`},
		{name: "non object query", raw: `[]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileQuery(json.RawMessage(test.raw), DefaultQueryPolicy())
			require.ErrorIs(t, err, ErrInvalidQuery)
		})
	}
}
