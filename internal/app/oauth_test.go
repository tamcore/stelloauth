package app

import (
	"errors"
	"strings"
	"testing"
)

func TestFriendlyOPError(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		message string
		want    string
	}{
		{
			name:    "expired contextId",
			code:    "invalid_request",
			message: "Received+an+expired+contextId,+can+occur+if+the+login+took+too+long",
			want:    "the login took too long and the session expired, please try again",
		},
		{
			name:    "other message passed through with + decoded",
			code:    "some_error",
			message: "Something+went+wrong",
			want:    "Something went wrong",
		},
		{
			name:    "code only",
			code:    "bad_request",
			message: "",
			want:    "login failed (bad_request)",
		},
		{
			name:    "nothing",
			code:    "",
			message: "",
			want:    "login failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := friendlyOPError(tc.code, tc.message); got != tc.want {
				t.Errorf("friendlyOPError(%q, %q) = %q, want %q", tc.code, tc.message, got, tc.want)
			}
		})
	}
}

func TestPerformOAuthWithExecutorRecordsSuccess(t *testing.T) {
	metrics := newOAuthMetrics()
	if err := metrics.initialize(configsJSON); err != nil {
		t.Fatalf("initialize metrics: %v", err)
	}
	req := OAuthRequest{
		Brand:    "MyPeugeot",
		Country:  "DE",
		Email:    "driver@example.com",
		Password: "secret",
	}

	code, err := performOAuthWithExecutor(
		req, "request-id", nil, nil, metrics,
		func(_, _, _, _, _ string, _ ProgressFunc, _ DebugFunc) (string, error) {
			return "oauth-code", nil
		},
	)
	if err != nil {
		t.Fatalf("performOAuthWithExecutor() error = %v", err)
	}
	if code != "oauth-code" {
		t.Fatalf("code = %q, want %q", code, "oauth-code")
	}

	body := scrapeMetrics(t, metrics.handler())
	if !strings.Contains(body, `stelloauth_oauth_success_total{brand="MyPeugeot",country="DE"} 1`) {
		t.Fatalf("success metric not incremented:\n%s", body)
	}
}

func TestPerformOAuthWithExecutorRecordsFailure(t *testing.T) {
	metrics := newOAuthMetrics()
	if err := metrics.initialize(configsJSON); err != nil {
		t.Fatalf("initialize metrics: %v", err)
	}
	req := OAuthRequest{
		Brand:    "MyPeugeot",
		Country:  "DE",
		Email:    "driver@example.com",
		Password: "secret",
	}
	wantErr := errors.New("login failed")

	_, err := performOAuthWithExecutor(
		req, "request-id", nil, nil, metrics,
		func(_, _, _, _, _ string, _ ProgressFunc, _ DebugFunc) (string, error) {
			return "", wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("performOAuthWithExecutor() error = %v, want %v", err, wantErr)
	}

	body := scrapeMetrics(t, metrics.handler())
	if !strings.Contains(body, `stelloauth_oauth_failure_total{brand="MyPeugeot",country="DE"} 1`) {
		t.Fatalf("failure metric not incremented:\n%s", body)
	}
}
