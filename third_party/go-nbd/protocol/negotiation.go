package protocol

// See https://github.com/NetworkBlockDevice/nbd/blob/master/doc/proto.md and https://github.com/abligh/gonbdserver/

const (
	NEGOTIATION_MAGIC_OLDSTYLE = uint64(0x4e42444d41474943)
	NEGOTIATION_MAGIC_OPTION   = uint64(0x49484156454F5054)
	NEGOTIATION_MAGIC_REPLY    = uint64(0x3e889045565a9)

	NEGOTIATION_HANDSHAKE_FLAG_FIXED_NEWSTYLE = uint16(1 << 0)

	NBD_OPT_EXPORT_NAME      = uint32(1)
	NBD_OPT_ABORT            = uint32(2)
	NBD_OPT_LIST             = uint32(3)
	NBD_OPT_STARTTLS         = uint32(5)
	NBD_OPT_INFO             = uint32(6)
	NBD_OPT_GO               = uint32(7)
	NBD_OPT_STRUCTURED_REPLY = uint32(8)

	NBD_FLAG_HAS_FLAGS            = uint16(1 << 0)
	NBD_FLAG_READ_ONLY            = uint16(1 << 1)
	NBD_FLAG_SEND_FLUSH           = uint16(1 << 2)
	NBD_FLAG_SEND_FUA             = uint16(1 << 3)
	NBD_FLAG_ROTATIONAL           = uint16(1 << 4)
	NBD_FLAG_SEND_TRIM            = uint16(1 << 5)
	NBD_FLAG_SEND_WRITE_ZEROES    = uint16(1 << 6)
	NBD_FLAG_SEND_DF              = uint16(1 << 7)
	NBD_FLAG_CAN_MULTI_CONN       = uint16(1 << 8)
	NBD_FLAG_SEND_RESIZE          = uint16(1 << 9)
	NBD_FLAG_SEND_CACHE           = uint16(1 << 10)
	NBD_FLAG_SEND_FAST_ZERO       = uint16(1 << 11)
	NBD_FLAG_BLOCK_STATUS_PAYLOAD = uint16(1 << 12)

	NEGOTIATION_TYPE_REPLY_ACK         = uint32(1)
	NEGOTIATION_TYPE_REPLY_SERVER      = uint32(2)
	NEGOTIATION_TYPE_REPLY_INFO        = uint32(3)
	NBD_REP_ERR_UNSUP                  = uint32(1 | uint32(1<<31))
	NEGOTIATION_TYPE_REPLY_ERR_UNKNOWN = uint32(6 | uint32(1<<31))

	NBD_INFO_EXPORT      = uint16(0)
	NBD_INFO_NAME        = uint16(1)
	NBD_INFO_DESCRIPTION = uint16(2)
	NBD_INFO_BLOCK_SIZE  = uint16(3)
)

type NegotiationNewstyleHeader struct {
	OldstyleMagic  uint64
	OptionMagic    uint64
	HandshakeFlags uint16
}

type NegotiationOptionHeader struct {
	OptionMagic uint64
	ID          uint32
	Length      uint32
}

type NegotiationReplyHeader struct {
	ReplyMagic uint64
	ID         uint32
	Type       uint32
	Length     uint32
}

type NegotiationReplyInfo struct {
	Type              uint16
	Size              uint64
	TransmissionFlags uint16
}

type NegotiationReplyNameHeader struct {
	Type uint16
}

type NegotiationReplyDescriptionHeader NegotiationReplyNameHeader

type NegotiationReplyBlockSize struct {
	Type               uint16
	MinimumBlockSize   uint32
	PreferredBlockSize uint32
	MaximumBlockSize   uint32
}
