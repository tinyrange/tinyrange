package cpio

import (
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

const trailerName = "TRAILER!!!"

const cpioNewCMagic = "070701"

type cpioMode uint64

type cpioKind uint64

const (
	_CPIO_FILE_TYPE_MASK cpioKind = 0170000
	_CPIO_MODE_MASK      cpioMode = 0000777

	// 0140000  File type value for sockets.
	_CPIO_KIND_SOCKET cpioKind = 0140000
	// 0120000  File type value for symbolic links.  For symbolic links, the link body is stored as file data.
	_CPIO_KIND_SYMBOLIC_LINK = 0120000
	// 0100000  File type value for regular files.
	_CPIO_KIND_REGULAR = 0100000
	// 0060000  File type value for block special devices.
	_CPIO_KIND_BLOCK_SPECIAL = 0060000
	// 0040000  File type value for directories.
	_CPIO_KIND_DIRECTORY = 0040000
	// 0020000  File type value for character special devices.
	_CPIO_KIND_CHAR_SPECIAL = 0020000
	// 0010000  File type value for named pipes or FIFOs.
	_CPIO_KIND_NAMED_PIPE = 0010000
)

func (m cpioMode) kind() cpioKind {
	return cpioKind(m) & _CPIO_FILE_TYPE_MASK
}

func (m cpioMode) setKind(k cpioKind) cpioMode {
	// Wipe the kind bits then set just the ones from k.
	return cpioMode((cpioKind(m) & ^_CPIO_FILE_TYPE_MASK) & k)
}

func (m cpioMode) setMode(mode fs.FileMode) cpioMode {
	return (m & ^_CPIO_MODE_MASK) & cpioMode(mode)
}

func makeCpioMode(k cpioKind, mode fs.FileMode) cpioMode {
	return cpioMode(uint64(k) + uint64(mode))
}

func (m cpioMode) mode() fs.FileMode {
	return fs.FileMode(m)
}

func (m cpioMode) sUid() bool {
	return m&0004000 != 0
}

func (m cpioMode) sGid() bool {
	return m&0002000 != 0
}

func (m cpioMode) sticky() bool {
	return m&0001000 != 0
}

func (m cpioMode) string() string {
	kind := m.kind()
	mode := m.mode()

	setuid := m.sUid()
	setgid := m.sGid()
	sticky := m.sticky()

	extra := ""

	if setuid {
		extra += "suid "
	}
	if setgid {
		extra += "sgid "
	}
	if sticky {
		extra += "sticky "
	}

	return fmt.Sprintf("%v %s %v", kind, extra, mode)
}

type cpioHeader struct {
	Ino       uint64
	Mode      cpioMode
	Uid       uint64
	Gid       uint64
	NLink     uint64
	MTime     uint64
	FileSize  uint64
	DevMajor  uint64
	DevMinor  uint64
	RDevMajor uint64
	RDevMinor uint64
	Name      string
}

func (hdr cpioHeader) asString() string {
	return fmt.Sprintf("%s%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X00000000%s%c", cpioNewCMagic,
		hdr.Ino, uint64(hdr.Mode), uint64(hdr.Uid), uint64(hdr.Gid), hdr.NLink, hdr.MTime, hdr.FileSize, hdr.DevMajor,
		hdr.DevMinor, hdr.RDevMajor, hdr.RDevMinor, len(hdr.Name)+1, hdr.Name, 0x00)
}

func roundUp(n, m uint64) uint64 {
	return ((n + m - 1) / m) * m
}

// writer
type cpioWriter struct {
	file io.Writer

	lastInode uint64
}

func (a *cpioWriter) writePadding(count uint64) error {
	bytes := make([]byte, count)

	_, err := a.file.Write(bytes)
	if err != nil {
		return err
	}

	return nil
}

func (a *cpioWriter) allocateInode() uint64 {
	a.lastInode += 1
	return a.lastInode
}

func (a *cpioWriter) writeHeader(hdr cpioHeader) error {
	count, err := io.WriteString(a.file, hdr.asString())
	if err != nil {
		return err
	}

	diff := roundUp(uint64(count), 4)
	err = a.writePadding(diff - uint64(count))
	if err != nil {
		return err
	}

	return nil
}

func (a *cpioWriter) writeContent(content []byte) error {
	count, err := a.file.Write(content)
	if err != nil {
		return err
	}

	diff := roundUp(uint64(count), 4)
	err = a.writePadding(diff - uint64(count))
	if err != nil {
		return err
	}

	return nil
}

func createCpioWriter(file io.Writer) *cpioWriter {
	return &cpioWriter{file: file, lastInode: 0}
}

type entry interface {
	Kind() cpioKind
	Name() string
	Chmod(mode fs.FileMode)
	Chown(uid int, gid int)
	Chtimes(mtime time.Time)
	Write(e entry, w *cpioWriter, prefix string) error
	Children() []entry
}

type cpioEntry struct {
	kind    cpioKind
	mode    fs.FileMode
	uid     int
	gid     int
	mtime   time.Time
	content []byte
	name    string
}

// Children implements entry.
func (ent *cpioEntry) Children() []entry       { panic("unimplemented") }
func (ent *cpioEntry) Name() string            { return ent.name }
func (ent *cpioEntry) Kind() cpioKind          { return ent.kind }
func (ent *cpioEntry) Chmod(mode fs.FileMode)  { ent.mode = mode }
func (ent *cpioEntry) Chtimes(mtime time.Time) { ent.mtime = mtime }
func (ent *cpioEntry) Chown(uid int, gid int) {
	ent.uid = uid
	ent.gid = gid
}

func (ent *cpioEntry) Write(e entry, w *cpioWriter, namePrefix string) error {
	inode := w.allocateInode()

	children := e.Children()

	var nLinks uint64 = 1

	if e.Kind() == _CPIO_KIND_DIRECTORY {
		nLinks = uint64(2 + len(children))
	}

	if len(ent.name) == 0 {
		return fmt.Errorf("bad entry")
	}

	hdr := cpioHeader{
		Ino:      inode,
		Mode:     makeCpioMode(e.Kind(), ent.mode),
		Uid:      uint64(ent.uid),
		Gid:      uint64(ent.gid),
		NLink:    nLinks,
		MTime:    uint64(ent.mtime.Unix()),
		FileSize: uint64(len(ent.content)),
		DevMajor: 10,
		DevMinor: 1,
		// RDevMajor: uint64(n.rDevMajor),
		// RDevMinor: uint64(n.rDevMinor),
		Name: namePrefix + ent.name,
	}

	err := w.writeHeader(hdr)
	if err != nil {
		return err
	}

	if len(ent.content) > 0 {
		err := w.writeContent(ent.content)
		if err != nil {
			return err
		}
	}

	for _, child := range children {
		prefix := namePrefix + ent.Name() + "/"

		if ent.Name() == "." {
			prefix = ""
		}

		err := child.Write(child, w, prefix)
		if err != nil {
			return err
		}
	}

	return nil
}

type file struct {
	*cpioEntry
}

func (file *file) Children() []entry { return []entry{} }

func (file *file) makeSymlink(linkname string) {
	file.kind = _CPIO_KIND_SYMBOLIC_LINK
	file.content = []byte(linkname)
}

func newFile(name string) (*file, error) {
	if name == "" {
		return nil, fmt.Errorf("empty name")
	}

	return &file{cpioEntry: &cpioEntry{name: name, kind: _CPIO_KIND_REGULAR, mtime: time.Unix(0, 0)}}, nil
}

type directory struct {
	*cpioEntry
	entries map[string]entry
}

func (dir *directory) create(name string) (*file, error) {
	child, err := newFile(name)
	if err != nil {
		return nil, err
	}

	dir.entries[name] = child

	return child, nil
}

func (dir *directory) mkdir(name string) (*directory, error) {
	child, err := newDirectory(name)
	if err != nil {
		return nil, err
	}

	dir.entries[name] = child

	return child, nil
}

func (dir *directory) Children() []entry {
	var ret []entry

	for _, child := range dir.entries {
		ret = append(ret, child)
	}

	slices.SortFunc(ret, func(a entry, b entry) int {
		return cmp.Compare(a.Name(), b.Name())
	})

	return ret
}

func newDirectory(name string) (*directory, error) {
	if name == "" {
		return nil, fmt.Errorf("empty name")
	}

	return &directory{
		cpioEntry: &cpioEntry{name: name, kind: _CPIO_KIND_DIRECTORY, mtime: time.Unix(0, 0)},
		entries:   make(map[string]entry),
	}, nil
}

var (
	_ entry = &cpioEntry{}
	_ entry = &file{}
	_ entry = &directory{}
)

type Filesystem struct {
	root *directory
}

func (fs *Filesystem) openPath(p string, mkdir bool) (*directory, string, error) {
	tokens := strings.Split(p, "/")

	current := fs.root

	if len(tokens) == 0 {
		return current, "", nil
	} else if len(tokens) == 1 {
		return current, tokens[0], nil
	} else if len(tokens) > 1 {
		for i, tk := range tokens[:len(tokens)-1] {
			child, ok := current.entries[tk]
			if !ok {
				if mkdir {
					var err error
					child, err = current.mkdir(tk)
					if err != nil {
						return nil, "", err
					}
				} else {
					return nil, "", fmt.Errorf("child %s not found in %s", tk, strings.Join(tokens[:i], "/"))
				}
			}

			dir, ok := child.(*directory)
			if !ok {
				return nil, "", fmt.Errorf("child %T is not a directory", child)
			}

			current = dir
		}

		return current, tokens[len(tokens)-1], nil
	}

	panic("unreachable")
}

func (fs *Filesystem) AddSimpleFile(filename string, contents []byte, executable bool) error {
	parent, name, err := fs.openPath(filename, true)
	if err != nil {
		return err
	}

	f, err := parent.create(name)
	if err != nil {
		return err
	}

	f.content = contents

	if executable {
		f.Chmod(0755)
	}

	return nil
}

func (fs *Filesystem) AddFromEntry(prefix string, hdr filesystem.Entry) error {
	cleanedName := strings.TrimPrefix(path.Clean(path.Join(prefix, hdr.Name())), "/")

	var ent entry

	switch hdr.Typeflag() {
	case filesystem.TypeRegular:
		parent, name, err := fs.openPath(cleanedName, true)
		if err != nil {
			return err
		}

		f, err := parent.create(name)
		if err != nil {
			return err
		}

		fh, err := hdr.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		contents, err := io.ReadAll(fh)
		if err != nil {
			return err
		}

		f.content = contents

		ent = f
	case filesystem.TypeSymlink:
		parent, name, err := fs.openPath(cleanedName, true)
		if err != nil {
			return err
		}

		f, err := parent.create(name)
		if err != nil {
			return err
		}

		f.makeSymlink(hdr.Linkname())

		ent = f
	case filesystem.TypeDirectory:
		parent, name, err := fs.openPath(cleanedName, true)
		if err != nil {
			return err
		}

		if cleanedName != "." {
			ent, err = parent.mkdir(name)
			if err != nil {
				return err
			}
		} else {
			ent = fs.root
		}
	default:
		return fmt.Errorf("Filesystem.AddFromEntry: Typeflag not implemented: %s", hdr.Typeflag())
	}

	ent.Chmod(hdr.Mode())
	ent.Chown(hdr.Uid(), hdr.Gid())
	ent.Chtimes(hdr.ModTime())

	return nil
}

func (fs *Filesystem) WriteTo(out io.Writer) (n int64, err error) {
	writer := createCpioWriter(out)

	if err := fs.root.Write(fs.root, writer, ""); err != nil {
		return 0, err
	}

	trailer := &file{
		cpioEntry: &cpioEntry{
			kind:  _CPIO_KIND_REGULAR,
			name:  trailerName,
			mtime: time.Unix(0, 0),
		},
	}

	if err := trailer.Write(trailer, writer, ""); err != nil {
		return 0, err
	}

	if _, err := out.Write(make([]byte, 4096)); err != nil {
		return 0, err
	}

	return 0, nil
}

func New() *Filesystem {
	dir, _ := newDirectory(".")
	return &Filesystem{root: dir}
}
