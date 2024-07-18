package record

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"go.starlark.net/starlark"
)

var endian = binary.LittleEndian

type recordType byte

const (
	_RECORD_INVALID recordType = iota
	_RECORD_DICT
	_RECORD_ENTRY
	_RECORD_LIST
	_RECORD_INT
	_RECORD_FLOAT
	_RECORD_BOOL
	_RECORD_STRING
	_RECORD_NONE
	_RECORD_LAST = _RECORD_NONE
)

type RecordWriter2 struct {
	writer    io.Writer
	recordBuf []byte
}

func (w *RecordWriter2) writeAt(buf []byte, off int) error {
	if len(buf)+off > len(w.recordBuf) {
		return fmt.Errorf("overflowed recordBuf: off=%d len=%d", off, len(buf))
	}

	copy(w.recordBuf[off:], buf)

	return nil
}

func (w *RecordWriter2) writeU32(val uint32, off int) error {
	if 4+off > len(w.recordBuf) {
		return fmt.Errorf("overflowed recordBuf: off=%d len=%d", off, 4)
	}

	endian.PutUint32(w.recordBuf[off:off+4], val)

	return nil
}

func (w *RecordWriter2) writeU64(val uint64, off int) error {
	if 8+off > len(w.recordBuf) {
		return fmt.Errorf("overflowed recordBuf: off=%d len=%d", off, 4)
	}

	endian.PutUint64(w.recordBuf[off:off+8], val)

	return nil
}

var HEADER_SIZE = 5

func (w *RecordWriter2) writeHeader(typ recordType, len int, off int) error {
	if err := w.writeU32(uint32(HEADER_SIZE+len), off); err != nil {
		return err
	}

	if err := w.writeAt([]byte{byte(typ)}, off+4); err != nil {
		return err
	}

	return nil
}

func (w *RecordWriter2) writeTo(val starlark.Value, off int) (int, error) {
	switch val := val.(type) {
	case starlark.Bool:
		if err := w.writeHeader(_RECORD_BOOL, 1, off); err != nil {
			return 0, err
		}

		if val {
			if err := w.writeAt([]byte{1}, off+HEADER_SIZE); err != nil {
				return 0, err
			}
		} else {
			if err := w.writeAt([]byte{0}, off+HEADER_SIZE); err != nil {
				return 0, err
			}
		}
		return HEADER_SIZE + 1, nil
	case starlark.Int:
		v, ok := val.Uint64()
		if ok {
			if err := w.writeHeader(_RECORD_INT, 8, off); err != nil {
				return 0, err
			}
			if err := w.writeU64(v, off+HEADER_SIZE); err != nil {
				return 0, err
			}

			return HEADER_SIZE + 8, nil
		}

		f := val.Float()

		u := math.Float64bits(float64(f))

		if err := w.writeHeader(_RECORD_FLOAT, 8, off); err != nil {
			return 0, err
		}
		if err := w.writeU64(u, off+HEADER_SIZE); err != nil {
			return 0, err
		}

		return HEADER_SIZE + 8, nil
	case starlark.Float:
		u := math.Float64bits(float64(val))

		if err := w.writeHeader(_RECORD_FLOAT, 8, off); err != nil {
			return 0, err
		}
		if err := w.writeU64(u, off+HEADER_SIZE); err != nil {
			return 0, err
		}

		return HEADER_SIZE + 8, nil
	case starlark.String:
		data := []byte(val)

		if err := w.writeHeader(_RECORD_STRING, len(data), off); err != nil {
			return 0, err
		}

		if err := w.writeAt(data, off+HEADER_SIZE); err != nil {
			return 0, err
		}

		return HEADER_SIZE + len(data), nil
	case *starlark.Dict:
		// dict is stored as a series of tuples with a set of items.
		// it starts with the number of items (stored as uint32).
		listLen := 0
		if err := w.writeU32(uint32(val.Len()), off+HEADER_SIZE+listLen); err != nil {
			return 0, err
		}
		listLen += 4

		it := val.Iterate()
		defer it.Done()

		var key starlark.Value
		for it.Next(&key) {
			keyStr, ok := starlark.AsString(key)
			if !ok {
				return 0, fmt.Errorf("could not convert %s to string", key.Type())
			}

			entStart := off + HEADER_SIZE + listLen

			localLen := 0

			if err := w.writeU32(uint32(len(keyStr)), entStart+HEADER_SIZE+localLen); err != nil {
				return 0, err
			}

			localLen += 4

			if err := w.writeAt([]byte(keyStr), entStart+HEADER_SIZE+localLen); err != nil {
				return 0, err
			}

			value, _, err := val.Get(key)
			if err != nil {
				return 0, err
			}

			valLen, err := w.writeTo(value, entStart+HEADER_SIZE+localLen)
			if err != nil {
				return 0, err
			}

			localLen += valLen

			if err := w.writeHeader(_RECORD_ENTRY, localLen, entStart); err != nil {
				return 0, err
			}

			listLen += HEADER_SIZE + localLen
		}

		if err := w.writeHeader(_RECORD_DICT, listLen, off); err != nil {
			return 0, err
		}

		return HEADER_SIZE + listLen, nil
	case *starlark.List:
		// list starts with a number of items (stored as uint32).
		listLen := 0
		if err := w.writeU32(uint32(val.Len()), off+HEADER_SIZE+listLen); err != nil {
			return 0, err
		}
		listLen += 4

		for i := 0; i < val.Len(); i++ {
			valLen, err := w.writeTo(val.Index(i), off+HEADER_SIZE+listLen)
			if err != nil {
				return 0, err
			}

			listLen += valLen
		}

		if err := w.writeHeader(_RECORD_LIST, listLen, off); err != nil {
			return 0, err
		}

		return HEADER_SIZE + listLen, nil
	case starlark.NoneType:
		if err := w.writeHeader(_RECORD_NONE, 0, off); err != nil {
			return 0, err
		}
		return HEADER_SIZE, nil
	default:
		return 0, fmt.Errorf("writeTo not implemented: %T %+v", val, val)
	}
}

func (w *RecordWriter2) WriteValue(val starlark.Value) error {
	len, err := w.writeTo(val, 0)
	if err != nil {
		return err
	}

	if _, err := w.writer.Write(w.recordBuf[:len]); err != nil {
		return err
	}

	return nil
}

func NewWriter2(w io.Writer) *RecordWriter2 {
	return &RecordWriter2{
		writer:    w,
		recordBuf: make([]byte, 4*1024*1024),
	}
}

type record struct {
	r        io.ReaderAt
	off      int64
	data     [1024 - 32 - 24]byte
	totalLen int
	rest     []byte // only filled when needed
}

// ReadAt implements io.ReaderAt.
func (r *record) ReadAt(p []byte, off int64) (n int, err error) {
	if int(off) < len(r.data) {
		// We can partially or completely fill the read with our already cached data.
		n = copy(p, r.data[off:])
	}

	if r.rest == nil {
		r.rest = make([]byte, r.totalLen-len(r.data))
		if _, err := r.ReadAt(r.rest, r.off+int64(len(r.data))); err != nil {
			return 0, err
		}
	}

	// The rest of the data comes from the data we pulled into memory in r.rest.
	n += copy(p[n:], r.rest[int(off)-len(r.data):])

	return
}

func (r *record) kind() recordType {
	return recordType(r.data[4])
}

var (
	_ io.ReaderAt = &record{}
)

type recordDict struct {
	rec *record
	off int64
}

func (*recordDict) String() string        { return "recordDict" }
func (*recordDict) Type() string          { return "recordDict" }
func (*recordDict) Hash() (uint32, error) { return 0, fmt.Errorf("recordDict is not hashable") }
func (*recordDict) Truth() starlark.Bool  { return starlark.True }
func (*recordDict) Freeze()               {}

var (
	_ starlark.Value = &recordDict{}
)

type RecordReader2 struct {
	err     chan error
	records chan *record
	r       io.ReaderAt
	off     int64
}

func (r *RecordReader2) readValues() {
	for {
		rec := &record{
			r:   r.r,
			off: r.off,
		}

		if _, err := r.r.ReadAt(rec.data[:], r.off); err != nil {
			r.err <- err
			return
		}

		recordLen := binary.LittleEndian.Uint32(rec.data[0:4])
		kind := rec.kind()

		if kind == _RECORD_INVALID || kind > _RECORD_LAST {
			r.err <- fmt.Errorf("invalid read")
			return
		}

		rec.totalLen = int(recordLen)

		r.off += int64(recordLen)

		r.records <- rec
	}
}

func (r *RecordReader2) ReadValue() (starlark.Value, error) {
	select {
	case err := <-r.err:
		return nil, err
	case record := <-r.records:
		switch record.kind() {
		case _RECORD_DICT:
			return &recordDict{rec: record, off: 0}, nil
		default:
			return nil, fmt.Errorf("unimplemented top level record: %d", record.kind())
		}
	}
}

func NewReader2(r io.ReaderAt) *RecordReader2 {
	reader := &RecordReader2{
		r:       r,
		err:     make(chan error),
		records: make(chan *record, 8),
	}

	go reader.readValues()

	return reader
}
