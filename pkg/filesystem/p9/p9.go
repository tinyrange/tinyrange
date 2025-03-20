package p9

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/common/binary"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/log"
)

const (
	IOUNIT_SIZE = 0
)

type serverFile struct {
	s          *Server
	file       filesystem.File
	fileHandle filesystem.FileHandle
}

func (f *serverFile) readAt(p []byte, off int64) (int, error) {
	if f.fileHandle != nil {
		f.s.debug("9p: readAt using existing handle", "p", p, "off", off)
		return f.fileHandle.ReadAt(p, off)
	}

	fh, err := f.file.Open()
	if err != nil {
		return -1, err
	}
	defer fh.Close()

	return fh.ReadAt(p, off)
}

func (f *serverFile) writeAt(p []byte, off int64) (int, error) {
	if f.fileHandle != nil {
		if mut, ok := f.fileHandle.(filesystem.WritableFileHandle); ok {
			f.s.debug("9p: writeAt using existing handle", "p", p, "off", off)
			return mut.WriteAt(p, off)
		}
	}

	mut, ok := f.file.(filesystem.MutableFile)
	if !ok {
		return -1, fs.ErrPermission
	}

	fh, err := mut.OpenMut()
	if err != nil {
		return -1, err
	}

	f.fileHandle = fh

	return fh.WriteAt(p, off)
}

func (f *serverFile) truncate(size int64) error {
	mut, ok := f.file.(filesystem.MutableFile)
	if !ok {
		return fs.ErrPermission
	}

	if err := mut.Truncate(size); err != nil {
		return err
	}

	if f.fileHandle != nil {
		if err := f.fileHandle.Close(); err != nil {
			return err
		}

		f.fileHandle = nil
	}

	return nil
}

func (f *serverFile) clunk() error {
	if f.fileHandle != nil {
		if err := f.fileHandle.Close(); err != nil {
			return err
		}
	}

	return nil
}

type Server struct {
	dir filesystem.Directory

	filePaths map[filesystem.FileInfo]uint64
	fileIds   map[uint32]*serverFile

	debugEnabled bool
	warnEnabled  bool
}

// Close implements core.Component.
func (*Server) Close() error {
	return nil
}

func (s *Server) debug(message string, args ...any) {
	if s.debugEnabled {
		log.Debug(message, args...)
	}
}

func (s *Server) warn(message string, args ...any) {
	if s.warnEnabled {
		log.Warn(message, args...)
	}
}

func (s *Server) error(message string, args ...any) {
	log.Error(message, args...)
}

func (s *Server) getFid(id uint32) (*serverFile, error) {
	fid, ok := s.fileIds[id]
	if !ok {
		return nil, fmt.Errorf("fid %0X not found", id)
	}

	return fid, nil
}

func (s *Server) setFid(id uint32, file filesystem.File, handle filesystem.FileHandle) error {
	if file == nil {
		return fmt.Errorf("file is nil")
	}

	s.fileIds[id] = &serverFile{
		s:          s,
		file:       file,
		fileHandle: handle,
	}

	return nil
}

func (s *Server) clunkFile(id uint32) error {
	if _, ok := s.fileIds[id]; !ok {
		return fmt.Errorf("fid %0X not found", id)
	}

	if err := s.fileIds[id].clunk(); err != nil {
		return err
	}

	delete(s.fileIds, id)

	return nil
}

func (s *Server) getQid(info filesystem.FileInfo) QID {
	kind := info.Kind()

	qid := QID{}

	if kind == filesystem.TypeRegular {
		qid.Type = TypeRegular
	} else if kind == filesystem.TypeDirectory {
		qid.Type = TypeDir
	} else if kind == filesystem.TypeSymlink {
		qid.Type = TypeSymlink
	}

	qid.Version = 1
	if path, ok := s.filePaths[info]; ok {
		qid.Path = path
	} else {
		qid.Path = uint64(len(s.filePaths) + 1)
		s.filePaths[info] = qid.Path
	}

	return qid
}

func (s *Server) handleMessage(msg *Message) (*Message, error) {
	var ret Message

	switch msg.Type {
	case MsgTgetattr:
		var body Tgetattr

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		stat, err := fid.file.Stat()
		if err != nil {
			return nil, err
		}

		retMsg := Rgetattr{}

		if body.RequestMask&P9_GETATTR_MODE != 0 {
			var kind uint32 = 0

			if stat.Mode().IsDir() {
				kind = 0040000
			} else if stat.Mode().IsRegular() {
				kind = 0100000
			} else if stat.Mode()&fs.ModeSymlink != 0 {
				kind = 0120000
			}

			if runtime.GOOS == "windows" {
				// Windows doesn't have the executable bit.
				retMsg.Mode = uint32(stat.Mode()&fs.ModePerm) | kind | 0o111
			} else {
				retMsg.Mode = uint32(stat.Mode()&fs.ModePerm) | kind
			}

			retMsg.Valid |= P9_GETATTR_MODE
		}

		if body.RequestMask&P9_GETATTR_NLINK != 0 {
			if stat.Kind() == filesystem.TypeDirectory {
				retMsg.Nlink = 1
			}

			retMsg.Valid |= P9_GETATTR_NLINK
		}

		if body.RequestMask&P9_GETATTR_UID != 0 {
			uid, _, err := filesystem.GetUidAndGid(fid.file)
			if err != nil {
				return nil, err
			}

			retMsg.Uid = uint32(uid)

			retMsg.Valid |= P9_GETATTR_UID
		}

		if body.RequestMask&P9_GETATTR_GID != 0 {
			_, gid, err := filesystem.GetUidAndGid(fid.file)
			if err != nil {
				return nil, err
			}

			retMsg.Gid = uint32(gid)

			retMsg.Valid |= P9_GETATTR_GID
		}

		// if body.RequestMask&P9_GETATTR_RDEV != 0 {
		// 	retMsg.Mode = uint32(stat.Mode())

		// 	retMsg.Valid &= P9_GETATTR_RDEV
		// }

		if body.RequestMask&P9_GETATTR_ATIME != 0 {
			retMsg.Atime = time.Now()

			retMsg.Valid |= P9_GETATTR_ATIME
		}

		if body.RequestMask&P9_GETATTR_MTIME != 0 {
			retMsg.Mtime = time.Now()

			retMsg.Valid |= P9_GETATTR_MTIME
		}

		if body.RequestMask&P9_GETATTR_CTIME != 0 {
			retMsg.Ctime = time.Now()

			retMsg.Valid |= P9_GETATTR_CTIME
		}

		if body.RequestMask&P9_GETATTR_INO != 0 {
			retMsg.Qid = s.getQid(stat)

			retMsg.Valid |= P9_GETATTR_INO
		}

		if body.RequestMask&P9_GETATTR_SIZE != 0 {
			retMsg.Size = uint64(stat.Size())

			retMsg.Valid |= P9_GETATTR_SIZE
		}

		if body.RequestMask&P9_GETATTR_BLOCKS != 0 {
			retMsg.Blocks = 1

			retMsg.Valid |= P9_GETATTR_BLOCKS
		}

		s.debug("9p: getattr", "retMsg", fmt.Sprintf("%+v", retMsg))

		return ret.EncodeBody(MsgRgetattr, msg.Tag, &retMsg)
	case MsgTversion:
		var body Tversion

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return ret.EncodeBody(MsgRversion, msg.Tag, &Rversion{
			Msize:   body.Msize,
			Version: body.Version,
		})
	case MsgTflush:
		var body Tflush

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return ret.EncodeBody(MsgRflush, msg.Tag, &Rflush{})
	case MsgTattach:
		var body Tattach

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		if err := s.setFid(body.Fid, s.dir, nil); err != nil {
			return nil, fmt.Errorf("failed to set root fid: %v", err)
		}

		return ret.EncodeBody(MsgRattach, msg.Tag, &Rattach{
			Qid: QID{Type: TypeDir, Version: 0, Path: 0},
		})
	case MsgTwalk:
		var body Twalk

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		retMsg := Rwalk{}

		var newFile filesystem.File

		if len(body.Nwname) > 0 {
			newDir, ok := fid.file.(filesystem.Directory)
			if !ok {
				return nil, fmt.Errorf("file is not a directory")
			}

			// Walk the path up to the penultimate element.
			for i, name := range body.Nwname {
				child, err := newDir.GetChild(name)
				if err == os.ErrNotExist {
					return ret.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: ENOENT})
				} else if err != nil {
					return nil, err
				}

				newInfo, err := child.Stat()
				if err != nil {
					return nil, err
				}

				if i != len(body.Nwname)-1 {
					newDir, ok = child.File.(filesystem.Directory)
					if !ok {
						return nil, fmt.Errorf("file is not a directory")
					}
				} else {
					newFile = child.File
				}

				newQid := s.getQid(newInfo)

				retMsg.Nwqid = append(retMsg.Nwqid, newQid)
			}
		} else {
			newFile = fid.file
		}

		if err := s.setFid(body.Newfid, newFile, nil); err != nil {
			return nil, fmt.Errorf("failed to set new fid: %v", err)
		}

		s.debug("", "Newfid", body.Newfid, "newInfo", newFile)

		return ret.EncodeBody(MsgRwalk, msg.Tag, &retMsg)
	case MsgTread:
		var body Tread

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		buf := make([]byte, body.Count)

		n, err := fid.readAt(buf, int64(body.Offset))
		if err == io.EOF {
			if n == 0 {
				return ret.EncodeBody(MsgRread, msg.Tag, &Rread{Count: 0, Data: buf[:0]})
			}
		} else if err != nil {
			return nil, err
		}

		return ret.EncodeBody(MsgRread, msg.Tag, &Rread{Count: uint32(n), Data: buf[:n]})
	case MsgTwrite:
		var body Twrite

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		n, err := fid.writeAt(body.Data, int64(body.Offset))
		if err != nil {
			return nil, err
		}

		return ret.EncodeBody(MsgRwrite, msg.Tag, &Rwrite{Count: uint32(n)})
	case MsgTclunk:
		var body Tclunk

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		if err := s.clunkFile(body.Fid); err != nil {
			return nil, err
		}

		return ret.EncodeBody(MsgRclunk, msg.Tag, &Rclunk{})
	case MsgTremove:
		var body Tremove

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Tremove not implemented")
	case MsgTstatfs:
		var body Tstatfs

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return ret.EncodeBody(MsgRstatfs, msg.Tag, &Rstatfs{
			Type:    0,
			Bsize:   4096,
			Blocks:  1024 * 1024 * 1024,
			Bfree:   512 * 1024 * 1024,
			Bavail:  512 * 1024 * 1024,
			Files:   1024 * 1024 * 1024,
			Ffree:   512 * 1024 * 1024,
			Fsid:    0xdeadbeef,
			Namelen: 255,
		})
	case MsgTlopen:
		var body Tlopen

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		stat, err := fid.file.Stat()
		if err != nil {
			return nil, err
		}

		return ret.EncodeBody(MsgRlopen, msg.Tag, &Rlopen{
			Qid:    s.getQid(stat),
			Iounit: IOUNIT_SIZE,
		})
	case MsgTlcreate:
		var body Tlcreate

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		dir, ok := fid.file.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		dirMode, err := dir.Stat()
		if err != nil {
			return nil, err
		}

		mode := fs.FileMode(body.Mode) & (^fs.FileMode(0666) | (dirMode.Mode() & 0666))

		f, err := dir.Create(body.Name, filesystem.NewMemoryFile(filesystem.TypeRegular))
		if err != nil {
			return nil, err
		}

		mutF, ok := f.(filesystem.MutableFile)
		if !ok {
			return nil, fs.ErrPermission
		}

		mutFh, err := mutF.OpenMut()
		if err != nil {
			return nil, err
		}

		s.debug("9p: message", "set mode", fs.FileMode(body.Mode)&fs.ModePerm)
		err = mutF.Chmod(mode)
		if err != nil {
			return nil, err
		}

		newInfo, err := mutF.Stat()
		if err != nil {
			return nil, err
		}

		// Maintain the file handle for the file.
		if err := s.setFid(body.Fid, mutF, mutFh); err != nil {
			return nil, fmt.Errorf("failed to set create fid: %v", err)
		}

		return msg.EncodeBody(MsgRlcreate, msg.Tag, &Rlcreate{
			Qid:    s.getQid(newInfo),
			Iounit: IOUNIT_SIZE,
		})
	case MsgTsymlink:
		var body Tsymlink

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		dir, ok := fid.file.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		f, err := dir.Create(body.Name, filesystem.NewSymlink(body.Symtgt))
		if err != nil {
			return nil, err
		}

		fSym, ok := f.(filesystem.Symlink)
		if !ok {
			s.warn("9p: created symlink but it is not a symlink", "f", f)
			return nil, fs.ErrInvalid
		}

		newInfo, err := fSym.Lstat()
		if err != nil {
			return nil, fmt.Errorf("failed to stat new symlink: %v", err)
		}

		return msg.EncodeBody(MsgRlcreate, msg.Tag, &Rsymlink{
			Qid: s.getQid(newInfo),
		})
	case MsgTmknod:
		var body Tmknod

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Tmknod not implemented")
	case MsgTrename:
		var body Trename

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Trename not implemented")
	case MsgTreadlink:
		var body Treadlink

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Treadlink not implemented")
	case MsgTsetattr:
		var body Tsetattr

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		if body.Valid&P9_SETATTR_MODE != 0 {
			mut, ok := fid.file.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			if err := mut.Chmod(fs.FileMode(body.Mode) & fs.ModePerm); err != nil {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_UID != 0 {
			mut, ok := fid.file.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			// not supported errors are ignored
			if err := mut.Chown(int(body.Uid), -1); err != nil && !errors.Is(err, filesystem.ErrNotSupported) {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_GID != 0 {
			mut, ok := fid.file.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			// not supported errors are ignored
			if err := mut.Chown(-1, int(body.Gid)); err != nil && !errors.Is(err, filesystem.ErrNotSupported) {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_SIZE != 0 {
			if err := fid.truncate(int64(body.Size)); err != nil {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_ATIME != 0 {
			// Ignored
		}
		if body.Valid&P9_SETATTR_MTIME != 0 {
			mut, ok := fid.file.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			if err := mut.Chtimes(time.Now()); err != nil {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_CTIME != 0 {
			mut, ok := fid.file.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			// Setting -1 should have the side effect of setting the current time.
			if err := mut.Chown(-1, -1); err != nil && !errors.Is(err, filesystem.ErrNotSupported) {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_ATIME_SET != 0 {
			// log.Warn("p9: unimplemented P9_SETATTR_ATIME_SET")
		}
		if body.Valid&P9_SETATTR_MTIME_SET != 0 {
			// log.Warn("p9: unimplemented P9_SETATTR_MTIME_SET")
		}

		_ = fid

		return msg.EncodeBody(MsgRsetattr, msg.Tag, &Rsetattr{})
	case MsgTxattrwalk:
		var body Txattrwalk

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return msg.EncodeBody(MsgRxattrwalk, msg.Tag, &Rxattrwalk{})
	case MsgTxattrcreate:
		var body Txattrcreate

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		// ignored
		return msg.EncodeBody(MsgRxattrcreate, msg.Tag, &Rxattrcreate{})
	case MsgTreaddir:
		var body Treaddir

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		dir, ok := fid.file.(filesystem.Directory)
		if !ok {
			return nil, fs.ErrInvalid
		}

		ents, err := dir.Readdir()
		if err != nil {
			return nil, err
		}

		retMsg := Rreaddir{}

		var currentOffset uint64 = 0

		for _, ent := range ents {
			dirent := Dirent{}

			stat, err := ent.File.Stat()
			if err != nil {
				return nil, err
			}

			// Populate the directory entry.
			dirent.Name = ent.Name
			dirent.Qid = s.getQid(stat)
			dirent.Type = uint8(dirent.Qid.Type)

			entrySize := 13 + 8 + 1 + 2 + len(ent.Name)

			currentOffset += uint64(entrySize)

			dirent.Offset = uint64(currentOffset)

			// We haven't reached the current offset yet.
			if currentOffset <= body.Offset {
				continue
			}

			// We've gone past the end so discard this item.
			if currentOffset >= (body.Offset + uint64(body.Count)) {
				break
			}

			retMsg.Count += uint32(entrySize)
			retMsg.Data = append(retMsg.Data, dirent)
		}

		return msg.EncodeBody(MsgRreaddir, msg.Tag, &retMsg)
	case MsgTfsync:
		var body Tfsync

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		_ = fid

		return ret.EncodeBody(MsgRfsync, msg.Tag, &Rfsync{})
	case MsgTlock:
		var body Tlock

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		// TOOD(joshua): This is a no-op for now.

		return ret.EncodeBody(MsgRlock, msg.Tag, &Rlock{
			Status: P9_LOCK_SUCCESS,
		})
	case MsgTgetlock:
		var body Tgetlock

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		// TOOD(joshua): This is a no-op for now.

		return ret.EncodeBody(MsgRgetlock, msg.Tag, &Rgetlock{
			Type:     P9_LOCK_SUCCESS,
			Start:    0,
			Length:   0,
			ProcId:   0,
			ClientId: "client",
		})
	case MsgTlink:
		var body Tlink

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Tlink not implemented")
	case MsgTmkdir:
		var body Tmkdir

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Dfid)
		if err != nil {
			return nil, err
		}

		mutDir, ok := fid.file.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		child, err := mutDir.Mkdir(body.Name)
		if err != nil {
			return nil, err
		}

		childStat, err := child.Stat()
		if err != nil {
			return nil, err
		}

		return msg.EncodeBody(MsgRmkdir, msg.Tag, &Rmkdir{
			Qid: s.getQid(childStat),
		})
	case MsgTrenameat:
		var body Trenameat

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		// log.Info("rename", "olddirfid", body.Olddirfid, "oldname", body.Oldname, "newdirfid", body.Newdirfid, "newname", body.Newname)

		// Get the old directory.
		oldFid, err := s.getFid(body.Olddirfid)
		if err != nil {
			return nil, err
		}

		oldMutDir, ok := oldFid.file.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		// Get the old file.
		oldFile, err := oldMutDir.GetChild(body.Oldname)
		if err != nil {
			return nil, err
		}

		// Get the new directory.
		newFid, err := s.getFid(body.Newdirfid)
		if err != nil {
			return nil, err
		}

		newMutDir, ok := newFid.file.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		// Check if we have an avalible fast path.
		if mutRename, ok := oldFile.File.(filesystem.MutableRenameFile); ok {
			if err := mutRename.Rename(newMutDir, body.Newname); err != nil {
				return nil, err
			}
		} else {
			// Create the new file.
			if _, err := newMutDir.Create(body.Newname, oldFile.File); err != nil {
				return nil, err
			}

			// Remove the old file.
			if err := oldMutDir.Unlink(body.Oldname); err != nil {
				return nil, err
			}
		}

		return ret.EncodeBody(MsgRrenameat, msg.Tag, &Rrenameat{})
	case MsgTunlinkat:
		var body Tunlinkat

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		s.debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Dirfd)
		if err != nil {
			return nil, err
		}

		mutDir, ok := fid.file.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		err = mutDir.Unlink(body.Name)
		if err != nil {
			return nil, err
		}

		return ret.EncodeBody(MsgRunlinkat, msg.Tag, &Runlinkat{})
	default:
		return nil, fmt.Errorf("9p: unknown message: %d", msg.Type)
	}
}

func (s *Server) sendMessage(client net.Conn, msg *Message) error {
	buf := new(bytes.Buffer)

	err := msg.Encode(binary.NewWriter(buf, binary.LittleEndian))
	if err != nil {
		return fmt.Errorf("failed to encode response: %v", err)
	}

	if _, err := client.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to send response: %v", err)
	}

	return nil
}

func (s *Server) readAndHandleMessage(client net.Conn, reader binary.BinaryReader) error {
	var msg Message

	defer func() {
		if r := recover(); r != nil {
			var msg Message

			s.error("9p: recovered from panic", "error", r)

			ret, err := msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: ENOSYS})
			if err != nil {
				s.error("9p: failed to encode response", "error", err)
				return
			}

			if err := s.sendMessage(client, ret); err != nil {
				s.error("9p: failed to send response", "error", err)
				return
			}
		}
	}()

	err := msg.Decode(reader)
	if err != nil {
		return fmt.Errorf("failed to decode message: %v", err)
	}

	ret, err := s.handleMessage(&msg)
	if errors.Is(err, os.ErrNotExist) {
		s.debug("9p: file not found", "kind", msg.Type, "error", err)
		ret, err = msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: ENOENT})
		if err != nil {
			return fmt.Errorf("failed to encode response: %v", err)
		}
	} else if errors.Is(err, os.ErrPermission) {
		s.warn("9p: permission denied", "kind", msg.Type, "error", err)
		ret, err = msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: EPERM})
		if err != nil {
			return fmt.Errorf("failed to encode response: %v", err)
		}
	} else if err != nil {
		// This error message pops up on Windows when the path is invalid.
		// We can safely ignore it and don't need to spam the logs.
		if strings.Contains(err.Error(), "The filename, directory name, or volume label syntax is incorrect") {
			s.warn("9p: invalid path", "kind", msg.Type, "error", err)
		} else {
			s.error("9p: error handling message", "kind", msg.Type, "error", err)
		}
		ret, err = msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: ENOSYS})
		if err != nil {
			return fmt.Errorf("failed to encode response: %v", err)
		}
	}

	return s.sendMessage(client, ret)
}

func (s *Server) handleClient(client net.Conn) error {
	defer client.Close()

	s.debug("9p: got connection", "addr", client.RemoteAddr())

	reader := binary.NewReader(client, binary.LittleEndian)

	for {
		if err := s.readAndHandleMessage(client, reader); err != nil {
			return err
		}
	}
}

// Execute implements core.Component.
func (s *Server) Serve(listener net.Listener) error {
	s.debug("starting 9p listener", "addr", listener.Addr())

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Error("9p: failed to accept", "error", err)
			return err
		}

		go func(client net.Conn) {
			err := s.handleClient(client)
			if err != nil {
				log.Error("9p: failed to handle client", "error", err)
				return
			}
		}(client)
	}
}

func NewServer(dir filesystem.Directory) *Server {
	return &Server{
		dir:       dir,
		filePaths: make(map[filesystem.FileInfo]uint64),
		fileIds:   make(map[uint32]*serverFile),

		debugEnabled: feature.HasFeature(feature.Feature9PVerbose),
		warnEnabled:  common.IsVerbose(),
	}
}
