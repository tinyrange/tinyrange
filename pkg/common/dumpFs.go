package common

import (
	"encoding/csv"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	modTime  time.Time
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
	return []string{f.fullName, kindString, f.mode.String(), fmt.Sprintf("%d", f.size), f.modTime.String()}
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

type fsWalker struct {
	mounts map[string]MountInfo

	records []fileInfo
}

func (w *fsWalker) walk(filename string) error {
	mount, ok := w.mounts[filename]
	if ok {
		if mount.Kind != "rootfs" && mount.Kind != "ext4" {
			return nil
		}
	}

	stat, err := os.Lstat(filename)
	if err != nil {
		return err
	}

	w.records = append(w.records, fileInfo{
		fullName: filename,
		mode:     stat.Mode(),
		size:     uint64(stat.Size()),
		modTime:  stat.ModTime(),
	})

	if stat.Mode().IsDir() {
		children, err := os.ReadDir(filename)
		if err != nil {
			return err
		}

		for _, child := range children {
			err := w.walk(filepath.Join(filename, child.Name()))
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

func DumpFs(outputFilename string) error {
	mountList, err := GetMounts()
	if err != nil {
		return err
	}

	fsWalker := &fsWalker{mounts: make(map[string]MountInfo)}

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
