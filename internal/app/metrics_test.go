package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testMetricsConfigs = `{
	"MyPeugeot": {
		"configs": {
			"DE": {},
			"FR": {}
		}
	}
}`

func TestOAuthMetricsInitializeConfiguredTargets(t *testing.T) {
	metrics := newOAuthMetrics()
	if err := metrics.initialize([]byte(testMetricsConfigs)); err != nil {
		t.Fatalf("initialize() error = %v", err)
	}

	body := scrapeMetrics(t, metrics.handler())
	for _, want := range []string{
		`stelloauth_oauth_failure_total{brand="MyPeugeot",country="DE"} 0`,
		`stelloauth_oauth_failure_total{brand="MyPeugeot",country="FR"} 0`,
		`stelloauth_oauth_success_total{brand="MyPeugeot",country="DE"} 0`,
		`stelloauth_oauth_success_total{brand="MyPeugeot",country="FR"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestOAuthMetricsRecordOutcome(t *testing.T) {
	metrics := newOAuthMetrics()
	if err := metrics.initialize([]byte(testMetricsConfigs)); err != nil {
		t.Fatalf("initialize() error = %v", err)
	}

	metrics.record("MyPeugeot", "DE", nil)
	metrics.record("MyPeugeot", "DE", errors.New("login failed"))

	body := scrapeMetrics(t, metrics.handler())
	for _, want := range []string{
		`stelloauth_oauth_failure_total{brand="MyPeugeot",country="DE"} 1`,
		`stelloauth_oauth_success_total{brand="MyPeugeot",country="DE"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestOAuthMetricsIgnoreUnknownTarget(t *testing.T) {
	metrics := newOAuthMetrics()
	if err := metrics.initialize([]byte(testMetricsConfigs)); err != nil {
		t.Fatalf("initialize() error = %v", err)
	}

	metrics.record("attacker-controlled", "XX", errors.New("unknown target"))

	body := scrapeMetrics(t, metrics.handler())
	if strings.Contains(body, "attacker-controlled") || strings.Contains(body, `country="XX"`) {
		t.Fatalf("metrics contain unknown target labels:\n%s", body)
	}
}

func TestOAuthMetricsInitializeRejectsInvalidConfigs(t *testing.T) {
	metrics := newOAuthMetrics()

	if err := metrics.initialize([]byte(`{`)); err == nil {
		t.Fatal("initialize() error = nil, want invalid configs error")
	}
}

func scrapeMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	return string(body)
}
