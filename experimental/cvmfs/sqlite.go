package cvmfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
)

var endian = binary.BigEndian

type BinaryReader []byte

func (r BinaryReader) read(off, len int64) []byte { return r[off : off+len] }
func (r BinaryReader) u16(off int64) uint16       { return endian.Uint16(r.read(off, 2)) }
func (r BinaryReader) u32(off int64) uint32       { return endian.Uint32(r.read(off, 4)) }
func (r BinaryReader) u64(off int64) uint64       { return endian.Uint64(r.read(off, 8)) }
func (r BinaryReader) u8(off int64) uint8         { return uint8(r[off]) }

func (r BinaryReader) u24(off int64) uint32 {
	var b [4]byte

	copy(b[1:], r[off:off+3])

	return endian.Uint32(b[:])
}

func (r BinaryReader) u48(off int64) uint64 {
	var b [8]byte

	copy(b[2:], r[off:off+6])

	return endian.Uint64(b[:])
}

func (r BinaryReader) decodeVarint(off int64, slice []byte) (uint64, int64) {
	var ret uint64

	for i := 0; i < len(slice); i++ {
		val := slice[i]
		ret |= uint64(val&0b0111_1111) << (i * 7)
	}

	return ret, off + int64(len(slice))
}

func (r BinaryReader) varint(off int64) (uint64, int64) {
	var i int64 = 0

	for i = 0; i < 9; i++ {
		if off+i >= int64(len(r)) {
			return 0, -1 // overflow
		}

		if r[off+i]&0b1000_0000 == 0 {
			break
		}
	}

	i += 1

	data := make([]byte, i)
	copy(data, r[off:off+i])

	slices.Reverse(data)

	return r.decodeVarint(off, data)
}

type Table struct {
	db       *SQLiteDatabase
	Name     string
	rootPage int64
	Sql      string
}

func (t *Table) Read(cb func(val []any) error) error {
	rowIds := make(map[uint64]bool)
	return t.db.readPage(int(t.rootPage), func(rowId uint64, payload BinaryReader) error {
		// HACK: Right now we just ignore reading duplicate rows.
		if _, ok := rowIds[rowId]; ok {
			return nil
		}

		rowIds[rowId] = true

		// Read the cell header length.
		hdrLen, payloadOff := payload.varint(0)
		if payloadOff == -1 {
			return fmt.Errorf("[outer] overflow reading page")
		}
		if hdrLen > uint64(len(payload)) {
			return fmt.Errorf("hdrLen is longer than the payload %d > %d: %+v", hdrLen, len(payload), payload)
		}

		var (
			types []uint64
			typ   uint64
		)

		for {
			if payloadOff >= int64(hdrLen) {
				break
			}

			typ, payloadOff = payload.varint(payloadOff)
			if payloadOff == -1 {
				return fmt.Errorf("[inner] overflow reading page")
			}

			types = append(types, typ)
		}

		var values []any

		for _, typ := range types {
			if typ == 0 {
				values = append(values, nil)
			} else if typ == 1 {
				values = append(values, int64(payload.u8(payloadOff)))

				payloadOff += 1
			} else if typ == 2 {
				values = append(values, int64(payload.u16(payloadOff)))

				payloadOff += 2
			} else if typ == 3 {
				values = append(values, int64(payload.u24(payloadOff)))

				payloadOff += 3
			} else if typ == 4 {
				values = append(values, int64(payload.u32(payloadOff)))

				payloadOff += 4
			} else if typ == 5 {
				values = append(values, int64(payload.u48(payloadOff)))

				payloadOff += 6
			} else if typ == 6 {
				values = append(values, int64(payload.u64(payloadOff)))

				payloadOff += 8
			} else if typ == 8 {
				values = append(values, int64(0))
			} else if typ == 9 {
				values = append(values, int64(1))
			} else if typ >= 12 && typ%2 == 0 {
				length := (typ - 12) / 2

				values = append(values, payload.read(payloadOff, int64(length)))

				payloadOff += int64(length)
			} else if typ >= 13 && typ%2 == 1 {
				length := (typ - 13) / 2

				values = append(values, string(payload.read(payloadOff, int64(length))))

				payloadOff += int64(length)
			} else {
				return fmt.Errorf("unknown value type: %d", typ)
			}
		}

		if err := cb(values); err != nil {
			return err
		}

		return nil
	})
}

type SQLiteDatabase struct {
	r        io.ReaderAt
	pageSize uint16
	tables   map[string]*Table
}

func (db *SQLiteDatabase) reader(off int64, len int64) (BinaryReader, error) {
	data := make([]byte, len)

	if _, err := db.r.ReadAt(data, off); err != nil {
		return nil, err
	}

	return BinaryReader(data), nil
}

func (db *SQLiteDatabase) readPage(page int, cbCell func(rowId uint64, r BinaryReader) error) error {
	var rawPageOffset int64 = (int64(page) - 1) * int64(db.pageSize)

	pageReader, err := db.reader(rawPageOffset, int64(db.pageSize))
	if err != nil {
		return err
	}

	var pageOffset int64 = 0

	if page == 1 {
		pageOffset += 100
	}

	bTreePageType := pageReader.u8(pageOffset)
	// firstFreeBlock := db.u16(pageOffset + 1)
	numberOfCells := pageReader.u16(pageOffset + 3)
	// startOfCellContent := db.u16(pageOffset + 5)
	// fragmentedFreeBytes := db.u8(pageOffset + 7)

	pageOffset += 8

	var rightMostPointer uint32 = 0
	if bTreePageType == 0x05 {
		rightMostPointer = pageReader.u32(pageOffset)
		pageOffset += 4
	}

	_ = rightMostPointer

	cellPointers := make([]uint16, numberOfCells)
	for i := 0; i < len(cellPointers); i++ {
		cellPointers[i] = pageReader.u16(pageOffset + int64(i)*2)
	}

	switch bTreePageType {
	case 0x00: // unknown
		// ignored since I'm not sure where these come from but it doesn't seem to be a fatal error.
		return nil
	case 0x02: // index interior
		return nil
	case 0x05: // table interior cell
		for i, cellPointer := range cellPointers {
			var off = int64(cellPointer)

			leftMostPointer := pageReader.u32(off)
			off += 4
			key, _ := pageReader.varint(off)

			_ = key

			if i == len(cellPointers)-1 {
				for x := leftMostPointer; x <= rightMostPointer; x++ {
					if x == uint32(page) {
						return fmt.Errorf("attempt to re-read own page")
					}

					if err := db.readPage(int(x), cbCell); err != nil {
						return err
					}
				}
			} else {
				if leftMostPointer == uint32(page) {
					return fmt.Errorf("attempt to re-read own page")
				}

				if err := db.readPage(int(leftMostPointer), cbCell); err != nil {
					return err
				}
			}
		}

		return nil
	case 0x0a: // index leaf
		return nil
	case 0x0d: // table exterior cell
		for _, cellPointer := range cellPointers {
			var off = int64(cellPointer)

			cellSize, off := pageReader.varint(off)
			rowId, off := pageReader.varint(off)

			payload := BinaryReader(pageReader.read(off, int64(cellSize)))

			if err := cbCell(rowId, payload); err != nil {
				return err
			}
		}

		return nil
	default:
		return fmt.Errorf("unknown bTreePageType: %x", bTreePageType)
	}
}

func (db *SQLiteDatabase) Tables() []string {
	var ret []string

	for k := range db.tables {
		ret = append(ret, k)
	}

	return ret
}

type ErrTableNotFound string

func (e ErrTableNotFound) Error() string {
	return fmt.Sprintf("table not found: %s", string(e))
}

func (db *SQLiteDatabase) Table(name string) (*Table, error) {
	tbl, ok := db.tables[name]
	if !ok {
		return nil, ErrTableNotFound(name)
	}

	return tbl, nil
}

func OpenDatabase(r io.ReaderAt) (*SQLiteDatabase, error) {
	db := &SQLiteDatabase{r: r, tables: make(map[string]*Table)}

	hdr, err := db.reader(0, 100)
	if err != nil {
		return nil, err
	}

	if string(hdr.read(0, 16)) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("bad magic")
	}

	db.pageSize = hdr.u16(16)

	schemaTable := &Table{db: db, rootPage: 1}

	if err := schemaTable.Read(func(val []any) error {
		if val[0].(string) != "table" {
			return nil
		}

		db.tables[val[1].(string)] = &Table{
			db:       db,
			rootPage: val[3].(int64),
			Sql:      val[4].(string),
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return db, nil
}

func ParseDatabase(data []byte) (*SQLiteDatabase, error) {
	return OpenDatabase(bytes.NewReader(data))
}
