package git

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Minimal packfile reader/writer supporting full objects and deltas.

type ObjType byte

const (
	ObjBad      ObjType = 0
	ObjCommit   ObjType = 1
	ObjTree     ObjType = 2
	ObjBlob     ObjType = 3
	ObjTag      ObjType = 4
	ObjOfsDelta ObjType = 6
	ObjRefDelta ObjType = 7
)

type RawObject struct {
	Type ObjType
	Data []byte
}

// BaseResolver resolves a base object by its 20-byte id (SHA-1).
// Returns (type, data, ok).
type BaseResolver func(id [20]byte) (ObjType, []byte, bool)

// ReadPack reads a pack stream and calls cb for each fully reconstructed object.
// It supports OFS_DELTA and REF_DELTA; the latter requires a resolver to find
// bases not present earlier in the same pack.
func ReadPack(r io.Reader, resolve BaseResolver, cb func(obj RawObject) error) error {
	br := bufio.NewReader(r)
	header := make([]byte, 4)
	if _, err := io.ReadFull(br, header); err != nil {
		return err
	}
	if !bytes.Equal(header, []byte("PACK")) {
		return fmt.Errorf("not a pack")
	}
	var ver uint32
	if err := binary.Read(br, binary.BigEndian, &ver); err != nil {
		return err
	}
	if ver != 2 && ver != 3 {
		return fmt.Errorf("unsupported pack v%d", ver)
	}
	var nobj uint32
	if err := binary.Read(br, binary.BigEndian, &nobj); err != nil {
		return err
	}

	// We need to keep reconstructed objects for OFS_DELTA references by offset.
	type entry struct {
		off  int64
		data []byte
		t    ObjType
		id   [20]byte
	}
	entries := make([]entry, 0, nobj)
	// Track offsets to find base by offset
	var consumed int64 = 12 // header bytes already consumed
	// Map for REF_DELTA lookup within this pack
	byID := make(map[[20]byte]entry)

	for i := uint32(0); i < nobj; i++ {
		objOff := consumed
		t, _, nbytes, err := readTypeSize(br)
		if err != nil {
			return err
		}
		consumed += int64(nbytes)

		var baseData []byte
		var baseType ObjType
		var refID [20]byte
		if t == ObjOfsDelta {
			// Read base offset (variable-length)
			baseOff, readBytes, err := readOfs(br)
			if err != nil {
				return err
			}
			consumed += int64(readBytes)
			// Find base entry by offset
			target := objOff - baseOff
			var found bool
			for _, e := range entries {
				if e.off == target {
					baseData = e.data
					baseType = e.t
					found = true
					break
				}
			}
			if !found {
				return errors.New("ofs-delta base not found")
			}
		} else if t == ObjRefDelta {
			if _, err := io.ReadFull(br, refID[:]); err != nil {
				return err
			}
			consumed += 20
			if e, ok := byID[refID]; ok {
				baseData, baseType = e.data, e.t
			} else if resolve != nil {
				if bt, bd, ok := resolve(refID); ok {
					baseData, baseType = bd, bt
				} else {
					return errors.New("ref-delta base not found")
				}
			} else {
				return errors.New("ref-delta base not found")
			}
		}

		// Read zlib-compressed data
		zr, err := zlib.NewReader(br)
		if err != nil {
			return err
		}
		var decomp bytes.Buffer
		if _, err := decomp.ReadFrom(zr); err != nil {
			zr.Close()
			return err
		}
		zr.Close()
		consumed += int64(decomp.Len()) // approximate; exact isn't needed for correctness here

		var out RawObject
		if t == ObjOfsDelta || t == ObjRefDelta {
			// Apply delta to baseData
			patched, err := applyDelta(baseData, decomp.Bytes())
			if err != nil {
				return err
			}
			// Delta encodes target type implicitly same as base; assume unchanged
			out = RawObject{Type: baseType, Data: patched}
		} else {
			out = RawObject{Type: t, Data: decomp.Bytes()}
		}
		// compute id for REF_DELTA mapping
		id := computeObjectID(out.Type, out.Data)
		ent := entry{off: objOff, data: out.Data, t: out.Type, id: id}
		entries = append(entries, ent)
		byID[id] = ent
		if err := cb(out); err != nil {
			return err
		}
	}

	// Drain checksum (20 bytes). We do not validate in minimal impl.
	_, _ = io.CopyN(io.Discard, br, 20)
	return nil
}

func computeObjectID(t ObjType, data []byte) [20]byte {
	hdr := []byte(headerFor(t, int64(len(data))))
	sum := sha1.Sum(append(hdr, data...))
	return sum
}

func headerFor(t ObjType, size int64) string {
	var kind string
	switch t {
	case ObjCommit:
		kind = "commit"
	case ObjTree:
		kind = "tree"
	case ObjBlob:
		kind = "blob"
	case ObjTag:
		kind = "tag"
	default:
		kind = "unknown"
	}
	return fmt.Sprintf("%s %d\x00", kind, size)
}

func readTypeSize(r *bufio.Reader) (ObjType, int64, int, error) {
	var size int64
	var t ObjType
	var shift uint = 4
	first, err := r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	n := 1
	t = ObjType((first >> 4) & 7)
	size = int64(first & 0x0f)
	if (first & 0x80) != 0 {
		for {
			b, err := r.ReadByte()
			if err != nil {
				return 0, 0, n, err
			}
			n++
			size |= int64(b&0x7f) << shift
			shift += 7
			if (b & 0x80) == 0 {
				break
			}
		}
	}
	return t, size, n, nil
}

func readOfs(r *bufio.Reader) (int64, int, error) {
	// Offset encoding: first byte has 7 bits, then each next adds 7 bits; MSB indicates more; value is cumulative with bit 7 contributes by 1
	var off int64
	var n int
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	n++
	off = int64(b & 0x7f)
	for (b & 0x80) != 0 {
		b, err = r.ReadByte()
		if err != nil {
			return 0, n, err
		}
		n++
		off = (off+1)<<7 | int64(b&0x7f)
	}
	return off, n, nil
}

func applyDelta(base, delta []byte) ([]byte, error) {
	rd := bytes.NewReader(delta)
	srcSize, err := readVarInt(rd)
	if err != nil {
		return nil, err
	}
	_ = srcSize // optional validation
	dstSize, err := readVarInt(rd)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, dstSize)
	for rd.Len() > 0 {
		op, _ := rd.ReadByte()
		if (op & 0x80) != 0 {
			// copy from base
			var off, sz int
			if (op & 0x01) != 0 {
				b, _ := rd.ReadByte()
				off |= int(b)
			}
			if (op & 0x02) != 0 {
				b, _ := rd.ReadByte()
				off |= int(b) << 8
			}
			if (op & 0x04) != 0 {
				b, _ := rd.ReadByte()
				off |= int(b) << 16
			}
			if (op & 0x08) != 0 {
				b, _ := rd.ReadByte()
				off |= int(b) << 24
			}
			if (op & 0x10) != 0 {
				b, _ := rd.ReadByte()
				sz |= int(b)
			}
			if (op & 0x20) != 0 {
				b, _ := rd.ReadByte()
				sz |= int(b) << 8
			}
			if (op & 0x40) != 0 {
				b, _ := rd.ReadByte()
				sz |= int(b) << 16
			}
			if sz == 0 {
				sz = 0x10000
			}
			out = append(out, base[off:off+sz]...)
		} else if op != 0 {
			// insert literal
			lit := make([]byte, int(op))
			if _, err := io.ReadFull(rd, lit); err != nil {
				return nil, err
			}
			out = append(out, lit...)
		} else {
			return nil, errors.New("invalid delta opcode 0")
		}
	}
	if int64(len(out)) != dstSize {
		return nil, errors.New("delta size mismatch")
	}
	return out, nil
}

func readVarInt(r io.ByteReader) (int64, error) {
	var v int64
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= int64(b&0x7f) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
	}
	return v, nil
}

// WritePack writes a pack containing the provided objects (as full objects only).
func WritePack(w io.Writer, objs []RawObject) error {
	var buf bytes.Buffer
	buf.WriteString("PACK")
	binary.Write(&buf, binary.BigEndian, uint32(2))
	binary.Write(&buf, binary.BigEndian, uint32(len(objs)))

	for _, o := range objs {
		// header: type+size varint
		var tbits byte
		switch o.Type {
		case ObjCommit:
			tbits = 1
		case ObjTree:
			tbits = 2
		case ObjBlob:
			tbits = 3
		case ObjTag:
			tbits = 4
		default:
			return fmt.Errorf("unsupported type %d", o.Type)
		}
		sz := uint64(len(o.Data))
		first := byte((tbits&7)<<4) | byte(sz&0x0f)
		sz >>= 4
		if sz != 0 {
			first |= 0x80
		}
		buf.WriteByte(first)
		for sz != 0 {
			b := byte(sz & 0x7f)
			sz >>= 7
			if sz != 0 {
				b |= 0x80
			}
			buf.WriteByte(b)
		}
		// data (zlib)
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(o.Data); err != nil {
			zw.Close()
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
	}
	sum := sha1.Sum(buf.Bytes())
	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(sum[:])
	return err
}
