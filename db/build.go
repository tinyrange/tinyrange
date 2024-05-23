package db

import (
	"archive/tar"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/tinyrange/pkg2/core"
	"go.starlark.net/starlark"
)

// The build system is a addressable managed cache. It is writable by the
// scripting language and can be used to derive downloaded packages and
// other information.

func hashSha512(s string) string {
	sum := sha512.Sum512([]byte(s))
	return hex.EncodeToString(sum[:])
}

type BuildResult interface {
	starlark.Value
	io.WriterTo
}

type archiveResult struct {
	fs     *StarDirectory
	format string
}

// WriteTo implements BuildResult.
func (a *archiveResult) WriteTo(w io.Writer) (n int64, err error) {
	switch a.format {
	case ".tar":
		writer := tar.NewWriter(w)

		return a.fs.WriteTar(writer)
	default:
		return -1, fmt.Errorf("archiveResult: unknown format: %s", a.format)
	}
}

func (*archiveResult) String() string        { return "archiveResult" }
func (*archiveResult) Type() string          { return "archiveResult" }
func (*archiveResult) Hash() (uint32, error) { return 0, fmt.Errorf("archiveResult is not hashable") }
func (*archiveResult) Truth() starlark.Bool  { return starlark.True }
func (*archiveResult) Freeze()               {}

var (
	_ BuildResult = &archiveResult{}
)

func writeBuildResult(result starlark.Value, target io.Writer) error {
	switch result := result.(type) {
	case BuildResult:
		_, err := result.WriteTo(target)

		return err
	default:
		return fmt.Errorf("%s(%T) is not a BuildResult", result.Type(), result)
	}
}

func getTagFragment(val starlark.Value) (string, string, error) {
	switch val := val.(type) {
	case starlark.String:
		return string(val), string(val), nil
	case *ScriptFile:
		return "<script_file>", val.filename, nil
	default:
		return "", "", fmt.Errorf("%s could not be converted into a tag fragment", val.Type())
	}
}

func getTag(tag starlark.Tuple) (string, string, error) {
	var (
		publicFragments  []string
		privateFragments []string
	)

	for _, val := range tag {
		public, private, err := getTagFragment(val)
		if err != nil {
			return "", "", err
		}

		publicFragments = append(publicFragments, public)
		privateFragments = append(privateFragments, private)
	}

	return strings.Join(publicFragments, "_"), strings.Join(privateFragments, "_"), nil
}

type buildContext struct {
}

// Attr implements starlark.HasAttrs.
func (b *buildContext) Attr(name string) (starlark.Value, error) {
	if name == "archive" {
		return starlark.NewBuiltin("BuildContext.archive", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				dir    *StarDirectory
				format string
			)

			format = ".tar"

			if err := starlark.UnpackArgs("BuildContext.archive", args, kwargs,
				"dir", &dir,
				"format?", &format,
			); err != nil {
				return starlark.None, err
			}

			return &archiveResult{
				fs:     dir,
				format: format,
			}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (b *buildContext) AttrNames() []string {
	return []string{"archive"}
}

func (*buildContext) String() string        { return "BuildContext" }
func (*buildContext) Type() string          { return "BuildContext" }
func (*buildContext) Hash() (uint32, error) { return 0, fmt.Errorf("BuildContext is not hashable") }
func (*buildContext) Truth() starlark.Bool  { return starlark.True }
func (*buildContext) Freeze()               {}

var (
	_ starlark.Value    = &buildContext{}
	_ starlark.HasAttrs = &buildContext{}
)

func build(eif *core.EnvironmentInterface, tag starlark.Tuple, builder *starlark.Function, args starlark.Tuple) (starlark.Value, error) {
	public, private, err := getTag(tag)
	if err != nil {
		return starlark.None, err
	}

	f, err := eif.Cache(getSha256([]byte(private)), 1, -1, func(w io.Writer) error {
		slog.Info("building", "tag", private)

		thread := &starlark.Thread{Name: public}

		ctx := &buildContext{}

		res, err := starlark.Call(thread, builder, append(starlark.Tuple{ctx}, args...), []starlark.Tuple{})
		if err != nil {
			return err
		}

		if err := writeBuildResult(res, w); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return starlark.None, err
	}

	return &StarFile{name: public, f: f, source: nil}, nil
}
