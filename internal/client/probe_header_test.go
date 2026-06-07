package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeHeaderRoundTripper_InjectsHeaders(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := wrapWithProbeHeaders(http.DefaultTransport)
	ctx := WithProbeHeaders(context.Background(), map[string]string{
		"X-Tingly-Probe-Service": "provider-uuid:gpt-4",
		"X-Tingly-Probe-Rule":    "rule-uuid",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "provider-uuid:gpt-4", gotHeaders.Get("X-Tingly-Probe-Service"))
	assert.Equal(t, "rule-uuid", gotHeaders.Get("X-Tingly-Probe-Rule"))
}

func TestProbeHeaderRoundTripper_NoOpWithoutHeaders(t *testing.T) {
	var gotProbeHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProbeHeader = r.Header.Get("X-Tingly-Probe-Service")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := wrapWithProbeHeaders(http.DefaultTransport)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Empty(t, gotProbeHeader, "probe header must not be present without WithProbeHeaders")
}

func TestGetProbeHeaders_RoundTrip(t *testing.T) {
	headers := map[string]string{"X-Tingly-Probe-Service": "p:m"}
	ctx := WithProbeHeaders(context.Background(), headers)

	got, ok := GetProbeHeaders(ctx)
	assert.True(t, ok)
	assert.Equal(t, "p:m", got["X-Tingly-Probe-Service"])

	_, ok = GetProbeHeaders(context.Background())
	assert.False(t, ok, "empty context must return false")
}

func TestApplyProbeHeadersToClient_OpenAI(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Tingly-Probe-Service")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	httpClient := &http.Client{Transport: http.DefaultTransport}
	oc := &OpenAIClient{HttpClient: httpClient}

	ApplyProbeHeadersToClient(oc)

	ctx := WithProbeHeaders(context.Background(), map[string]string{"X-Tingly-Probe-Service": "p1:m1"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := oc.HttpClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "p1:m1", gotHeader)
}
