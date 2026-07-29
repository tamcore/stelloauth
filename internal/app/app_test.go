package app

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestRunRequiresCDPURL(t *testing.T) {
	t.Setenv("CLOAK_CDP_URL", "")

	err := Run()
	if err == nil {
		t.Fatal("Run() error = nil, want required CLOAK_CDP_URL error")
	}
	if !strings.Contains(err.Error(), "CLOAK_CDP_URL is required") {
		t.Fatalf("Run() error = %q, want required CLOAK_CDP_URL error", err)
	}
}

func TestServerAddressesUseMetricsEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDRESS", "127.0.0.2")
	t.Setenv("PORT", "8081")
	t.Setenv("METRICS_ADDRESS", "127.0.0.3")
	t.Setenv("METRICS_PORT", "9091")

	appAddr, metricsAddr := serverAddresses()

	if appAddr != "127.0.0.2:8081" {
		t.Errorf("application address = %q, want %q", appAddr, "127.0.0.2:8081")
	}
	if metricsAddr != "127.0.0.3:9091" {
		t.Errorf("metrics address = %q, want %q", metricsAddr, "127.0.0.3:9091")
	}
}

func TestServeHTTPServersFailsWhenMetricsListenerUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	}()

	err = serveHTTPServers(
		"127.0.0.1:0",
		listener.Addr().String(),
		http.NotFoundHandler(),
		http.NotFoundHandler(),
	)
	if err == nil {
		t.Fatal("serveHTTPServers() error = nil, want metrics listener error")
	}
	if !strings.Contains(err.Error(), "metrics listener") {
		t.Fatalf("serveHTTPServers() error = %q, want metrics listener context", err)
	}
}
