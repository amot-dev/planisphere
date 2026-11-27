// Package httptestutil provides shared HTTP RoundTripper adapters and stub helpers for backend tests.
package httptestutil

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// RoundTripFunc adapts a function to the http.RoundTripper interface.
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip executes transport against request.
func (transport RoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// NewClient returns an *http.Client backed by roundTrip without network sockets.
func NewClient(roundTrip RoundTripFunc) *http.Client {
	return &http.Client{Transport: roundTrip}
}

// StubDefaultTransport replaces http.DefaultTransport for the duration of testingContext and returns the prior transport.
func StubDefaultTransport(testingContext *testing.T, roundTrip RoundTripFunc) http.RoundTripper {
	testingContext.Helper()
	originalTransport := http.DefaultTransport
	testingContext.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})
	http.DefaultTransport = roundTrip
	return originalTransport
}

// Response constructs an in-memory *http.Response with statusCode and body.
func Response(statusCode int, body string) *http.Response {
	statusText := http.StatusText(statusCode)
	statusLine := fmt.Sprintf("%d", statusCode)
	if statusText != "" {
		statusLine = fmt.Sprintf("%d %s", statusCode, statusText)
	}
	return &http.Response{
		StatusCode: statusCode,
		Status:     statusLine,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
