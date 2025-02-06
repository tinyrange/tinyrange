package archive2

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"strings"
	"time"
)

type staticPrintf struct {
	buf []byte
	off int
}

func (s *staticPrintf) next(n int) []byte {
	if s.off+n > len(s.buf) {
		panic("staticPrintf: buffer overflow")
	}

	off := s.off
	s.off += n
	return s.buf[off : off+n]
}

func (s *staticPrintf) WriteInt8(v uint8) {
	s.WriteBytes([]byte{v})
}

func (s *staticPrintf) WriteInt16(v int16) {
	s.WriteBytes([]byte{
		byte(v >> 8),
		byte(v),
	})
}

func (s *staticPrintf) WriteInt32(v int32) {
	s.WriteBytes([]byte{
		byte(v >> 24),
		byte(v >> 16),
		byte(v >> 8),
		byte(v),
	})
}

func (s *staticPrintf) WriteInt64(v int64) {
	s.WriteBytes([]byte{
		byte(v >> 56),
		byte(v >> 48),
		byte(v >> 40),
		byte(v >> 32),
		byte(v >> 24),
		byte(v >> 16),
		byte(v >> 8),
		byte(v),
	})
}

func (s *staticPrintf) WriteBytes(b []byte) {
	hex.Encode(s.next(len(b)*2), b)
}

func (s *staticPrintf) WriteString(str string) {
	copy(s.next(len(str)), str)
}

func (s *staticPrintf) WriteRune(r rune) {
	s.WriteString(string(r))
}

func (s *staticPrintf) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(s.buf[:s.off])
	return int64(n), err
}

func (s *staticPrintf) Reset() {
	s.off = 0
}

func (s *staticPrintf) Grow(n int) {
	if n < 0 {
		panic("staticPrintf: negative count")
	}

	if s.off+n > len(s.buf) {
		s.buf = append(s.buf, make([]byte, n)...)
	}
}

var (
	_ io.WriterTo = (*staticPrintf)(nil)
)

type EntryKind uint8

func (k EntryKind) String() string {
	switch k {
	case EntryKindRegular:
		return "regular"
	case EntryKindDirectory:
		return "directory"
	case EntryKindSymlink:
		return "symlink"
	case EntryKindHardlink:
		return "hardlink"
	default:
		return "invalid"
	}
}

const (
	EntryKindInvalid EntryKind = iota
	EntryKindRegular
	EntryKindDirectory
	EntryKindSymlink
	EntryKindHardlink
)

type EntryFactory struct {
	kind     EntryKind
	name     string
	linkname string
	size     int64
	mode     uint32
	uid      int
	gid      int
	modTime  int64
}

func (e EntryFactory) Kind(k EntryKind) EntryFactory {
	e.kind = k
	return e
}

func (e EntryFactory) Name(s string) EntryFactory {
	if strings.ContainsRune(s, '\t') {
		panic("name contains tab character")
	}

	e.name = s
	return e
}

func (e EntryFactory) Linkname(s string) EntryFactory {
	if strings.ContainsRune(s, '\t') {
		panic("name contains tab character")
	}

	e.linkname = s
	return e
}

func (e EntryFactory) Size(s int64) EntryFactory {
	e.size = s
	return e
}

func (e EntryFactory) Mode(s fs.FileMode) EntryFactory {
	e.mode = uint32(s)
	return e
}

func (e EntryFactory) Owner(uid, gid int) EntryFactory {
	e.uid = uid
	e.gid = gid
	return e
}

func (e EntryFactory) ModTime(t time.Time) EntryFactory {
	e.modTime = t.Unix()
	return e
}

// kind mode uid:gid modTime size offset hash
const staticFormat = " %02x %08x %08x:%08x %016x %016x %016x %064x"
const staticSize = 2 + 8 + 8 + 8 + 16 + 16 + 16 + 64 + 9

const kindOffset = 1
const kindSize = 2
const modeOffset = kindOffset + kindSize + 1
const modeSize = 8
const uidOffset = modeOffset + modeSize + 1
const uidSize = 8
const gidOffset = uidOffset + uidSize + 1
const gidSize = 8
const modTimeOffset = gidOffset + gidSize + 1
const modTimeSize = 16
const sizeOffset = modTimeOffset + modTimeSize + 1
const sizeSize = 16
const offsetOffset = sizeOffset + sizeSize + 1
const offsetSize = 16
const hashOffset = offsetOffset + offsetSize + 1
const hashSize = 64

// static assert for staticSize
var _ [0]struct{} = [(hashOffset + hashSize + 1) - staticSize]struct{}{}

const fullFormat = "%04x" + staticFormat + " %s\t%s\n"

func (e EntryFactory) encode(s *staticPrintf, hashBytes []byte, offset int64) error {
	lineLength := staticSize + len(e.name) + len(e.linkname) + 2
	s.Grow(8 + 1 + lineLength)

	s.WriteInt16(int16(lineLength))
	s.WriteRune(' ')
	s.WriteInt8(uint8(e.kind))
	s.WriteRune(' ')
	s.WriteInt32(int32(e.mode))
	s.WriteRune(' ')
	s.WriteInt32(int32(e.uid))
	s.WriteRune(':')
	s.WriteInt32(int32(e.gid))
	s.WriteRune(' ')
	s.WriteInt64(e.modTime)
	s.WriteRune(' ')
	s.WriteInt64(e.size)
	s.WriteRune(' ')
	s.WriteInt64(offset)
	s.WriteRune(' ')
	s.WriteBytes(hashBytes)
	s.WriteRune(' ')
	s.WriteString(e.name)
	s.WriteRune('\t')
	s.WriteString(e.linkname)
	s.WriteRune('\n')

	return nil
}

type hashedWriter struct {
	writer io.Writer
	hash   hash.Hash
}

func (w *hashedWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	if err == nil {
		_, err = w.hash.Write(p[:n])
	}
	return
}

var _ io.Writer = (*hashedWriter)(nil)

type ArchiveWriter struct {
	index io.Writer

	hashedWriter hashedWriter

	contentsOffset int64

	hashBytes [32]byte

	copyBuffer []byte

	limitReader io.LimitedReader

	staticPrintf staticPrintf
}

func (w *ArchiveWriter) WriteEntry(entry EntryFactory, r io.Reader) error {
	if entry.kind == EntryKindInvalid {
		return fmt.Errorf("invalid entry kind")
	}
	if entry.name == "" {
		return fmt.Errorf("empty entry name")
	}

	w.hashedWriter.hash.Reset()
	w.staticPrintf.Reset()

	if r != nil && entry.size > 0 {
		w.limitReader.R = r
		w.limitReader.N = entry.size

		n, err := io.CopyBuffer(&w.hashedWriter, &w.limitReader, w.copyBuffer)
		if err != nil {
			return fmt.Errorf("failed to write contents: %w", err)
		}
		if n != entry.size {
			return fmt.Errorf("failed to write contents: short write")
		}

		hashBytes := w.hashedWriter.hash.Sum(w.hashBytes[:0])

		if err := entry.encode(&w.staticPrintf, hashBytes, w.contentsOffset); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		if _, err := w.staticPrintf.WriteTo(w.index); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		w.contentsOffset += n
	} else {
		hashBytes := w.hashedWriter.hash.Sum(w.hashBytes[:0])

		if err := entry.encode(&w.staticPrintf, hashBytes, 0); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		if _, err := w.staticPrintf.WriteTo(w.index); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}
	}

	return nil
}

func NewArchiveWriter(index io.Writer, contents io.Writer) *ArchiveWriter {
	return &ArchiveWriter{
		index: index,
		hashedWriter: hashedWriter{
			writer: contents,
			hash:   sha256.New(),
		},
		copyBuffer: make([]byte, 32*1024),
	}
}

type ArchiveReader struct {
	contentsReader io.ReaderAt
	nameEnd        int
	linkNameEnd    int

	lenHex   [4]byte
	lenBytes [2]byte
	buf      [10 * 1024]byte
	index    io.Reader
}

func (e *ArchiveReader) rawKind() []byte {
	return e.buf[kindOffset : kindOffset+kindSize]
}

func (e *ArchiveReader) rawMode() []byte {
	return e.buf[modeOffset : modeOffset+modeSize]
}

func (e *ArchiveReader) rawUid() []byte {
	return e.buf[uidOffset : uidOffset+uidSize]
}

func (e *ArchiveReader) rawGid() []byte {
	return e.buf[gidOffset : gidOffset+gidSize]
}

func (e *ArchiveReader) rawModTime() []byte {
	return e.buf[modTimeOffset : modTimeOffset+modTimeSize]
}

func (e *ArchiveReader) rawSize() []byte {
	return e.buf[sizeOffset : sizeOffset+sizeSize]
}

func (e *ArchiveReader) rawOffset() []byte {
	return e.buf[offsetOffset : offsetOffset+offsetSize]
}

func (e *ArchiveReader) rawHash() []byte {
	return e.buf[hashOffset : hashOffset+hashSize]
}

func (e *ArchiveReader) Kind() EntryKind {
	var kindBytes [1]byte
	hex.Decode(kindBytes[:], e.rawKind())
	return EntryKind(kindBytes[0])
}

func (e *ArchiveReader) Size() int64 {
	var sizeBytes [8]byte
	hex.Decode(sizeBytes[:], e.rawSize())
	return int64(binary.BigEndian.Uint64(sizeBytes[:]))
}

func (e *ArchiveReader) Mode() fs.FileMode {
	var modeBytes [4]byte
	hex.Decode(modeBytes[:], e.rawMode())
	return fs.FileMode(binary.BigEndian.Uint32(modeBytes[:]))
}

func (e *ArchiveReader) Owner() (int, int) {
	var uidBytes [4]byte
	hex.Decode(uidBytes[:], e.rawUid())

	var gidBytes [4]byte
	hex.Decode(gidBytes[:], e.rawGid())

	return int(binary.BigEndian.Uint32(uidBytes[:])), int(binary.BigEndian.Uint32(gidBytes[:]))
}

func (e *ArchiveReader) ModTime() time.Time {
	var modTimeBytes [8]byte
	hex.Decode(modTimeBytes[:], e.rawModTime())
	return time.Unix(int64(binary.BigEndian.Uint64(modTimeBytes[:])), 0)
}

func (e *ArchiveReader) Hash() []byte {
	var hashBytes [32]byte
	hex.Decode(hashBytes[:], e.rawHash())
	return hashBytes[:]
}

type Handle interface {
	io.Reader
	io.ReaderAt
}

func (e *ArchiveReader) offset() int64 {
	var offsetBytes [8]byte
	hex.Decode(offsetBytes[:], e.rawOffset())
	return int64(binary.BigEndian.Uint64(offsetBytes[:]))
}

func (e *ArchiveReader) Open() (Handle, error) {
	if e.Kind() != EntryKindRegular {
		return nil, fs.ErrInvalid
	}

	off := e.offset()

	return io.NewSectionReader(e.contentsReader, off, e.Size()), nil
}

func (e *ArchiveReader) Name() string {
	return string(e.buf[staticSize:e.nameEnd])
}

func (e *ArchiveReader) Linkname() string {
	return string(e.buf[e.nameEnd+1 : e.linkNameEnd])
}

func (r *ArchiveReader) NextEntry() error {
	_, err := io.ReadFull(r.index, r.lenHex[:])
	if err == io.EOF {
		return err
	} else if err != nil {
		return fmt.Errorf("failed to read index entry length: %w", err)
	}

	// slog.Info("lenHex", "lenHex", string(lenHex[:]))

	if _, err := hex.Decode(r.lenBytes[:], r.lenHex[:]); err != nil {
		return fmt.Errorf("failed to decode index entry length: %w", err)
	}

	lineLen := binary.BigEndian.Uint16(r.lenBytes[:])

	if lineLen < staticSize {
		return fmt.Errorf("invalid index entry length: %d < %d", lineLen, staticSize)
	}

	n, err := io.ReadFull(r.index, r.buf[:lineLen])
	if err != nil {
		return fmt.Errorf("failed to read index entry data: %w", err)
	}
	if n != int(lineLen) {
		return fmt.Errorf("failed to read index entry data: short read")
	}

	r.nameEnd = bytes.IndexRune(r.buf[staticSize:lineLen], '\t') + staticSize
	if r.nameEnd == -1 {
		return fmt.Errorf("invalid index entry format, could not find nameEnd")
	}

	r.linkNameEnd = int(lineLen - 1)

	return nil
}

func NewArchiveReader(index io.Reader, contents io.ReaderAt) *ArchiveReader {
	return &ArchiveReader{
		index:          bufio.NewReaderSize(index, 10*1024),
		contentsReader: contents,
	}
}
