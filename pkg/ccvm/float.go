package ccvm

const (
	FFLAG_INVALID_OP  = (1 << 4)
	FFLAG_DIVIDE_ZERO = (1 << 3)
	FFLAG_OVERFLOW    = (1 << 2)
	FFLAG_UNDERFLOW   = (1 << 1)
	FFLAG_INEXACT     = (1 << 0)
	FCLASS_NINF       = (1 << 0)
	FCLASS_NNORMAL    = (1 << 1)
	FCLASS_NSUBNORMAL = (1 << 2)
	FCLASS_NZERO      = (1 << 3)
	FCLASS_PZERO      = (1 << 4)
	FCLASS_PSUBNORMAL = (1 << 5)
	FCLASS_PNORMAL    = (1 << 6)
	FCLASS_PINF       = (1 << 7)
	FCLASS_SNAN       = (1 << 8)
	FCLASS_QNAN       = (1 << 9)
)

func isnanSf32(a uint32) bool {
	return (a>>23)&0xff == 0xff && a&0x7fffff != 0
}

func issignanSf32(a uint32) bool {
	return (a>>22)&0xff == 0xff && a&0x7fffff != 0
}

func isnanSf64(a uint64) bool {
	return (a>>52)&0x7ff == 0x7ff && a&0xfffffffffffff != 0
}

func issignanSf64(a uint64) bool {
	return (a>>51)&0x7ff == 0x7ff && a&0xfffffffffffff != 0
}

func fEqualQuietSf32(a uint32, b uint32, flags *uint32) uint32 {
	if isnanSf32(a) || isnanSf32(b) {
		if issignanSf32(a) || issignanSf32(b) {
			*flags |= FFLAG_INVALID_OP
		}
		return 0
	}

	if (uint32)((a|b)<<1) == 0 {
		return 1 /* zero case */
	}

	if a == b {
		return 1
	} else {
		return 0
	}
}

func fEqualQuietSf64(a uint64, b uint64, flags *uint32) uint64 {
	if isnanSf64(a) || isnanSf64(b) {
		if issignanSf64(a) || issignanSf64(b) {
			*flags |= FFLAG_INVALID_OP
		}
		return 0
	}

	if (uint64)((a|b)<<1) == 0 {
		return 1 /* zero case */
	}

	if a == b {
		return 1
	} else {
		return 0
	}
}

func fCvtI64Sf64(a uint64, roundingMode uint8, flags *uint32) uint64 {
	// TODO(joshua): Implement int to float conversion.
	return a
}

func fMaSf64(a uint64, b uint64, c uint64, roundingMode uint8, flags *uint32) uint64 {
	// TODO(joshua): Implement fused multiply-add.
	return a
}
