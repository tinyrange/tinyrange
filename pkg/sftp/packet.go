package sftp

import (
	"bytes"
	gbinary "encoding/binary"
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/pkg/common/binary"
)

type ResponsePacket interface {
	binary.Encodable

	Type() packetType
}

type BasePacket struct {
	RequestId uint32
}

type pktInit struct {
	Version uint32
}

// Decode implements Decodable.
func (pkt *pktInit) Decode(r binary.BinaryReader) error {
	pkt.Version = r.Uint32()
	return r.Error()
}

type pktVersion struct {
	Version uint32
}

// Encode implements ResponsePacket.
func (pkt *pktVersion) Encode(r binary.BinaryWriter) error {
	r.Uint32(pkt.Version)
	return r.Error()
}

// Type implements ResponsePacket.
func (*pktVersion) Type() packetType { return packetTypeVersion }

type FileXferAttr uint32

var (
	fileXferAttrSize        FileXferAttr = 0x00000001
	fileXferAttrUidgid      FileXferAttr = 0x00000002
	fileXferAttrPermissions FileXferAttr = 0x00000004
	fileXferAttrAcmodtime   FileXferAttr = 0x00000008
	fileXferAttrExtended    FileXferAttr = 0x80000000
)

type attrs struct {
	Flags       FileXferAttr
	Size        uint64
	Uid         uint32
	Gid         uint32
	Permissions uint32
	Atime       uint32
	Mtime       uint32
}

func (a *attrs) Decode(r binary.BinaryReader) {
	a.Flags = FileXferAttr(r.Uint32())
	if a.Flags&fileXferAttrSize != 0 {
		a.Size = r.Uint64()
	}
	if a.Flags&fileXferAttrUidgid != 0 {
		a.Uid = r.Uint32()
		a.Gid = r.Uint32()
	}
	if a.Flags&fileXferAttrPermissions != 0 {
		a.Permissions = r.Uint32()
	}
	if a.Flags&fileXferAttrAcmodtime != 0 {
		a.Atime = r.Uint32()
		a.Mtime = r.Uint32()
	}
	// if a.Flags&fileAttrExtended != 0 {
	// 	count := r.Uint32()
	// }
}

func (a *attrs) Encode(r binary.BinaryWriter) {
	r.Uint32(uint32(a.Flags))
	if a.Flags&fileXferAttrSize != 0 {
		r.Uint64(a.Size)
	}
	if a.Flags&fileXferAttrUidgid != 0 {
		r.Uint32(a.Uid)
		r.Uint32(a.Gid)
	}
	if a.Flags&fileXferAttrPermissions != 0 {
		r.Uint32(a.Permissions)
	}
	if a.Flags&fileXferAttrAcmodtime != 0 {
		r.Uint32(a.Atime)
		r.Uint32(a.Mtime)
	}
}

type pktAttrs struct {
	Attrs attrs
}

// Encode implements ResponsePacket.
func (pkt *pktAttrs) Encode(r binary.BinaryWriter) error {
	pkt.Attrs.Encode(r)
	return r.Error()
}

// Type implements ResponsePacket.
func (*pktAttrs) Type() packetType { return packetTypeAttrs }

type OpenFlags uint32

const (
	openFlagRead   OpenFlags = 0x00000001
	openFlagWrite  OpenFlags = 0x00000002
	openFlagAppend OpenFlags = 0x00000004
	openFlagCreat  OpenFlags = 0x00000008
	openFlagTrunc  OpenFlags = 0x00000010
	openFlagExcl   OpenFlags = 0x00000020
)

type pktOpen struct {
	Path  string
	Flags OpenFlags
	Attrs attrs
}

func (p *pktOpen) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	p.Flags = OpenFlags(r.Uint32())
	p.Attrs.Decode(r)
	return r.Error()
}

type pktClose struct {
	Handle string
}

func (p *pktClose) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Handle = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktRead struct {
	Handle string
	Offset uint64
	Len    uint32
}

func (p *pktRead) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Handle = string(r.Bytes(int(strLen)))
	p.Offset = r.Uint64()
	p.Len = r.Uint32()
	return r.Error()
}

type pktWrite struct {
	Handle string
	Offset uint64
	Data   string
}

func (p *pktWrite) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Handle = string(r.Bytes(int(strLen)))
	p.Offset = r.Uint64()
	dataLen := r.Uint32()
	p.Data = string(r.Bytes(int(dataLen)))
	return r.Error()
}

type pktRemove struct {
	Filename string
}

func (p *pktRemove) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Filename = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktRename struct {
	OldPath string
	NewPath string
}

func (p *pktRename) Decode(r binary.BinaryReader) error {
	oldLen := r.Uint32()
	p.OldPath = string(r.Bytes(int(oldLen)))
	newLen := r.Uint32()
	p.NewPath = string(r.Bytes(int(newLen)))
	return r.Error()
}

type pktMkdir struct {
	Path  string
	Attrs attrs
}

// Decode implements Decodable.
func (p *pktMkdir) Decode(r binary.BinaryReader) error {
	pathLen := r.Uint32()
	p.Path = string(r.Bytes(int(pathLen)))
	p.Attrs.Decode(r)
	return r.Error()
}

type pktRmdir struct {
	Path string
}

// Decode implements Decodable.
func (p *pktRmdir) Decode(r binary.BinaryReader) error {
	pathLen := r.Uint32()
	p.Path = string(r.Bytes(int(pathLen)))
	return r.Error()
}

type pktOpenDir struct {
	Path string
}

func (p *pktOpenDir) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktReadDir struct {
	Handle string
}

func (p *pktReadDir) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Handle = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktStat struct {
	Path string
}

func (p *pktStat) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktLstat struct {
	Path string
}

func (p *pktLstat) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktFstat struct {
	Handle string
}

func (p *pktFstat) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Handle = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktSetStat struct {
	Path  string
	Attrs attrs
}

// Decode implements Decodable.
func (p *pktSetStat) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	p.Attrs.Decode(r)
	return r.Error()
}

type pktFSetStat struct {
	Handle string
	Attrs  attrs
}

// Decode implements Decodable.
func (p *pktFSetStat) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Handle = string(r.Bytes(int(strLen)))
	p.Attrs.Decode(r)
	return r.Error()
}

type pktReadlink struct {
	Path string
}

// Decode implements Decodable.
func (p *pktReadlink) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktSymlink struct {
	LinkPath   string
	TargetPath string
}

// Decode implements Decodable.
func (p *pktSymlink) Decode(r binary.BinaryReader) error {
	linkLen := r.Uint32()
	p.LinkPath = string(r.Bytes(int(linkLen)))
	targetLen := r.Uint32()
	p.TargetPath = string(r.Bytes(int(targetLen)))
	return r.Error()
}

type pktRealPath struct {
	Path string
}

// Decode implements Decodable.
func (p *pktRealPath) Decode(r binary.BinaryReader) error {
	strLen := r.Uint32()
	p.Path = string(r.Bytes(int(strLen)))
	return r.Error()
}

type pktHandle struct {
	Handle string
}

// Type implements ResponsePacket.
func (*pktHandle) Type() packetType { return packetTypeHandle }

// Encode implements Encodable.
func (p *pktHandle) Encode(r binary.BinaryWriter) error {
	r.Uint32(uint32(len(p.Handle)))
	r.Bytes([]byte(p.Handle))
	return r.Error()
}

type name struct {
	Filename string
	Longname string
	Attrs    attrs
}

type pktNames struct {
	Names []name
}

// Type implements ResponsePacket.
func (*pktNames) Type() packetType { return packetTypeName }

// Encode implements Encodable.
func (p *pktNames) Encode(r binary.BinaryWriter) error {
	r.Uint32(uint32(len(p.Names)))
	for _, name := range p.Names {
		r.Uint32(uint32(len(name.Filename)))
		r.Bytes([]byte(name.Filename))
		r.Uint32(uint32(len(name.Longname)))
		r.Bytes([]byte(name.Longname))
		r.Struct(&name.Attrs)
	}
	return r.Error()
}

type pktData struct {
	Data []byte
}

// Type implements ResponsePacket.
func (*pktData) Type() packetType { return packetTypeData }

func (p *pktData) Encode(r binary.BinaryWriter) error {
	r.Uint32(uint32(len(p.Data)))
	r.Bytes([]byte(p.Data))
	return r.Error()
}

type errorCode uint32

var (
	errOk               errorCode = 0
	errEOF              errorCode = 1
	errNoSuchFile       errorCode = 2
	errPermissionDenied errorCode = 3
	errFailure          errorCode = 4
	errBadMessage       errorCode = 5
	errNoConnection     errorCode = 6
	errConnectionLost   errorCode = 7
	errOpUnsupported    errorCode = 8
)

type pktStatus struct {
	Code     errorCode
	Message  string
	Language string
}

// Type implements ResponsePacket.
func (*pktStatus) Type() packetType { return packetTypeStatus }

// Encode implements Encodable.
func (p *pktStatus) Encode(r binary.BinaryWriter) error {
	r.Uint32(uint32(p.Code))
	r.Uint32(uint32(len(p.Message)))
	r.Bytes([]byte(p.Message))
	r.Uint32(uint32(len(p.Language)))
	r.Bytes([]byte(p.Language))
	return r.Error()
}

var (
	_ ResponsePacket = &pktVersion{}
	_ ResponsePacket = &pktAttrs{}
	_ ResponsePacket = &pktHandle{}
	_ ResponsePacket = &pktNames{}
	_ ResponsePacket = &pktStatus{}
	_ ResponsePacket = &pktData{}
)

type packetType byte

const (
	packetTypeInit          packetType = 1
	packetTypeVersion       packetType = 2
	packetTypeOpen          packetType = 3
	packetTypeClose         packetType = 4
	packetTypeRead          packetType = 5
	packetTypeWrite         packetType = 6
	packetTypeLstat         packetType = 7
	packetTypeFstat         packetType = 8
	packetTypeSetstat       packetType = 9
	packetTypeFsetstat      packetType = 10
	packetTypeOpendir       packetType = 11
	packetTypeReaddir       packetType = 12
	packetTypeRemove        packetType = 13
	packetTypeMkdir         packetType = 14
	packetTypeRmdir         packetType = 15
	packetTypeRealpath      packetType = 16
	packetTypeStat          packetType = 17
	packetTypeRename        packetType = 18
	packetTypeReadlink      packetType = 19
	packetTypeSymlink       packetType = 20
	packetTypeStatus        packetType = 101
	packetTypeHandle        packetType = 102
	packetTypeData          packetType = 103
	packetTypeName          packetType = 104
	packetTypeAttrs         packetType = 105
	packetTypeExtended      packetType = 200
	packetTypeExtendedReply packetType = 201
)

type rawPacket struct {
	kind packetType
	data []byte
}

func (p *rawPacket) decode() (any, uint32, error) {
	r := bytes.NewReader(p.data)

	var pkt binary.Decodable
	switch p.kind {
	// 4
	case packetTypeInit:
		pkt = &pktInit{}

	// 6.3
	case packetTypeOpen:
		pkt = &pktOpen{}
	case packetTypeClose:
		pkt = &pktClose{}

	// 6.4
	case packetTypeRead:
		pkt = &pktRead{}
	case packetTypeWrite:
		pkt = &pktWrite{}

	// 6.5
	case packetTypeRemove:
		pkt = &pktRemove{}
	case packetTypeRename:
		pkt = &pktRename{}

	// 6.6
	case packetTypeMkdir:
		pkt = &pktMkdir{}
	case packetTypeRmdir:
		pkt = &pktRmdir{}

	// 6.7
	case packetTypeOpendir:
		pkt = &pktOpenDir{}
	case packetTypeReaddir:
		pkt = &pktReadDir{}

	// 6.8
	case packetTypeStat:
		pkt = &pktStat{}
	case packetTypeLstat:
		pkt = &pktLstat{}
	case packetTypeFstat:
		pkt = &pktFstat{}

	// 6.9
	case packetTypeSetstat:
		pkt = &pktSetStat{}
	case packetTypeFsetstat:
		pkt = &pktFSetStat{}

	// 6.10
	case packetTypeReadlink:
		pkt = &pktReadlink{}
	case packetTypeSymlink:
		pkt = &pktSymlink{}

	// 6.11
	case packetTypeRealpath:
		pkt = &pktRealPath{}

	default:
		return nil, 0, fmt.Errorf("unknown packet: %d", p.kind)
	}

	reader := binary.NewReader(r, gbinary.BigEndian)

	var id uint32 = 0

	if p.kind != packetTypeInit {
		id = reader.Uint32()

		if err := reader.Error(); err != nil {
			return nil, 0, err
		}
	}

	err := pkt.Decode(reader)
	if err != nil {
		return nil, 0, err
	}

	return pkt, id, nil
}

func writePacket(w io.Writer, id uint32, pkt ResponsePacket) error {
	packetData := new(bytes.Buffer)

	pktType := pkt.Type()

	writer := binary.NewWriter(packetData, gbinary.BigEndian)

	if pktType != packetTypeVersion {
		writer.Uint32(id)

		if err := writer.Error(); err != nil {
			return err
		}
	}

	err := pkt.Encode(writer)
	if err != nil {
		return err
	}

	packetHeader := struct {
		Length uint32
		Type   byte
	}{
		Length: uint32(packetData.Len() + 1),
		Type:   byte(pktType),
	}
	err = gbinary.Write(w, gbinary.BigEndian, &packetHeader)
	if err != nil {
		return err
	}

	_, err = w.Write(packetData.Bytes())
	if err != nil {
		return err
	}

	return nil
}

func readRawPacket(r io.Reader) (*rawPacket, error) {
	var len uint32
	err := gbinary.Read(r, gbinary.BigEndian, &len)
	if err != nil {
		return nil, err
	}

	data := make([]byte, len)

	_, err = io.ReadFull(r, data)
	if err != nil {
		return nil, err
	}

	return &rawPacket{kind: packetType(data[0]), data: data[1:]}, nil
}
