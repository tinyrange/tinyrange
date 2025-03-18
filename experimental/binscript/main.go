package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tinyrange/tinyrange/pkg/log"
	"go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func simpleBuiltin(name string, f func() (starlark.Value, error)) *starlark.Builtin {
	return starlark.NewBuiltin(name, func(
		thread *starlark.Thread,
		builtin *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		return f()
	})
}

type BinaryReader struct {
	file   io.ReaderAt
	offset int64
}

func (b *BinaryReader) Attr(name string) (starlark.Value, error) {
	if name == "bytes" {
		return starlark.NewBuiltin("BinaryReader.bytes", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				length int
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"length", &length,
			); err != nil {
				return starlark.None, err
			}

			buf := make([]byte, length)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}

			b.offset += int64(length)

			return starlark.Bytes(string(buf)), nil
		}), nil
	} else if name == "ascii" {
		return starlark.NewBuiltin("BinaryReader.ascii", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				length int
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"length", &length,
			); err != nil {
				return starlark.None, err
			}

			buf := make([]byte, length)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}

			b.offset += int64(length)

			return starlark.String(string(buf)), nil
		}), nil
	} else if name == "seek" {
		return starlark.NewBuiltin("BinaryReader.seek", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				offset int
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"offset", &offset,
			); err != nil {
				return starlark.None, err
			}

			b.offset = int64(offset)

			return starlark.None, nil
		}), nil
	} else if name == "skip" {
		return starlark.NewBuiltin("BinaryReader.skip", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				length int
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"length", &length,
			); err != nil {
				return starlark.None, err
			}

			b.offset += int64(length)

			return b, nil
		}), nil
	} else if name == "tell" {
		return starlark.NewBuiltin("BinaryReader.tell", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return starlark.MakeInt(int(b.offset)), nil
		}), nil
	} else if name == "u64le" {
		return simpleBuiltin("BinaryReader.u64le", func() (starlark.Value, error) {
			buf := make([]byte, 8)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 8

			return starlark.MakeUint(uint(binary.LittleEndian.Uint64(buf))), nil
		}), nil
	} else if name == "u32le" {
		return simpleBuiltin("BinaryReader.u32le", func() (starlark.Value, error) {
			buf := make([]byte, 4)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 4

			return starlark.MakeUint(uint(binary.LittleEndian.Uint32(buf))), nil
		}), nil
	} else if name == "u24le" {
		return simpleBuiltin("BinaryReader.u24le", func() (starlark.Value, error) {
			buf := make([]byte, 3)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 3

			return starlark.MakeUint(uint(binary.LittleEndian.Uint32(append(buf, 0)))), nil
		}), nil
	} else if name == "u16le" {
		return simpleBuiltin("BinaryReader.u16le", func() (starlark.Value, error) {
			buf := make([]byte, 2)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 2

			return starlark.MakeUint(uint(binary.LittleEndian.Uint16(buf))), nil
		}), nil
	} else if name == "u8" {
		return simpleBuiltin("BinaryReader.u8", func() (starlark.Value, error) {
			buf := make([]byte, 1)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 1

			return starlark.MakeUint(uint(buf[0])), nil
		}), nil
	} else if name == "i64le" {
		return simpleBuiltin("BinaryReader.i64le", func() (starlark.Value, error) {
			buf := make([]byte, 8)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 8

			return starlark.MakeInt(int(binary.LittleEndian.Uint64(buf))), nil
		}), nil
	} else if name == "i32le" {
		return simpleBuiltin("BinaryReader.i32le", func() (starlark.Value, error) {
			buf := make([]byte, 4)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 4

			return starlark.MakeInt(int(binary.LittleEndian.Uint32(buf))), nil
		}), nil
	} else if name == "i24le" {
		return simpleBuiltin("BinaryReader.i24le", func() (starlark.Value, error) {
			buf := make([]byte, 3)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 3

			return starlark.MakeInt(int(binary.LittleEndian.Uint32(append(buf, 0)))), nil
		}), nil
	} else if name == "i16le" {
		return simpleBuiltin("BinaryReader.i16le", func() (starlark.Value, error) {
			buf := make([]byte, 2)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 2

			return starlark.MakeInt(int(binary.LittleEndian.Uint16(buf))), nil
		}), nil
	} else if name == "i8" {
		return simpleBuiltin("BinaryReader.i8", func() (starlark.Value, error) {
			buf := make([]byte, 1)
			if _, err := b.file.ReadAt(buf, b.offset); err != nil {
				return starlark.None, err
			}
			b.offset += 1

			return starlark.MakeInt(int(int8(buf[0]))), nil
		}), nil
	} else if name == "slice" {
		return starlark.NewBuiltin("BinaryReader.slice", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				offset int
				length int
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"offset", &offset,
				"length", &length,
			); err != nil {
				return starlark.None, err
			}

			sliceReader := io.NewSectionReader(b.file, int64(offset), int64(length))

			return newBinaryReader(sliceReader), nil
		}), nil
	} else if name == "clone" {
		return starlark.NewBuiltin("BinaryReader.clone", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return newBinaryReader(b.file), nil
		}), nil
	} else {
		return nil, nil
	}
}

func (b *BinaryReader) AttrNames() []string {
	return []string{"bytes"}
}

func (b *BinaryReader) Freeze()               {}
func (b *BinaryReader) Hash() (uint32, error) { return 0, fmt.Errorf("BinaryReader is not hashable") }
func (b *BinaryReader) Truth() starlark.Bool  { return starlark.True }
func (b *BinaryReader) Type() string          { return "BinaryReader" }
func (b *BinaryReader) String() string {
	return fmt.Sprintf("BinaryReader(%s)", b.file)
}

var (
	_ starlark.Value    = &BinaryReader{}
	_ starlark.HasAttrs = &BinaryReader{}
)

func newBinaryReader(file io.ReaderAt) *BinaryReader {
	return &BinaryReader{file: file}
}

func printableByte(b byte) byte {
	if b < 32 || b > 126 {
		return '.'
	}

	return b
}

func printableBytes(data []byte) string {
	ret := ""

	for i := 0; i < len(data); i += 16 {
		end := i + 16
		if end > len(data) {
			end = len(data)
		}

		ret += fmt.Sprintf("%08x | % 32x | ", i, data[i:end])

		for j := i; j < end; j++ {
			ret += fmt.Sprintf("%c", printableByte(data[j]))
		}

		ret += "\n"
	}

	return ret
}

func appMain() error {
	flag.Parse()

	script := flag.Arg(0)
	binaryFile := flag.Arg(1)

	thread := &starlark.Thread{
		Name: "main",
	}

	universe := starlark.StringDict{
		"hex": starlark.NewBuiltin("hex", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				data starlark.Bytes
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"data", &data,
			); err != nil {
				return starlark.None, err
			}

			return starlark.String(fmt.Sprintf("%x", data)), nil
		}),
		"hexdump": starlark.NewBuiltin("hexdump", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				data starlark.Bytes
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"data", &data,
			); err != nil {
				return starlark.None, err
			}

			return starlark.String(printableBytes([]byte(data))), nil
		}),
		"assert": starlark.NewBuiltin("assert", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				value    starlark.Value
				expected starlark.Value
				nonFatal bool
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"value", &value,
				"expected", &expected,
				"non_fatal?", &nonFatal,
			); err != nil {
				return starlark.None, err
			}

			equal, err := starlark.Equal(value, expected)
			if err != nil {
				return starlark.None, err
			}

			if !equal {
				if nonFatal {
					log.Error("assertion failed", "value", value, "expected", expected)
				} else {
					return starlark.None, fmt.Errorf("assertion failed: %s != %s", value, expected)
				}
			}

			return value, nil
		}),
		"error": starlark.NewBuiltin("error", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				message string
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"message", &message,
			); err != nil {
				return starlark.None, err
			}

			return nil, fmt.Errorf("%s", message)
		}),
		"oct": starlark.NewBuiltin("oct", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				data starlark.Int
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"data", &data,
			); err != nil {
				return starlark.None, err
			}

			return starlark.String(fmt.Sprintf("%o", data)), nil
		}),
	}

	universe["json"] = json.Module

	globals, err := starlark.ExecFileOptions(syntax.LegacyFileOptions(), thread, script, nil, universe)
	if err != nil {
		return err
	}

	main, ok := globals["main"]
	if !ok {
		return fmt.Errorf("no main function found")
	}

	f, err := os.Open(binaryFile)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := starlark.Call(thread, main, []starlark.Value{newBinaryReader(f)}, nil); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}
