package ccvm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// FDT Code.
const FDT_MAGIC = 0xd00dfeed
const FDT_VERSION = 17

type FdtHeader struct {
	Magic           uint32
	TotalSize       uint32
	OffDtStruct     uint32
	OffDtStrings    uint32
	OffMemRsvmap    uint32
	Version         uint32
	LastCompVersion uint32
	BootCpuidPhys   uint32
	SizeDtStrings   uint32
	SizeDtStruct    uint32
}

type FdtReserveEntry struct {
	Address uint64
	Size    uint64
}

const (
	FDT_BEGIN_NODE = 1
	FDT_END_NODE   = 2
	FDT_PROP       = 3
	FDT_NOP        = 4
	FDT_END        = 9
)

type Fdt struct {
	table         []byte
	stringTable   []string
	openNodeCount int
}

func newFdt() *Fdt {
	return &Fdt{}
}

func (fdt *Fdt) put32(v int) {
	fdt.table = append(fdt.table, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func (fdt *Fdt) putData(data []byte) {
	fdt.table = append(fdt.table, data...)
	for i := 0; i < -len(data)&3; i++ {
		fdt.table = append(fdt.table, 0)
	}
}

func (fdt *Fdt) beginNode(name string) {
	fdt.put32(FDT_BEGIN_NODE)
	fdt.putData(append([]byte(name), 0x00))
	fdt.openNodeCount++
}

func (fdt *Fdt) beginNodeNum(name string, n uint64) {
	buf := fmt.Sprintf("%s@%x", name, n)
	fdt.beginNode(buf)
}

func (fdt *Fdt) endNode() {
	fdt.put32(FDT_END_NODE)
	fdt.openNodeCount--
}

func (fdt *Fdt) getStringOffset(name string) int {
	pos := 0

	for _, str := range fdt.stringTable {
		if str == name {
			return pos
		}
		pos += len(str) + 1
	}

	fdt.stringTable = append(fdt.stringTable, name)

	return pos
}

func (fdt *Fdt) prop(propName string, data []byte) {
	fdt.put32(FDT_PROP)
	fdt.put32(len(data))
	fdt.put32(fdt.getStringOffset(propName))
	fdt.putData(data)
}

func (fdt *Fdt) propTabU32(propName string, tab []uint32) {
	fdt.put32(FDT_PROP)
	fdt.put32(len(tab) * 4)
	fdt.put32(fdt.getStringOffset(propName))
	for _, v := range tab {
		fdt.put32(int(v))
	}
}

func (fdt *Fdt) propU32(propName string, val uint32) {
	fdt.propTabU32(propName, []uint32{val})
}

func (fdt *Fdt) propTabU64(propName string, v0 uint64) {
	fdt.propTabU32(propName, []uint32{uint32(v0 >> 32), uint32(v0)})
}

func (fdt *Fdt) propTabU64_2(propName string, v0, v1 uint64) {
	fdt.propTabU32(propName, []uint32{uint32(v0 >> 32), uint32(v0), uint32(v1 >> 32), uint32(v1)})
}

func (fdt *Fdt) propStr(propName, str string) {
	fdt.prop(propName, append([]byte(str), 0x00))
}

func (fdt *Fdt) propTabStr(propName string, strs ...string) {
	size := 0
	for _, str := range strs {
		size += len(str) + 1
	}

	tab := make([]byte, size)
	size = 0
	for _, str := range strs {
		copy(tab[size:], str)
		size += len(str) + 1
	}

	fdt.prop(propName, tab)
}

func (fdt *Fdt) output() ([]byte, error) {
	buf := new(bytes.Buffer)

	if fdt.openNodeCount != 0 {
		return nil, fmt.Errorf("open node count is not 0")
	}

	fdt.put32(FDT_END)

	dtStructSize := len(fdt.table)
	stringBuf := append([]byte(strings.Join(fdt.stringTable, "\x00")), 0x00)

	pos := 0

	h := FdtHeader{
		Magic:           FDT_MAGIC,
		Version:         FDT_VERSION,
		LastCompVersion: 16,
		BootCpuidPhys:   0,
		SizeDtStrings:   uint32(len(stringBuf)),
		SizeDtStruct:    uint32(dtStructSize),
	}
	if err := binary.Write(buf, binary.BigEndian, h); err != nil {
		return nil, err
	}
	pos += int(binary.Size(h))

	h.OffDtStruct = uint32(pos)
	if _, err := buf.Write(fdt.table); err != nil {
		return nil, err
	}

	for buf.Len()%8 != 0 {
		if err := buf.WriteByte(0); err != nil {
			return nil, err
		}
	}
	pos += buf.Len() - pos

	h.OffMemRsvmap = uint32(pos)
	re := FdtReserveEntry{
		Address: 0,
		Size:    0,
	}

	if err := binary.Write(buf, binary.BigEndian, re); err != nil {
		return nil, err
	}
	pos += binary.Size(re)

	h.OffDtStrings = uint32(pos)
	if _, err := buf.Write([]byte(strings.Join(fdt.stringTable, "\x00"))); err != nil {
		return nil, err
	}

	for buf.Len()%8 != 0 {
		if err := buf.WriteByte(0); err != nil {
			return nil, err
		}
	}

	h.TotalSize = uint32(buf.Len())

	out := buf.Bytes()

	hdrBuf := new(bytes.Buffer)
	if err := binary.Write(hdrBuf, binary.BigEndian, h); err != nil {
		return nil, err
	}

	copy(out[:len(hdrBuf.Bytes())], hdrBuf.Bytes())

	return out, nil
}
