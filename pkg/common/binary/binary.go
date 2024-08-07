package binary

import (
	"bytes"
	"encoding/binary"
	"io"
)

var (
	LittleEndian = binary.LittleEndian
	BigEndian    = binary.BigEndian
	NativeEndian = binary.NativeEndian
)

type BinaryReader interface {
	Bytes(count int) []byte

	Uint64() uint64
	Uint32() uint32
	Uint16() uint16
	Uint8() uint8

	Int64() int64
	Int32() int32
	Int16() int16
	Int8() int8

	Struct(s any)

	Tell() uint64

	Error() error
}

type BinaryWriter interface {
	Bytes(b []byte)

	Uint64(v uint64)
	Uint32(v uint32)
	Uint16(v uint16)
	Uint8(v uint8)

	Int64(v int64)
	Int32(v int32)
	Int16(v int16)
	Int8(v int8)

	Struct(s any)

	Tell() uint64

	Error() error
}

type Encodable interface {
	Encode(r BinaryWriter) error
}

type Decodable interface {
	Decode(r BinaryReader) error
}

type binaryReader struct {
	reader io.Reader
	order  binary.ByteOrder
	offset uint64
	err    error
}

// Tell implements BinaryReader.
func (r *binaryReader) Tell() uint64 {
	return r.offset
}

// Struct implements BinaryReader.
func (r *binaryReader) Struct(s any) {
	if r.err != nil {
		return
	}

	err := binary.Read(r.reader, r.order, s)
	if err != nil {
		r.err = err
		return
	}
}

// Bytes implements BinaryReader.
func (r *binaryReader) Bytes(count int) []byte {
	if r.err != nil {
		return nil
	}

	buf := make([]byte, count)

	_, err := io.ReadFull(r.reader, buf)
	if err != nil {
		r.err = err
		return nil
	}

	r.offset += uint64(count)

	return buf
}

// Error implements BinaryReader.
func (r *binaryReader) Error() error {
	err := r.err
	r.err = nil
	return err
}

// Int8 implements BinaryReader.
func (r *binaryReader) Int8() int8 {
	if r.err != nil {
		return -1
	}

	bytes := r.Bytes(1)

	if len(bytes) != 1 {
		return -1
	}

	return int8(bytes[0])
}

// Int16 implements BinaryReader.
func (r *binaryReader) Int16() int16 {
	if r.err != nil {
		return -1
	}

	bytes := r.Bytes(2)

	if len(bytes) != 2 {
		return -1
	}

	return int16(r.order.Uint16(bytes))
}

// Int32 implements BinaryReader.
func (r *binaryReader) Int32() int32 {
	if r.err != nil {
		return -1
	}

	bytes := r.Bytes(4)

	if len(bytes) != 4 {
		return -1
	}

	return int32(r.order.Uint32(bytes))
}

// Int64 implements BinaryReader.
func (r *binaryReader) Int64() int64 {
	if r.err != nil {
		return -1
	}

	bytes := r.Bytes(8)

	if len(bytes) != 8 {
		return -1
	}

	return int64(r.order.Uint64(bytes))
}

// Uint8 implements BinaryReader.
func (r *binaryReader) Uint8() uint8 {
	if r.err != nil {
		return 0xff
	}

	bytes := r.Bytes(1)

	if len(bytes) != 1 {
		return 0xff
	}

	return uint8(bytes[0])
}

// Uint16 implements BinaryReader.
func (r *binaryReader) Uint16() uint16 {
	if r.err != nil {
		return 0xffff
	}

	bytes := r.Bytes(2)

	if len(bytes) != 2 {
		return 0xffff
	}

	return r.order.Uint16(bytes)
}

// Uint32 implements BinaryReader.
func (r *binaryReader) Uint32() uint32 {
	if r.err != nil {
		return 0xffff_ffff
	}

	bytes := r.Bytes(4)

	if len(bytes) != 4 {
		return 0xffff_ffff
	}

	return r.order.Uint32(bytes)
}

// Uint64 implements BinaryReader.
func (r *binaryReader) Uint64() uint64 {
	if r.err != nil {
		return 0xffff_ffff_ffff_ffff
	}

	bytes := r.Bytes(8)

	if len(bytes) != 8 {
		return 0xffff_ffff_ffff_ffff
	}

	return r.order.Uint64(bytes)
}

type binaryWriter struct {
	writer io.Writer
	order  binary.ByteOrder
	offset uint64
	err    error
}

// Tell implements BinaryWriter.
func (w *binaryWriter) Tell() uint64 {
	return w.offset
}

// Struct implements BinaryWriter.
func (w *binaryWriter) Struct(s any) {
	if w.err != nil {
		return
	}

	err := binary.Write(w.writer, w.order, s)
	if err != nil {
		w.err = err
		return
	}
}

// Bytes implements BinaryWriter.
func (w *binaryWriter) Bytes(b []byte) {
	_, err := w.writer.Write(b)
	if err != nil {
		w.err = err
		return
	}

	w.offset += uint64(len(b))
}

// Error implements BinaryWriter.
func (w *binaryWriter) Error() error {
	err := w.err
	w.err = nil
	return err
}

// Int16 implements BinaryWriter.
func (w *binaryWriter) Int16(v int16) {
	buf := make([]byte, 2)
	w.order.PutUint16(buf, uint16(v))
	w.Bytes(buf)
}

// Int32 implements BinaryWriter.
func (w *binaryWriter) Int32(v int32) {
	buf := make([]byte, 4)
	w.order.PutUint32(buf, uint32(v))
	w.Bytes(buf)
}

// Int64 implements BinaryWriter.
func (w *binaryWriter) Int64(v int64) {
	buf := make([]byte, 8)
	w.order.PutUint64(buf, uint64(v))
	w.Bytes(buf)
}

// Int8 implements BinaryWriter.
func (w *binaryWriter) Int8(v int8) {
	w.Bytes([]byte{byte(v)})
}

// Uint16 implements BinaryWriter.
func (w *binaryWriter) Uint16(v uint16) {
	buf := make([]byte, 2)
	w.order.PutUint16(buf, uint16(v))
	w.Bytes(buf)
}

// Uint32 implements BinaryWriter.
func (w *binaryWriter) Uint32(v uint32) {
	buf := make([]byte, 4)
	w.order.PutUint32(buf, uint32(v))
	w.Bytes(buf)
}

// Uint64 implements BinaryWriter.
func (w *binaryWriter) Uint64(v uint64) {
	buf := make([]byte, 8)
	w.order.PutUint64(buf, uint64(v))
	w.Bytes(buf)
}

// Uint8 implements BinaryWriter.
func (w *binaryWriter) Uint8(v uint8) {
	w.Bytes([]byte{byte(v)})
}

var (
	_ BinaryReader = &binaryReader{}
	_ BinaryWriter = &binaryWriter{}
)

func NewReader(r io.Reader, order binary.ByteOrder) BinaryReader {
	return &binaryReader{
		reader: r,
		order:  order,
	}
}

func BytesReader(data []byte, order binary.ByteOrder) BinaryReader {
	return NewReader(bytes.NewReader(data), order)
}

func NewWriter(w io.Writer, order binary.ByteOrder) BinaryWriter {
	return &binaryWriter{
		writer: w,
		order:  order,
	}
}
