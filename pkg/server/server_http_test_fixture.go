package server

import (
	"net/http"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

// fiberRoundTripper is a custom http.RoundTripper that routes requests through the in-memory Fiber test server.
// It is used to simulate HTTP requests directly against the server without starting a real network listener.
type fiberRoundTripper struct {
	t       *testing.T
	srv     *HTTP
	timeout int
}

// RoundTrip executes an HTTP request using Fiber’s in-memory test engine.
// It returns the response or fails the test if an error occurs.
func (f *fiberRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	// fasthttp v1.70+ requires a Host header per HTTP/1.1 spec (CVE-2026-5160).
	// Test requests built from relative URLs have no host, so we inject one.
	if req.Host == "" {
		req.Host = "localhost"
	}
	return f.srv.app.Test(req, f.timeout)
}

// TestFixture provides an isolated test fixture for executing HTTP requests
// against a fully initialized server instance using in-memory transport.
type TestFixture struct {
	t            *testing.T
	roundTripper http.RoundTripper
}

// Client returns a preconfigured Resty client that uses the in-memory test server.
// It automatically fails the test on unexpected transport errors.
func (f *TestFixture) Client() *resty.Client {
	f.t.Helper()

	c := resty.New()
	c.OnError(func(_ *resty.Request, err error) {
		require.NoError(f.t, err, "HTTP request ended with unexpected error")
	})
	c.GetClient().Transport = f.roundTripper

	return c
}

// NewTestFixture creates a new test fixture with a fully initialized server instance
// and a custom in-memory HTTP round tripper. Panics if server initialization fails.
func NewTestFixture(t *testing.T, opts ...Option) *TestFixture {
	return &TestFixture{
		t: t,
		roundTripper: &fiberRoundTripper{
			t:       t,
			timeout: -1,
			srv:     New(opts...),
		},
	}
}
