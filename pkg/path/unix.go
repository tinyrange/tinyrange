package path

import (
	"errors"
	"os"
	"strings"
)

// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/internal/bytealg/lastindexbyte_generic.go
func lastIndexByteString(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// A lazybuf is a lazily constructed path buffer.
// It supports append, reading previously appended bytes,
// and retrieving the final string. It does not allocate a buffer
// to hold the output until that output diverges from s.
type lazybuf struct {
	s   string
	buf []byte
	w   int
}

func (b *lazybuf) index(i int) byte {
	if b.buf != nil {
		return b.buf[i]
	}
	return b.s[i]
}

func (b *lazybuf) append(c byte) {
	if b.buf == nil {
		if b.w < len(b.s) && b.s[b.w] == c {
			b.w++
			return
		}
		b.buf = make([]byte, len(b.s))
		copy(b.buf, b.s[:b.w])
	}
	b.buf[b.w] = c
	b.w++
}

func (b *lazybuf) string() string {
	if b.buf == nil {
		return b.s[:b.w]
	}
	return string(b.buf[:b.w])
}

const UnixPathSeparator = '/'

type unixPathImplementation struct{}

// Join implements Path.
func (u *unixPathImplementation) Join(elem ...string) string {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/filepath/path_unix.go
	for i, e := range elem {
		if e != "" {
			return u.Clean(strings.Join(elem[i:], string(UnixPathSeparator)))
		}
	}
	return ""
}

// Base implements Path.
func (u *unixPathImplementation) Base(path string) string {
	if path == "" {
		return "."
	}
	// Strip trailing slashes.
	for len(path) > 0 && path[len(path)-1] == '/' {
		path = path[0 : len(path)-1]
	}
	// Find the last element
	if i := lastIndexByteString(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	// If empty now, it had only slashes.
	if path == "" {
		return "/"
	}
	return path
}

// Dir implements Path.
func (u *unixPathImplementation) Dir(path string) string {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/path.go
	dir, _ := u.Split(path)
	return u.Clean(dir)
}

// Clean implements Path.
func (u *unixPathImplementation) Clean(path string) string {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/path.go
	if path == "" {
		return "."
	}

	rooted := path[0] == '/'
	n := len(path)

	// Invariants:
	//	reading from path; r is index of next byte to process.
	//	writing to buf; w is index of next byte to write.
	//	dotdot is index in buf where .. must stop, either because
	//		it is the leading slash or it is a leading ../../.. prefix.
	out := lazybuf{s: path}
	r, dotdot := 0, 0
	if rooted {
		out.append('/')
		r, dotdot = 1, 1
	}

	for r < n {
		switch {
		case path[r] == '/':
			// empty path element
			r++
		case path[r] == '.' && (r+1 == n || path[r+1] == '/'):
			// . element
			r++
		case path[r] == '.' && path[r+1] == '.' && (r+2 == n || path[r+2] == '/'):
			// .. element: remove to last /
			r += 2
			switch {
			case out.w > dotdot:
				// can backtrack
				out.w--
				for out.w > dotdot && out.index(out.w) != '/' {
					out.w--
				}
			case !rooted:
				// cannot backtrack, but not rooted, so append .. element.
				if out.w > 0 {
					out.append('/')
				}
				out.append('.')
				out.append('.')
				dotdot = out.w
			}
		default:
			// real path element.
			// add slash if needed
			if rooted && out.w != 1 || !rooted && out.w != 0 {
				out.append('/')
			}
			// copy element
			for ; r < n && path[r] != '/'; r++ {
				out.append(path[r])
			}
		}
	}

	// Turn empty string into "."
	if out.w == 0 {
		return "."
	}

	return out.string()
}

// Ext implements Path.
func (u *unixPathImplementation) Ext(path string) string {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/path.go
	for i := len(path) - 1; i >= 0 && path[i] != '/'; i-- {
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}

// Abs implements Path.
func (u *unixPathImplementation) Abs(p string) (string, error) {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/filepath/path.go
	if u.IsAbs(p) {
		return u.Clean(p), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return u.Join(wd, p), nil
}

// IsAbs implements Path.
func (u *unixPathImplementation) IsAbs(path string) bool {
	return len(path) > 0 && path[0] == '/'
}

// Split implements Path.
func (u *unixPathImplementation) Split(path string) (dir, file string) {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/path.go
	i := lastIndexByteString(path, '/')
	return path[:i+1], path[i+1:]
}

func countString(s string, c byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			n++
		}
	}
	return n
}

const separator = '/'

// Rel implements Path.
func (u *unixPathImplementation) Rel(basepath, targpath string) (string, error) {
	// From: golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/path/filepath/path.go
	base := u.Clean(basepath)
	targ := u.Clean(targpath)
	if targ == base {
		return ".", nil
	}
	if base == "." {
		base = ""
	}

	// Can't use IsAbs - `\a` and `a` are both relative in Windows.
	baseSlashed := len(base) > 0 && base[0] == separator
	targSlashed := len(targ) > 0 && targ[0] == separator
	if baseSlashed != targSlashed {
		return "", errors.New("Rel: can't make " + targpath + " relative to " + basepath)
	}
	// Position base[b0:bi] and targ[t0:ti] at the first differing elements.
	bl := len(base)
	tl := len(targ)
	var b0, bi, t0, ti int
	for {
		for bi < bl && base[bi] != separator {
			bi++
		}
		for ti < tl && targ[ti] != separator {
			ti++
		}
		if targ[t0:ti] != base[b0:bi] {
			break
		}
		if bi < bl {
			bi++
		}
		if ti < tl {
			ti++
		}
		b0 = bi
		t0 = ti
	}
	if base[b0:bi] == ".." {
		return "", errors.New("Rel: can't make " + targpath + " relative to " + basepath)
	}
	if b0 != bl {
		// Base elements left. Must go up before going down.
		seps := countString(base[b0:bl], separator)
		size := 2 + seps*3
		if tl != t0 {
			size += 1 + tl - t0
		}
		buf := make([]byte, size)
		n := copy(buf, "..")
		for i := 0; i < seps; i++ {
			buf[n] = separator
			copy(buf[n+1:], "..")
			n += 3
		}
		if t0 != tl {
			buf[n] = separator
			copy(buf[n+1:], targ[t0:])
		}
		return string(buf), nil
	}
	return targ[t0:], nil
}

var (
	_ Path = &unixPathImplementation{}
)

var Unix = &unixPathImplementation{}
