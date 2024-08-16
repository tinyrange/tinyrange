package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/experimental/vncClient/rfb"
	"github.com/tinyrange/tinyrange/experimental/vncClient/window"
)

var (
	addr     = flag.String("addr", "127.0.0.1:5901", "The address of the server to connect to.")
	password = flag.String("password", "password", "The password to set if requested by the server")
)

func appMain() error {
	flag.Parse()

	win := window.New()
	defer win.Close()

	nConn, err := net.Dial("tcp", *addr)
	if err != nil {
		return err
	}

	conn, err := rfb.NewConn(nConn)
	if err != nil {
		return err
	}
	defer conn.Close()

	updateTicks := time.NewTicker(time.Second / 60)
	defer updateTicks.Stop()

	var buttonState rfb.Buttons

	for {
		select {
		case evt := <-conn.Events:
			switch evt := evt.(type) {
			case *rfb.ConnectedEvent:
				// create a window.
				slog.Info("connected", "width", evt.FrameBufferWidth, "height", evt.FrameBufferHeight, "name", evt.Name)

				if err := win.Create(int(evt.FrameBufferWidth), int(evt.FrameBufferHeight), evt.Name); err != nil {
					return err
				}

				if err := conn.RequestUpdate(false); err != nil {
					return err
				}
			case *rfb.ErrorEvent:
				return evt
			case *rfb.UpdateRectangleEvent:
				if err := win.DrawImage(evt.Image, evt.BGRA); err != nil {
					return err
				}
			default:
				slog.Info("unrecognized", "event", evt)
			}
		case evt := <-win.Events:
			switch evt := evt.(type) {
			case *window.ClosedEvent:
				return nil
			case *window.MouseMoveEvent:
				if err := conn.SendPointerEvent(buttonState, uint16(evt.X), uint16(evt.Y)); err != nil {
					return err
				}
			case *window.MousePressEvent:
				buttonState.Set(rfb.ButtonFromIndex(uint8(evt.Button)))
				if err := conn.SendPointerEvent(buttonState, uint16(evt.X), uint16(evt.Y)); err != nil {
					return err
				}
			case *window.MouseReleaseEvent:
				buttonState.Unset(rfb.ButtonFromIndex(uint8(evt.Button)))
				if err := conn.SendPointerEvent(buttonState, uint16(evt.X), uint16(evt.Y)); err != nil {
					return err
				}
			case *window.KeyPressEvent:
				if err := conn.SendKeyEvent(true, uint32(evt.Sym)); err != nil {
					return err
				}
			case *window.KeyReleaseEvent:
				if err := conn.SendKeyEvent(false, uint32(evt.Sym)); err != nil {
					return err
				}
			default:
				slog.Info("unrecognized", "event", evt)
			}
		case <-updateTicks.C:
			if err := conn.RequestUpdate(true); err != nil {
				return err
			}
		}
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
