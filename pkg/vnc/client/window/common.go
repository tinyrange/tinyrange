package window

import (
	"image"
)

type Event interface {
	eventTag()
}

type ErrorEvent struct {
	error
}

// eventTag implements Event.
func (e *ErrorEvent) eventTag() { panic("unimplemented") }

type ClosedEvent struct {
}

// eventTag implements Event.
func (c *ClosedEvent) eventTag() { panic("unimplemented") }

type MouseMoveEvent struct {
	X int
	Y int
}

// eventTag implements Event.
func (m *MouseMoveEvent) eventTag() {
	panic("unimplemented")
}

type MousePressEvent struct {
	X      int
	Y      int
	Button byte
}

// eventTag implements Event.
func (m *MousePressEvent) eventTag() {
	panic("unimplemented")
}

type MouseReleaseEvent struct {
	X      int
	Y      int
	Button byte
}

// eventTag implements Event.
func (m *MouseReleaseEvent) eventTag() {
	panic("unimplemented")
}

type KeyPressEvent struct {
	Sym int32
}

// eventTag implements Event.
func (k *KeyPressEvent) eventTag() {
	panic("unimplemented")
}

type KeyReleaseEvent struct {
	Sym int32
}

// eventTag implements Event.
func (k *KeyReleaseEvent) eventTag() {
	panic("unimplemented")
}

var (
	_ Event = &ErrorEvent{}
	_ Event = &ClosedEvent{}
	_ Event = &MouseMoveEvent{}
	_ Event = &MousePressEvent{}
	_ Event = &MouseReleaseEvent{}
	_ Event = &KeyPressEvent{}
	_ Event = &KeyReleaseEvent{}
)

type Window interface {
	Close() error
	Create(width int, height int, name string) error
	DrawImage(img image.Image, bgra bool) error
	Events() chan Event
}
