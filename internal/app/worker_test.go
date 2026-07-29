package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleWorker_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/worker", nil)
	w := httptest.NewRecorder()

	handleWorker(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleWorker_InvalidBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/worker", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handleWorker(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleWorker_MissingParams(t *testing.T) {
	body := `{"url":"","email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/worker", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleWorker(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var resp workerError
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Message != "Missing required params" {
		t.Errorf("message = %q, want 'Missing required params'", resp.Message)
	}
	if resp.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", resp.Code)
	}
}

func TestHandleWorker_BadURL(t *testing.T) {
	// Well-formed request but the URL carries no redirect_uri to key capture on.
	body := `{"url":"https://idpcvs.peugeot.com/am/oauth2/authorize?client_id=x","email":"a@b.c","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/worker", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleWorker(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	var resp workerError
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp.Message, "redirect_uri") {
		t.Errorf("message = %q, want it to mention redirect_uri", resp.Message)
	}
}

func TestRedirectScheme(t *testing.T) {
	cases := []struct {
		name    string
		authURL string
		want    string
		wantErr bool
	}{
		{
			name:    "mymap redirect",
			authURL: "https://idpcvs.peugeot.com/am/oauth2/authorize?client_id=x&redirect_uri=mymap%3A%2F%2Foauth2redirect%2Fde&response_type=code",
			want:    "mymap",
		},
		{
			name:    "opel custom scheme",
			authURL: "https://example.com/authorize?redirect_uri=myopel%3A%2F%2Foauth2redirect%2Fde",
			want:    "myopel",
		},
		{
			name:    "missing redirect_uri",
			authURL: "https://example.com/authorize?client_id=x",
			wantErr: true,
		},
		{
			name:    "redirect_uri without scheme",
			authURL: "https://example.com/authorize?redirect_uri=%2Fjust%2Fa%2Fpath",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := redirectScheme(tc.authURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got scheme %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("scheme = %q, want %q", got, tc.want)
			}
		})
	}
}
