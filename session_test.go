package main

import (
	"context"
	"testing"
	"time"
)

func TestSessionGate_AcquireImmediateNoWait(t *testing.T) {
	g := newSessionGate(1, time.Second)
	waited := false
	if err := g.Acquire(context.Background(), func() { waited = true }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waited {
		t.Error("onWait should not fire when a slot is free")
	}
	g.Release()
}

func TestSessionGate_SecondCallWaitsThenTimesOut(t *testing.T) {
	g := newSessionGate(1, 50*time.Millisecond)
	if err := g.Acquire(context.Background(), nil); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	waited := false
	err := g.Acquire(context.Background(), func() { waited = true })
	if err != ErrSessionBusy {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	if !waited {
		t.Error("onWait should have fired while blocking")
	}
	g.Release()
}

func TestSessionGate_ReleaseLetsNextAcquire(t *testing.T) {
	g := newSessionGate(1, time.Second)
	_ = g.Acquire(context.Background(), nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		g.Release()
	}()
	if err := g.Acquire(context.Background(), nil); err != nil {
		t.Fatalf("expected acquire after release, got %v", err)
	}
	g.Release()
}
