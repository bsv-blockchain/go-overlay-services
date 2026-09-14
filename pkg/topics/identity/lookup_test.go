package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lookupFixtureFile struct {
	Fixtures []lookupFixture `json:"fixtures"`
}

type lookupFixture struct {
	Name             string `json:"name"`
	AtomicBEEFBase64 string `json:"atomicBEEFBase64"`
	AtomicBEEFSHA256 string `json:"atomicBEEFSha256"`
	TxID             string `json:"txid"`
}

type testProjection struct {
	records    map[transaction.Outpoint]Record
	upserts    int
	deletes    int
	finds      int
	lastQuery  Query
	upsertErr  error
	deleteErr  error
	findErr    error
	forceFound []transaction.Outpoint
}

func newTestProjection() *testProjection {
	return &testProjection{records: make(map[transaction.Outpoint]Record)}
}

func (p *testProjection) Upsert(ctx context.Context, record Record) error {
	p.upserts++
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.upsertErr != nil {
		return p.upsertErr
	}
	p.records[record.Outpoint] = record
	return nil
}

func (p *testProjection) Delete(ctx context.Context, outpoint transaction.Outpoint) error {
	p.deletes++
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.deleteErr != nil {
		return p.deleteErr
	}
	delete(p.records, outpoint)
	return nil
}

func (p *testProjection) Find(ctx context.Context, query Query) ([]transaction.Outpoint, error) {
	p.finds++
	p.lastQuery = query
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.findErr != nil {
		return nil, p.findErr
	}
	if p.forceFound != nil {
		return append([]transaction.Outpoint(nil), p.forceFound...), nil
	}

	found := make([]transaction.Outpoint, 0, len(p.records))
	for outpoint, record := range p.records {
		if testRecordMatches(query, record) {
			found = append(found, outpoint)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].String() < found[j].String() })
	if query.Offset >= len(found) {
		return []transaction.Outpoint{}, nil
	}
	found = found[query.Offset:]
	if len(found) > query.Limit {
		found = found[:query.Limit]
	}
	return found, nil
}

func testRecordMatches(query Query, record Record) bool {
	switch query.Kind {
	case QueryKindSerialNumber:
		return record.Certificate.SerialNumber == query.SerialNumber
	case QueryKindAttributes:
		for _, predicate := range query.AttributePredicates {
			if predicate.Match != AttributeMatchExact || record.Certificate.Fields[predicate.Field] != predicate.Value {
				return false
			}
		}
	case QueryKindIdentityKeyAndTypes:
		if record.Certificate.Subject != query.IdentityKey || !containsString(query.CertificateTypes, record.Certificate.Type) {
			return false
		}
	case QueryKindIdentityKey:
		if record.Certificate.Subject != query.IdentityKey {
			return false
		}
	case QueryKindCertifiers:
		if !containsString(query.Certifiers, record.Certificate.Certifier) {
			return false
		}
	default:
		return false
	}
	return len(query.Certifiers) == 0 || containsString(query.Certifiers, record.Certificate.Certifier)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestLookupServiceAdmitsFixtureReplayAndRemovesProjection(t *testing.T) {
	atomic, expectedOutpoint := c01AtomicBEEF(t)
	projection := newTestProjection()
	service := newLookupService(t, projection)
	payload := &engine.OutputAdmittedByTopic{Topic: Topic, OutputIndex: 0, AtomicBEEF: atomic}

	require.NoError(t, service.OutputAdmittedByTopic(context.Background(), payload))
	require.Len(t, projection.records, 1)
	record, exists := projection.records[expectedOutpoint]
	require.True(t, exists)
	assert.Equal(t, expectedOutpoint, record.Outpoint)
	assert.Equal(t, "Alice", record.Certificate.Fields["name"])
	assert.Contains(t, record.SearchableAttributes, record.Certificate.Fields["name"])

	// Engine replay must replace the same outpoint rather than create a second row.
	require.NoError(t, service.OutputAdmittedByTopic(context.Background(), payload))
	assert.Equal(t, 2, projection.upserts)
	assert.Len(t, projection.records, 1)

	spent := &engine.OutputSpent{Topic: Topic, Outpoint: &expectedOutpoint}
	require.NoError(t, service.OutputSpent(context.Background(), spent))
	require.NoError(t, service.OutputSpent(context.Background(), spent))
	assert.Empty(t, projection.records)

	require.NoError(t, service.OutputAdmittedByTopic(context.Background(), payload))
	require.NoError(t, service.OutputEvicted(context.Background(), &expectedOutpoint))
	require.NoError(t, service.OutputEvicted(context.Background(), &expectedOutpoint))
	assert.Empty(t, projection.records)
	assert.Equal(t, 4, projection.deletes)
}

func TestLookupServiceIgnoresOtherTopicsAndRejectsInvalidNotifications(t *testing.T) {
	atomic, outpoint := c01AtomicBEEF(t)
	projection := newTestProjection()
	service := newLookupService(t, projection)

	require.NoError(t, service.OutputAdmittedByTopic(context.Background(), &engine.OutputAdmittedByTopic{Topic: "tm_other", AtomicBEEF: []byte{1}}))
	require.NoError(t, service.OutputSpent(context.Background(), &engine.OutputSpent{Topic: "tm_other"}))
	assert.Zero(t, projection.upserts)
	assert.Zero(t, projection.deletes)

	for _, payload := range []*engine.OutputAdmittedByTopic{
		nil,
		{Topic: Topic, AtomicBEEF: []byte{}},
		{Topic: Topic, OutputIndex: 1, AtomicBEEF: atomic},
	} {
		err := service.OutputAdmittedByTopic(context.Background(), payload)
		require.ErrorIs(t, err, ErrInvalidNotification)
	}
	require.ErrorIs(t, service.OutputSpent(context.Background(), nil), ErrInvalidNotification)
	require.ErrorIs(t, service.OutputSpent(context.Background(), &engine.OutputSpent{Topic: Topic}), ErrInvalidNotification)
	require.ErrorIs(t, service.OutputEvicted(context.Background(), nil), ErrInvalidNotification)
	assert.Zero(t, projection.upserts)
	assert.Zero(t, projection.deletes)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.OutputAdmittedByTopic(canceled, &engine.OutputAdmittedByTopic{Topic: Topic, AtomicBEEF: atomic}), context.Canceled)
	require.ErrorIs(t, service.OutputEvicted(canceled, &outpoint), context.Canceled)
	assert.Zero(t, projection.upserts)
	assert.Zero(t, projection.deletes)
}

func TestLookupServiceBuildsFormulaAnswersAndPreservesQueryPrecedence(t *testing.T) {
	projection := newTestProjection()
	first := recordForTest(t, "01", "serial", "subject", "type", "alice")
	second := recordForTest(t, "02", "other", "subject", "second-type", "bob")
	projection.records[first.Outpoint] = first
	projection.records[second.Outpoint] = second
	service := newLookupService(t, projection)
	typed, typedErr := service.Lookup(t.Context(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"identityKey":"subject","certificateTypes":["second-type"]}`)})
	require.NoError(t, typedErr)
	require.Len(t, typed.Formulas, 1)
	require.Equal(t, second.Outpoint, *typed.Formulas[0].Outpoint)

	answer, err := service.Lookup(context.Background(), &lookup.LookupQuestion{
		Service: Service,
		Query:   json.RawMessage(`{"serialNumber":"serial","attributes":{"userName":"ignored"},"identityKey":"ignored"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, QueryKindSerialNumber, projection.lastQuery.Kind)
	assert.Equal(t, lookup.AnswerTypeFormula, answer.Type)
	require.Len(t, answer.Formulas, 1)
	assert.Equal(t, first.Outpoint, *answer.Formulas[0].Outpoint)
	assert.Nil(t, answer.Outputs)
	assert.Nil(t, answer.Result)

	projection.lastQuery = Query{}
	answer, err = service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"attributes":{"userName":"alice"}}`)})
	require.NoError(t, err)
	assert.Equal(t, QueryKindAttributes, projection.lastQuery.Kind)
	require.Len(t, answer.Formulas, 1)

	emptyIdentity := recordForTest(t, "03", "empty", "", "type", "empty")
	projection.records[emptyIdentity.Outpoint] = emptyIdentity
	answer, err = service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"identityKey":"","certifiers":["certifier"]}`)})
	require.NoError(t, err)
	assert.Equal(t, QueryKindIdentityKey, projection.lastQuery.Kind)
	require.Len(t, answer.Formulas, 1)
	assert.Equal(t, emptyIdentity.Outpoint, *answer.Formulas[0].Outpoint)

	projection.forceFound = []transaction.Outpoint{first.Outpoint, second.Outpoint}
	answer, err = service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["certifier"],"limit":2}`)})
	require.NoError(t, err)
	require.Len(t, answer.Formulas, 2)
	assert.NotSame(t, answer.Formulas[0].Outpoint, answer.Formulas[1].Outpoint)
	assert.Equal(t, first.Outpoint, *answer.Formulas[0].Outpoint)
	assert.Equal(t, second.Outpoint, *answer.Formulas[1].Outpoint)
}

func TestLookupServiceRejectsInvalidQueriesBeforeProjection(t *testing.T) {
	projection := newTestProjection()
	service := newLookupService(t, projection)
	tests := []struct {
		name     string
		canceled bool
		question *lookup.LookupQuestion
		want     error
	}{
		{name: "nil question", want: ErrInvalidQuery},
		{name: "wrong service", question: &lookup.LookupQuestion{Service: "other", Query: json.RawMessage(`{"certifiers":["x"]}`)}, want: ErrInvalidQuery},
		{name: "missing raw query", question: &lookup.LookupQuestion{Service: Service}, want: ErrInvalidQuery},
		{name: "unsafe attribute path", question: &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"attributes":{"$where":"x"}}`)}, want: ErrInvalidQuery},
		{name: "limit over cap", question: &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["x"],"limit":10001}`)}, want: ErrInvalidQuery},
		{name: "canceled", canceled: true, question: &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["x"]}`)}, want: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := service.Lookup(ctx, test.question)
			require.ErrorIs(t, err, test.want)
		})
	}
	assert.Zero(t, projection.finds)

	answer, err := service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"attributes":{}}`)})
	require.NoError(t, err)
	assert.Empty(t, answer.Formulas)
	assert.Zero(t, projection.finds)
}

func TestLookupServiceEnforcesProjectionResultContract(t *testing.T) {
	projection := newTestProjection()
	service, err := NewLookupServiceWithPolicies(projection, DefaultAdmissionPolicy(), QueryPolicy{MaxResults: 2, MaxOffset: 3})
	require.NoError(t, err)
	first := recordForTest(t, "11", "one", "subject", "type", "one")
	second := recordForTest(t, "22", "two", "subject", "type", "two")
	third := recordForTest(t, "33", "three", "subject", "type", "three")

	projection.forceFound = []transaction.Outpoint{first.Outpoint, second.Outpoint, third.Outpoint}
	_, err = service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["certifier"]}`)})
	require.ErrorIs(t, err, ErrQueryBudget)

	_, err = service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["certifier"],"limit":2}`)})
	require.ErrorIs(t, err, ErrProjectionContract)

	projection.forceFound = nil
	projection.records[first.Outpoint] = first
	projection.records[second.Outpoint] = second
	answer, err := service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["certifier"],"limit":1}`)})
	require.NoError(t, err)
	require.Len(t, answer.Formulas, 1)
}

func TestLookupServiceWrapsProjectionFailuresAndValidatesConstruction(t *testing.T) {
	projection := newTestProjection()
	projection.findErr = context.DeadlineExceeded
	service := newLookupService(t, projection)
	_, err := service.Lookup(context.Background(), &lookup.LookupQuestion{Service: Service, Query: json.RawMessage(`{"certifiers":["certifier"]}`)})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "identity projection query")

	_, err = NewLookupServiceWithPolicies(nil, DefaultAdmissionPolicy(), DefaultQueryPolicy())
	require.ErrorIs(t, err, ErrProjectionContract)
	var nilProjection *testProjection
	_, err = NewLookupServiceWithPolicies(nilProjection, DefaultAdmissionPolicy(), DefaultQueryPolicy())
	require.ErrorIs(t, err, ErrProjectionContract)
	_, err = NewLookupServiceWithPolicies(newTestProjection(), AdmissionPolicy{}, DefaultQueryPolicy())
	require.ErrorIs(t, err, ErrInvalidPolicy)
	_, err = NewLookupServiceWithPolicies(newTestProjection(), DefaultAdmissionPolicy(), QueryPolicy{})
	require.ErrorIs(t, err, ErrInvalidQueryPolicy)

	policy := DefaultAdmissionPolicy()
	policy.MaxNotificationBytes = 1
	budgeted, err := NewLookupServiceWithPolicies(newTestProjection(), policy, DefaultQueryPolicy())
	require.NoError(t, err)
	atomic, _ := c01AtomicBEEF(t)
	require.ErrorIs(t, budgeted.OutputAdmittedByTopic(context.Background(), &engine.OutputAdmittedByTopic{Topic: Topic, AtomicBEEF: atomic}), ErrAdmissionBudget)
}

func TestLookupServiceMetadataAndHistoryCallbacksAreNoOps(t *testing.T) {
	projection := newTestProjection()
	service := newLookupService(t, projection)
	outpoint := recordForTest(t, "44", "serial", "subject", "type", "name").Outpoint

	firstMetadata := service.GetMetaData()
	secondMetadata := service.GetMetaData()
	require.NotNil(t, firstMetadata)
	assert.NotSame(t, firstMetadata, secondMetadata)
	assert.Equal(t, "Identity Lookup Service", firstMetadata.Name)
	assert.Contains(t, service.GetDocumentation(), "Identity Lookup Service")

	require.NoError(t, service.OutputNoLongerRetainedInHistory(context.Background(), &outpoint, Topic))
	require.NoError(t, service.OutputBlockHeightUpdated(context.Background(), &outpoint.Txid, 1, 2))
	assert.Zero(t, projection.deletes)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.OutputNoLongerRetainedInHistory(canceled, &outpoint, Topic), context.Canceled)
	require.ErrorIs(t, service.OutputBlockHeightUpdated(canceled, &outpoint.Txid, 1, 2), context.Canceled)
	assert.Zero(t, projection.deletes)
}

func newLookupService(t *testing.T, projection Projection) *LookupService {
	t.Helper()
	service, err := NewLookupService(projection)
	require.NoError(t, err)
	return service
}

func c01AtomicBEEF(t *testing.T) ([]byte, transaction.Outpoint) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", "data", "identity-topic-fixtures.json"))
	require.NoError(t, err)
	var fixtureFile lookupFixtureFile
	require.NoError(t, json.Unmarshal(contents, &fixtureFile))
	for _, fixture := range fixtureFile.Fixtures {
		if fixture.Name != "c01-confirmed-signed-spend" {
			continue
		}
		atomic, err := base64.StdEncoding.DecodeString(fixture.AtomicBEEFBase64)
		require.NoError(t, err)
		digest := sha256.Sum256(atomic)
		require.Equal(t, fixture.AtomicBEEFSHA256, hex.EncodeToString(digest[:]))
		beef, parsedTxID, err := transaction.NewBeefFromAtomicBytes(atomic)
		require.NoError(t, err)
		require.NotNil(t, beef)
		txid, err := chainhash.NewHashFromHex(fixture.TxID)
		require.NoError(t, err)
		require.Equal(t, txid.String(), parsedTxID.String())
		return atomic, transaction.Outpoint{Txid: *txid, Index: 0}
	}
	t.Fatal("c01-confirmed-signed-spend fixture is missing")
	return nil, transaction.Outpoint{}
}

func recordForTest(t *testing.T, byteValue, serial, subject, certificateType, userName string) Record {
	t.Helper()
	hash, err := chainhash.NewHashFromHex(strings.Repeat(byteValue, chainhash.HashSize))
	require.NoError(t, err)
	return Record{
		Outpoint: transaction.Outpoint{Txid: *hash},
		Certificate: Certificate{
			Type: certificateType, SerialNumber: serial, Subject: subject, Certifier: "certifier",
			Fields: map[string]string{"userName": userName},
		},
	}
}
