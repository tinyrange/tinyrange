package main

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"runtime/pprof"
	"strings"
	"time"
)

var hexLookup [0xffff]int16

func init() {
	for i := 0; i < 0xffff; i++ {
		hexLookup[i] = -1
	}
	for i := 0; i <= 0xff; i++ {
		var b [2]byte
		hex.Encode(b[:], []byte{byte(i)})
		hexLookup[binary.LittleEndian.Uint16(b[:])] = int16(i)
	}
}

func hexDecode(dst, src []byte) (int, error) {
	if len(src)%2 != 0 {
		return 0, fmt.Errorf("hexDecode: odd length")
	}

	for i := 0; i < len(src); i += 2 {
		h := hexLookup[binary.LittleEndian.Uint16(src[i:])]
		if h < 0 {
			return i / 2, fmt.Errorf("hexDecode: invalid hex character")
		}

		dst[i/2] = byte(h)
	}

	return len(src) / 2, nil
}

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
		return e
	}

	e.name = s
	return e
}

func (e EntryFactory) Linkname(s string) EntryFactory {
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

func (e EntryFactory) Encode(w io.Writer, hash hash.Hash, offset int64) error {
	hashBytes := make([]byte, 0, hash.Size())
	if e.size > 0 {
		hashBytes = hash.Sum(hashBytes)
	}

	lineLength := staticSize + len(e.name) + len(e.linkname) + 2

	_, err := fmt.Fprintf(w, "%04x"+staticFormat+" %s\t%s\n",
		lineLength, uint8(e.kind), e.mode, e.uid, e.gid, e.modTime, e.size, offset, hashBytes, e.name, e.linkname)
	if err != nil {
		return fmt.Errorf("failed to write index entry: %w", err)
	}

	return nil
}

type ArchiveWriter struct {
	index    io.Writer
	contents io.Writer

	contentsOffset int64
}

func (w *ArchiveWriter) WriteEntry(entry EntryFactory, r io.Reader) error {
	if entry.kind == EntryKindInvalid {
		return fmt.Errorf("invalid entry kind")
	}
	if entry.name == "" {
		return fmt.Errorf("empty entry name")
	}

	contentsHash := sha256.New()

	if r != nil && entry.size > 0 {
		n, err := io.CopyN(io.MultiWriter(w.contents, contentsHash), r, entry.size)
		if err != nil {
			return fmt.Errorf("failed to write contents: %w", err)
		}

		if err := entry.Encode(w.index, contentsHash, w.contentsOffset); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}

		w.contentsOffset += n
	} else {
		if err := entry.Encode(w.index, contentsHash, 0); err != nil {
			return fmt.Errorf("failed to write index entry: %w", err)
		}
	}

	return nil
}

func NewArchiveWriter(index io.Writer, contents io.Writer) *ArchiveWriter {
	return &ArchiveWriter{
		index:    index,
		contents: contents,
	}
}

type Entry struct {
	reader     io.ReaderAt
	staticData [staticSize]byte
	name       string
	linkname   string
}

func (e *Entry) rawKind() []byte {
	return e.staticData[kindOffset : kindOffset+kindSize]
}

func (e *Entry) rawMode() []byte {
	return e.staticData[modeOffset : modeOffset+modeSize]
}

func (e *Entry) rawUid() []byte {
	return e.staticData[uidOffset : uidOffset+uidSize]
}

func (e *Entry) rawGid() []byte {
	return e.staticData[gidOffset : gidOffset+gidSize]
}

func (e *Entry) rawModTime() []byte {
	return e.staticData[modTimeOffset : modTimeOffset+modTimeSize]
}

func (e *Entry) rawSize() []byte {
	return e.staticData[sizeOffset : sizeOffset+sizeSize]
}

func (e *Entry) rawOffset() []byte {
	return e.staticData[offsetOffset : offsetOffset+offsetSize]
}

func (e *Entry) rawHash() []byte {
	return e.staticData[hashOffset : hashOffset+hashSize]
}

func (e *Entry) Kind() EntryKind {
	var kindBytes [1]byte
	hex.Decode(kindBytes[:], e.rawKind())
	return EntryKind(kindBytes[0])
}

func (e *Entry) Size() int64 {
	var sizeBytes [8]byte
	hexDecode(sizeBytes[:], e.rawSize())
	return int64(binary.BigEndian.Uint64(sizeBytes[:]))
}

func (e *Entry) Mode() fs.FileMode {
	var modeBytes [4]byte
	hexDecode(modeBytes[:], e.rawMode())
	return fs.FileMode(binary.BigEndian.Uint32(modeBytes[:]))
}

func (e *Entry) Owner() (int, int) {
	var uidBytes [4]byte
	hexDecode(uidBytes[:], e.rawUid())

	var gidBytes [4]byte
	hexDecode(gidBytes[:], e.rawGid())

	return int(binary.BigEndian.Uint32(uidBytes[:])), int(binary.BigEndian.Uint32(gidBytes[:]))
}

func (e *Entry) ModTime() time.Time {
	var modTimeBytes [8]byte
	hexDecode(modTimeBytes[:], e.rawModTime())
	return time.Unix(int64(binary.BigEndian.Uint64(modTimeBytes[:])), 0)
}

func (e *Entry) Hash() []byte {
	var hashBytes [32]byte
	hexDecode(hashBytes[:], e.rawHash())
	return hashBytes[:]
}

type Handle interface {
	io.Reader
	io.ReaderAt
}

func (e *Entry) offset() int64 {
	var offsetBytes [8]byte
	hexDecode(offsetBytes[:], e.rawOffset())
	return int64(binary.BigEndian.Uint64(offsetBytes[:]))
}

func (e *Entry) Open() (Handle, error) {
	if e.Kind() != EntryKindRegular {
		return nil, fs.ErrInvalid
	}

	off := e.offset()

	return io.NewSectionReader(e.reader, off, e.Size()), nil
}

func (e *Entry) Name() string {
	return e.name
}

func (e *Entry) Linkname() string {
	return e.linkname
}

type ArchiveReader struct {
	Entry
	lenHex   [4]byte
	lenBytes [2]byte
	buf      [0xffff]byte
	index    io.Reader
	contents io.ReaderAt
}

func (r *ArchiveReader) NextEntry() error {
	// slog.Info("readEntry")]
	_, err := io.ReadFull(r.index, r.lenHex[:])
	if err == io.EOF {
		return err
	} else if err != nil {
		return fmt.Errorf("failed to read index entry length: %w", err)
	}

	// slog.Info("lenHex", "lenHex", string(lenHex[:]))

	if _, err := hexDecode(r.lenBytes[:], r.lenHex[:]); err != nil {
		return fmt.Errorf("failed to decode index entry length: %w", err)
	}

	lineLen := binary.BigEndian.Uint16(r.lenBytes[:])

	if lineLen < staticSize {
		return fmt.Errorf("invalid index entry length")
	}

	n, err := io.ReadFull(r.index, r.buf[:lineLen])
	if err != nil {
		return fmt.Errorf("failed to read index entry data: %w", err)
	}
	if n != int(lineLen) {
		return fmt.Errorf("failed to read index entry data: short read")
	}

	// slog.Info("data", "data", string(r.buf[:lineLen]))

	copy(r.staticData[:], r.buf[:staticSize])

	tokens := strings.SplitN(string(r.buf[staticSize:lineLen]), "\t", 2)
	if len(tokens) != 2 {
		return fmt.Errorf("invalid index entry format")
	}

	r.reader = r.contents
	r.name, r.linkname = tokens[0], tokens[1]

	return nil
}

func NewArchiveReader(index io.Reader, contents io.ReaderAt) *ArchiveReader {
	return &ArchiveReader{
		index:    bufio.NewReader(index),
		contents: contents,
	}
}

var (
	read       = flag.Bool("read", false, "read the archive")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func appMain() error {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fmt.Errorf("failed to create CPU profile: %w", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("failed to start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if !*read {
		if flag.NArg() != 1 {
			return fmt.Errorf("usage: %s <filename>", os.Args[0])
		}

		filename := flag.Arg(0)

		f, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer f.Close()

		reader := tar.NewReader(f)

		indexFile, err := os.Create(filename + ".index")
		if err != nil {
			return fmt.Errorf("failed to create index file: %w", err)
		}
		defer indexFile.Close()

		contentsFile, err := os.Create(filename + ".contents")
		if err != nil {
			return fmt.Errorf("failed to create contents file: %w", err)
		}
		defer contentsFile.Close()

		writer := NewArchiveWriter(indexFile, contentsFile)

		start := time.Now()

		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return fmt.Errorf("failed to read tar header: %w", err)
			}

			var ent EntryFactory

			switch header.Typeflag {
			case tar.TypeReg:
				ent = ent.Kind(EntryKindRegular)
			case tar.TypeDir:
				ent = ent.Kind(EntryKindDirectory)
			case tar.TypeSymlink:
				ent = ent.Kind(EntryKindSymlink)
			case tar.TypeLink:
				ent = ent.Kind(EntryKindHardlink)
			case tar.TypeXGlobalHeader:
				continue
			default:
				return fmt.Errorf("unsupported tar entry type: %v", header.Typeflag)
			}

			ent = ent.Name(header.Name).
				Linkname(header.Linkname).
				Size(header.Size).
				Mode(header.FileInfo().Mode()).
				Owner(header.Uid, header.Gid).
				ModTime(header.ModTime)

			if err := writer.WriteEntry(ent, reader); err != nil {
				return fmt.Errorf("failed to write entry: %w", err)
			}
		}

		slog.Info("elapsed", "time", time.Since(start))

		return nil
	} else {
		if flag.NArg() != 1 {
			return fmt.Errorf("usage: %s -read <filename>", os.Args[0])
		}

		slog.Info("reading archive")

		filename := flag.Arg(0)

		indexFile, err := os.Open(filename + ".index")
		if err != nil {
			return fmt.Errorf("failed to open index file: %w", err)
		}
		defer indexFile.Close()

		contentsFile, err := os.Open(filename + ".contents")
		if err != nil {
			return fmt.Errorf("failed to open contents file: %w", err)
		}
		defer contentsFile.Close()

		reader := NewArchiveReader(indexFile, contentsFile)

		start := time.Now()

		total := 0

		for {
			err := reader.NextEntry()
			if err == io.EOF {
				break
			} else if err != nil {
				return fmt.Errorf("failed to read entry: %w", err)
			}

			total += int(reader.Kind())

			// slog.Info("entry",
			// 	"kind", reader.Kind(),
			// 	"name", reader.Name(),
			// 	"size", reader.Size(),
			// 	"mode", reader.Mode(),
			// 	"modTime", reader.ModTime(),
			// 	"hash", fmt.Sprintf("%x", reader.Hash()),
			// )

			// _ = ent
		}

		slog.Info("elapsed", "time", time.Since(start), "total", total)

		return nil
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
