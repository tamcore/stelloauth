package app

import (
	"context"
	"errors"
	"time"
)

// ErrSessionBusy is returned when no browser session frees up within the wait timeout.
var ErrSessionBusy = errors.New("all browser sessions are busy")

// SessionGate bounds concurrent browser sessions. CloakBrowser's free tier
// allows a single concurrent session, so the default capacity is 1.
type SessionGate struct {
	slots       chan struct{}
	waitTimeout time.Duration
}

func newSessionGate(maxSessions int, waitTimeout time.Duration) *SessionGate {
	if maxSessions < 1 {
		maxSessions = 1
	}
	return &SessionGate{
		slots:       make(chan struct{}, maxSessions),
		waitTimeout: waitTimeout,
	}
}

// Acquire reserves a session slot. If one is free it returns immediately.
// Otherwise onWait (if non-nil) is invoked once and the call blocks until a
// slot frees up, the wait timeout elapses (ErrSessionBusy), or ctx is done.
func (g *SessionGate) Acquire(ctx context.Context, onWait func()) error {
	select {
	case g.slots <- struct{}{}:
		return nil
	default:
	}

	if onWait != nil {
		onWait()
	}

	timer := time.NewTimer(g.waitTimeout)
	defer timer.Stop()

	select {
	case g.slots <- struct{}{}:
		return nil
	case <-timer.C:
		return ErrSessionBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a previously acquired slot. Safe to call at most once per Acquire.
func (g *SessionGate) Release() {
	select {
	case <-g.slots:
	default:
	}
}
