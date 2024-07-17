package record

import (
	"bufio"
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

func ReadRecordsFromFile(f filesystem.File) ([]starlark.Value, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	scan := bufio.NewScanner(fh)
	scan.Buffer(make([]byte, 16*1024), 8*1024*1024)

	var ret []starlark.Value

	for scan.Scan() {
		val, err := common.StarlarkJsonDecode(nil, starlark.Tuple{starlark.String(scan.Text())}, []starlark.Tuple{})
		if err != nil {
			return nil, err
		}

		ret = append(ret, val)
	}

	if err := scan.Err(); err != nil {
		return nil, err
	}

	return ret, nil
}

type RecordWriter struct {
	w io.WriteCloser
}

func (r *RecordWriter) emitString(s string) error {
	_, err := fmt.Fprintf(r.w, "%s\n", s)
	if err != nil {
		return err
	}

	return nil
}

func (r *RecordWriter) Emit(val starlark.Value) error {
	res, err := common.StarlarkJsonEncode(nil, starlark.Tuple{val}, []starlark.Tuple{})
	if err != nil {
		return err
	}

	resString := string(res.(starlark.String))

	return r.emitString(resString)
}

// WriteTo implements BuildResult.
func (r *RecordWriter) WriteTo(w io.Writer) (n int64, err error) {
	return 0, r.w.Close()
}

// Attr implements starlark.HasAttrs.
func (r *RecordWriter) Attr(name string) (starlark.Value, error) {
	if name == "emit" {
		return starlark.NewBuiltin("RecordWriter.emit", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"val", &val,
			); err != nil {
				return starlark.None, err
			}

			if err := r.Emit(val); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (r *RecordWriter) AttrNames() []string {
	return []string{"emit"}
}

func (*RecordWriter) String() string { return "RecordWriter" }
func (*RecordWriter) Type() string   { return "RecordWriter" }
func (*RecordWriter) Hash() (uint32, error) {
	return 0, fmt.Errorf("RecordWriter is not hashable")
}
func (*RecordWriter) Truth() starlark.Bool { return starlark.True }
func (*RecordWriter) Freeze()              {}

var (
	_ starlark.Value     = &RecordWriter{}
	_ starlark.HasAttrs  = &RecordWriter{}
	_ common.BuildResult = &RecordWriter{}
)

func NewWriter(w io.WriteCloser) *RecordWriter {
	return &RecordWriter{w: w}
}
