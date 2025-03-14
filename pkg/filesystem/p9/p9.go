package p9

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common/binary"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

const (
	IOUNIT_SIZE = 0
	P9_DEBUG    = true
)

type Server struct {
	dir filesystem.Directory

	filePaths map[filesystem.FileInfo]uint64
	fileIds   map[uint32]filesystem.File
}

// Close implements core.Component.
func (*Server) Close() error {
	return nil
}

func (s *Server) getFid(id uint32) (filesystem.File, error) {
	fid, ok := s.fileIds[id]
	if !ok {
		return nil, fmt.Errorf("fid %0X not found", id)
	}

	return fid, nil
}

func (s *Server) setFid(id uint32, file filesystem.File) error {
	if file == nil {
		return fmt.Errorf("file is nil")
	}

	s.fileIds[id] = file

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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		stat, err := fid.Stat()
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

			retMsg.Mode = uint32(stat.Mode()&fs.ModePerm) | kind

			retMsg.Valid |= P9_GETATTR_MODE
		}

		if body.RequestMask&P9_GETATTR_NLINK != 0 {
			if stat.Kind() == filesystem.TypeDirectory {
				retMsg.Nlink = 1
			}

			retMsg.Valid |= P9_GETATTR_NLINK
		}

		if body.RequestMask&P9_GETATTR_UID != 0 {
			uid, _, err := filesystem.GetUidAndGid(fid)
			if err != nil {
				return nil, err
			}

			retMsg.Uid = uint32(uid)

			retMsg.Valid |= P9_GETATTR_UID
		}

		if body.RequestMask&P9_GETATTR_GID != 0 {
			_, gid, err := filesystem.GetUidAndGid(fid)
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

		if P9_DEBUG {
			slog.Debug("9p: getattr", "retMsg", fmt.Sprintf("%+v", retMsg))
		}

		return ret.EncodeBody(MsgRgetattr, msg.Tag, &retMsg)
	case MsgTversion:
		var body Tversion

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		return ret.EncodeBody(MsgRversion, msg.Tag, &Rversion{
			Msize:   body.Msize,
			Version: body.Version,
		})
	case MsgTflush:
		var body Tflush

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		return ret.EncodeBody(MsgRflush, msg.Tag, &Rflush{})
	case MsgTattach:
		var body Tattach

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		if err := s.setFid(body.Fid, s.dir); err != nil {
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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		retMsg := Rwalk{}

		var newFile filesystem.File

		if len(body.Nwname) > 0 {
			newDir, ok := fid.(filesystem.Directory)
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
			newFile = fid
		}

		if err := s.setFid(body.Newfid, newFile); err != nil {
			return nil, fmt.Errorf("failed to set new fid: %v", err)
		}

		if P9_DEBUG {
			slog.Debug("", "Newfid", body.Newfid, "newInfo", newFile)
		}

		return ret.EncodeBody(MsgRwalk, msg.Tag, &retMsg)
	case MsgTread:
		var body Tread

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		buf := make([]byte, body.Count)

		fh, err := fid.Open()
		if err != nil {
			return nil, err
		}
		defer fh.Close()

		n, err := fh.ReadAt(buf, int64(body.Offset))
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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		mut, ok := fid.(filesystem.MutableFile)
		if !ok {
			return nil, fmt.Errorf("file is not mutable")
		}

		fh, err := mut.OpenMut()
		if err != nil {
			return nil, err
		}
		defer fh.Close()

		n, err := fh.WriteAt(body.Data, int64(body.Offset))
		if err != nil {
			return nil, err
		}

		return ret.EncodeBody(MsgRwrite, msg.Tag, &Rwrite{Count: uint32(n)})
	case MsgTclunk:
		var body Tclunk

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		delete(s.fileIds, body.Fid)

		return ret.EncodeBody(MsgRclunk, msg.Tag, &Rclunk{})
	case MsgTremove:
		var body Tremove

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		slog.Debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Tremove not implemented")
	case MsgTstatfs:
		var body Tstatfs

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		slog.Debug("9p: message", "type", msg.Type, "body", body)

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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		stat, err := fid.Stat()
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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		dir, ok := fid.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		f, err := dir.Create(body.Name, filesystem.NewMemoryFile(filesystem.TypeRegular))
		if err != nil {
			return nil, err
		}

		memF, ok := f.(filesystem.MutableFile)
		if !ok {
			return nil, fs.ErrPermission
		}

		err = memF.Chmod(fs.FileMode(body.Mode) & fs.ModePerm)
		if err != nil {
			return nil, err
		}

		newInfo, err := memF.Stat()
		if err != nil {
			return nil, err
		}

		if err := s.setFid(body.Fid, memF); err != nil {
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

		slog.Debug("9p: message", "type", msg.Type, "body", body)

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		dir, ok := fid.(filesystem.MutableDirectory)
		if !ok {
			return nil, fs.ErrPermission
		}

		f, err := dir.Create(body.Name, filesystem.NewSymlink(body.Symtgt))
		if err != nil {
			return nil, err
		}

		newInfo, err := f.Stat()
		if err != nil {
			return nil, err
		}

		return msg.EncodeBody(MsgRlcreate, msg.Tag, &Rsymlink{
			Qid: s.getQid(newInfo),
		})
	case MsgTmknod:
		var body Tmknod

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		slog.Debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Tmknod not implemented")
	case MsgTrename:
		var body Trename

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		slog.Debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Trename not implemented")
	case MsgTreadlink:
		var body Treadlink

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		slog.Debug("9p: message", "type", msg.Type, "body", body)

		return nil, fmt.Errorf("9p: Treadlink not implemented")
	case MsgTsetattr:
		var body Tsetattr

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		if body.Valid&P9_SETATTR_MODE != 0 {
			mut, ok := fid.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			if err := mut.Chmod(fs.FileMode(body.Mode) & fs.ModePerm); err != nil {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_UID != 0 {
			mut, ok := fid.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			// not supported errors are ignored
			if err := mut.Chown(int(body.Uid), -1); err != nil && !errors.Is(err, filesystem.ErrNotSupported) {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_GID != 0 {
			mut, ok := fid.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			// not supported errors are ignored
			if err := mut.Chown(-1, int(body.Gid)); err != nil && !errors.Is(err, filesystem.ErrNotSupported) {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_SIZE != 0 {
			mut, ok := fid.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			if err := mut.Truncate(int64(body.Size)); err != nil {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_ATIME != 0 {
			// Ignored
		}
		if body.Valid&P9_SETATTR_MTIME != 0 {
			mut, ok := fid.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			if err := mut.Chtimes(time.Now()); err != nil {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_CTIME != 0 {
			mut, ok := fid.(filesystem.MutableFile)
			if !ok {
				return nil, fs.ErrPermission
			}

			// Setting -1 should have the side effect of setting the current time.
			if err := mut.Chown(-1, -1); err != nil && !errors.Is(err, filesystem.ErrNotSupported) {
				return nil, err
			}
		}
		if body.Valid&P9_SETATTR_ATIME_SET != 0 {
			// slog.Warn("p9: unimplemented P9_SETATTR_ATIME_SET")
		}
		if body.Valid&P9_SETATTR_MTIME_SET != 0 {
			// slog.Warn("p9: unimplemented P9_SETATTR_MTIME_SET")
		}

		_ = fid

		return msg.EncodeBody(MsgRsetattr, msg.Tag, &Rsetattr{})
	case MsgTxattrwalk:
		var body Txattrwalk

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		return msg.EncodeBody(MsgRxattrwalk, msg.Tag, &Rxattrwalk{})
	case MsgTxattrcreate:
		var body Txattrcreate

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		// ignored
		return msg.EncodeBody(MsgRxattrcreate, msg.Tag, &Rxattrcreate{})
	case MsgTreaddir:
		var body Treaddir

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Fid)
		if err != nil {
			return nil, err
		}

		dir, ok := fid.(filesystem.Directory)
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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		// TOOD(joshua): This is a no-op for now.

		return ret.EncodeBody(MsgRlock, msg.Tag, &Rlock{
			Status: P9_LOCK_SUCCESS,
		})
	case MsgTlink:
		var body Tlink

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		return nil, fmt.Errorf("9p: Tlink not implemented")
	case MsgTmkdir:
		var body Tmkdir

		if err := msg.DecodeBody(&body); err != nil {
			return nil, err
		}

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Dfid)
		if err != nil {
			return nil, err
		}

		mutDir, ok := fid.(filesystem.MutableDirectory)
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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		// slog.Info("rename", "olddirfid", body.Olddirfid, "oldname", body.Oldname, "newdirfid", body.Newdirfid, "newname", body.Newname)

		// Get the old directory.
		oldFid, err := s.getFid(body.Olddirfid)
		if err != nil {
			return nil, err
		}

		oldMutDir, ok := oldFid.(filesystem.MutableDirectory)
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

		newMutDir, ok := newFid.(filesystem.MutableDirectory)
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

		if P9_DEBUG {
			slog.Debug("9p: message", "type", msg.Type, "body", body)
		}

		fid, err := s.getFid(body.Dirfd)
		if err != nil {
			return nil, err
		}

		mutDir, ok := fid.(filesystem.MutableDirectory)
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

func (s *Server) handleClient(client net.Conn) error {
	defer client.Close()

	slog.Debug("9p: got connection", "addr", client.RemoteAddr())

	reader := binary.NewReader(client, binary.LittleEndian)

	buf := new(bytes.Buffer)

	for {
		var msg Message

		err := msg.Decode(reader)
		if err != nil {
			return fmt.Errorf("failed to decode message: %v", err)
		}

		ret, err := s.handleMessage(&msg)
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("9p: file not found", "kind", msg.Type, "error", err)
			ret, err = msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: ENOENT})
			if err != nil {
				return fmt.Errorf("failed to encode response: %v", err)
			}
		} else if errors.Is(err, os.ErrPermission) {
			slog.Debug("9p: permission denied", "kind", msg.Type, "error", err)
			ret, err = msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: EPERM})
			if err != nil {
				return fmt.Errorf("failed to encode response: %v", err)
			}
		} else if err != nil {
			slog.Warn("9p: error handling message", "kind", msg.Type, "error", err)
			ret, err = msg.EncodeBody(MsgRlerror, msg.Tag, &Rlerror{Ecode: ENOSYS})
			if err != nil {
				return fmt.Errorf("failed to encode response: %v", err)
			}
		}

		buf.Reset()

		err = ret.Encode(binary.NewWriter(buf, binary.LittleEndian))
		if err != nil {
			return fmt.Errorf("failed to encode response: %v", err)
		}

		_, err = client.Write(buf.Bytes())
		if err != nil {
			return fmt.Errorf("failed to send response: %v", err)
		}
	}
}

// Execute implements core.Component.
func (s *Server) Serve(listener net.Listener) error {
	slog.Debug("starting 9p listener", "addr", listener.Addr())

	for {
		client, err := listener.Accept()
		if err != nil {
			slog.Error("9p: failed to accept", "error", err)
			return err
		}

		go func(client net.Conn) {
			err := s.handleClient(client)
			if err != nil {
				slog.Error("9p: failed to handle client", "error", err)
				return
			}
		}(client)
	}
}

func NewServer(dir filesystem.Directory) *Server {
	return &Server{
		dir:       dir,
		filePaths: make(map[filesystem.FileInfo]uint64),
		fileIds:   make(map[uint32]filesystem.File),
	}
}
