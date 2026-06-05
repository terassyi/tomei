package age

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestValidateRedirectURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		wantErr bool
		errSub  string
	}{
		// Rejected — non-HTTPS / private / loopback / link-local / multicast / unspecified.
		{raw: "http://example.com", wantErr: true, errSub: "non-https"},
		{raw: "https://192.168.0.1", wantErr: true, errSub: "private"},
		{raw: "https://10.0.0.1", wantErr: true, errSub: "private"},
		{raw: "https://172.16.0.1", wantErr: true, errSub: "private"},
		{raw: "https://127.0.0.1", wantErr: true, errSub: "loopback"},
		{raw: "https://169.254.0.1", wantErr: true, errSub: "link-local"},
		{raw: "https://224.0.0.1", wantErr: true, errSub: "multicast"},
		{raw: "https://0.0.0.0", wantErr: true, errSub: "unspecified"},
		{raw: "https://[::1]", wantErr: true, errSub: "loopback"},
		{raw: "https://[fe80::1]", wantErr: true, errSub: "link-local"},
		{raw: "https://[fc00::1]", wantErr: true, errSub: "private"},
		{raw: "https://[ff02::1]", wantErr: true, errSub: "multicast"},
		// Allowed — public IP, public DNS name.
		{raw: "https://example.com"},
		{raw: "https://8.8.8.8"},
		{raw: "https://api.github.com/repos/x/y/releases/tags/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			err := validateRedirectURL(mustURL(t, tt.raw), false)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSub)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("err=%q, want containing %q", err, tt.errSub)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateRedirectURL_AllowLoopback(t *testing.T) {
	t.Parallel()
	// With allowLoopback=true (test-only escape), 127.0.0.1 / [::1] pass.
	for _, raw := range []string{"https://127.0.0.1", "https://[::1]"} {
		if err := validateRedirectURL(mustURL(t, raw), true); err != nil {
			t.Errorf("%s should be allowed with loopback escape: %v", raw, err)
		}
	}
	// allowLoopback does NOT bypass other rejections.
	if err := validateRedirectURL(mustURL(t, "https://192.168.0.1"), true); err == nil {
		t.Errorf("private IP must still be rejected even with allowLoopback")
	}
}

// lastModifiedFetcherForTest constructs a fetcher pointed at a TLS test
// server: uses the server's TLS-trusting client and allows loopback.
func lastModifiedFetcherForTest(srv *httptest.Server) *lastModifiedFetcher {
	return &lastModifiedFetcher{
		client:        srv.Client(),
		timeout:       5 * time.Second,
		allowLoopback: true,
	}
}

const fixedLastModified = "Wed, 21 Oct 2015 07:28:00 GMT"

func mustParse(t *testing.T, layout, s string) time.Time {
	t.Helper()
	v, err := time.Parse(layout, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func TestLastModifiedFetcher_Fetch(t *testing.T) {
	t.Parallel()
	want := mustParse(t, http.TimeFormat, fixedLastModified)

	t.Run("HEAD 200 with Last-Modified", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead {
				t.Errorf("expected HEAD, got %s", r.Method)
			}
			w.Header().Set("Last-Modified", fixedLastModified)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		f := lastModifiedFetcherForTest(srv)
		got, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: srv.URL})
		if err != nil || !ok || !got.Equal(want) {
			t.Errorf("got (%v,%v,%v), want (%v, true, nil)", got, ok, err, want)
		}
	})

	t.Run("HEAD 405 → GET Range fallback", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Last-Modified", fixedLastModified)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0})
		}))
		defer srv.Close()
		f := lastModifiedFetcherForTest(srv)
		got, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: srv.URL})
		if err != nil || !ok || !got.Equal(want) {
			t.Errorf("got (%v,%v,%v), want (%v, true, nil)", got, ok, err, want)
		}
	})

	t.Run("HEAD 501 → GET Range fallback", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			w.Header().Set("Last-Modified", fixedLastModified)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		f := lastModifiedFetcherForTest(srv)
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: srv.URL})
		if err != nil || !ok {
			t.Errorf("expected fallback success, got err=%v ok=%v", err, ok)
		}
	})

	t.Run("HEAD 200 no header → GET fallback also no header", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		f := lastModifiedFetcherForTest(srv)
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: srv.URL})
		if err != nil || ok {
			t.Errorf("expected (ok=false, nil), got ok=%v err=%v", ok, err)
		}
	})

	t.Run("HEAD 500 surfaces error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		f := lastModifiedFetcherForTest(srv)
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: srv.URL})
		if err == nil || ok {
			t.Errorf("expected (false, err), got ok=%v err=%v", ok, err)
		}
	})

	t.Run("malformed Last-Modified value returns error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Last-Modified", "garbage")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		f := lastModifiedFetcherForTest(srv)
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: srv.URL})
		if err == nil || ok {
			t.Errorf("expected (false, err), got ok=%v err=%v", ok, err)
		}
	})

	t.Run("empty URL returns ok=false nil", func(t *testing.T) {
		t.Parallel()
		f := &lastModifiedFetcher{client: http.DefaultClient}
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified})
		if err != nil || ok {
			t.Errorf("expected (false, nil), got ok=%v err=%v", ok, err)
		}
	})

	t.Run("wrong source returns ok=false nil", func(t *testing.T) {
		t.Parallel()
		f := &lastModifiedFetcher{client: http.DefaultClient}
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceAquaGitHubReleases, URL: "https://example.com"})
		if err != nil || ok {
			t.Errorf("expected (false, nil), got ok=%v err=%v", ok, err)
		}
	})

	t.Run("HTTP scheme rejected by entry gate", func(t *testing.T) {
		t.Parallel()
		f := &lastModifiedFetcher{client: http.DefaultClient}
		_, ok, err := f.Fetch(context.Background(), Key{Source: SourceLastModified, URL: "http://example.com"})
		if err == nil || ok {
			t.Errorf("expected SSRF rejection, got ok=%v err=%v", ok, err)
		}
	})
}
