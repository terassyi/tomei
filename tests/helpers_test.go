//go:build integration

package tests

import "net/http"

// roundTripFunc is a helper for mocking http.RoundTripper in integration tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
