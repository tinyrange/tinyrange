package main

import (
	"archive/tar"
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

type filesystemBuilder struct {
}

const CURRENT_ARCHIVE_VERSION = 1

type archiveWriter struct {
	contents      io.WriterAt
	index         *bufio.Writer
	currentOffset int64
}

func (w *archiveWriter) Flush() error {
	return w.index.Flush()
}

func (w *archiveWriter) AddEntry(
	kind filesystem.FileType, // kind of entry
	name string, // name of entry
	linkname string, // link target
	size int64, // size of entry
	mode fs.FileMode, // FileMode of entry
	uid int, // user id
	gid int, // group id
	modTime time.Time, // modification time in microseconds since the unix epoch
	devMajor int64, // major device number
	devMinor int64, // minor device number
	contents io.Reader, // contents of entry
) error {
	var hdr RawArchiveEntryHeader
	var ent RawArchiveEntry

	ent.SetContentsOffset(uint64(w.currentOffset))
	ent.SetKind(uint8(kind))
	ent.SetNameLen(uint16(len(name)))
	ent.SetLinknameLen(uint16(len(linkname)))
	ent.SetContentSize(uint64(size))
	ent.SetMode(uint64(mode))
	ent.SetUid(uint32(uid))
	ent.SetGid(uint32(gid))
	ent.SetModTime(uint64(modTime.UnixMicro()))
	ent.SetDevmajor(uint64(devMajor))
	ent.SetDevminor(uint64(devMinor))

	hdr.SetEntrySize(uint16(ent.Size()))
	hdr.SetVersion(CURRENT_ARCHIVE_VERSION)
	hdr.SetTotalSize(uint16(ent.Size()) + uint16(len(name)+len(linkname)))

	// write the header
	if _, err := w.index.Write(hdr[:]); err != nil {
		return err
	}

	// write the entry
	if _, err := w.index.Write(ent[:]); err != nil {
		return err
	}

	// write the name
	if _, err := w.index.WriteString(name); err != nil {
		return err
	}

	// write the linkname
	if _, err := w.index.WriteString(linkname); err != nil {
		return err
	}

	offWrite := io.NewOffsetWriter(w.contents, w.currentOffset)

	if _, err := io.CopyN(offWrite, contents, size); err != nil {
		return err
	}

	w.currentOffset += size

	return nil
}

func createWriter(contentsFile io.WriterAt, indexFile io.Writer) *archiveWriter {
	return &archiveWriter{
		contents: contentsFile,
		index:    bufio.NewWriter(indexFile),
	}
}

func createFilesystemWriter(baseFilename string) (*archiveWriter, []io.Closer, error) {
	// create the contents file
	contentsFile, err := os.Create(baseFilename + ".contents")
	if err != nil {
		return nil, nil, err
	}

	// create the index file
	indexFile, err := os.Create(baseFilename + ".index")
	if err != nil {
		return nil, nil, err
	}

	return createWriter(contentsFile, indexFile), []io.Closer{
		contentsFile, indexFile,
	}, nil
}

type archiveFileHandle struct {
	*io.SectionReader
}

func (h *archiveFileHandle) Close() error {
	return nil
}

var (
	_ filesystem.FileHandle = &archiveFileHandle{}
)

type ArchiveEntry struct {
	hdr          RawArchiveEntryHeader
	raw          RawArchiveEntry
	entryBytes   []byte
	contentsFile io.ReaderAt
}

func (e *ArchiveEntry) Kind() filesystem.FileType {
	return filesystem.FileType(e.raw.Kind())
}

// Name implements fs.FileInfo.
func (e *ArchiveEntry) Name() string {
	return string(e.NameBytes())
}

func (e *ArchiveEntry) NameBytes() []byte {
	return e.entryBytes[e.raw.Size() : uint16(e.raw.Size())+e.raw.NameLen()]
}

func (e *ArchiveEntry) Linkname() string {
	return string(e.entryBytes[uint16(e.raw.Size())+e.raw.NameLen() : uint16(e.raw.Size())+e.raw.NameLen()+e.raw.LinknameLen()])
}

// Size implements fs.FileInfo.
func (e *ArchiveEntry) Size() int64 {
	return int64(e.raw.ContentSize())
}

// Mode implements fs.FileInfo.
func (e *ArchiveEntry) Mode() fs.FileMode {
	return fs.FileMode(e.raw.Mode())
}

// IsDir implements fs.FileInfo.
func (e *ArchiveEntry) IsDir() bool {
	return e.Kind() == filesystem.TypeDirectory
}

// ModTime implements fs.FileInfo.
func (e *ArchiveEntry) ModTime() time.Time {
	return time.UnixMicro(int64(e.raw.ModTime()))
}

// Sys implements fs.FileInfo.
func (e *ArchiveEntry) Sys() any {
	return nil
}

func (e *ArchiveEntry) Open() (filesystem.FileHandle, error) {
	return &archiveFileHandle{
		SectionReader: io.NewSectionReader(e.contentsFile, int64(e.raw.ContentsOffset()), int64(e.raw.ContentSize())),
	}, nil
}

func (e *ArchiveEntry) ReadAt(p []byte, off int64) (int, error) {
	// bounds check
	if off < 0 || off >= e.Size() {
		return 0, io.EOF
	}

	// cap to the size of the entry
	if off+int64(len(p)) > e.Size() {
		p = p[:e.Size()-off]
	}

	// read from the underlying contents at the offset
	return e.contentsFile.ReadAt(p, off+int64(e.raw.ContentsOffset()))
}

func (e *ArchiveEntry) Stat() (filesystem.FileInfo, error) {
	return e, nil
}

func (e *ArchiveEntry) Digest() *filesystem.FileDigest {
	return nil
}

var (
	_ filesystem.FileInfo = &ArchiveEntry{}
	_ filesystem.File     = &ArchiveEntry{}
	_ io.ReaderAt         = &ArchiveEntry{}
)

type archiveReader struct {
	indexFile    *bufio.Reader
	contentsFile io.ReaderAt
}

func (r *archiveReader) ReadEntry(ent *ArchiveEntry) error {
	if _, err := io.ReadFull(r.indexFile, ent.hdr[:]); err != nil {
		return err
	}

	if ent.hdr.Version() != CURRENT_ARCHIVE_VERSION {
		return fmt.Errorf("unsupported archive version: %d", ent.hdr.Version())
	}

	if cap(ent.entryBytes) < int(ent.hdr.TotalSize()) {
		ent.entryBytes = make([]byte, ent.hdr.TotalSize())
	} else {
		ent.entryBytes = ent.entryBytes[:ent.hdr.TotalSize()]
	}

	if _, err := io.ReadFull(r.indexFile, ent.entryBytes); err != nil {
		return err
	}

	ent.raw = RawArchiveEntry(ent.entryBytes[:ent.hdr.EntrySize()])

	ent.contentsFile = r.contentsFile

	return nil
}

func newReader(indexFile io.Reader, contentsFile io.ReaderAt) *archiveReader {
	return &archiveReader{
		indexFile:    bufio.NewReaderSize(indexFile, 32*1024),
		contentsFile: contentsFile,
	}
}

func tarToArchive(input *tar.Reader, output *archiveWriter) error {
	for {
		header, err := input.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		var kind filesystem.FileType

		switch header.Typeflag {
		case tar.TypeReg:
			kind = filesystem.TypeRegular
		case tar.TypeDir:
			kind = filesystem.TypeDirectory
		case tar.TypeSymlink:
			kind = filesystem.TypeSymlink
		case tar.TypeLink:
			kind = filesystem.TypeLink
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("unsupported entry type: %v", header.Typeflag)
		}

		if err := output.AddEntry(
			kind,
			header.Name,
			header.Linkname,
			header.Size,
			header.FileInfo().Mode(),
			header.Uid,
			header.Gid,
			header.ModTime,
			header.Devmajor,
			header.Devminor,
			input,
		); err != nil {
			return err
		}
	}

	if err := output.Flush(); err != nil {
		return err
	}

	return nil
}

func RangeTokens(input []byte, sep byte) func(yield func(token []byte) bool) {
	return func(yield func(token []byte) bool) {
		var start int

		for i, b := range input {
			if b == sep {
				if !yield(input[start:i]) {
					return
				}
				start = i + 1
			}
		}

		if !yield(input[start:]) {
			return
		}
	}
}

var (
	mode       = flag.String("mode", "convert", "convert or build")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func appMain() error {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	switch *mode {
	case "convert":
		for _, arg := range flag.Args() {
			baseOut := strings.TrimSuffix(arg, ".tar")

			// open the tar file
			tarFile, err := os.Open(arg)
			if err != nil {
				return err
			}
			defer tarFile.Close()

			// create the filesystem writer
			writer, closers, err := createFilesystemWriter(baseOut)
			if err != nil {
				return err
			}
			defer func() {
				for _, closer := range closers {
					closer.Close()
				}
			}()

			// convert the tar file to the filesystem
			if err := tarToArchive(tar.NewReader(tarFile), writer); err != nil {
				return err
			}
		}

		return nil
	case "build":
		start := time.Now()

		n := 1000

		pb := progressbar.Default(int64(n), "building")
		defer pb.Close()

		for range n {
			var closers []io.Closer
			defer func() {
				for _, closer := range closers {
					closer.Close()
				}
			}()

			for _, arg := range flag.Args() {
				// assume that index files are passed in.
				indexFile, err := os.Open(arg)
				if err != nil {
					return err
				}
				defer indexFile.Close()

				contentsFile, err := os.Open(strings.TrimSuffix(arg, ".index") + ".contents")
				if err != nil {
					return err
				}
				closers = append(closers, contentsFile)

				reader := newReader(indexFile, contentsFile)

				var ent ArchiveEntry

				for {
					err := reader.ReadEntry(&ent)
					if err == io.EOF {
						break
					} else if err != nil {
						return err
					}

					for range RangeTokens(ent.NameBytes(), '/') {
						// do nothing
					}
				}
			}

			pb.Add(1)
		}

		slog.Info("read archive", "times", n, "duration", time.Since(start))

		return nil
	default:
		return fmt.Errorf("invalid mode: %s", *mode)
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
