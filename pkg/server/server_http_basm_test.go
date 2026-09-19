package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/adapters"
)

const basmTestHash = "0000000000000000000000000000000000000000000000000000000000000000"

var errPrivateProviderDetail = errors.New("private detail")

// sourceBASMProvider is a deterministic source-backed test double. It only
// exercises HTTP adaptation; it makes no durability or chain-verification claim.
type sourceBASMProvider struct {
	err   error
	rawTx string
	wait  bool
}

func (p sourceBASMProvider) ProvideTopicAnchorTip(ctx context.Context, topic string) (basm.TopicAnchorTip, error) {
	if err := p.contextError(ctx); err != nil {
		return basm.TopicAnchorTip{}, err
	}
	return basm.TopicAnchorTip{Topic: topic, BlockHeight: -1}, p.err
}

func (p sourceBASMProvider) ProvideTopicAnchorRange(ctx context.Context, topic string, from, to uint32) (basm.TopicAnchorRange, error) {
	if err := p.contextError(ctx); err != nil {
		return basm.TopicAnchorRange{}, err
	}
	anchors := make([]basm.Anchor, 0, to-from+1)
	for height := from; height <= to; height++ {
		anchors = append(anchors, basm.Anchor{TopicBlockAnchor: basm.TopicBlockAnchor{Topic: topic, BlockHeight: height}})
		if height == to {
			break
		}
	}
	return basm.TopicAnchorRange{Topic: topic, Anchors: anchors}, p.err
}

func (p sourceBASMProvider) ProvideAdmittedList(ctx context.Context, topic string, height uint32, _ *basm.Hash) (basm.AdmittedList, error) {
	if err := p.contextError(ctx); err != nil {
		return basm.AdmittedList{}, err
	}
	return basm.AdmittedList{Topic: topic, BlockHeight: height, Admitted: []basm.AdmittedTxRef{}}, p.err
}

func (p sourceBASMProvider) ProvideCompoundMerklePath(ctx context.Context, topic string, height uint32, txids []basm.Hash) (basm.CompoundMerklePath, error) {
	if err := p.contextError(ctx); err != nil {
		return basm.CompoundMerklePath{}, err
	}
	return basm.CompoundMerklePath{Topic: topic, BlockHeight: height, TxIDs: txids, MerklePath: "00"}, p.err
}

func (p sourceBASMProvider) ProvideRawTransactions(ctx context.Context, txids []basm.Hash) (basm.RawTransactions, error) {
	if err := p.contextError(ctx); err != nil {
		return basm.RawTransactions{}, err
	}
	rawTx := p.rawTx
	if rawTx == "" {
		rawTx = "00"
	}
	transactions := make([]basm.RawTransactionRecord, 0, len(txids))
	for _, txid := range txids {
		transactions = append(transactions, basm.RawTransactionRecord{TxID: txid, RawTx: rawTx})
	}
	return basm.RawTransactions{Transactions: transactions, Missing: []basm.Hash{}}, p.err
}

func (p sourceBASMProvider) contextError(ctx context.Context) error {
	if !p.wait {
		return p.err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestBASMHTTPRoutesServeAllReadShapes(t *testing.T) {
	app := newBASMTestApp(t, sourceBASMProvider{}, basm.DefaultReadLimits(), "/api/v1")
	tests := []struct {
		name  string
		path  string
		body  string
		topic bool
	}{
		{name: "tip", path: "/requestTopicAnchorTip", body: "{}", topic: true},
		{name: "range", path: "/requestTopicAnchorRange", body: `{"fromHeight":4,"toHeight":5}`, topic: true},
		{name: "admitted list", path: "/requestAdmittedList", body: `{"blockHeight":4}`, topic: true},
		{name: "compound merkle path", path: "/requestCompoundMerklePath", body: `{"blockHeight":4,"txids":["` + basmTestHash + `"]}`, topic: true},
		{name: "raw transactions without topic", path: "/requestRawTransactions", body: `{"txids":["` + basmTestHash + `"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := basmRequest("/api/v1"+tt.path, tt.body)
			if tt.topic {
				request.Header.Set("x-bsv-topic", "topic")
			}
			response := testBASMRequest(t, app, request)
			assert.Equal(t, fiber.StatusOK, response.StatusCode)
			var body map[string]any
			require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
			assert.NotEqual(t, "error", body["status"])
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestBASMHTTPRoutesReturnRedactedBoundedErrors(t *testing.T) {
	limits := basm.DefaultReadLimits()
	limits.MaxRequestBytes = 128
	limits.MaxResponseBytes = 128
	app := newBASMTestApp(t, sourceBASMProvider{rawTx: strings.Repeat("aa", 128)}, limits, "")
	tests := []struct {
		name   string
		path   string
		body   string
		header func(*http.Request)
		status int
	}{
		{name: "unsupported default", path: "/requestTopicAnchorTip", body: "{}", header: addTopic, status: fiber.StatusNotImplemented},
		{name: "missing topic", path: "/requestTopicAnchorTip", body: "{}", status: fiber.StatusBadRequest},
		{name: "duplicate topic", path: "/requestTopicAnchorTip", body: "{}", header: func(r *http.Request) { r.Header.Add("x-bsv-topic", "one"); r.Header.Add("x-bsv-topic", "two") }, status: fiber.StatusBadRequest},
		{name: "malformed json", path: "/requestRawTransactions", body: "{]", status: fiber.StatusBadRequest},
		{name: "malformed range", path: "/requestTopicAnchorRange", body: `{"fromHeight":5,"toHeight":4}`, header: addTopic, status: fiber.StatusBadRequest},
		{name: "malformed txids", path: "/requestRawTransactions", body: `{"txids":"bad"}`, status: fiber.StatusBadRequest},
		{name: "body too large", path: "/requestRawTransactions", body: strings.Repeat("x", 129), status: fiber.StatusRequestEntityTooLarge},
		{name: "response too large", path: "/requestRawTransactions", body: `{"txids":["` + basmTestHash + `"]}`, status: fiber.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := basmRequest(tt.path, tt.body)
			if tt.header != nil {
				tt.header(request)
			}
			if tt.name == "unsupported default" {
				response := testBASMRequest(t, newBASMTestApp(t, nil, basm.DefaultReadLimits(), ""), request)
				assertBASMError(t, response, tt.status)
				require.NoError(t, response.Body.Close())
				return
			}
			response := testBASMRequest(t, app, request)
			assertBASMError(t, response, tt.status)
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestBASMHTTPRoutesTreatTypedNilProviderAsUnsupported(t *testing.T) {
	var provider *engine.BASMReadService
	tests := []struct {
		name string
		app  *fiber.App
		path string
	}{
		{
			name: "register routes config",
			app:  newBASMTestApp(t, provider, basm.DefaultReadLimits(), ""),
			path: "/requestTopicAnchorTip",
		},
		{
			name: "with BASM provider",
			app:  New(WithBASMProvider(provider)).app,
			path: "/api/v1/requestTopicAnchorTip",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := basmRequest(tt.path, `{}`)
			addTopic(request)
			response := testBASMRequest(t, tt.app, request)
			assertBASMError(t, response, fiber.StatusNotImplemented)
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestBASMHTTPRoutesMapProviderFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "not ready", err: engine.ErrBASMNotReady, status: fiber.StatusServiceUnavailable},
		{name: "not found", err: engine.ErrBASMNotFound, status: fiber.StatusNotFound},
		{name: "invalid data", err: engine.ErrBASMInvalidData, status: fiber.StatusInternalServerError},
		{name: "wrapped unsupported", err: errors.Join(errPrivateProviderDetail, engine.ErrBASMUnsupported), status: fiber.StatusNotImplemented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newBASMTestApp(t, sourceBASMProvider{err: tt.err}, basm.DefaultReadLimits(), "")
			request := basmRequest("/requestTopicAnchorTip", "{}")
			addTopic(request)
			response := testBASMRequest(t, app, request)
			assertBASMError(t, response, tt.status)
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestBASMHTTPRoutesHonorRequestContextAndPrefix(t *testing.T) {
	limits := basm.DefaultReadLimits()
	limits.RequestTimeout = time.Millisecond
	timeoutApp := newBASMTestApp(t, sourceBASMProvider{wait: true}, limits, "")
	request := basmRequest("/requestTopicAnchorTip", "{}")
	addTopic(request)
	response := testBASMRequest(t, timeoutApp, request)
	assertBASMError(t, response, fiber.StatusGatewayTimeout)
	require.NoError(t, response.Body.Close())

	cancelledApp := fiber.New()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledApp.Use(func(c *fiber.Ctx) error { c.SetUserContext(cancelled); return c.Next() })
	RegisterRoutes(cancelledApp, &RegisterRoutesConfig{Engine: adapters.NewNoopEngineProvider(), BASMProvider: sourceBASMProvider{}, BASMLimits: basm.DefaultReadLimits()})
	request = basmRequest("/requestTopicAnchorTip", "{}")
	addTopic(request)
	response = testBASMRequest(t, cancelledApp, request)
	assertBASMError(t, response, fiber.StatusRequestTimeout)
	require.NoError(t, response.Body.Close())

	prefixed := newBASMTestApp(t, sourceBASMProvider{}, basm.DefaultReadLimits(), "/prefix")
	request = basmRequest("/prefix/requestRawTransactions", `{"txids":[]}`)
	response = testBASMRequest(t, prefixed, request)
	assert.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
}

func TestBASMHTTPRoutesSupportCORSAndLargeValidRawResponse(t *testing.T) {
	app := newBASMTestApp(t, sourceBASMProvider{rawTx: strings.Repeat("aa", 8<<20+1)}, basm.DefaultReadLimits(), "")
	request := basmRequest("/requestRawTransactions", `{"txids":["`+basmTestHash+`"]}`)
	response := testBASMRequest(t, app, request)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Greater(t, len(body), 16<<20)
	require.NoError(t, response.Body.Close())

	preflight := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/requestTopicAnchorTip", nil)
	preflight.Header.Set("Origin", "https://example.test")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "x-bsv-topic")
	response = testBASMRequest(t, app, preflight)
	assert.Equal(t, fiber.StatusNoContent, response.StatusCode)
	assert.Equal(t, "*", response.Header.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, response.Header.Get("Access-Control-Allow-Credentials"))
	require.NoError(t, response.Body.Close())
}

func TestBASMHTTPRoutesLeaveAdminAuthenticationInPlace(t *testing.T) {
	app := newBASMTestApp(t, sourceBASMProvider{}, basm.DefaultReadLimits(), "")
	request := basmRequest("/admin/startGASPSync", "")
	response := testBASMRequest(t, app, request)
	assert.Equal(t, fiber.StatusUnauthorized, response.StatusCode)
	require.NoError(t, response.Body.Close())
}

func newBASMTestApp(t *testing.T, provider engine.BASMProvider, limits basm.ReadLimits, baseURL string) *fiber.App {
	t.Helper()
	return RegisterRoutesWithErrorHandler(fiber.New(), &RegisterRoutesConfig{
		AdminBearerToken: "admin-token",
		Engine:           adapters.NewNoopEngineProvider(),
		BASMProvider:     provider,
		BASMLimits:       limits,
		OctetStreamLimit: 1 << 20,
		BaseURL:          baseURL,
	})
}

func basmRequest(path, body string) *http.Request {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return request
}

func addTopic(request *http.Request) { request.Header.Set("x-bsv-topic", "topic") }

func testBASMRequest(t *testing.T, app *fiber.App, request *http.Request) *http.Response {
	t.Helper()
	response, err := app.Test(request, -1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func assertBASMError(t *testing.T, response *http.Response, status int) {
	t.Helper()
	assert.Equal(t, status, response.StatusCode)
	var body struct {
		Status  string `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	assert.Equal(t, "error", body.Status)
	assert.NotEmpty(t, body.Code)
	assert.NotEmpty(t, body.Message)
	assert.NotContains(t, body.Message, "sourceBASMProvider")
}

var _ engine.BASMProvider = sourceBASMProvider{}
