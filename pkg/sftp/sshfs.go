// Based on: https://datatracker.ietf.org/doc/html/draft-ietf-secsh-filexfer-02

package sftp

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"

	"github.com/google/uuid"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"golang.org/x/crypto/ssh"
)

const (
	SFTP_DEBUG = true
)

type fileHandle struct {
	file   filesystem.File
	handle filesystem.FileHandle
}

type directoryHandle struct {
	entries []filesystem.DirectoryEntry
	off     int
}

func (d *directoryHandle) next() (filesystem.File, error) {
	if d.off >= len(d.entries) {
		return nil, io.EOF
	}

	ent := d.entries[d.off]

	d.off += 1

	return ent.File, nil
}

func asDirectory(f filesystem.File) (filesystem.Directory, error) {
	if dir, ok := f.(filesystem.Directory); ok {
		return dir, nil
	}
	return nil, fs.ErrInvalid
}

func asMutable(f filesystem.File) (filesystem.MutableFile, error) {
	if mut, ok := f.(filesystem.MutableFile); ok {
		return mut, nil
	}
	return nil, fs.ErrInvalid
}

func asMutableDirectory(f filesystem.File) (filesystem.MutableDirectory, error) {
	if mut, ok := f.(filesystem.MutableDirectory); ok {
		return mut, nil
	}
	return nil, fs.ErrInvalid
}

var PACKET_KINDS = map[packetType]string{
	packetTypeInit:          "Init",
	packetTypeVersion:       "Version",
	packetTypeOpen:          "Open",
	packetTypeClose:         "Close",
	packetTypeRead:          "Read",
	packetTypeWrite:         "Write",
	packetTypeLstat:         "Lstat",
	packetTypeFstat:         "Fstat",
	packetTypeSetstat:       "Setstat",
	packetTypeFsetstat:      "Fsetstat",
	packetTypeOpendir:       "Opendir",
	packetTypeReaddir:       "Readdir",
	packetTypeRemove:        "Remove",
	packetTypeMkdir:         "Mkdir",
	packetTypeRmdir:         "Rmdir",
	packetTypeRealpath:      "Realpath",
	packetTypeStat:          "Stat",
	packetTypeRename:        "Rename",
	packetTypeReadlink:      "Readlink",
	packetTypeSymlink:       "Symlink",
	packetTypeStatus:        "Status",
	packetTypeHandle:        "Handle",
	packetTypeData:          "Data",
	packetTypeName:          "Name",
	packetTypeAttrs:         "Attrs",
	packetTypeExtended:      "Extended",
	packetTypeExtendedReply: "ExtendedReply",
}

type SSHFSServer struct {
	openHandles      map[string]*fileHandle
	directoryHandles map[string]*directoryHandle
	fs               filesystem.Directory
}

// Close implements core.Component.
func (*SSHFSServer) Close() error {
	return nil
}

func (s *SSHFSServer) setAttributes(file filesystem.MutableFile, attrs attrs) error {
	if attrs.Flags&fileXferAttrUidgid != 0 {
		err := file.Chown(int(attrs.Uid), int(attrs.Gid))
		if err != nil {
			return err
		}
	}
	if attrs.Flags&fileXferAttrPermissions != 0 {
		err := file.Chmod(fs.FileMode(attrs.Permissions))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SSHFSServer) infoToAttrs(info filesystem.FileInfo, uid, gid int) attrs {
	var kind uint32 = 0

	if info.Mode().IsDir() {
		kind = 0040000
	} else if info.Mode().IsRegular() {
		kind = 0100000
	} else if info.Mode()&fs.ModeSymlink != 0 {
		kind = 0120000
	}

	return attrs{
		Flags:       fileXferAttrSize | fileXferAttrUidgid | fileXferAttrPermissions | fileXferAttrAcmodtime,
		Size:        uint64(info.Size()),
		Uid:         uint32(uid),
		Gid:         uint32(gid),
		Permissions: uint32(info.Mode()&fs.ModePerm) | kind,
		Atime:       0,
		Mtime:       uint32(info.ModTime().Unix()),
	}
}

func (s *SSHFSServer) allocateHandleId() string {
	return uuid.NewString()
}

func (s *SSHFSServer) lookup(path string) (filesystem.File, error) {
	ent, err := filesystem.OpenPath(s.fs, path)
	if err != nil {
		return nil, err
	}

	return ent.File, nil
}

func (s *SSHFSServer) PktInit(ctx sftpContext, pkt *pktInit) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: init", "version", pkt.Version)
	}

	// We only support version 3 right now.
	if pkt.Version != 3 {
		return nil, fmt.Errorf("bad version from client: %d", pkt.Version)
	}

	return &pktVersion{Version: 3}, nil
}

func (s *SSHFSServer) PktOpen(ctx sftpContext, pkt *pktOpen) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: open", "open", fmt.Sprintf("%+v", pkt))
	}

	dirname := path.Dir(pkt.Path)
	basename := path.Base(pkt.Path)

	dirF, err := s.lookup(dirname)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "directory not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	dir, ok := dirF.(filesystem.Directory)
	if !ok {
		return nil, fs.ErrInvalid
	}

	var file filesystem.File

	if pkt.Flags&openFlagCreat != 0 {
		mut, ok := dir.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrInvalid
		}

		file := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := mut.Create(basename, file); err != nil {
			return nil, err
		}

		err = s.setAttributes(file, pkt.Attrs)
		if err != nil {
			return nil, err
		}
	} else {
		file, err = dir.GetChild(basename)
		if err == fs.ErrNotExist {
			return &pktStatus{
				Code:     errNoSuchFile,
				Message:  "directory not found",
				Language: "en",
			}, nil
		} else if err != nil {
			return nil, err
		}
	}

	handle, err := file.Open()
	if err != nil {
		return nil, err
	}

	id := s.allocateHandleId()

	s.openHandles[id] = &fileHandle{file: file, handle: handle}

	return &pktHandle{Handle: id}, nil
}

func (s *SSHFSServer) PktClose(ctx sftpContext, pkt *pktClose) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: close", "pkt", fmt.Sprintf("%+v", pkt))
	}

	fh, ok := s.openHandles[pkt.Handle]
	if ok {
		err := fh.handle.Close()
		if err != nil {
			return nil, err
		}

		delete(s.openHandles, pkt.Handle)

		return &pktStatus{
			Code:     errOk,
			Message:  "Ok",
			Language: "en",
		}, nil
	}

	_, ok = s.directoryHandles[pkt.Handle]
	if ok {
		delete(s.directoryHandles, pkt.Handle)

		return &pktStatus{
			Code:     errOk,
			Message:  "Ok",
			Language: "en",
		}, nil
	}

	return nil, fmt.Errorf("file handle not found: %s", pkt.Handle)
}

func (s *SSHFSServer) PktRead(ctx sftpContext, pkt *pktRead) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: read", "pkt", fmt.Sprintf("%+v", pkt))
	}

	fh, ok := s.openHandles[pkt.Handle]
	if !ok {
		return nil, fmt.Errorf("file handle not found: %s", pkt.Handle)
	}

	data := make([]byte, pkt.Len)

	n, err := fh.handle.ReadAt(data, int64(pkt.Offset))
	if err == io.EOF {
		if n == 0 {
			return &pktStatus{
				Message:  "EOF",
				Language: "en",
			}, nil
		} else {
			// fallthrough
		}
	} else if err != nil {
		return nil, err
	}

	return &pktData{
		Data: data[:n],
	}, nil
}

// PktWrite implements filesystem.
func (s *SSHFSServer) PktWrite(ctx sftpContext, pkt *pktWrite) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: write", "pkt", fmt.Sprintf("%+v", pkt))
	}

	fh, ok := s.openHandles[pkt.Handle]
	if !ok {
		return nil, fmt.Errorf("file handle not found: %s", pkt.Handle)
	}

	mut, ok := fh.file.(filesystem.WritableFileHandle)
	if !ok {
		return nil, fmt.Errorf("file is readonly: %s", pkt.Handle)
	}

	data := []byte(pkt.Data)

	n, err := mut.WriteAt(data, int64(pkt.Offset))
	if err != nil {
		return nil, err
	}

	if n != len(data) {
		return nil, fmt.Errorf("short write: %d != %d", n, len(data))
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktStat implements filesystem.
func (s *SSHFSServer) PktStat(ctx sftpContext, pkt *pktStat) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: stat in", "pkt", fmt.Sprintf("%+v", pkt))
	}

	child, err := s.lookup(pkt.Path)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	info, err := child.Stat()
	if err != nil {
		return nil, err
	}

	uid, gid, err := filesystem.GetUidAndGid(child)
	if err != nil {
		return nil, err
	}

	attrs := s.infoToAttrs(info, uid, gid)

	return &pktAttrs{Attrs: attrs}, nil
}

func (s *SSHFSServer) PktLstat(ctx sftpContext, pkt *pktLstat) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: lstat in", "pkt", fmt.Sprintf("%+v", pkt))
	}

	child, err := s.lookup(pkt.Path)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	info, err := child.Stat()
	if err != nil {
		return nil, err
	}

	uid, gid, err := filesystem.GetUidAndGid(child)
	if err != nil {
		return nil, err
	}

	attrs := s.infoToAttrs(info, uid, gid)

	return &pktAttrs{Attrs: attrs}, nil
}

// PktFstat implements filesystem.
func (s *SSHFSServer) PktFstat(ctx sftpContext, pkt *pktFstat) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: fstat", "pkt", fmt.Sprintf("%+v", pkt))
	}

	fh, ok := s.openHandles[pkt.Handle]
	if !ok {
		return nil, fmt.Errorf("file handle not found: %s", pkt.Handle)
	}

	info, err := fh.file.Stat()
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	uid, gid, err := filesystem.GetUidAndGid(fh.file)
	if err != nil {
		return nil, err
	}

	attrs := s.infoToAttrs(info, uid, gid)

	return &pktAttrs{Attrs: attrs}, nil
}

func (s *SSHFSServer) PktOpenDir(ctx sftpContext, pkt *pktOpenDir) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: opendir", "pkt", fmt.Sprintf("%+v", pkt))
	}

	dirF, err := s.lookup(pkt.Path)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "directory not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	dir, err := asDirectory(dirF)
	if err != nil {
		return nil, err
	}

	ents, err := dir.Readdir()
	if err != nil {
		return nil, err
	}

	id := s.allocateHandleId()

	s.directoryHandles[id] = &directoryHandle{
		entries: ents,
	}

	return &pktHandle{Handle: id}, nil
}

func (s *SSHFSServer) PktReadDir(ctx sftpContext, pkt *pktReadDir) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: readdir", "pkt", fmt.Sprintf("%+v", pkt))
	}

	dh, ok := s.directoryHandles[pkt.Handle]
	if !ok {
		return nil, fmt.Errorf("directory handle not found: %s", pkt.Handle)
	}

	ret := &pktNames{}

	for {
		f, err := dh.next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		info, err := f.Stat()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		uid, gid, err := filesystem.GetUidAndGid(f)
		if err != nil {
			return nil, err
		}

		attr := s.infoToAttrs(info, uid, gid)

		ret.Names = append(ret.Names, name{
			Filename: info.Name(),
			Longname: info.Name(),
			Attrs:    attr,
		})

		if len(ret.Names) == 20 {
			break
		}
	}

	if len(ret.Names) == 0 {
		return &pktStatus{
			Code:     errEOF,
			Message:  "EOF",
			Language: "en",
		}, nil
	}

	return ret, nil
}

// PktMkdir implements filesystem.
func (s *SSHFSServer) PktMkdir(ctx sftpContext, pkt *pktMkdir) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: mkdir", "pkt", fmt.Sprintf("%+v", pkt))
	}

	dir := path.Dir(pkt.Path)
	base := path.Base(pkt.Path)

	file, err := s.lookup(dir)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "directory not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	fileDir, err := asDirectory(file)
	if err != nil {
		return nil, err
	}

	mutDir, err := asMutableDirectory(fileDir)
	if err != nil {
		return nil, err
	}

	newDir, err := mutDir.Mkdir(base)
	if err != nil {
		return nil, err
	}

	err = s.setAttributes(newDir, pkt.Attrs)
	if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktSetStat implements filesystem.
func (s *SSHFSServer) PktSetStat(ctx sftpContext, pkt *pktSetStat) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: setstat", "pkt", fmt.Sprintf("%+v", pkt))
	}

	f, err := s.lookup(pkt.Path)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	mut, err := asMutable(f)
	if err != nil {
		return nil, err
	}

	err = s.setAttributes(mut, pkt.Attrs)
	if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktFSetStat implements filesystem.
func (s *SSHFSServer) PktFSetStat(ctx sftpContext, pkt *pktFSetStat) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: fsetstat", "pkt", fmt.Sprintf("%+v", pkt))
	}

	fh, ok := s.openHandles[pkt.Handle]
	if !ok {
		return nil, fmt.Errorf("file handle not found: %s", pkt.Handle)
	}

	mut, err := asMutable(fh.file)
	if err != nil {
		return nil, err
	}

	err = s.setAttributes(mut, pkt.Attrs)
	if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktRename implements filesystem.
func (s *SSHFSServer) PktRename(ctx sftpContext, pkt *pktRename) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: rename", "pkt", fmt.Sprintf("%+v", pkt))
	}

	file, err := s.lookup(pkt.OldPath)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	oldDirName := path.Dir(pkt.OldPath)
	oldBase := path.Base(pkt.OldPath)

	oldDirHandle, err := s.lookup(oldDirName)
	if err != nil {
		return nil, err
	}

	oldDir, err := asMutableDirectory(oldDirHandle)
	if err != nil {
		return nil, err
	}

	dir := path.Dir(pkt.NewPath)
	base := path.Base(pkt.NewPath)

	target, err := s.lookup(dir)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "directory not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	targetDir, err := asMutableDirectory(target)
	if err != nil {
		return nil, err
	}

	// If the file already exists in the target path then overwrite it.
	err = targetDir.Unlink(base)
	if err == fs.ErrNotExist {
		// fallthrough
	} else if err != nil {
		return nil, err
	}

	// Create the file at the new location.
	err = targetDir.Create(base, file)
	if err != nil {
		return nil, err
	}

	// Unlink the file at the old location.
	err = oldDir.Unlink(oldBase)
	if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktSymlink implements filesystem.
func (s *SSHFSServer) PktSymlink(ctx sftpContext, pkt *pktSymlink) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: symlink", "pkt", fmt.Sprintf("%+v", pkt))
	}

	dir := path.Dir(pkt.LinkPath)
	base := path.Base(pkt.LinkPath)

	target, err := s.lookup(dir)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	targetDir, err := asMutableDirectory(target)
	if err != nil {
		return nil, err
	}

	targetDir.Create(base, filesystem.NewSymlink(pkt.TargetPath))
	if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktReadlink implements filesystem.
func (s *SSHFSServer) PktReadlink(ctx sftpContext, pkt *pktReadlink) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: readlink", "pkt", fmt.Sprintf("%+v", pkt))
	}

	file, err := s.lookup(pkt.Path)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	linkname, err := filesystem.GetLinkName(file)
	if err != nil {
		return nil, err
	}

	return &pktNames{Names: []name{
		{Filename: linkname, Longname: linkname, Attrs: attrs{}},
	}}, nil
}

// PktRealPath implements filesystem.
func (s *SSHFSServer) PktRealPath(ctx sftpContext, pkt *pktRealPath) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: realpath", "pkt", fmt.Sprintf("%+v", pkt))
	}

	return &pktNames{Names: []name{
		{Filename: pkt.Path, Longname: pkt.Path, Attrs: attrs{}},
	}}, nil
}

// PktRmdir implements filesystem.
func (s *SSHFSServer) PktRmdir(ctx sftpContext, pkt *pktRmdir) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: rmdir", "pkt", fmt.Sprintf("%+v", pkt))
	}

	dir := path.Dir(pkt.Path)
	base := path.Base(pkt.Path)

	file, err := s.lookup(dir)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "directory not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	fileDir, err := asMutableDirectory(file)
	if err != nil {
		return nil, err
	}

	err = fileDir.Unlink(base)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

// PktRemove implements filesystem.
func (s *SSHFSServer) PktRemove(ctx sftpContext, pkt *pktRemove) (ResponsePacket, error) {
	if SFTP_DEBUG {
		slog.Debug("sftp: remove", "pkt", fmt.Sprintf("%+v", pkt))
	}

	dir := path.Dir(pkt.Filename)
	base := path.Base(pkt.Filename)

	file, err := s.lookup(dir)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "directory not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	fileDir, err := asMutableDirectory(file)
	if err != nil {
		return nil, err
	}

	err = fileDir.Unlink(base)
	if err == fs.ErrNotExist {
		return &pktStatus{
			Code:     errNoSuchFile,
			Message:  "file not found",
			Language: "en",
		}, nil
	} else if err != nil {
		return nil, err
	}

	return &pktStatus{
		Code:     errOk,
		Message:  "Ok",
		Language: "en",
	}, nil
}

func (s *SSHFSServer) ServeSftp(channel ssh.Channel) error {
	for {
		rawPkt, err := readRawPacket(channel)
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		pkt, id, err := rawPkt.decode()
		if err != nil {
			return err
		}

		ret, err := handlePacket(s, channel, pkt)
		if err != nil {
			slog.Warn("failed to handle packet", "kind", rawPkt.kind, "error", err)
			continue
		}

		err = writePacket(channel, id, ret)
		if err != nil {
			return err
		}
	}
}

var (
	_ sftpFilesystem = &SSHFSServer{}
)

func New(fs filesystem.Directory) *SSHFSServer {
	return &SSHFSServer{
		openHandles:      make(map[string]*fileHandle),
		directoryHandles: make(map[string]*directoryHandle),
		fs:               fs,
	}
}
