package build2

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinyrange/tinyrange/pkg/feature"
)

// token represents a reusable token that can be locked and unlocked.
type token struct {
	locker *tokenLocker

	mu      sync.Mutex
	closed  bool
	donated bool
	donate  chan struct{}
}

// Donate unlocks another waiting locker by signaling through the donate channel.
// It is safe to call Donate multiple times; subsequent calls after the first have no effect.
func (t *token) Donate() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed || t.donated {
		return
	}

	t.donated = true

	t.donate <- struct{}{}
}

// Lock waits until it can acquire a token or receives a donation.
func (t *token) Lock() io.Closer {
	if t.locker.debug {
		slog.Info("try lock", "currentlyLocked", t.locker.currentlyLocked.Load())
	}

	select {
	case <-t.locker.c:
		if t.locker.debug {
			slog.Info("acquire token", "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		t.locker.currentlyLocked.Add(1)
		return t
	case <-t.donate:
		return t
	}
}

// Close releases the token back to the locker unless it was donated or already closed.
func (t *token) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return errors.New("token already closed")
	}

	t.closed = true

	if t.donated {
		return nil // Do not return the token to the locker if it was donated.
	}

	// Non-blocking send to avoid deadlock if the locker channel is full.
	select {
	case t.locker.c <- struct{}{}:
		if t.locker.debug {
			slog.Info("return token", "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		t.locker.currentlyLocked.Add(-1)
		return nil
	default:
		if t.locker.debug {
			slog.Error("locker channel is full, cannot return token", "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		return errors.New("locker channel is full, cannot return token")
	}
}

// tokenLocker hands out closable locks from a pool of a limited size.
type tokenLocker struct {
	c               chan struct{}
	currentlyLocked atomic.Int32
	debug           bool
}

// New creates a new token associated with the locker.
func (t *tokenLocker) New() *token {
	return &token{
		locker: t,
		donate: make(chan struct{}, 1),
	}
}

// newTokenLocker initializes a new token locker with a given size.
func newTokenLocker(size int) *tokenLocker {
	tl := &tokenLocker{
		c:     make(chan struct{}, size),
		debug: feature.HasFeature(feature.FeatureTokenLockerDebug),
	}

	if tl.debug {
		slog.Info("token locker debug enabled", "size", size)
		go func() {
			for {
				if tl.currentlyLocked.Load() < 0 {
					slog.Error("currentlyLocked is less than 0", "value", tl.currentlyLocked.Load())
				} else if tl.currentlyLocked.Load() > int32(size) {
					slog.Error("currentlyLocked is greater than size", "value", tl.currentlyLocked.Load(), "size", size)
				}

				time.Sleep(1 * time.Second)
			}
		}()
	}

	// Fill the channel to represent available tokens.
	for i := 0; i < size; i++ {
		tl.c <- struct{}{}
	}

	return tl
}
