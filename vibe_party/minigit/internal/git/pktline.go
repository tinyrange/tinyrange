package git

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var (
	ErrShortPkt = errors.New("short pkt-line header")
	ErrBadPkt   = errors.New("bad pkt-line length")
)

// PktLine reader/writer per Git protocol.
// Length is 4 hex digits including the header itself. 0000 is flush.
// In v2, 0001 is a delim packet.

type PktReader struct{ r *bufio.Reader }

func NewPktReader(r io.Reader) *PktReader { return &PktReader{r: bufio.NewReader(r)} }

// NewPktReaderFromBuf creates a PktReader using an existing bufio.Reader so
// callers can continue reading from the same buffer after pkt-line parsing.
func NewPktReaderFromBuf(br *bufio.Reader) *PktReader { return &PktReader{r: br} }

// ReadPacket returns data for a single pkt-line frame.
// len==0 indicates a flush (0000). len==-1 indicates delim (0001).
func (p *PktReader) ReadPacket() ([]byte, int, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(p.r, hdr); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, io.EOF
		}
		return nil, 0, ErrShortPkt
	}
	if string(hdr) == "0000" {
		return nil, 0, nil
	}
	if string(hdr) == "0001" {
		return nil, -1, nil
	}
	var n uint64
	_, err := fmt.Sscanf(string(hdr), "%04x", &n)
	if err != nil || n < 4 {
		return nil, 0, ErrBadPkt
	}
	plen := int(n) - 4
	buf := make([]byte, plen)
	if _, err := io.ReadFull(p.r, buf); err != nil {
		return nil, 0, err
	}
	return buf, plen, nil
}

type PktWriter struct {
	w io.Writer
}

func NewPktWriter(w io.Writer) *PktWriter { return &PktWriter{w: w} }

func (p *PktWriter) WritePacket(data []byte) error {
	n := len(data) + 4
	hdr := fmt.Sprintf("%04x", n)
	if _, err := io.WriteString(p.w, hdr); err != nil {
		return err
	}
	_, err := p.w.Write(data)
	return err
}

func (p *PktWriter) WriteFlush() error {
	_, err := io.WriteString(p.w, "0000")
	return err
}

func (p *PktWriter) WriteDelim() error {
	_, err := io.WriteString(p.w, "0001")
	return err
}

// Helper to write a line with trailing \n.
func (p *PktWriter) WriteStringLine(s string) error {
	return p.WritePacket([]byte(s))
}

func Hex(data []byte) string { return hex.EncodeToString(data) }
