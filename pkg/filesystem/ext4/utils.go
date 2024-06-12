package ext4

func join_uint16_uint16(a uint16, b uint16) uint32 {
	return uint32(a) | uint32(b)<<16
}

func split_uint16_uint16(v uint32) (uint16, uint16) {
	return uint16(v & 0xffff), uint16(v >> 16)
}

func join_uint32_uint16(a uint32, b uint16) uint64 {
	return uint64(a) | uint64(b)<<32
}

func split_uint32_uint16(v uint64) (uint32, uint16) {
	return uint32(v & 0xffff_ffff), uint16(v >> 32)
}

func join_uint32_uint32(a uint32, b uint32) uint64 {
	return uint64(a) | uint64(b)<<32
}

func split_uint32_uint32(v uint64) (uint32, uint32) {
	return uint32(v & 0xffff_ffff), uint32(v >> 32)
}
