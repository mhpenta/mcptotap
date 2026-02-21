package mcptotap

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureTransport struct {
	gotHeader http.Header
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.gotHeader = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    req,
	}, nil
}

func TestBuildHTTPClientAppliesBearerAndAPIKey(t *testing.T) {
	capture := &captureTransport{}
	hc := buildHTTPClient(remoteConfig{
		bearerToken: "token123",
		xAPIKey:     "key123",
		httpClient:  &http.Client{Transport: capture},
	})

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := hc.Do(req); err != nil {
		t.Fatalf("do request: %v", err)
	}

	if got := capture.gotHeader.Get("Authorization"); got != "Bearer token123" {
		t.Fatalf("authorization header = %q, want %q", got, "Bearer token123")
	}
	if got := capture.gotHeader.Get("x-api-key"); got != "key123" {
		t.Fatalf("x-api-key header = %q, want %q", got, "key123")
	}
}
