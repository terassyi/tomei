package age

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"time"
)

// lastModifiedFetcher resolves SourceLastModified keys by HEAD'ing the
// URL and reading the Last-Modified header. Falls back to a 1-byte
// Range GET if the server returns 405 or 501.
type lastModifiedFetcher struct {
	client        *http.Client
	timeout       time.Duration
	allowLoopback bool // test-only
}

// httpStatusError carries the HTTP status code from a non-2xx response
// so callers can match on specific codes via errors.As. Used to
// distinguish 405/501 (HEAD-not-supported, retry with GET) from other
// failure modes that should propagate as-is.
type httpStatusError struct{ Code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("http %d", e.Code) }

func (l *lastModifiedFetcher) Fetch(ctx context.Context, key Key) (time.Time, bool, error) {
	if key.Source != SourceLastModified || key.URL == "" {
		return time.Time{}, false, nil
	}
	// Entry SSRF gate — runs BEFORE any socket is opened. The
	// http.Client's CheckRedirect covers redirect targets but the
	// initial URL also needs to pass.
	u, err := url.Parse(key.URL)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse %q: %w", key.URL, err)
	}
	if err := validateRedirectURL(u, l.allowLoopback); err != nil {
		return time.Time{}, false, err
	}

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	// 1) HEAD
	t, headErr := tryLastModified(ctx, l.client, http.MethodHead, key.URL, nil)
	if headErr == nil && !t.IsZero() {
		return t, true, nil
	}
	var herr *httpStatusError
	switch {
	case headErr != nil && !errors.As(headErr, &herr):
		// Network / parse failure — surface as-is.
		return time.Time{}, false, headErr
	case headErr != nil && herr.Code != http.StatusMethodNotAllowed && herr.Code != http.StatusNotImplemented:
		// Other 4xx/5xx — propagate.
		return time.Time{}, false, headErr
	}
	// headErr is either nil (HEAD 2xx but no Last-Modified header) or
	// 405/501. Either way try the Range GET — some CDNs only emit
	// Last-Modified on body responses.
	t, getErr := tryLastModified(ctx, l.client, http.MethodGet, key.URL,
		http.Header{"Range": []string{"bytes=0-0"}})
	if getErr != nil {
		return time.Time{}, false, getErr
	}
	if t.IsZero() {
		return time.Time{}, false, nil
	}
	return t, true, nil
}

// tryLastModified issues one HTTP request and extracts Last-Modified.
// Returns the parsed time + nil err on a 2xx/206 response with a
// well-formed header. Returns zero time + nil err on a 2xx/206 with no
// Last-Modified (caller decides whether to retry with a different
// method). Returns a wrapped *httpStatusError on any non-2xx/206 status.
func tryLastModified(ctx context.Context, client *http.Client, method, rawURL string, hdr http.Header) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("create request: %w", err)
	}
	// maps.Copy preserves all values per key (Set + nested loop would
	// silently drop all but the last; direct map assignment is what the
	// modernize linter prefers via this helper). Today only
	// Range: bytes=0-0 is sent, so it's effectively a single-key set.
	maps.Copy(req.Header, hdr)
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %s: %w", method, rawURL, err)
	}
	defer func() {
		// Drain a small amount so the conn can be reused for Range GET
		// fallback; ignore errors (we only care about headers).
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return time.Time{}, &httpStatusError{Code: resp.StatusCode}
	}
	lm := resp.Header.Get("Last-Modified")
	if lm == "" {
		return time.Time{}, nil
	}
	t, perr := http.ParseTime(lm)
	if perr != nil {
		return time.Time{}, fmt.Errorf("parse Last-Modified %q: %w", lm, perr)
	}
	return t, nil
}

// validateRedirectURL enforces the package's SSRF policy: HTTPS scheme
// only, and reject IP literals that point at private / loopback /
// link-local / multicast / unspecified ranges. DNS names are passed
// through (no resolution — avoids TOCTOU and the latency of an extra
// DNS round-trip; CUE manifests are trusted as the security boundary).
//
// Called both at the entry of lastModifiedFetcher.Fetch and from the
// http.Client.CheckRedirect installed by New(). Single source of truth
// for the gate.
//
// allowLoopback is set only by the test-only withAllowLoopback option
// so httptest.NewTLSServer (which binds to 127.0.0.1) can be exercised
// by happy-path tests. Production code paths never set it.
func validateRedirectURL(u *url.URL, allowLoopback bool) error {
	if u == nil {
		return errors.New("nil url")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("non-https scheme %q rejected", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("empty host")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// DNS name — pass through.
		return nil
	}
	if allowLoopback && ip.IsLoopback() {
		return nil
	}
	switch {
	case ip.IsPrivate():
		return fmt.Errorf("private IP %s rejected", ip)
	case ip.IsLoopback():
		return fmt.Errorf("loopback IP %s rejected", ip)
	case ip.IsLinkLocalUnicast():
		return fmt.Errorf("link-local IP %s rejected", ip)
	case ip.IsMulticast():
		return fmt.Errorf("multicast IP %s rejected", ip)
	case ip.IsUnspecified():
		return fmt.Errorf("unspecified IP %s rejected", ip)
	}
	return nil
}
