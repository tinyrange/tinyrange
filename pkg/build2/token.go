package build2

import (
	"errors"
	"io"
	"sync"
)

// token represents a reusable token from a limited‐size pool.
// A token may be “donated” – that is, its closing responsibility is
// passed on to a Lock caller. Because only one Lock per token is allowed,
// a token is marked as locked when Lock is first called.
type token struct {
	locker *tokenLocker

	mu      sync.Mutex
	closed  bool // once set true, the token’s slot has been or will be returned
	donated bool // set if Donate() has been called
	locked  bool // ensures Lock is only called once

	// donate is a buffered channel used to signal a waiting Lock.
	donate chan struct{}
}

// Donate “donates” this token – it signals any waiting Lock that this
// token’s closing responsibility is being transferred. Once Donate is
// called, the donor must not later call Close.
func (t *token) Donate() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If already closed or already donated, nothing to do.
	if t.closed || t.donated {
		return
	}
	t.donated = true

	// Send a donation signal in non‐blocking mode.
	select {
	case t.donate <- struct{}{}:
	default:
	}
}

// Lock waits to acquire a token from the pool, either by taking one
// from the pool’s channel or by receiving the donation signal.
// Lock is allowed only once per token; subsequent calls panic.
func (t *token) Lock() io.Closer {
	t.mu.Lock()
	if t.locked {
		t.mu.Unlock()
		panic("token already locked")
	}
	t.locked = true
	t.mu.Unlock()

	// Wait for either a real token or a donation signal.
	select {
	case <-t.locker.c:
		// Got a token from the pool.
		return t
	case <-t.donate:
		// Got the donation signal: return a wrapper that will eventually
		// be responsible for returning the token’s slot.
		return &donatedToken{token: t}
	}
}

// Close should be called exactly once on a token that wasn’t donated.
// It returns the token’s slot to the pool, using a non‐blocking send
// to avoid deadlock if the pool’s channel is already full.
func (t *token) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return errors.New("token already closed")
	}

	// The donor must not call Close.
	if t.donated {
		return errors.New("donated token must be closed by the donation recipient")
	}

	t.closed = true

	// Return token’s slot non‐blockingly.
	select {
	case t.locker.c <- struct{}{}:
		// Returned successfully.
	default:
		// If the channel is full then the token’s slot is already available.
	}
	return nil
}

// donatedToken is a thin wrapper over token. It is returned by Lock()
// when the donation signal is selected. Its Close method returns the token's
// slot; only one call to Close is allowed.
type donatedToken struct {
	token *token
	mu    sync.Mutex
}

// Close for a donatedToken returns the token’s slot back to the pool.
// It is an error to call Close more than once.
func (dt *donatedToken) Close() error {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	dt.token.mu.Lock()
	defer dt.token.mu.Unlock()

	if dt.token.closed {
		return errors.New("token already closed")
	}

	dt.token.closed = true

	// Return the token’s slot non‐blockingly.
	select {
	case dt.token.locker.c <- struct{}{}:
	default:
		// If the channel is full then the token’s slot is already available.
	}
	return nil
}

// tokenLocker represents a pool of available tokens.
type tokenLocker struct {
	// c holds available token “slots.”
	c chan struct{}
}

// New creates a new token associated with the tokenLocker.
// Each token may be locked once.
func (tl *tokenLocker) New() *token {
	return &token{
		locker: tl,
		donate: make(chan struct{}, 1),
	}
}

// newTokenLocker initializes a new tokenLocker with the given pool size.
// The channel is pre-filled to represent available tokens.
func newTokenLocker(size int) *tokenLocker {
	tl := &tokenLocker{
		c: make(chan struct{}, size),
	}
	for i := 0; i < size; i++ {
		tl.c <- struct{}{}
	}
	return tl
}
