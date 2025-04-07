package common

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/tinyrange/tinyrange/pkg/path"
)

type MountInfo struct {
	Source  string
	Target  string
	Kind    string
	Options string
}

type fileInfo struct {
	fullName string
	mode     fs.FileMode
	size     uint64
	uid      uint32
	gid      uint32
	hash     string
}

func getKind(mode fs.FileMode) string {
	var ret []string
	if mode&fs.ModeDir != 0 {
		ret = append(ret, "dir")
	}
	if mode&fs.ModeSymlink != 0 {
		ret = append(ret, "symlink")
	}
	if mode&fs.ModeNamedPipe != 0 {
		ret = append(ret, "pipe")
	}
	if mode&fs.ModeSocket != 0 {
		ret = append(ret, "socket")
	}
	if mode&fs.ModeDevice != 0 {
		ret = append(ret, "dev")
	}
	if mode&fs.ModeCharDevice != 0 {
		ret = append(ret, "chardev")
	}
	if mode&fs.ModeIrregular != 0 {
		ret = append(ret, "irregular")
	}
	if len(ret) == 0 {
		return "file"
	} else {
		return strings.Join(ret, ",")
	}
}

func (f fileInfo) encode() []string {
	kindString := getKind(f.mode)
	return []string{f.fullName, kindString, f.mode.String(), fmt.Sprintf("%d", f.size), fmt.Sprintf("%d", f.uid), fmt.Sprintf("%d", f.gid), f.hash}
}

func GetMounts() ([]MountInfo, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("GetMounts only works on Linux")
	}

	content, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, err
	}

	var ret []MountInfo

	lines := strings.Split(string(content), "\n")

	for _, line := range lines {
		tokens := strings.Split(line, " ")
		if len(tokens) < 4 {
			continue
		}

		ret = append(ret, MountInfo{
			Source:  tokens[0],
			Target:  tokens[1],
			Kind:    tokens[2],
			Options: tokens[3],
		})
	}

	return ret, nil
}

func getFileHash(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()

	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}

	hashBytes := hash.Sum(nil)

	return hex.EncodeToString(hashBytes), nil
}

type fsWalker struct {
	mounts    map[string]MountInfo
	hashFiles bool

	records []fileInfo
}

func (w *fsWalker) walk(filename string) error {
	mount, ok := w.mounts[filename]
	if ok {
		// We want to skip all mounts except rootfs and ext4``
		if mount.Kind != "rootfs" &&
			mount.Kind != "ext4" &&
			// overlayfs is a special case, we want to see the contents of the overlay
			mount.Kind != "overlay" {
			return nil
		}
	}

	stat, err := os.Lstat(filename)
	if err != nil {
		return err
	}

	statSys := stat.Sys().(*syscall.Stat_t)

	record := fileInfo{
		fullName: filename,
		mode:     stat.Mode(),
		size:     uint64(stat.Size()),
		uid:      statSys.Uid,
		gid:      statSys.Gid,
	}

	if w.hashFiles && stat.Mode().IsRegular() {
		hash, err := getFileHash(filename)
		if err != nil {
			return err
		}

		record.hash = hash

		s := strings.Join(record.encode(), ",")
		fmt.Fprintf(os.Stderr, "%s\n", s)
	}

	w.records = append(w.records, record)

	if stat.Mode().IsDir() {
		children, err := os.ReadDir(filename)
		if err != nil {
			return err
		}

		for _, child := range children {
			err := w.walk(path.Native.Join(filename, child.Name()))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *fsWalker) writeCsv(wr io.Writer) error {
	csvWriter := csv.NewWriter(wr)

	for _, record := range w.records {
		err := csvWriter.Write(record.encode())
		if err != nil {
			return err
		}
	}

	csvWriter.Flush()

	return csvWriter.Error()
}

func DumpFs(outputFilename string, hashFiles bool) error {
	mountList, err := GetMounts()
	if err != nil {
		return err
	}

	fsWalker := &fsWalker{
		mounts:    make(map[string]MountInfo),
		hashFiles: hashFiles,
	}

	for _, mount := range mountList {
		fsWalker.mounts[mount.Target] = mount
	}

	err = fsWalker.walk("/")
	if err != nil {
		return err
	}

	w, err := os.Create(outputFilename)
	if err != nil {
		return err
	}
	defer w.Close()

	err = fsWalker.writeCsv(w)
	if err != nil {
		return err
	}

	return nil
}
