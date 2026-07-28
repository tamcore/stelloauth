package app

import (
	"testing"
	"time"
)

func TestGetIntEnv(t *testing.T) {
	t.Setenv("CLOAK_MAX_SESSIONS", "3")
	if got := getIntEnv("CLOAK_MAX_SESSIONS", 1); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if got := getIntEnv("CLOAK_MISSING_INT", 7); got != 7 {
		t.Errorf("default not used: got %d", got)
	}
	t.Setenv("CLOAK_MAX_SESSIONS", "notanint")
	if got := getIntEnv("CLOAK_MAX_SESSIONS", 1); got != 1 {
		t.Errorf("invalid value should fall back to default, got %d", got)
	}
}

func TestGetDurationEnv(t *testing.T) {
	t.Setenv("CLOAK_QUEUE_TIMEOUT", "90s")
	if got := getDurationEnv("CLOAK_QUEUE_TIMEOUT", 60*time.Second); got != 90*time.Second {
		t.Errorf("got %v, want 90s", got)
	}
	if got := getDurationEnv("CLOAK_MISSING_DUR", 30*time.Second); got != 30*time.Second {
		t.Errorf("default not used: got %v", got)
	}
	t.Setenv("CLOAK_QUEUE_TIMEOUT", "garbage")
	if got := getDurationEnv("CLOAK_QUEUE_TIMEOUT", 60*time.Second); got != 60*time.Second {
		t.Errorf("invalid value should fall back to default, got %v", got)
	}
}
