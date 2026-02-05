package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "error" {
		t.Errorf("expected status 'error', got '%s'", resp.Status)
	}
}

func TestExtractFormAction(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		baseURL  string
		expected string
	}{
		{
			name:     "absolute URL",
			html:     `<form action="https://example.com/login" method="post">`,
			baseURL:  "https://example.com",
			expected: "https://example.com/login",
		},
		{
			name:     "relative URL",
			html:     `<form action="/login" method="post">`,
			baseURL:  "https://example.com",
			expected: "https://example.com/login",
		},
		{
			name:     "no form",
			html:     `<div>no form here</div>`,
			baseURL:  "https://example.com",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFormAction(tt.html, tt.baseURL)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestExtractHiddenFields(t *testing.T) {
	html := `
		<input type="hidden" name="csrf_token" value="abc123">
		<input type="hidden" name="realm" value="test">
		<input value="xyz" type="hidden" name="other">
	`

	fields := extractHiddenFields(html)

	if fields["csrf_token"] != "abc123" {
		t.Errorf("expected csrf_token='abc123', got '%s'", fields["csrf_token"])
	}
	if fields["realm"] != "test" {
		t.Errorf("expected realm='test', got '%s'", fields["realm"])
	}
}

func TestExtractCode(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expected    string
		expectError bool
	}{
		{
			name:        "valid code",
			url:         "mymap://oauth2redirect/gb?code=abc123xyz",
			expected:    "abc123xyz",
			expectError: false,
		},
		{
			name:        "no code",
			url:         "mymap://oauth2redirect/gb?error=access_denied",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := extractCode(tt.url)
			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if code != tt.expected {
				t.Errorf("expected code '%s', got '%s'", tt.expected, code)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	result := getEnv("NONEXISTENT_VAR_12345", "default")
	if result != "default" {
		t.Errorf("expected 'default', got '%s'", result)
	}
}
