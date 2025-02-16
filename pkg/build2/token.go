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

var (
	modeFresh    = int32(0)
	modeWaiting  = int32(1)
	modeLocked   = int32(2)
	modeDonated  = int32(3)
	modeReleased = int32(4)
)

// token represents a reusable token that can be locked and unlocked.
type token struct {
	locker *tokenLocker

	mode       atomic.Int32
	mu         sync.Mutex
	lockReason string
	closed     bool
	donated    bool
	donate     chan struct{}
}

// Donate unlocks another waiting locker by signaling through the donate channel.
// It is safe to call Donate multiple times; subsequent calls after the first have no effect.
func (t *token) Donate() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed || t.donated {
		return
	}

	if t.mode.Load() > modeWaiting {
		if t.locker.debug {
			panic("unexpected token mode")
		}
	}

	t.donated = true

	t.donate <- struct{}{}
}

// Lock waits until it can acquire a token or receives a donation.
func (t *token) Lock(reason string) io.Closer {
	if t.locker.debug {
		slog.Info("try lock", "reason", reason, "currentlyLocked", t.locker.currentlyLocked.Load())
	}

	if !t.mode.CompareAndSwap(modeFresh, modeWaiting) {
		if t.locker.debug {
			slog.Error("token already waiting", "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		return nil
	}

	select {
	case <-t.locker.c:
		if !t.mode.CompareAndSwap(modeWaiting, modeLocked) {
			if t.locker.debug {
				panic("unexpected token mode")
			}
		}

		t.lockReason = reason

		if t.locker.debug {
			slog.Info("acquire token", "reason", reason, "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		t.locker.currentlyLocked.Add(1)
		return t
	case <-t.donate:
		if !t.mode.CompareAndSwap(modeWaiting, modeDonated) {
			if t.locker.debug {
				panic("unexpected token mode")
			}
		}

		return t
	}
}

// Close releases the token back to the locker unless it was donated or already closed.
func (t *token) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		if t.locker.debug {
			slog.Error("token already closed", "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		return errors.New("token already closed")
	}

	t.closed = true

	switch t.mode.Load() {
	case modeLocked:
		if t.locker.debug {
			slog.Info("release token", "reason", t.lockReason, "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		t.mode.Store(modeReleased)

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
	case modeDonated:
		if t.locker.debug {
			slog.Info("donated token", "currentlyLocked", t.locker.currentlyLocked.Load())
		}

		t.mode.Store(modeReleased)

		return nil
	default:
		if t.locker.debug {
			panic("unexpected token mode")
		}

		return errors.New("unexpected token mode")
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
