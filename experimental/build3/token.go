package main

import (
	"errors"
	"io"
	"sync"
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

	// Non-blocking send to avoid deadlock if no one is listening.
	t.donate <- struct{}{}
}

// Lock waits until it can acquire a token or receives a donation.
func (t *token) Lock() io.Closer {
	select {
	case <-t.locker.c:
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
		return nil
	default:
		return errors.New("locker channel is full, cannot return token")
	}
}

// tokenLocker hands out closable locks from a pool of a limited size.
type tokenLocker struct {
	c chan struct{}
}

// New creates a new token associated with the locker.
func (t *tokenLocker) New() *token {
	return &token{
		locker: t,
		donate: make(chan struct{}), // Unbuffered channel to sync donate signal.
	}
}

// newTokenLocker initializes a new token locker with a given size.
func newTokenLocker(size int) *tokenLocker {
	tl := &tokenLocker{
		c: make(chan struct{}, size),
	}

	// Fill the channel to represent available tokens.
	for i := 0; i < size; i++ {
		tl.c <- struct{}{}
	}

	return tl
}
