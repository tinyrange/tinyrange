//go:build linux

package window

import (
	"fmt"
	"image"
	"log/slog"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"
)

type windowImpl struct {
	mtx    sync.Mutex
	X      *xgbutil.XUtil
	win    *xwindow.Window
	canvas *xgraphics.Image
	events chan Event
	closed bool
}

// Events implements Window.
func (window *windowImpl) Events() chan Event {
	return window.events
}

func (window *windowImpl) writeEvent(evt Event) {
	window.mtx.Lock()
	defer window.mtx.Unlock()

	if window.closed {
		return
	}

	window.events <- evt
}

func (window *windowImpl) Close() error {
	window.mtx.Lock()
	defer window.mtx.Unlock()

	window.closed = true

	if window.win != nil {
		window.win.Destroy()
	}

	if window.X != nil {
		window.X.Conn().Close()
	}

	return nil
}

func (window *windowImpl) DrawImage(img image.Image, bgra bool) error {
	window.mtx.Lock()
	defer window.mtx.Unlock()

	subImg := window.canvas.SubImage(img.Bounds())
	if subImg == nil {
		return fmt.Errorf("no image returned for (%s)", img.Bounds())
	}

	sub := subImg.(*xgraphics.Image)

	sub.ForExp(func(x, y int) (r uint8, g uint8, b uint8, a uint8) {
		r32, g32, b32, a32 := img.At(x, y).RGBA()
		if bgra {
			return uint8(b32), uint8(g32), uint8(r32), uint8(a32)
		} else {
			return uint8(r32), uint8(g32), uint8(b32), uint8(a32)
		}
	})

	// Now draw the changes to the pixmap.
	sub.XDraw()

	// And paint them to the window.
	sub.XPaint(window.win.Id)

	return nil
}

func (window *windowImpl) Create(width int, height int, title string) error {
	window.mtx.Lock()
	defer window.mtx.Unlock()

	X, err := xgbutil.NewConn()
	if err != nil {
		return err
	}

	keybind.Initialize(X)

	window.X = X

	xevent.ErrorHandlerSet(X, func(err xgb.Error) {
		slog.Error("error", "err", err)
	})

	window.canvas = xgraphics.New(X, image.Rect(0, 0, width, height))

	window.win = window.canvas.XShowExtra(title, true)

	window.win.Listen(xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease |
		xproto.EventMaskPointerMotion |
		xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease)

	xevent.KeyPressFun(func(xu *xgbutil.XUtil, event xevent.KeyPressEvent) {
		sym := keybind.KeysymGet(window.X, event.Detail, 0)
		window.writeEvent(&KeyPressEvent{Sym: int32(sym)})
	}).Connect(X, window.win.Id)
	xevent.KeyReleaseFun(func(xu *xgbutil.XUtil, event xevent.KeyReleaseEvent) {
		sym := keybind.KeysymGet(window.X, event.Detail, 0)
		window.writeEvent(&KeyReleaseEvent{Sym: int32(sym)})
	}).Connect(X, window.win.Id)

	xevent.ButtonPressFun(func(xu *xgbutil.XUtil, event xevent.ButtonPressEvent) {
		window.writeEvent(&MousePressEvent{X: int(event.EventX), Y: int(event.EventY), Button: byte(event.Detail)})
	}).Connect(X, window.win.Id)

	xevent.ButtonReleaseFun(func(xu *xgbutil.XUtil, event xevent.ButtonReleaseEvent) {
		window.writeEvent(&MouseReleaseEvent{X: int(event.EventX), Y: int(event.EventY), Button: byte(event.Detail)})
	}).Connect(X, window.win.Id)

	xevent.MotionNotifyFun(func(xu *xgbutil.XUtil, event xevent.MotionNotifyEvent) {
		window.writeEvent(&MouseMoveEvent{X: int(event.EventX), Y: int(event.EventY)})
	}).Connect(X, window.win.Id)

	go func() {
		xevent.Main(X)
		window.writeEvent(&ClosedEvent{})
	}()

	return nil
}

func New() (Window, error) {
	return &windowImpl{
		events: make(chan Event),
	}, nil
}
