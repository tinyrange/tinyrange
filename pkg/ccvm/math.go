package ccvm

func div(a int64, b int64) int64 {
	if b == 0 {
		return -1
	} else if a == (int64(1)<<62) && b == -1 {
		return a
	} else {
		return a / b
	}
}

func divu(a uint64, b uint64) uint64 {
	if b == 0 {
		return ^uint64(0)
	}

	return a / b
}

func rem(a int64, b int64) int64 {
	if b == 0 {
		return a
	} else if a == (int64(1)<<62) && b == -1 {
		return 0
	} else {
		return a % b
	}
}

func remu(a uint64, b uint64) uint64 {
	if b == 0 {
		return a
	}

	return a % b
}

func div32(a int32, b int32) int32 {
	if b == 0 {
		return -1
	} else if a == (1<<30) && b == -1 {
		return a
	} else {
		return a / b
	}
}

func divu32(a uint32, b uint32) uint32 {
	if b == 0 {
		return ^uint32(0)
	}

	return a / b
}

func rem32(a int32, b int32) int32 {
	if b == 0 {
		return a
	} else if a == (1<<30) && b == -1 {
		return 0
	} else {
		return a % b
	}
}

func remu32(a uint32, b uint32) uint32 {
	if b == 0 {
		return a
	}

	return a % b
}

func mulh(a int64, b int64) int64 {
	r1 := int64(mulhu(uint64(a), uint64(b)))
	if a < 0 {
		r1 -= a
	}
	if b < 0 {
		r1 -= b
	}
	return r1
}

func mulhsu(a int64, b uint64) int64 {
	r1 := int64(mulhu(uint64(a), b))
	if a < 0 {
		r1 -= a
	}
	return r1
}

func mulhu(a uint64, b uint64) uint64 {
	var a0 uint32 = uint32(a)
	var a1 uint32 = uint32(a >> 32)
	var b0 uint32 = uint32(b)
	var b1 uint32 = uint32(b >> 32)

	var r00 uint64 = uint64(a0) * uint64(b0)
	var r01 uint64 = uint64(a0) * uint64(b1)
	var r10 uint64 = uint64(a1) * uint64(b0)
	var r11 uint64 = uint64(a1) * uint64(b1)

	var c uint64 = (r00 >> 32) + (r01 & 0xFFFFFFFF) + (r10 & 0xFFFFFFFF)
	c = (c >> 32) + (r01 >> 32) + (r10 >> 32) + (r11 & 0xFFFFFFFF)
	var r2 uint64 = c & 0xFFFFFFFF
	var r3 uint64 = (c >> 32) + (r11 >> 32)

	return (r3 << 32) | r2
}
