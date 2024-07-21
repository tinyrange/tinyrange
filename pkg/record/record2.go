package record

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math"

	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

var endian = binary.LittleEndian

func hashKey(k string) uint32 {
	h := fnv.New32()

	h.Write([]byte(k))

	return h.Sum32()
}

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
	writer    io.WriteCloser
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
		// then it has a hash of the name followed bu the offset of the name.
		listLen := 0
		if err := w.writeU32(uint32(val.Len()), off+HEADER_SIZE+listLen); err != nil {
			return 0, err
		}
		listLen += 4

		count := val.Len()
		tableOffset := off + HEADER_SIZE + listLen

		listLen += count * 8

		it := val.Iterate()
		defer it.Done()

		var key starlark.Value
		for it.Next(&key) {
			keyStr, ok := starlark.AsString(key)
			if !ok {
				return 0, fmt.Errorf("could not convert %s to string", key.Type())
			}

			keyHash := hashKey(keyStr)

			if err := w.writeU32(uint32(keyHash), tableOffset); err != nil {
				return 0, err
			}

			if err := w.writeU32(uint32(listLen), tableOffset+4); err != nil {
				return 0, err
			}

			tableOffset += 8

			entStart := off + HEADER_SIZE + listLen

			localLen := 0

			if err := w.writeU32(uint32(len(keyStr)), entStart+HEADER_SIZE+localLen); err != nil {
				return 0, err
			}

			localLen += 4

			if err := w.writeAt([]byte(keyStr), entStart+HEADER_SIZE+localLen); err != nil {
				return 0, err
			}

			localLen += len(keyStr)

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

func (w *RecordWriter2) Emit(val starlark.Value) error {
	len, err := w.writeTo(val, 0)
	if err != nil {
		return err
	}

	if _, err := w.writer.Write(w.recordBuf[:len]); err != nil {
		return err
	}

	return nil
}

// WriteTo implements BuildResult.
func (r *RecordWriter2) WriteTo(w io.Writer) (n int64, err error) {
	return 0, r.writer.Close()
}

// Attr implements starlark.HasAttrs.
func (r *RecordWriter2) Attr(name string) (starlark.Value, error) {
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
func (r *RecordWriter2) AttrNames() []string {
	return []string{"emit"}
}

func (*RecordWriter2) String() string { return "RecordWriter" }
func (*RecordWriter2) Type() string   { return "RecordWriter" }
func (*RecordWriter2) Hash() (uint32, error) {
	return 0, fmt.Errorf("RecordWriter is not hashable")
}
func (*RecordWriter2) Truth() starlark.Bool { return starlark.True }
func (*RecordWriter2) Freeze()              {}

var (
	_ starlark.Value     = &RecordWriter2{}
	_ starlark.HasAttrs  = &RecordWriter2{}
	_ common.BuildResult = &RecordWriter2{}
)

func NewWriter2(w io.WriteCloser) *RecordWriter2 {
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

func (r *record) recordToString(off int64, length uint32) (starlark.Value, error) {
	data := make([]byte, length-uint32(HEADER_SIZE))

	if _, err := r.ReadAt(data, off+int64(HEADER_SIZE)); err != nil {
		return starlark.None, fmt.Errorf("could not read: %+v", err)
	}

	return starlark.String(data), nil
}

func (r *record) recordToList(off int64) (starlark.Value, error) {
	var buf [5]byte

	val, err := r.readU32(buf, off+int64(HEADER_SIZE))
	if err != nil {
		return starlark.None, err
	}

	return &recordList{rec: r, off: off, len: int(val)}, nil
}

func (r *record) recordToDict(off int64) (starlark.Value, error) {
	var buf [5]byte

	itemCount, err := r.readU32(buf, off+int64(HEADER_SIZE))
	if err != nil {
		return starlark.None, err
	}

	itemIndex := make([]byte, itemCount*8)

	if _, err := r.ReadAt(itemIndex, off+int64(HEADER_SIZE)+4); err != nil {
		return starlark.None, err
	}

	return &recordDict{rec: r, off: off, len: int(itemCount), itemIndex: itemIndex}, nil
}

func (r *record) toStarlarkValue(off int64) (starlark.Value, error) {
	var buf [5]byte

	kind, length, err := r.readRecord(buf, off)
	if err != nil {
		return starlark.None, err
	}

	if kind == _RECORD_INVALID || kind > _RECORD_LAST {
		return starlark.None, fmt.Errorf("invalid read")
	}

	_ = length

	switch kind {
	case _RECORD_NONE:
		return starlark.None, nil
	case _RECORD_DICT:
		return r.recordToDict(off)
	case _RECORD_ENTRY:
		return starlark.None, fmt.Errorf("attempt to convert entry to Value")
	case _RECORD_LIST:
		return r.recordToList(off)
	case _RECORD_STRING:
		return r.recordToString(off, length)
	default:
		return starlark.None, fmt.Errorf("toStarlarkValue unimplemented: %d", kind)
	}
}

func (r *record) readRest() error {
	// Read the rest of the data from the underlying file.
	if r.rest == nil {
		r.rest = make([]byte, r.totalLen-len(r.data))
		if _, err := r.r.ReadAt(r.rest, r.off+int64(len(r.data))); err != nil {
			return err
		}
	}

	return nil
}

func (r *record) compare(p []byte, off int64) (bool, error) {
	if off > int64(r.totalLen) {
		return false, io.EOF
	}

	if int(off) < len(r.data) {
		// Some of the request can be fulfilled with the first fragment.
		fragLength := min(len(p), len(r.data)-int(off))

		if !bytes.Equal(p[:fragLength], r.data[off:off+int64(fragLength)]) {
			return false, nil
		}

		// Make sure we don't check data we've already checked.
		p = p[fragLength:]
		off += int64(fragLength)

		// Have we checked everything.
		if len(p) <= 0 {
			return true, nil
		}
	}

	if err := r.readRest(); err != nil {
		return false, err
	}

	return bytes.Equal(p, r.rest[int(off)-len(r.data):int(off)-len(r.data)+len(p)]), nil
}

// ReadAt implements io.ReaderAt.
func (r *record) ReadAt(p []byte, off int64) (n int, err error) {
	if off > int64(r.totalLen) {
		return 0, io.EOF
	}

	if int(off) < len(r.data) {
		// We can partially or completely fill the read with our already cached data.
		n = copy(p, r.data[off:])

		// Check to see if we can return now.
		if int(off)+len(p) < len(r.data) {
			return
		}

		// Make sure we don't reread data.
		off += int64(n)
	}

	if err := r.readRest(); err != nil {
		return 0, err
	}

	// slog.Info("read rest", "off1", off, "len", len(r.data), "off2", int(off)-len(r.data))

	// The rest of the data comes from the data we pulled into memory in r.rest.
	n += copy(p[n:], r.rest[int(off)-len(r.data):])

	return
}

func (r *record) readU32(buf [5]byte, off int64) (uint32, error) {
	if _, err := r.ReadAt(buf[:4], off); err != nil {
		return 0, err
	}

	return endian.Uint32(buf[:4]), nil
}

// (type, totalLength, error)
func (r *record) readRecord(buf [5]byte, off int64) (recordType, uint32, error) {
	// slog.Info("readRecord", "off", off)
	// get the header.

	if _, err := r.ReadAt(buf[:5], off); err != nil {
		return _RECORD_INVALID, 0, err
	}

	// Get the fields and validate the record.
	length := endian.Uint32(buf[0:4])
	kind := recordType(buf[4])

	return kind, length, nil
}

func (r *record) kind() recordType {
	return recordType(r.data[4])
}

var (
	_ io.ReaderAt = &record{}
)

type recordDict struct {
	rec       *record
	off       int64
	len       int
	itemIndex []byte
}

// Get implements starlark.Mapping.
func (r *recordDict) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	key, ok := starlark.AsString(k)
	if !ok {
		return starlark.None, false, fmt.Errorf("could not convert %s to string", k.Type())
	}

	keyHash := hashKey(key)

	var buf [5]byte

	for i := 0; i < r.len; i++ {
		if keyHash != endian.Uint32(r.itemIndex[i*8:i*8+4]) {
			// The hash of the name doesn't match so we can safely skip it.
			continue
		}

		off := r.off + int64(HEADER_SIZE) + int64(endian.Uint32(r.itemIndex[i*8+4:i*8+8]))

		kind, _, err := r.rec.readRecord(buf, off)
		if err != nil {
			return starlark.None, false, err
		}

		if kind != _RECORD_ENTRY {
			return starlark.None, false, fmt.Errorf("record is not a entry: %d", kind)
		}

		ok, err := r.rec.compare([]byte(key), off+int64(HEADER_SIZE)+4)
		if err != nil {
			return starlark.None, false, err
		}

		if ok {
			// get string length
			strLen, err := r.rec.readU32(buf, off+int64(HEADER_SIZE))
			if err != nil {
				return starlark.None, false, err
			}

			// Convert the record to a starlark value.
			val, err := r.rec.toStarlarkValue(off + int64(HEADER_SIZE) + 4 + int64(strLen))
			if err != nil {
				return starlark.None, false, err
			}
			return val, true, nil
		}
	}

	return starlark.None, false, nil
}

func (*recordDict) String() string        { return "recordDict" }
func (*recordDict) Type() string          { return "recordDict" }
func (*recordDict) Hash() (uint32, error) { return 0, fmt.Errorf("recordDict is not hashable") }
func (*recordDict) Truth() starlark.Bool  { return starlark.True }
func (*recordDict) Freeze()               {}

var (
	_ starlark.Value   = &recordDict{}
	_ starlark.Mapping = &recordDict{}
)

type recordListIterator struct {
	rec *record
	off int64
	i   int
	len int
}

// Done implements starlark.Iterator.
func (r *recordListIterator) Done() {
	r.i = r.len
}

// Next implements starlark.Iterator.
func (r *recordListIterator) Next(p *starlark.Value) bool {
	if r.i == r.len {
		return false
	}

	var buf [5]byte

	_, len, err := r.rec.readRecord(buf, r.off)
	if err != nil {
		slog.Warn("error iterating", "err", err)
		return false
	}

	val, err := r.rec.toStarlarkValue(r.off)
	if err != nil {
		slog.Warn("error iterating", "err", err)
		return false
	}

	r.off += int64(len)
	r.i += 1

	*p = val

	return true
}

var (
	_ starlark.Iterator = &recordListIterator{}
)

type recordList struct {
	rec *record
	off int64
	len int
}

// Iterate implements starlark.Iterable.
func (r *recordList) Iterate() starlark.Iterator {
	return &recordListIterator{
		rec: r.rec,
		off: r.off + int64(HEADER_SIZE) + 4,
		len: r.Len(),
	}
}

// Index implements starlark.Indexable.
func (r *recordList) Index(i int) starlark.Value {
	panic("unimplemented")
}

// Len implements starlark.Indexable.
func (r *recordList) Len() int {
	return r.len
}

func (*recordList) String() string        { return "recordList" }
func (*recordList) Type() string          { return "recordList" }
func (*recordList) Hash() (uint32, error) { return 0, fmt.Errorf("recordList is not hashable") }
func (*recordList) Truth() starlark.Bool  { return starlark.True }
func (*recordList) Freeze()               {}

var (
	_ starlark.Value     = &recordList{}
	_ starlark.Iterable  = &recordList{}
	_ starlark.Indexable = &recordList{}
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
		return record.toStarlarkValue(0)
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
