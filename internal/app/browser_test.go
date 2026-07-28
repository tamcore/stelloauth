package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverCDPWebSocketURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"Browser":"Chrome/146","webSocketDebuggerUrl":"ws://host:9222/devtools/browser/abc"}`))
	}))
	defer srv.Close()

	ws, err := discoverCDPWebSocketURL(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "ws://host:9222/devtools/browser/abc" {
		t.Errorf("got %q", ws)
	}
}

func TestDiscoverCDPWebSocketURL_Fingerprint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("fingerprint"); got != "req-123" {
			t.Errorf("fingerprint not forwarded: got %q", got)
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://host:9222/fingerprint/req-123/devtools/browser/abc"}`))
	}))
	defer srv.Close()

	ws, err := discoverCDPWebSocketURL(srv.URL, "req-123", srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "ws://host:9222/fingerprint/req-123/devtools/browser/abc" {
		t.Errorf("got %q", ws)
	}
}

func TestDiscoverCDPWebSocketURL_TrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Errorf("path not normalized: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://x/y"}`))
	}))
	defer srv.Close()

	if _, err := discoverCDPWebSocketURL(srv.URL+"/", "", srv.Client()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverCDPWebSocketURL_MissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Browser":"Chrome/146"}`))
	}))
	defer srv.Close()

	if _, err := discoverCDPWebSocketURL(srv.URL, "", srv.Client()); err == nil {
		t.Fatal("expected error for missing webSocketDebuggerUrl")
	}
}
