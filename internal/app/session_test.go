package app

import (
	"errors"
	"testing"
	"time"
)

func TestSessionGate_AcquireImmediateNoWait(t *testing.T) {
	g := newSessionGate(1, time.Second)
	waited := false
	if err := g.Acquire(t.Context(), func() { waited = true }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waited {
		t.Error("onWait should not fire when a slot is free")
	}
	g.Release()
}

func TestSessionGate_SecondCallWaitsThenTimesOut(t *testing.T) {
	g := newSessionGate(1, 50*time.Millisecond)
	if err := g.Acquire(t.Context(), nil); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	waited := false
	err := g.Acquire(t.Context(), func() { waited = true })
	if !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	if !waited {
		t.Error("onWait should have fired while blocking")
	}
	g.Release()
}

func TestSessionGate_ReleaseLetsNextAcquire(t *testing.T) {
	g := newSessionGate(1, time.Second)
	_ = g.Acquire(t.Context(), nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		g.Release()
	}()
	if err := g.Acquire(t.Context(), nil); err != nil {
		t.Fatalf("expected acquire after release, got %v", err)
	}
	g.Release()
}
