package rfb

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"net"
)

type frameBufferRectangle struct {
	XPos         uint16
	YPos         uint16
	Width        uint16
	Height       uint16
	EncodingType int32
}

type keyEvent struct {
	MessageType uint8
	DownFlag    uint8
	Padding     [2]byte
	Key         uint32
}

type Buttons uint8

const (
	ButtonLeft       Buttons = 1 << 0
	ButtonMiddle             = 1 << 1
	ButtonRight              = 1 << 2
	ButtonScrollUp           = 1 << 3
	ButtonScrollDown         = 1 << 4
)

func (b *Buttons) Set(b2 Buttons) {
	*b |= b2
}

func (b *Buttons) Unset(b2 Buttons) {
	*b &= ^b2
}

func ButtonFromIndex(idx byte) Buttons {
	switch idx {
	case 1:
		return ButtonLeft
	case 2:
		return ButtonMiddle
	case 3:
		return ButtonRight
	case 4:
		return ButtonScrollUp
	case 5:
		return ButtonScrollDown
	default:
		return 0
	}
}

type pointerEvent struct {
	MessageType uint8
	ButtonMask  Buttons
	XPos        uint16
	YPos        uint16
}

type frameBufferUpdateRequest struct {
	MessageType uint8
	Incremental uint8
	XPos        uint16
	YPos        uint16
	Width       uint16
	Height      uint16
}

type PixelFormat struct {
	BitsPerPixel  uint8
	Depth         uint8
	BigEndianFlag uint8
	TrueColorFlag uint8
	RedMax        uint16
	GreenMax      uint16
	BlueMax       uint16
	RedShift      uint8
	GreenShift    uint8
	BlueShift     uint8
	Padding       [3]byte
}

type ServerInit struct {
	FrameBufferWidth  uint16
	FrameBufferHeight uint16
	PixelFormat       PixelFormat
	NameLength        uint32
}

type ConnectedEvent struct {
	ServerInit
	Name string
}

// eventTag implements RFBEvent.
func (r *ConnectedEvent) eventTag() { panic("unimplemented") }

type ErrorEvent struct {
	error
}

// eventTag implements RFBEvent.
func (r *ErrorEvent) eventTag() { panic("unimplemented") }

type UpdateRectangleEvent struct {
	image.Image
	BGRA bool
}

// eventTag implements Event.
func (u *UpdateRectangleEvent) eventTag() { panic("unimplemented") }

var (
	_ Event = &ErrorEvent{}
	_ Event = &ConnectedEvent{}
	_ Event = &UpdateRectangleEvent{}
)

type Event interface {
	eventTag()
}

type Connection struct {
	Conn        net.Conn
	Events      chan Event
	closed      bool
	serverInit  ServerInit
	pixelFormat PixelFormat
}

func (rfb *Connection) writeEvent(evt Event) {
	if rfb.closed {
		return
	}

	rfb.Events <- evt
}

func (rfb *Connection) Close() error {
	if !rfb.closed {
		rfb.closed = true
		close(rfb.Events)
		return rfb.Conn.Close()
	}

	return nil
}

func (rfb *Connection) readBytes(count int) ([]byte, error) {
	b := make([]byte, count)

	if _, err := io.ReadFull(rfb.Conn, b); err != nil {
		return nil, err
	}

	return b, nil
}

func (rfb *Connection) RequestUpdate(incremental bool) error {
	var b uint8 = 0
	if incremental {
		b = 1
	}

	return binary.Write(rfb.Conn, binary.BigEndian, &frameBufferUpdateRequest{
		MessageType: 3,
		Incremental: b,
		XPos:        0,
		YPos:        0,
		Width:       rfb.serverInit.FrameBufferWidth,
		Height:      rfb.serverInit.FrameBufferHeight,
	})
}

func (rfb *Connection) SendKeyEvent(down bool, sym uint32) error {
	var b uint8 = 0
	if down {
		b = 1
	}

	return binary.Write(rfb.Conn, binary.BigEndian, &keyEvent{
		MessageType: 4,
		DownFlag:    b,
		Key:         sym,
	})
}

func (rfb *Connection) SendPointerEvent(buttons Buttons, xPos uint16, yPos uint16) error {
	return binary.Write(rfb.Conn, binary.BigEndian, &pointerEvent{
		MessageType: 5,
		ButtonMask:  buttons,
		XPos:        xPos,
		YPos:        yPos,
	})
}

func (rfb *Connection) receiveLoop() {
	defer rfb.Close()

	securityTypeCount, err := rfb.readBytes(1)
	if err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	if securityTypeCount[0] == 0 {
		rfb.writeEvent(&ErrorEvent{error: fmt.Errorf("failed to connect to server")})
		return
	}

	securityTypes, err := rfb.readBytes(int(securityTypeCount[0]))
	if err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	acceptsNone := false
	for _, typ := range securityTypes {
		if typ == 0x01 {
			acceptsNone = true
			break
		}
	}
	if !acceptsNone {
		rfb.writeEvent(&ErrorEvent{error: fmt.Errorf("server requires a password")})
		return
	}

	if _, err := rfb.Conn.Write([]byte{0x01}); err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	// Check the result of the security init.
	resBytes, err := rfb.readBytes(4)
	if err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	res := binary.BigEndian.Uint32(resBytes)

	if res != 0 {
		rfb.writeEvent(&ErrorEvent{error: fmt.Errorf("security handshake failed")})
		return
	}

	// Send the ClientInit message to the server.
	// Kick out all the other clients.
	if _, err := rfb.Conn.Write([]byte{0x00}); err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	// Get the ServerInit response from the server.
	var serverInit ServerInit

	if err := binary.Read(rfb.Conn, binary.BigEndian, &serverInit); err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	nameBytes, err := rfb.readBytes(int(serverInit.NameLength))
	if err != nil {
		rfb.writeEvent(&ErrorEvent{error: err})
		return
	}

	rfb.serverInit = serverInit
	rfb.pixelFormat = serverInit.PixelFormat

	// Post a RFBConnected message.
	rfb.writeEvent(&ConnectedEvent{ServerInit: serverInit, Name: string(nameBytes)})

	// Start the main loop.
	for {
		msgType, err := rfb.readBytes(1)
		if err != nil {
			rfb.writeEvent(&ErrorEvent{error: err})
			return
		}

		switch msgType[0] {
		case 0: // framebuffer update
			updateHead, err := rfb.readBytes(3)
			if err != nil {
				rfb.writeEvent(&ErrorEvent{error: err})
				return
			}

			rectCount := binary.BigEndian.Uint16(updateHead[1:])

			for i := 0; i < int(rectCount); i++ {
				var rectHead frameBufferRectangle

				if err := binary.Read(rfb.Conn, binary.BigEndian, &rectHead); err != nil {
					rfb.writeEvent(&ErrorEvent{error: err})
					return
				}

				switch rectHead.EncodingType {
				case 0: // raw
					buff, err := rfb.readBytes(int(rfb.pixelFormat.BitsPerPixel) / 8 * int(rectHead.Width) * int(rectHead.Height))
					if err != nil {
						rfb.writeEvent(&ErrorEvent{error: err})
						return
					}

					rfb.writeEvent(&UpdateRectangleEvent{
						Image: &image.RGBA{
							Pix:    buff,
							Stride: int(rectHead.Width) * 4,
							Rect: image.Rect(
								int(rectHead.XPos),
								int(rectHead.YPos),
								int(rectHead.XPos+rectHead.Width),
								int(rectHead.YPos+rectHead.Height),
							),
						},
						BGRA: rfb.pixelFormat.BlueShift == 0,
					})
				default:
					rfb.writeEvent(&ErrorEvent{error: fmt.Errorf("unknown rectangle encoding: %d", msgType[0])})
					return
				}
			}
		default:
			rfb.writeEvent(&ErrorEvent{error: fmt.Errorf("unknown event: %d", msgType[0])})
			return
		}
	}
}

func NewConn(conn net.Conn) (*Connection, error) {
	// Get the version from the server.
	version := make([]byte, 12)

	if _, err := io.ReadFull(conn, version); err != nil {
		defer conn.Close()

		return nil, err
	}

	// Check that the version is RFB 3.8
	if string(version) != "RFB 003.008\n" {
		defer conn.Close()

		return nil, fmt.Errorf("unknown version: %s", version)
	}

	// Write the same version to the server.
	if _, err := conn.Write(version); err != nil {
		defer conn.Close()

		return nil, err
	}

	// And so thus ends Phase 1.

	rfb := &Connection{Conn: conn, Events: make(chan Event)}

	go rfb.receiveLoop()

	return rfb, nil
}
