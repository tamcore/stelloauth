package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Initialize rate limiter for tests (disabled)
	initRateLimiter()
	os.Exit(m.Run())
}

func TestHandleIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("expected Content-Type text/html, got %s", w.Header().Get("Content-Type"))
	}

	body := w.Body.String()
	if !strings.Contains(body, "Stellantis OAuth Helper") {
		t.Error("expected body to contain 'Stellantis OAuth Helper'")
	}
}

func TestHandleIndex_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()

	handleIndex(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandleOAuth_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/oauth", nil)
	w := httptest.NewRecorder()

	handleOAuth(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleOAuth_InvalidBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/oauth", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	handleOAuth(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleOAuth_MissingFields(t *testing.T) {
	body := `{"brand":"MyPeugeot","country":"","email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/oauth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleOAuth(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp OAuthResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "error" {
		t.Errorf("expected status 'error', got '%s'", resp.Status)
	}
}

func TestGetEnv(t *testing.T) {
	result := getEnv("NONEXISTENT_VAR_12345", "default")
	if result != "default" {
		t.Errorf("expected 'default', got '%s'", result)
	}
}

const testIP = "81.2.69.142"

func TestHandleGeo_NilDB(t *testing.T) {
	countryDB = nil
	req := httptest.NewRequest(http.MethodGet, "/geo", nil)
	req.Header.Set("X-Forwarded-For", testIP)
	w := httptest.NewRecorder()

	handleGeo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["country"] != "" {
		t.Errorf("country = %q, want empty when DB disabled", resp["country"])
	}
}

func TestHandleGeo_Resolves(t *testing.T) {
	db, err := loadCountryDB(writeTemp(t, "geo.mmdb", buildTestCountryDB(t)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	countryDB = db
	defer func() { countryDB = nil }()

	req := httptest.NewRequest(http.MethodGet, "/geo", nil)
	req.Header.Set("X-Forwarded-For", testIP)
	w := httptest.NewRecorder()

	handleGeo(w, req)

	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["country"] != "GB" {
		t.Errorf("country = %q, want GB", resp["country"])
	}
}

func TestParseClientIP(t *testing.T) {
	cases := map[string]string{
		testIP:            testIP, // bare (XFF / X-Real-IP form)
		testIP + ":52048": testIP, // host:port (RemoteAddr form)
		"[::1]:52048":     "::1",
		"garbage":         "", // unparseable -> ok=false
	}
	for in, want := range cases {
		got, ok := parseClientIP(in)
		if want == "" {
			if ok {
				t.Errorf("parseClientIP(%q) ok=true, want false", in)
			}
			continue
		}
		if !ok || got.String() != want {
			t.Errorf("parseClientIP(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}
}
