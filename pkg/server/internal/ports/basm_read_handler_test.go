package ports

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
)

func TestBASMReadHandlerRejectsExplicitInvalidLimitsBeforeProvider(t *testing.T) {
	provider := newHandlerProvider()
	limits := basm.DefaultReadLimits()
	limits.MaxResponseBytes = 0
	handler := NewBASMReadHandler(provider, limits)

	status, code := callBASMHandler(t, handler, "tip", true, "tm", `{}`)
	assert.Equal(t, fiber.StatusInternalServerError, status)
	assert.Equal(t, "invalid_data", code)
	assert.Zero(t, provider.calls)
}

func TestBASMReadHandlerBoundsEscapedTopicAndResponseBeforeMarshal(t *testing.T) {
	provider := newHandlerProvider()
	limits := basm.DefaultReadLimits()
	limits.MaxTopicBytes = 16
	limits.MaxResponseBytes = 200
	handler := NewBASMReadHandler(provider, limits)
	topic := strings.Repeat("<", 12) // encoding/json emits each as a six-byte escape.
	provider.tip = basm.TopicAnchorTip{Topic: topic, BlockHeight: -1}

	status, code := callBASMHandler(t, handler, "tip", true, topic, `{}`)
	assert.Equal(t, fiber.StatusRequestEntityTooLarge, status)
	assert.Equal(t, "limit_exceeded", code)
}

func TestBASMReadHandlerRejectsMalformedProviderResponses(t *testing.T) {
	txid := testBASMHash(1)
	other := testBASMHash(2)
	tests := []struct {
		name   string
		kind   string
		topic  bool
		body   string
		mutate func(*handlerProvider)
	}{
		{
			name: "unsafe tip height", kind: "tip", topic: true, body: `{}`,
			mutate: func(p *handlerProvider) { p.tip = basm.TopicAnchorTip{Topic: "tm", BlockHeight: int64(1) << 32} },
		},
		{
			name: "reordered range", kind: "range", topic: true, body: `{"fromHeight":0,"toHeight":1}`,
			mutate: func(p *handlerProvider) {
				p.rangeResponse.Anchors = []basm.Anchor{handlerAnchor("tm", 1), handlerAnchor("tm", 0)}
			},
		},
		{
			name: "null range array", kind: "range", topic: true, body: `{"fromHeight":0,"toHeight":0}`,
			mutate: func(p *handlerProvider) { p.rangeResponse.Anchors = nil },
		},
		{
			name: "null admitted array", kind: "list", topic: true, body: `{"blockHeight":0}`,
			mutate: func(p *handlerProvider) { p.list.Admitted = nil },
		},
		{
			name: "duplicate admitted transaction", kind: "list", topic: true, body: `{"blockHeight":0}`,
			mutate: func(p *handlerProvider) {
				p.list.Admitted = []basm.AdmittedTxRef{{TxID: txid, BlockIndex: 0}, {TxID: txid, BlockIndex: 1}}
			},
		},
		{
			name: "unsafe admitted index", kind: "list", topic: true, body: `{"blockHeight":0}`,
			mutate: func(p *handlerProvider) {
				p.list.Admitted = []basm.AdmittedTxRef{{TxID: txid, BlockIndex: basm.MaxSafeJSONInteger + 1}}
			},
		},
		{
			name: "reordered proof txids", kind: "proof", topic: true, body: `{"blockHeight":0,"txids":["` + txid.String() + `","` + other.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.proof.TxIDs = []basm.Hash{other, txid} },
		},
		{
			name: "uppercase proof hex", kind: "proof", topic: true, body: `{"blockHeight":0,"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.proof.MerklePath = "AA" },
		},
		{
			name: "odd proof hex", kind: "proof", topic: true, body: `{"blockHeight":0,"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.proof.MerklePath = "0" },
		},
		{
			name: "empty proof hex", kind: "proof", topic: true, body: `{"blockHeight":0,"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.proof.MerklePath = "" },
		},
		{
			name: "null raw arrays", kind: "raw", body: `{"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.raw.Transactions, p.raw.Missing = nil, nil },
		},
		{
			name: "unknown raw mapping", kind: "raw", body: `{"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.raw.Transactions = []basm.RawTransactionRecord{{TxID: other, RawTx: "00"}} },
		},
		{
			name: "duplicate raw mapping", kind: "raw", body: `{"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) {
				p.raw.Transactions = []basm.RawTransactionRecord{{TxID: txid, RawTx: "00"}}
				p.raw.Missing = []basm.Hash{txid}
			},
		},
		{
			name: "missing raw mapping", kind: "raw", body: `{"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) {
				p.raw.Transactions, p.raw.Missing = []basm.RawTransactionRecord{}, []basm.Hash{}
			},
		},
		{
			name: "nonlowercase raw hex", kind: "raw", body: `{"txids":["` + txid.String() + `"]}`,
			mutate: func(p *handlerProvider) { p.raw.Transactions[0].RawTx = "AA" },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newHandlerProvider()
			provider.proof.TxIDs = []basm.Hash{txid}
			provider.raw.Transactions = []basm.RawTransactionRecord{{TxID: txid, RawTx: "00"}}
			provider.raw.Missing = []basm.Hash{}
			test.mutate(provider)
			status, code := callBASMHandler(t, NewBASMReadHandler(provider, basm.DefaultReadLimits()), test.kind, test.topic, "tm", test.body)
			assert.Equal(t, fiber.StatusInternalServerError, status)
			assert.Equal(t, "invalid_data", code)
		})
	}
}

func TestBASMReadHandlerAcceptsShapeValidRawAndProofPlaceholders(t *testing.T) {
	txid := testBASMHash(1)
	provider := newHandlerProvider()
	provider.proof.TxIDs = []basm.Hash{txid}
	provider.proof.MerklePath = "00"
	provider.raw = basm.RawTransactions{Transactions: []basm.RawTransactionRecord{{TxID: txid, RawTx: "00"}}, Missing: []basm.Hash{}}
	handler := NewBASMReadHandler(provider, basm.DefaultReadLimits())

	status, code := callBASMHandler(t, handler, "proof", true, "tm", `{"blockHeight":0,"txids":["`+txid.String()+`"]}`)
	assert.Equal(t, fiber.StatusOK, status)
	assert.Empty(t, code)
	provider.tip = basm.TopicAnchorTip{Topic: "tm", BlockHeight: 0}
	status, code = callBASMHandler(t, handler, "tip", true, "tm", `{}`)
	assert.Equal(t, fiber.StatusOK, status)
	assert.Empty(t, code)
	status, code = callBASMHandler(t, handler, "raw", false, "", `{"txids":["`+txid.String()+`"]}`)
	assert.Equal(t, fiber.StatusOK, status)
	assert.Empty(t, code)
}

func TestBASMResponseBudgetCoversJSONForLargestRecordShapes(t *testing.T) {
	first, second := testBASMHash(1), testBASMHash(2)
	raw := basm.RawTransactions{
		Transactions: []basm.RawTransactionRecord{{TxID: first, RawTx: "00"}, {TxID: second, RawTx: "aabb"}},
		Missing:      []basm.Hash{},
	}
	rawBudget := responseBudget{limit: uint64(^uint32(0)), ok: true}
	rawBudget.add(48)
	for _, record := range raw.Transactions {
		rawBudget.add(128)
		rawBudget.addLiteralString(record.RawTx)
	}
	rawBudget.addRepeated(uint64(len(raw.Missing)), 67)
	assertBudgetCoversJSON(t, rawBudget, raw)

	list := basm.AdmittedList{
		Topic:       "<topic>",
		BlockHeight: 1,
		BlockHash:   &first,
		Admitted:    []basm.AdmittedTxRef{{TxID: first, BlockIndex: basm.MaxSafeJSONInteger}, {TxID: second, BlockIndex: basm.MaxSafeJSONInteger - 1}},
	}
	listBudget := responseBudget{limit: uint64(^uint32(0)), ok: true}
	listBudget.add(112)
	listBudget.addString(list.Topic)
	listBudget.add(66)
	listBudget.addRepeated(uint64(len(list.Admitted)), 128)
	assertBudgetCoversJSON(t, listBudget, list)
}

func assertBudgetCoversJSON(t *testing.T, budget responseBudget, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.True(t, budget.ok)
	assert.GreaterOrEqual(t, budget.size, uint64(len(encoded)))
}

type handlerProvider struct {
	calls         int
	tip           basm.TopicAnchorTip
	rangeResponse basm.TopicAnchorRange
	list          basm.AdmittedList
	proof         basm.CompoundMerklePath
	raw           basm.RawTransactions
}

func newHandlerProvider() *handlerProvider {
	hash := testBASMHash(9)
	return &handlerProvider{
		tip:           basm.TopicAnchorTip{Topic: "tm", BlockHeight: -1},
		rangeResponse: basm.TopicAnchorRange{Topic: "tm", Anchors: []basm.Anchor{handlerAnchor("tm", 0)}},
		list:          basm.AdmittedList{Topic: "tm", BlockHeight: 0, BlockHash: &hash, Admitted: []basm.AdmittedTxRef{}},
		proof:         basm.CompoundMerklePath{Topic: "tm", BlockHeight: 0, TxIDs: []basm.Hash{}, MerklePath: "00"},
		raw:           basm.RawTransactions{Transactions: []basm.RawTransactionRecord{}, Missing: []basm.Hash{}},
	}
}

func (p *handlerProvider) ProvideTopicAnchorTip(_ context.Context, _ string) (basm.TopicAnchorTip, error) {
	p.calls++
	return p.tip, nil
}

func (p *handlerProvider) ProvideTopicAnchorRange(_ context.Context, _ string, _, _ uint32) (basm.TopicAnchorRange, error) {
	p.calls++
	return p.rangeResponse, nil
}

func (p *handlerProvider) ProvideAdmittedList(_ context.Context, _ string, _ uint32, _ *basm.Hash) (basm.AdmittedList, error) {
	p.calls++
	return p.list, nil
}

func (p *handlerProvider) ProvideCompoundMerklePath(_ context.Context, _ string, _ uint32, _ []basm.Hash) (basm.CompoundMerklePath, error) {
	p.calls++
	return p.proof, nil
}

func (p *handlerProvider) ProvideRawTransactions(_ context.Context, _ []basm.Hash) (basm.RawTransactions, error) {
	p.calls++
	return p.raw, nil
}

func handlerAnchor(topic string, height uint32) basm.Anchor {
	return basm.Anchor{TopicBlockAnchor: basm.TopicBlockAnchor{Topic: topic, BlockHeight: height}}
}

func testBASMHash(value byte) basm.Hash {
	var hash basm.Hash
	for index := range hash {
		hash[index] = value
	}
	return hash
}

func callBASMHandler(t *testing.T, handler *BASMReadHandler, kind string, topicRequired bool, topic, body string) (int, string) {
	t.Helper()
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error { return handler.Handle(c, kind, topicRequired) })
	request := httptest.NewRequestWithContext(context.Background(), "POST", "/", strings.NewReader(body))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	if topicRequired {
		request.Header.Set("x-bsv-topic", topic)
	}
	response, err := app.Test(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	var errorBody basmErrorResponse
	_ = json.NewDecoder(response.Body).Decode(&errorBody)
	return response.StatusCode, errorBody.Code
}
