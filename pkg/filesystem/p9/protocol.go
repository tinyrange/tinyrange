package p9

import (
	"bytes"

	"github.com/tinyrange/tinyrange/pkg/common/binary"
)

// From: //gvisor/pkg/p9/p9.go

const (
	ENOENT = 2
	ENOSYS = 38
)

// MsgType is a type identifier.
type MsgType uint8

// MsgType declarations.
const (
	MsgTlerror      MsgType = 6
	MsgRlerror      MsgType = 7
	MsgTstatfs      MsgType = 8
	MsgRstatfs      MsgType = 9
	MsgTlopen       MsgType = 12
	MsgRlopen       MsgType = 13
	MsgTlcreate     MsgType = 14
	MsgRlcreate     MsgType = 15
	MsgTsymlink     MsgType = 16
	MsgRsymlink     MsgType = 17
	MsgTmknod       MsgType = 18
	MsgRmknod       MsgType = 19
	MsgTrename      MsgType = 20
	MsgRrename      MsgType = 21
	MsgTreadlink    MsgType = 22
	MsgRreadlink    MsgType = 23
	MsgTgetattr     MsgType = 24
	MsgRgetattr     MsgType = 25
	MsgTsetattr     MsgType = 26
	MsgRsetattr     MsgType = 27
	MsgTxattrwalk   MsgType = 30
	MsgRxattrwalk   MsgType = 31
	MsgTxattrcreate MsgType = 32
	MsgRxattrcreate MsgType = 33
	MsgTreaddir     MsgType = 40
	MsgRreaddir     MsgType = 41
	MsgTfsync       MsgType = 50
	MsgRfsync       MsgType = 51
	MsgTlink        MsgType = 70
	MsgRlink        MsgType = 71
	MsgTmkdir       MsgType = 72
	MsgRmkdir       MsgType = 73
	MsgTrenameat    MsgType = 74
	MsgRrenameat    MsgType = 75
	MsgTunlinkat    MsgType = 76
	MsgRunlinkat    MsgType = 77
	MsgTversion     MsgType = 100
	MsgRversion     MsgType = 101
	MsgTauth        MsgType = 102
	MsgRauth        MsgType = 103
	MsgTattach      MsgType = 104
	MsgRattach      MsgType = 105
	MsgTflush       MsgType = 108
	MsgRflush       MsgType = 109
	MsgTwalk        MsgType = 110
	MsgRwalk        MsgType = 111
	MsgTread        MsgType = 116
	MsgRread        MsgType = 117
	MsgTwrite       MsgType = 118
	MsgRwrite       MsgType = 119
	MsgTclunk       MsgType = 120
	MsgRclunk       MsgType = 121
	MsgTremove      MsgType = 122
	MsgRremove      MsgType = 123
	MsgTflushf      MsgType = 124
	MsgRflushf      MsgType = 125
	MsgTwalkgetattr MsgType = 126
	MsgRwalkgetattr MsgType = 127
	MsgTucreate     MsgType = 128
	MsgRucreate     MsgType = 129
	MsgTumkdir      MsgType = 130
	MsgRumkdir      MsgType = 131
	MsgTumknod      MsgType = 132
	MsgRumknod      MsgType = 133
	MsgTusymlink    MsgType = 134
	MsgRusymlink    MsgType = 135
	MsgTlconnect    MsgType = 136
	MsgRlconnect    MsgType = 137
	MsgTallocate    MsgType = 138
	MsgRallocate    MsgType = 139
	MsgTchannel     MsgType = 250
	MsgRchannel     MsgType = 251
)

func (m MsgType) String() string {
	switch m {
	case MsgTlerror:
		return "Tlerror"
	case MsgRlerror:
		return "Rlerror"
	case MsgTstatfs:
		return "Tstatfs"
	case MsgRstatfs:
		return "Rstatfs"
	case MsgTlopen:
		return "Tlopen"
	case MsgRlopen:
		return "Rlopen"
	case MsgTlcreate:
		return "Tlcreate"
	case MsgRlcreate:
		return "Rlcreate"
	case MsgTsymlink:
		return "Tsymlink"
	case MsgRsymlink:
		return "Rsymlink"
	case MsgTmknod:
		return "Tmknod"
	case MsgRmknod:
		return "Rmknod"
	case MsgTrename:
		return "Trename"
	case MsgRrename:
		return "Rrename"
	case MsgTreadlink:
		return "Treadlink"
	case MsgRreadlink:
		return "Rreadlink"
	case MsgTgetattr:
		return "Tgetattr"
	case MsgRgetattr:
		return "Rgetattr"
	case MsgTsetattr:
		return "Tsetattr"
	case MsgRsetattr:
		return "Rsetattr"
	case MsgTxattrwalk:
		return "Txattrwalk"
	case MsgRxattrwalk:
		return "Rxattrwalk"
	case MsgTxattrcreate:
		return "Txattrcreate"
	case MsgRxattrcreate:
		return "Rxattrcreate"
	case MsgTreaddir:
		return "Treaddir"
	case MsgRreaddir:
		return "Rreaddir"
	case MsgTfsync:
		return "Tfsync"
	case MsgRfsync:
		return "Rfsync"
	case MsgTlink:
		return "Tlink"
	case MsgRlink:
		return "Rlink"
	case MsgTmkdir:
		return "Tmkdir"
	case MsgRmkdir:
		return "Rmkdir"
	case MsgTrenameat:
		return "Trenameat"
	case MsgRrenameat:
		return "Rrenameat"
	case MsgTunlinkat:
		return "Tunlinkat"
	case MsgRunlinkat:
		return "Runlinkat"
	case MsgTversion:
		return "Tversion"
	case MsgRversion:
		return "Rversion"
	case MsgTauth:
		return "Tauth"
	case MsgRauth:
		return "Rauth"
	case MsgTattach:
		return "Tattach"
	case MsgRattach:
		return "Rattach"
	case MsgTflush:
		return "Tflush"
	case MsgRflush:
		return "Rflush"
	case MsgTwalk:
		return "Twalk"
	case MsgRwalk:
		return "Rwalk"
	case MsgTread:
		return "Tread"
	case MsgRread:
		return "Rread"
	case MsgTwrite:
		return "Twrite"
	case MsgRwrite:
		return "Rwrite"
	case MsgTclunk:
		return "Tclunk"
	case MsgRclunk:
		return "Rclunk"
	case MsgTremove:
		return "Tremove"
	case MsgRremove:
		return "Rremove"
	case MsgTflushf:
		return "Tflushf"
	case MsgRflushf:
		return "Rflushf"
	case MsgTwalkgetattr:
		return "Twalkgetattr"
	case MsgRwalkgetattr:
		return "Rwalkgetattr"
	case MsgTucreate:
		return "Tucreate"
	case MsgRucreate:
		return "Rucreate"
	case MsgTumkdir:
		return "Tumkdir"
	case MsgRumkdir:
		return "Rumkdir"
	case MsgTumknod:
		return "Tumknod"
	case MsgRumknod:
		return "Rumknod"
	case MsgTusymlink:
		return "Tusymlink"
	case MsgRusymlink:
		return "Rusymlink"
	case MsgTlconnect:
		return "Tlconnect"
	case MsgRlconnect:
		return "Rlconnect"
	case MsgTallocate:
		return "Tallocate"
	case MsgRallocate:
		return "Rallocate"
	case MsgTchannel:
		return "Tchannel"
	case MsgRchannel:
		return "Rchannel"
	default:
		return "<unknown>"
	}
}

// QIDType represents the file type for QIDs.
//
// QIDType corresponds to the high 8 bits of a Plan 9 file mode.
type QIDType uint8

const (
	// TypeDir represents a directory type.
	TypeDir QIDType = 0x80

	// TypeAppendOnly represents an append only file.
	TypeAppendOnly QIDType = 0x40

	// TypeExclusive represents an exclusive-use file.
	TypeExclusive QIDType = 0x20

	// TypeMount represents a mounted channel.
	TypeMount QIDType = 0x10

	// TypeAuth represents an authentication file.
	TypeAuth QIDType = 0x08

	// TypeTemporary represents a temporary file.
	TypeTemporary QIDType = 0x04

	// TypeSymlink represents a symlink.
	TypeSymlink QIDType = 0x02

	// TypeLink represents a hard link.
	TypeLink QIDType = 0x01

	// TypeRegular represents a regular file.
	TypeRegular QIDType = 0x00
)

// QID is a unique file identifier.
//
// This may be embedded in other requests and responses.
type QID struct {
	// Type is the highest order byte of the file mode.
	Type QIDType

	// Version is an arbitrary server version number.
	Version uint32

	// Path is a unique server identifier for this path (e.g. inode).
	Path uint64
}

// Encode implements binary.Encodable.
func (m *QID) Encode(r binary.BinaryWriter) error {
	r.Uint8(uint8(m.Type))
	r.Uint32(uint32(m.Version))
	r.Uint64(uint64(m.Path))

	return r.Error()
}

type Message struct {
	data []byte
	Type MsgType
	Tag  uint16
}

// Decode implements binary.Decodable.
func (m *Message) Decode(r binary.BinaryReader) error {
	len := r.Uint32()
	m.Type = MsgType(r.Uint8())
	m.Tag = r.Uint16()
	m.data = r.Bytes(int(len - 2 - 4 - 1))

	return r.Error()
}

// Encode implements binary.Encodable.
func (m *Message) Encode(r binary.BinaryWriter) error {
	r.Uint32(uint32(len(m.data) + 1 + 4 + 2))
	r.Uint8(uint8(m.Type))
	r.Uint16(m.Tag)
	r.Bytes(m.data)

	return r.Error()
}

func (m *Message) DecodeBody(dec binary.Decodable) error {
	reader := binary.NewReader(bytes.NewReader(m.data), binary.LittleEndian)

	err := dec.Decode(reader)
	if err != nil {
		return err
	}

	return nil
}

func (m *Message) EncodeBody(typ MsgType, tag uint16, enc binary.Encodable) (*Message, error) {
	buf := new(bytes.Buffer)

	writer := binary.NewWriter(buf, binary.LittleEndian)

	err := enc.Encode(writer)
	if err != nil {
		return nil, err
	}

	m.Type = typ
	m.Tag = tag
	m.data = buf.Bytes()

	return m, nil
}

var (
	_ binary.Encodable = &Message{}
	_ binary.Decodable = &Message{}
)

func readString(r binary.BinaryReader) string {
	len := r.Uint16()
	str := r.Bytes(int(len))

	return string(str)
}

func writeString(r binary.BinaryWriter, s string) {
	r.Uint16(uint16(len(s)))
	r.Bytes([]byte(s))
}

const (
	P9_GETATTR_MODE   uint64 = 0x00000001
	P9_GETATTR_NLINK  uint64 = 0x00000002
	P9_GETATTR_UID    uint64 = 0x00000004
	P9_GETATTR_GID    uint64 = 0x00000008
	P9_GETATTR_RDEV   uint64 = 0x00000010
	P9_GETATTR_ATIME  uint64 = 0x00000020
	P9_GETATTR_MTIME  uint64 = 0x00000040
	P9_GETATTR_CTIME  uint64 = 0x00000080
	P9_GETATTR_INO    uint64 = 0x00000100
	P9_GETATTR_SIZE   uint64 = 0x00000200
	P9_GETATTR_BLOCKS uint64 = 0x00000400

	P9_GETATTR_BTIME        uint64 = 0x00000800
	P9_GETATTR_GEN          uint64 = 0x00001000
	P9_GETATTR_DATA_VERSION uint64 = 0x00002000

	P9_GETATTR_BASIC uint64 = 0x000007ff /* Mask for fields up to BLOCKS */
	P9_GETATTR_ALL   uint64 = 0x00003fff /* Mask for All fields above */
)

const (
	P9_SETATTR_MODE      = 0x00000001
	P9_SETATTR_UID       = 0x00000002
	P9_SETATTR_GID       = 0x00000004
	P9_SETATTR_SIZE      = 0x00000008
	P9_SETATTR_ATIME     = 0x00000010
	P9_SETATTR_MTIME     = 0x00000020
	P9_SETATTR_CTIME     = 0x00000040
	P9_SETATTR_ATIME_SET = 0x00000080
	P9_SETATTR_MTIME_SET = 0x00000100
)

var (
	_ binary.Encodable = &QID{}
)
