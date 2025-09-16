package archive

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
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
	case EntryKindInvalid:
		return "invalid"
	case EntryKindExtended:
		return "extended"
	case EntryKindDeleted:
		return "deleted"
	default:
		return fmt.Sprintf("unknown(%d)", k)
	}
}

const (
	EntryKindInvalid   EntryKind = iota
	EntryKindRegular             // Regular file with contents
	EntryKindDirectory           // Directory
	EntryKindSymlink             // Symbolic link to another file
	EntryKindHardlink            // Hard link to another file
	EntryKindExtended            // Extended file with metadata
	EntryKindDeleted             // Deleted file
)

type Entry struct {
	Kind EntryKind

	Name     string
	Linkname string

	Size int64

	Mode fs.FileMode

	Uid int
	Gid int

	ModTime time.Time
}

const (
	ArchiveMagic = "ARCHIVE0\n"

	// kind mode uid:gid modTime size offset hash
	staticSize = 2 + 8 + 8 + 8 + 16 + 16 + 16 + 64 + 9

	kindOffset    = 1
	kindSize      = 2
	modeOffset    = kindOffset + kindSize + 1
	modeSize      = 8
	uidOffset     = modeOffset + modeSize + 1
	uidSize       = 8
	gidOffset     = uidOffset + uidSize + 1
	gidSize       = 8
	modTimeOffset = gidOffset + gidSize + 1
	modTimeSize   = 16
	sizeOffset    = modTimeOffset + modTimeSize + 1
	sizeSize      = 16
	offsetOffset  = sizeOffset + sizeSize + 1
	offsetSize    = 16
	hashOffset    = offsetOffset + offsetSize + 1
	hashSize      = 64

	terminatorSize = len("\t\n")
)

// static assert for staticSize
var _ [0]struct{} = [(hashOffset + hashSize + 1) - staticSize]struct{}{}

func (e *Entry) encode(s *staticPrintf, hashBytes []byte, offset int64) error {
	if strings.ContainsAny(e.Name, "\n\t") || strings.ContainsAny(e.Linkname, "\n\t") {
		return fmt.Errorf("invalid entry name or linkname contains control characters")
	}
	lineLength := staticSize + len(e.Name) + len(e.Linkname) + terminatorSize
	s.Grow(8 + 1 + lineLength)

	s.WriteInt16(int16(lineLength))
	s.WriteRune(' ')
	s.WriteInt8(uint8(e.Kind))
	s.WriteRune(' ')
	s.WriteInt32(int32(e.Mode))
	s.WriteRune(' ')
	s.WriteInt32(int32(e.Uid))
	s.WriteRune(':')
	s.WriteInt32(int32(e.Gid))
	s.WriteRune(' ')
	s.WriteInt64(e.ModTime.Unix())
	s.WriteRune(' ')
	s.WriteInt64(e.Size)
	s.WriteRune(' ')
	s.WriteInt64(offset)
	s.WriteRune(' ')
	s.WriteBytes(hashBytes)
	s.WriteRune(' ')
	s.WriteString(e.Name)
	s.WriteRune('\t')
	s.WriteString(e.Linkname)
	s.WriteRune('\n')

	return nil
}

type hashedWriter struct {
	writer io.Writer
	hash   hash.Hash
}

func (w *hashedWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil {
		_, err = w.hash.Write(p[:n])
	}
	return n, err
}

var _ io.Writer = (*hashedWriter)(nil)

type Writer struct {
	index          io.Writer        // the index file writer
	hashedWriter   hashedWriter     // an optimized version of io.MultiWriter that also hashes the data
	contentsOffset int64            // the current offset in the contents file (maintained separately from the writer)
	hashBytes      [32]byte         // used to store the hash of the contents
	copyBuffer     []byte           // used to copy data from the reader to the contents
	limitReader    io.LimitedReader // used to limit the number of bytes written to the contents
	staticPrintf   staticPrintf     // used to write data to the index
	enablePadding  bool             // whether to enable padding the size of files to 4096 bytes
}

var paddingBytes [4096]byte

func (w *Writer) WriteEntry(entry *Entry, r io.Reader) error {
	if entry.Kind == EntryKindInvalid {
		return errors.New("invalid entry kind")
	}
	if entry.Name == "" {
		return errors.New("empty entry name")
	}

	w.hashedWriter.hash.Reset()
	w.staticPrintf.Reset()

	if r != nil && entry.Size > 0 {
		w.limitReader.R = r
		w.limitReader.N = entry.Size

		// write contents
		n, err := io.CopyBuffer(&w.hashedWriter, &w.limitReader, w.copyBuffer)
		if err != nil {
			return fmt.Errorf("failed to write contents: %w", err)
		}
		if n != entry.Size {
			return errors.New("failed to write contents: short write")
		}

		// ensure that each file is aligned to 4096 bytes
		if n%4096 != 0 && w.enablePadding {
			padding := 4096 - (n % 4096)
			if _, err := w.hashedWriter.writer.Write(paddingBytes[:padding]); err != nil {
				return fmt.Errorf("failed to write padding: %w", err)
			}
			n += padding
		}

		hashBytes := w.hashedWriter.hash.Sum(w.hashBytes[:0])

		// encode entry
		if err := entry.encode(&w.staticPrintf, hashBytes, w.contentsOffset); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		// write entry to index
		if _, err := w.staticPrintf.WriteTo(w.index); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		w.contentsOffset += n
	} else {
		// no contents
		hashBytes := w.hashedWriter.hash.Sum(w.hashBytes[:0])

		// encode entry
		if err := entry.encode(&w.staticPrintf, hashBytes, 0); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		// write entry to index
		if _, err := w.staticPrintf.WriteTo(w.index); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}
	}

	return nil
}

func (w *Writer) DisablePadding() {
	w.enablePadding = false
}

func NewWriter(index, contents io.Writer) (*Writer, error) {
	ret := &Writer{
		index: index,
		hashedWriter: hashedWriter{
			writer: contents,
			hash:   sha256.New(),
		},
		copyBuffer:    make([]byte, 32*1024),
		enablePadding: true,
	}

	if _, err := ret.index.Write([]byte(ArchiveMagic)); err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	return ret, nil
}

type Reader struct {
	index          *bufio.Reader
	indexCloser    io.Closer
	contentsReader io.ReaderAt
	nameEnd        int
	linkNameEnd    int
	lenHex         [4]byte
	lenBytes       [2]byte
	buf            [10 * 1024]byte
}

// Close implements io.Closer.
func (ar *Reader) Close() error {
	if ar.indexCloser != nil {
		return ar.indexCloser.Close()
	}
	return nil
}

func (ar *Reader) rawKind() []byte {
	return ar.buf[kindOffset : kindOffset+kindSize]
}

func (ar *Reader) rawMode() []byte {
	return ar.buf[modeOffset : modeOffset+modeSize]
}

func (ar *Reader) rawUID() []byte {
	return ar.buf[uidOffset : uidOffset+uidSize]
}

func (ar *Reader) rawGID() []byte {
	return ar.buf[gidOffset : gidOffset+gidSize]
}

func (ar *Reader) rawModTime() []byte {
	return ar.buf[modTimeOffset : modTimeOffset+modTimeSize]
}

func (ar *Reader) rawSize() []byte {
	return ar.buf[sizeOffset : sizeOffset+sizeSize]
}

func (ar *Reader) rawOffset() []byte {
	return ar.buf[offsetOffset : offsetOffset+offsetSize]
}

func (ar *Reader) rawHash() []byte {
	return ar.buf[hashOffset : hashOffset+hashSize]
}

// Kind returns the kind of the entry.
func (ar *Reader) Kind() EntryKind {
	var kindBytes [1]byte
	if _, err := hex.Decode(kindBytes[:], ar.rawKind()); err != nil {
		return EntryKindInvalid
	}
	return EntryKind(kindBytes[0])
}

// Size returns the size of the entry in bytes.
func (ar *Reader) Size() int64 {
	var sizeBytes [8]byte
	if _, err := hex.Decode(sizeBytes[:], ar.rawSize()); err != nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(sizeBytes[:]))
}

// Mode returns the mode of the entry.
func (ar *Reader) Mode() fs.FileMode {
	var modeBytes [4]byte
	if _, err := hex.Decode(modeBytes[:], ar.rawMode()); err != nil {
		return 0
	}
	return fs.FileMode(binary.BigEndian.Uint32(modeBytes[:]))
}

// Owner returns the uid and gid of the entry.
func (ar *Reader) Owner() (uid, gid int) {
	var uidBytes [4]byte
	if _, err := hex.Decode(uidBytes[:], ar.rawUID()); err != nil {
		return 0, 0
	}

	var gidBytes [4]byte
	if _, err := hex.Decode(gidBytes[:], ar.rawGID()); err != nil {
		return 0, 0
	}

	return int(binary.BigEndian.Uint32(uidBytes[:])), int(binary.BigEndian.Uint32(gidBytes[:]))
}

// ModTime returns the modification time of the entry.
func (ar *Reader) ModTime() time.Time {
	var modTimeBytes [8]byte
	if _, err := hex.Decode(modTimeBytes[:], ar.rawModTime()); err != nil {
		return time.Time{}
	}
	return time.Unix(int64(binary.BigEndian.Uint64(modTimeBytes[:])), 0)
}

// Hash returns the SHA256 hash of the contents of the entry.
func (ar *Reader) Hash() []byte {
	var hashBytes [32]byte
	if _, err := hex.Decode(hashBytes[:], ar.rawHash()); err != nil {
		return nil
	}
	return hashBytes[:]
}

type Handle interface {
	io.Reader
	io.ReaderAt
}

func (ar *Reader) offset() (int64, error) {
	var offsetBytes [8]byte
	if _, err := hex.Decode(offsetBytes[:], ar.rawOffset()); err != nil {
		return 0, fmt.Errorf("failed to decode offset: %w", err)
	}
	return int64(binary.BigEndian.Uint64(offsetBytes[:])), nil
}

// Open returns a handle to the entry.
func (ar *Reader) Open() (Handle, error) {
	if ar.Kind() != EntryKindRegular && ar.Kind() != EntryKindExtended {
		return nil, fs.ErrInvalid
	}

	off, err := ar.offset()
	if err != nil {
		return nil, err
	}

	return io.NewSectionReader(ar.contentsReader, off, ar.Size()), nil
}

// Name returns the name of the entry.
func (ar *Reader) Name() string {
	return string(ar.buf[staticSize:ar.nameEnd])
}

// Linkname returns the linkname of the entry.
func (ar *Reader) Linkname() string {
	return string(ar.buf[ar.nameEnd+1 : ar.linkNameEnd])
}

// NextEntry reads the next entry from the index.
func (ar *Reader) NextEntry() error {
	// Read the length of the index entry first.
	_, err := io.ReadFull(ar.index, ar.lenHex[:])
	if err == io.EOF {
		return err
	} else if err != nil {
		return fmt.Errorf("failed to read index entry length: %w", err)
	}

	// Decode the length of the index entry.
	if _, err := hex.Decode(ar.lenBytes[:], ar.lenHex[:]); err != nil {
		return fmt.Errorf("failed to decode index entry length: %w", err)
	}

	lineLen := binary.BigEndian.Uint16(ar.lenBytes[:])

	// If the line length is less than the static size, then the index entry is invalid.
	if lineLen < staticSize {
		return fmt.Errorf("invalid index entry length: %d < %d", lineLen, staticSize)
	}

	// Check for overflows of our buffer.
	if lineLen > uint16(len(ar.buf)) {
		return fmt.Errorf("index entry length too large: %d > %d", lineLen, len(ar.buf))
	}

	// Read the rest of the index entry.
	n, err := io.ReadFull(ar.index, ar.buf[:lineLen])
	if err != nil {
		return fmt.Errorf("failed to read index entry data: %w", err)
	}
	if n != int(lineLen) {
		return errors.New("failed to read index entry data: short read")
	}

	// The filename and linkname are separated by a tab character.
	ar.nameEnd = bytes.IndexRune(ar.buf[staticSize:lineLen], '\t')
	if ar.nameEnd == -1 {
		return errors.New("invalid index entry format, could not find nameEnd")
	}
	ar.nameEnd += staticSize

	ar.linkNameEnd = int(lineLen - 1)

	return nil
}

func (ar *Reader) validateHeader() error {
	var headerBytes [9]byte

	_, err := io.ReadFull(ar.index, headerBytes[:])
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	if string(headerBytes[:]) != ArchiveMagic {
		return errors.New("invalid header")
	}

	return nil
}

var (
	_ io.Closer = (*Reader)(nil)
)

func NewReader(index io.Reader, indexCloser io.Closer, contents io.ReaderAt) (*Reader, error) {
	ret := &Reader{
		index:          bufio.NewReaderSize(index, 10*1024),
		indexCloser:    indexCloser,
		contentsReader: contents,
	}

	if err := ret.validateHeader(); err != nil {
		return nil, err
	}

	return ret, nil
}
