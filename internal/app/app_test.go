package app

import (
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
