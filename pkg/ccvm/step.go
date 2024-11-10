package ccvm

import (
	"errors"
	"fmt"
)

var (
	ErrJump = errors.New("jump")
)

type stepFunc func(vm *VirtualMachine, code *code, insn uint32) (bool, error)

func stepCQ0(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	// compressed quadrant zero.
	funct3 := (insn >> 13) & 7
	rd := ((insn >> 2) & 7) | 8
	switch funct3 {
	case 0: // c.addi4spn
		imm := getField1(insn, 11, 4, 5) |
			getField1(insn, 7, 6, 9) |
			getField1(insn, 6, 2, 2) |
			getField1(insn, 5, 3, 3)
		if imm == 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}

		vm.reg[rd] = uint64(int64(vm.reg[2]) + int64(imm))

		code.next2()
		return true, nil
	case 1: // c.fld
		if vm.fs == 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}

		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		rs1 := ((insn >> 7) & 7) | 8
		addr := uint64(int64(vm.reg[rs1]) + int64(imm))

		rval, err := vm.readU64(addr)
		if err != nil {
			return false, err
		}

		vm.fpReg[rd] = rval | F64_HIGH
		vm.fs = 3

		code.next2()
		return true, nil
	case 2: // c.lw
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 6, 2, 2) |
			getField1(insn, 5, 6, 6)
		rs1 := ((insn >> 7) & 7) | 8
		addr := uint64(int64(vm.reg[rs1]) + int64(imm))

		rval, err := vm.readU32(addr)
		if err != nil {
			return false, err
		}
		vm.reg[rd] = uint64(int32(rval))

		code.next2()
		return true, nil
	case 3: // c.ld
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		rs1 := ((insn >> 7) & 7) | 8
		addr := uint64(int64(vm.reg[rs1]) + int64(imm))
		rval, err := vm.readU64(addr)
		if err != nil {
			return false, err
		}
		vm.reg[rd] = uint64(int64(rval))

		code.next2()
		return true, nil
	case 5: // c.fsd
		if vm.fs == 0 {
			return false, fmt.Errorf("vm.fs == 0")
		}

		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		rs1 := ((insn >> 7) & 7) | 8
		addr := uint64(int64(vm.reg[rs1]) + int64(imm))

		vm.log("c.fsd addr=%x\n", addr)

		if err := vm.writeU64(addr, vm.fpReg[rd]); err != nil {
			return false, err
		}

		code.next2()
		return true, nil
	case 6: // c.sw
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 6, 2, 2) |
			getField1(insn, 5, 6, 6)
		rs1 := ((insn >> 7) & 7) | 8
		addr := uint64(int64(vm.reg[rs1]) + int64(imm))
		val := vm.reg[rd]
		if err := vm.writeU32(addr, uint32(val)); err != nil {
			return false, err
		}

		code.next2()
		return true, nil
	case 7: // c.sd
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		rs1 := ((insn >> 7) & 7) | 8
		addr := uint64(int64(vm.reg[rs1]) + int64(imm))
		val := vm.reg[rd]

		vm.log("c.sd addr=%x val=%x\n", addr, val)

		if err := vm.writeU64(addr, val); err != nil {
			return false, err
		}

		code.next2()
		return true, nil
	default:
		return false, fmt.Errorf("invalid instruction CQ0: funct3=%d insn= 0x%08X", funct3, insn)
	}
}

func stepCQ1(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	// compressed quadrant one
	rd := (insn >> 7) & 0x1f
	funct3 := (insn >> 13) & 7
	switch funct3 {
	case 0: // c.addi
		imm := sext(int32(getField1(insn, 12, 5, 5)|
			getField1(insn, 2, 0, 4)),
			6)
		vm.reg[rd] = uint64(int64(vm.reg[rd]) + int64(imm))

		code.next2()
		return true, nil
	case 1: // c.addiw
		if rd != 0 {
			imm := sext(int32(getField1(insn, 12, 5, 5)|
				getField1(insn, 2, 0, 4)),
				6)
			vm.reg[rd] = uint64(int32(vm.reg[rd]) + imm)
		}

		code.next2()
		return true, nil
	case 2: // c.li
		if rd != 0 {
			imm := sext(int32(getField1(insn, 12, 5, 5)|
				getField1(insn, 2, 0, 4)),
				6)
			vm.reg[rd] = uint64(imm)
		}

		code.next2()
		return true, nil
	case 3:
		if rd == 2 { // c.addi16sp
			imm := sext(int32(getField1(insn, 12, 9, 9)|
				getField1(insn, 6, 4, 4)|
				getField1(insn, 5, 6, 6)|
				getField1(insn, 3, 7, 8)|
				getField1(insn, 2, 5, 5)),
				10)
			if imm == 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			vm.reg[2] = uint64(int64(vm.reg[2]) + int64(imm))

			code.next2()
			return true, nil
		} else if rd != 0 { // c.lui
			imm := sext(int32(getField1(insn, 12, 17, 17)|
				getField1(insn, 2, 12, 16)),
				18)
			vm.reg[rd] = uint64(imm)

			code.next2()
			return true, nil
		} else {
			return false, ErrInvalidInstruction{insn: insn}
		}
	case 4:
		funct3 = (insn >> 10) & 3
		rd = ((insn >> 7) & 7) | 8
		switch true {
		case funct3 == 0 || funct3 == 1: // c.srli/c.srai
			imm := getField1(insn, 12, 5, 5) |
				getField1(insn, 2, 0, 4)

			if funct3 == 0 {
				vm.reg[rd] = uint64(int64(uint64(vm.reg[rd]) >> imm))
			} else {
				vm.reg[rd] = uint64(int64(vm.reg[rd]) >> imm)
			}

			code.next2()
			return true, nil
		case funct3 == 2: // c.andi
			imm := sext(int32(getField1(insn, 12, 5, 5)|
				getField1(insn, 2, 0, 4)),
				6)
			vm.reg[rd] &= uint64(imm)

			code.next2()
			return true, nil
		case funct3 == 3:
			rs2 := ((insn >> 2) & 7) | 8
			funct3 = ((insn >> 5) & 3) | ((insn >> (12 - 2)) & 4)
			switch funct3 {
			case 0: // c.sub
				vm.reg[rd] = uint64(int64(vm.reg[rd]) - int64(vm.reg[rs2]))

				code.next2()
				return true, nil
			case 1: // c.xor
				vm.reg[rd] = vm.reg[rd] ^ vm.reg[rs2]

				code.next2()
				return true, nil
			case 2: // c.or
				vm.reg[rd] = vm.reg[rd] | vm.reg[rs2]

				code.next2()
				return true, nil
			case 3: // c.and
				vm.reg[rd] = vm.reg[rd] & vm.reg[rs2]

				code.next2()
				return true, nil
			case 4: // c.subw
				vm.reg[rd] = uint64(int32(vm.reg[rd]) - int32(vm.reg[rs2]))

				code.next2()
				return true, nil
			case 5: // c.addw
				vm.reg[rd] = uint64(int32(vm.reg[rd]) + int32(vm.reg[rs2]))

				code.next2()
				return true, nil
			default:
				return false, fmt.Errorf("invalid instruction CQ1.4.3: funct3=%d insn= 0x%08X", funct3, insn)
			}
		default:
			return false, fmt.Errorf("invalid instruction CQ1.4: funct3=%d insn= 0x%08X", funct3, insn)
		}
	case 5: // c.j
		imm := sext(int32(getField1(insn, 12, 11, 11)|
			getField1(insn, 11, 4, 4)|
			getField1(insn, 9, 8, 9)|
			getField1(insn, 8, 10, 10)|
			getField1(insn, 7, 6, 6)|
			getField1(insn, 6, 7, 7)|
			getField1(insn, 3, 1, 3)|
			getField1(insn, 2, 5, 5)),
			12)
		vm.pc = uint64(int64(vm.pc) + int64(imm))

		return true, ErrJump
	case 6: // c.beqz
		rs1 := ((insn >> 7) & 7) | 8
		imm := sext(int32(getField1(insn, 12, 8, 8)|
			getField1(insn, 10, 3, 4)|
			getField1(insn, 5, 6, 7)|
			getField1(insn, 3, 1, 2)|
			getField1(insn, 2, 5, 5)),
			9)

		if vm.reg[rs1] == 0 {
			vm.pc = uint64(int64(vm.pc) + int64(imm))

			return true, ErrJump
		}

		code.next2()
		return true, nil
	case 7: // c.bnez
		rs1 := ((insn >> 7) & 7) | 8
		imm := sext(int32(getField1(insn, 12, 8, 8)|
			getField1(insn, 10, 3, 4)|
			getField1(insn, 5, 6, 7)|
			getField1(insn, 3, 1, 2)|
			getField1(insn, 2, 5, 5)),
			9)
		if vm.reg[rs1] != 0 {
			vm.pc = uint64(int64(vm.pc) + int64(imm))

			return true, ErrJump
		}

		code.next2()
		return true, nil
	default:
		return false, fmt.Errorf("invalid instruction CQ1: funct3=%d insn= 0x%08X", funct3, insn)
	}
}

func stepCQ2(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	// compressed quadrant two
	rd := (insn >> 7) & 0x1f
	funct3 := (insn >> 13) & 7
	rs2 := (insn >> 2) & 0x1f
	switch funct3 {
	case 0: // c.slli
		imm := getField1(insn, 12, 5, 5) | rs2
		if rd != 0 {
			vm.reg[rd] = uint64(int64(vm.reg[rd] << imm))
		}

		code.next2()
		return true, nil
	case 2: // c.lwsp
		imm := getField1(insn, 12, 5, 5) |
			(rs2 & (7 << 2)) |
			getField1(insn, 2, 6, 7)
		addr := uint64(int64(vm.reg[2]) + int64(imm))

		rval, err := vm.readU32(addr)
		if err != nil {
			return false, err
		}

		if rd != 0 {
			vm.reg[rd] = uint64(int32(rval))
		}

		code.next2()
		return true, nil
	case 3: // c.ldsp
		imm := getField1(insn, 12, 5, 5) |
			(rs2 & (3 << 3)) |
			getField1(insn, 2, 6, 8)

		addr := uint64(int64(vm.reg[2]) + int64(imm))

		rval, err := vm.readU64(addr)
		if err != nil {
			return false, err
		}

		if rd != 0 {
			vm.reg[rd] = uint64(int64(rval))
		}

		code.next2()
		return true, nil
	case 4:
		if ((insn >> 12) & 1) == 0 {
			if rs2 == 0 {
				// c.jr
				if rd == 0 {
					return false, ErrInvalidInstruction{insn: insn}
				}
				vm.pc = vm.reg[rd] & ^uint64(1)
				return true, ErrJump
			} else {
				// c.mv
				if rd != 0 {
					vm.reg[rd] = vm.reg[rs2]
				}
			}
		} else {
			if rs2 == 0 {
				if rd == 0 {
					return false, ErrBreakpoint{}
				} else {
					val := vm.pc + 2
					vm.pc = vm.reg[rd] & ^uint64(1)
					vm.reg[1] = val
					return true, ErrJump
				}
			} else {
				vm.reg[rd] = uint64(int64(vm.reg[rd]) + int64(vm.reg[rs2]))
			}
		}

		code.next2()
		return true, nil
	case 6: // c.swsp
		imm := getField1(insn, 9, 2, 5) |
			getField1(insn, 7, 6, 7)
		addr := uint64(int64(vm.reg[2]) + int64(imm))

		if err := vm.writeU32(addr, uint32(vm.reg[rs2])); err != nil {
			return false, err
		}

		code.next2()
		return true, nil
	case 7: // c.sdsp
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 7, 6, 8)

		addr := uint64(int64(vm.reg[2]) + int64(imm))

		if err := vm.writeU64(addr, vm.reg[rs2]); err != nil {
			return false, err
		}

		code.next2()
		return true, nil
	default:
		return false, fmt.Errorf("invalid instruction CQ2: funct3=%d insn= 0x%08X", funct3, insn)
	}
}

func stepLoad(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	funct3 := (insn >> 12) & 7
	rs1 := (insn >> 15) & 0x1f
	imm := int32(insn) >> 20
	addr := uint64(int64(vm.reg[rs1]) + int64(imm))

	var val uint64

	switch funct3 {
	case 0:
		rval, err := vm.readU8(addr)
		if err != nil {
			return false, err
		}
		val = uint64(int8(rval))
	case 1:
		rval, err := vm.readU16(addr)
		if err != nil {
			return false, err
		}
		val = uint64(int16(rval))
	case 2:
		rval, err := vm.readU32(addr)
		if err != nil {
			return false, err
		}
		val = uint64(int32(rval))
	case 3:
		rval, err := vm.readU64(addr)
		if err != nil {
			return false, err
		}
		val = uint64(int64(rval))
	case 4:
		rval, err := vm.readU8(addr)
		if err != nil {
			return false, err
		}
		val = uint64(rval)
	case 5:
		rval, err := vm.readU16(addr)
		if err != nil {
			return false, err
		}
		val = uint64(rval)
	case 6:
		rval, err := vm.readU32(addr)
		if err != nil {
			return false, err
		}
		val = uint64(rval)
	default:
		return false, fmt.Errorf("unimplemented load: %d", funct3)
	}

	if rd != 0 {
		vm.reg[rd] = val
	}

	code.next4()
	return true, nil
}

func stepFpLoad(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	if vm.fs == 0 {
		return false, ErrInvalidInstruction{insn: insn}
	}

	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	funct3 := (insn >> 12) & 7
	imm := int32(insn) >> 20
	addr := uint64(int64(vm.reg[rs1]) + int64(imm))

	switch funct3 {
	case 0x02: // flw
		rval, err := vm.readU32(addr)
		if err != nil {
			return false, err
		}
		vm.fpReg[rd] = uint64(rval) | F32_HIGH
	case 0x03: // fld
		rval, err := vm.readU64(addr)
		if err != nil {
			return false, err
		}
		vm.fpReg[rd] = uint64(rval) | F64_HIGH
	default:
		return false, fmt.Errorf("invalid fpload")
	}

	vm.fs = 3

	code.next4()
	return true, nil
}

func stepMiscMem(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	funct3 := (insn >> 12) & 7

	switch funct3 {
	case 0: // fence
		if (insn & 0xf00fff80) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
	case 1: // fence.i
		if insn != 0x0000100f {
			return false, ErrInvalidInstruction{insn: insn}
		}
	default:
		return false, fmt.Errorf("invalid misc-mem: funct3=%d", funct3)
	}

	code.next4()
	return true, nil
}

func stepRsi(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	var val uint64

	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	funct3 := (insn >> 12) & 7
	imm := int32(insn) >> 20

	switch funct3 {
	case 0: // addi
		val = uint64(int64(vm.reg[rs1]) + int64(imm))
	case 1: // slli
		if (imm & ^(64 - 1)) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		val = uint64(int64(vm.reg[rs1]) << (imm & (64 - 1)))
	case 2: // slti
		if int64(vm.reg[rs1]) < int64(imm) {
			val = 1
		} else {
			val = 0
		}
	case 3: // sltiu
		if vm.reg[rs1] < uint64(imm) {
			val = 1
		} else {
			val = 0
		}
	case 4: // xori
		val = uint64(vm.reg[rs1] ^ uint64(imm))
	case 5: // srli/srai
		if (imm & ^((64 - 1) | 0x400)) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		if imm&0x400 != 0 {
			val = uint64(int64(vm.reg[rs1]) >> (imm & (64 - 1)))
		} else {
			val = uint64(int64(uint64(vm.reg[rs1]) >> (imm & (64 - 1))))
		}
	case 6: // ori
		val = vm.reg[rs1] | uint64(imm)
	case 7: // andi
		val = vm.reg[rs1] & uint64(imm)
	default:
		return false, fmt.Errorf("invalid 0x13: funct3=%d", funct3)
	}

	if rd != 0 {
		vm.reg[rd] = val
	}

	code.next4()
	return true, nil
}

func stepAuipc(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f

	if rd != 0 {
		vm.reg[rd] = uint64(int64(vm.pc) + int64(int32(insn&0xfffff000)))
	}

	code.next4()
	return true, nil
}

func stepOpImm32(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	funct3 := (insn >> 12) & 7
	imm := int32(insn) >> 20

	val := vm.reg[rs1]

	switch funct3 {
	case 0: // addiw
		val = uint64(int32(val) + imm)
	case 1: // slliw
		if (imm & ^31) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		val = uint64(int32(val << (imm & 31)))
	case 5: // srliw/sraiw
		if (imm & ^(31 | 0x400)) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		if (imm & 0x400) != 0 {
			val = uint64(int32(val) >> (imm & 31))
		} else {
			val = uint64(int32(uint32(val) >> (imm & 31)))
		}
	default:
		return false, fmt.Errorf("invalid 0x1b: %d", funct3)
	}

	if rd != 0 {
		vm.reg[rd] = val
	}

	code.next4()
	return true, nil
}

func stepStore(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f

	funct3 := (insn >> 12) & 7
	imm := int32(rd) | ((int32(insn) >> (25 - 5)) & 0xfe0)
	imm = (imm << 20) >> 20
	addr := uint64(int64(vm.reg[rs1]) + int64(imm))
	val := vm.reg[rs2]

	switch funct3 {
	case 0:
		if err := vm.writeU8(addr, uint8(val)); err != nil {
			return false, err
		}
	case 1:
		if err := vm.writeU16(addr, uint16(val)); err != nil {
			return false, err
		}
	case 2: // sw
		if addr == 0x00 && imm == 0x00 && vm.stopOnZero {
			return false, ErrStopOnZero
		}

		if err := vm.writeU32(addr, uint32(val)); err != nil {
			return false, err
		}
	case 3:
		if err := vm.writeU64(addr, val); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("invalid store: %d", funct3)
	}

	code.next4()
	return true, nil
}

func stepFpStore(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	if vm.fs == 0 {
		return false, ErrInvalidInstruction{insn: insn}
	}

	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f
	funct3 := (insn >> 12) & 7

	imm := rd | ((insn >> (25 - 5)) & 0xfe0)
	imm = (imm << 20) >> 20

	addr := uint64(int64(vm.reg[rs1]) + int64(imm))

	switch funct3 {
	case 2: // fsw
		if err := vm.writeU32(addr, uint32(vm.fpReg[rs2])); err != nil {
			return false, err
		}
	case 3: // fsd
		if err := vm.writeU64(addr, vm.fpReg[rs2]); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("invalid fp store: funct3=%d", funct3)
	}

	code.next4()
	return true, nil
}

func stepMath(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f
	imm := insn >> 25

	val := vm.reg[rs1]
	val2 := vm.reg[rs2]

	if imm == 1 {
		funct3 := (insn >> 12) & 7
		switch funct3 {
		case 0: // mul
			val = uint64(int64(int64(val) * int64(val2)))
		case 1: // mulh
			val = uint64(mulh(int64(val), int64(val2)))
		case 2: // mulhsu
			val = uint64(mulhsu(int64(val), val2))
		case 3: // mulhu
			val = uint64(mulhu(val, val2))
		case 4: // div
			val = uint64(div(int64(val), int64(val2)))
		case 5: // divu
			val = uint64(divu(val, val2))
		case 6: // rem
			val = uint64(rem(int64(val), int64(val2)))
		case 7: // remu
			val = uint64(remu(val, val2))
		default:
			return false, fmt.Errorf("unknown 0x33-1: %d", funct3)
		}
	} else {
		if imm & ^uint32(0x20) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		funct3 := ((insn >> 12) & 7) | ((insn >> (30 - 3)) & (1 << 3))
		switch funct3 {
		case 0: // add
			val = uint64(int64(val) + int64(val2))
		case 0 | 8: // sub
			val = uint64(int64(val) - int64(val2))
		case 1: // sll
			val = uint64(int64(val) << (val2 & 63))
		case 2: // slt
			if int64(val) < int64(val2) {
				val = 1
			} else {
				val = 0
			}
		case 3: // sltu
			if val < val2 {
				val = 1
			} else {
				val = 0
			}
		case 4: // xor
			val = val ^ val2
		case 5: // srl
			val = uint64(int64(uint64(val) >> (val2 & (64 - 1))))
		case 5 | 8: // sra
			val = uint64(int64(val) >> (val2 & (64 - 1)))
		case 6: // or
			val = val | val2
		case 7: // and
			val = val & val2
		default:
			return false, fmt.Errorf("unknown 0x33-!1: %d", funct3)
		}
	}

	if rd != 0 {
		vm.reg[rd] = val
	}

	code.next4()
	return true, nil
}

func stepLui(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f

	if rd != 0 {
		vm.reg[rd] = uint64(int32(insn & 0xfffff000))
	}

	code.next4()
	return true, nil
}

func stepOp32(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f

	imm := insn >> 25
	val := vm.reg[rs1]
	val2 := vm.reg[rs2]
	if imm == 1 {
		funct3 := (insn >> 12) & 7
		switch funct3 {
		case 0: // mul
			val = uint64(int32(val) * int32(val2))
		case 4: // div
			val = uint64(div32(int32(val), int32(val2)))
		case 5: // divu
			val = uint64(divu32(uint32(val), uint32(val2)))
		case 6: // rem
			val = uint64(rem32(int32(val), int32(val2)))
		case 7: // remu
			val = uint64(remu32(uint32(val), uint32(val2)))
		default:
			return false, fmt.Errorf("invalid OP-32.1 funct3=%d", funct3)
		}

	} else {
		if imm & ^uint32(0x20) != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}

		funct3 := ((insn >> 12) & 7) | ((insn >> (30 - 3)) & (1 << 3))
		switch funct3 {
		case 0: // addw
			val = uint64(int32(val) + int32(val2))
		case 0 | 8: // subw
			val = uint64(int32(val) - int32(val2))
		case 1: // sllw
			val = uint64(int32(uint32(val) << (val2 & 31)))
		case 5: // srlw
			val = uint64(int32(uint32(val) >> (val2 & 31)))
		case 5 | 8: // sraw
			val = uint64(int32(val) >> (val2 & 31))
		default:
			return false, fmt.Errorf("invalid OP-32.0 funct3=%d", funct3)
		}
	}

	if rd != 0 {
		vm.reg[rd] = val
	}

	code.next4()
	return true, nil
}

func stepFmAdd(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	if vm.fs == 0 {
		return false, ErrInvalidInstruction{insn: insn}
	}

	funct3 := (insn >> 25) & 3
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f
	rs3 := insn >> 27

	rm := vm.getInsnRm((insn >> 12) & 7)
	if rm < 0 {
		return false, ErrInvalidInstruction{insn: insn}
	}

	switch funct3 {
	case 1:
		vm.fpReg[rd] = fMaSf64(vm.fpReg[rs1], vm.fpReg[rs2], vm.fpReg[rs3], uint8(rm), &vm.fflags) | F64_HIGH
	default:
		return false, fmt.Errorf("invalid fmadd: %d", funct3)
	}

	vm.fs = 3

	code.next4()
	return true, nil
}

func (vm *VirtualMachine) getInsnRm(rm uint32) int {
	if rm == 7 {
		return int(vm.frm)
	}
	if rm >= 5 {
		return -1
	} else {
		return int(rm)
	}
}

func stepFp(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	if vm.fs == 0 {
		return false, ErrInvalidInstruction{insn: insn}
	}

	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f
	imm := insn >> 25
	rm := (insn >> 12) & 7

	switch true {
	case imm == (0x1e << 2): // fmv.s.x
		if rs2 != 0 || rm != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		vm.fpReg[rd] = uint64(int32(vm.reg[rs1]))
		vm.fs = 3

		code.next4()
		return true, nil
	case imm == (0x14 << 2): // feq.s.x
		var val uint64

		switch rm {
		case 2: // feq
			val = uint64(fEqualQuietSf32(uint32(vm.fpReg[rs1]), uint32(vm.fpReg[rs2]), &vm.fflags))
		default:
			return false, fmt.Errorf("invalid floating point 0x14 instruction: imm=%d insn=%08x", imm, insn)
		}

		if rd != 0 {
			vm.reg[rd] = val
		}

		code.next4()
		return true, nil
	case imm == (0x00<<2)|1: // fadd.d
		// TODO(joshua): implement
		code.next4()
		return true, nil
	case imm == (0x01<<2)|1: // fsub.d
		// TODO(joshua): implement
		code.next4()
		return true, nil
	case imm == (0x02<<2)|1: // fmul.d
		// TODO(joshua): implement
		code.next4()
		return true, nil
	case imm == (0x14<<2)|1: // feq.d.x
		var val uint64

		switch rm {
		case 2: // feq
			// val = glue(eq_quiet_sf, F_SIZE)(s->fp_reg[rs1], s->fp_reg[rs2],
			// 	&s->fflags);
			val = fEqualQuietSf64(vm.fpReg[rs1], vm.fpReg[rs2], &vm.fflags)
		default:
			return false, fmt.Errorf("invalid floating point 0x14 instruction: imm=%d insn=%08x", imm, insn)
		}

		if rd != 0 {
			vm.reg[rd] = val
		}

		code.next4()
		return true, nil
	case imm == (0x1a<<2)|1:
		rm := vm.getInsnRm(rm)
		if rm < 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		switch rs2 {
		case 2:
			vm.fpReg[rd] = fCvtI64Sf64(vm.reg[rs1], uint8(rm), &vm.fflags) | F64_HIGH
		default:
			return false, fmt.Errorf("invalid floating point 0x1a instruction: imm=%d insn=%08x", imm, insn)
		}

		vm.fs = 3

		code.next4()
		return true, nil
	case imm == (0x1e<<2)|1: // fmv.d.x
		if rs2 != 0 || rm != 0 {
			return false, ErrInvalidInstruction{insn: insn}
		}
		vm.fpReg[rd] = uint64(int64(vm.reg[rs1]))
		vm.fs = 3

		code.next4()
		return true, nil
	default:
		return false, fmt.Errorf("invalid floating point instruction: imm=%d insn=%08x", imm, insn)
	}
}

func stepBranch(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f
	funct3 := (insn >> 12) & 7

	var cond bool
	switch funct3 >> 1 {
	case 0: // beq/bne
		vm.log("branch %x == %x\n", vm.reg[rs1], vm.reg[rs2])
		cond = (vm.reg[rs1] == vm.reg[rs2])
	case 2: // blt/bge
		vm.log("branch %x < %x\n", vm.reg[rs1], vm.reg[rs2])
		cond = (int64(vm.reg[rs1]) < int64(vm.reg[rs2]))
	case 3: // bltu/bgeu
		vm.log("branch %x < %x\n", vm.reg[rs1], vm.reg[rs2])
		cond = vm.reg[rs1] < vm.reg[rs2]
	default:
		return false, fmt.Errorf("invalid 0x63: %d", funct3>>1)
	}

	if (funct3 & 1) != 0 {
		cond = !cond
	}

	if cond {
		vm.log("branch taken\n")
		sInsn := int32(insn)
		imm := ((sInsn >> (31 - 12)) & (1 << 12)) |
			((sInsn >> (25 - 5)) & 0x7e0) |
			((sInsn >> (8 - 1)) & 0x1e) |
			((sInsn << (11 - 7)) & (1 << 11))
		imm = (imm << 19) >> 19
		vm.pc = uint64(int64(vm.pc) + int64(imm))
		return true, ErrJump
	}

	code.next4()
	return true, nil
}

func stepJalr(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	imm := int32(insn) >> 20

	val := vm.pc + 4
	vm.pc = uint64((int64(vm.reg[rs1]) + int64(imm)) & ^int64(0x1))
	if rd != 0 {
		vm.reg[rd] = val
	}
	return true, ErrJump
}

func stepJal(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	sinst := int32(insn)

	rd := (insn >> 7) & 0x1f
	imm := ((sinst >> (31 - 20)) & (1 << 20)) |
		((sinst >> (21 - 1)) & 0x7fe) |
		((sinst >> (20 - 11)) & (1 << 11)) |
		(sinst & 0xff000)
	imm = (imm << 11) >> 11
	if rd != 0 {
		vm.reg[rd] = vm.pc + 4
	}
	vm.pc = uint64(int64(vm.pc) + int64(imm))
	return true, ErrJump
}

func stepSystem(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	var val uint64
	funct3 := (insn >> 12) & 7
	imm := insn >> 20
	if funct3&4 != 0 {
		val = uint64(rs1)
	} else {
		val = vm.reg[rs1]
	}

	funct3 &= 3
	switch true {
	case funct3 == 0:
		switch imm {
		case 0x000: // ecall
			if (insn & 0x000fff80) != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			return false, ErrUserECall{}
		case 0x001: // ebreak
			if (insn & 0x000fff80) != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			return false, ErrBreakpoint{}
		case 0x102: // sret
			if (insn & 0x000fff80) != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			if vm.priv < PRV_S {
				return false, fmt.Errorf("invalid sret: priv=%d", vm.priv)
			}

			vm.handleSret()

			return false, nil
		case 0x105: // wfi
			if (insn & 0x00007f80) != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			if vm.priv == PRV_U {
				return false, ErrInvalidInstruction{insn: insn}
			}
			/* go to power down if no enabled interrupts are
			   pending */
			// if ((s->mip & s->mie) == 0)
			// {
			// 	s->power_down_flag = TRUE;
			// 	s->pc = GET_PC() + 4;
			// 	goto done_interp;
			// }
			vm.pc += 4
			return true, ErrJump
		case 0x302: // mret
			if (insn & 0x000fff80) != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			if vm.priv < PRV_M {
				return false, fmt.Errorf("invalid mret: priv=%d", vm.priv)
			}

			vm.handleMret()

			return false, nil
		default:
			if (imm >> 5) == 0x09 {
				// sfence.vma
				if insn&0x00007f80 != 0 {
					return false, ErrInvalidInstruction{insn: insn}
				}
				if vm.priv == PRV_U {
					return false, fmt.Errorf("sfence.vma is not supported in U mode")
				}
				if rs1 == 0 {
					vm.tlbFlushAll()
				} else {
					vm.tlbFlushVaddr(vm.reg[rs1])
				}
				// the current code TLB may have been flushed
				vm.pc += 4
				return true, ErrJump
			} else {
				return false, fmt.Errorf("invalid 0x73:0: imm=%x", imm)
			}
		}
	case funct3 == 1: // csrrw
		val2, err := vm.csrRead(imm, rs1 != 0)
		if _, ok := err.(ErrIllegalInstruction); ok {
			return false, ErrIllegalInstruction{insn: insn}
		} else if err != nil {
			return false, err
		}

		vm.log("csrRead(%x)=%x\n", imm, val2)

		val2 = uint64(int64(val2))

		vm.log("csrWrite(%x)=%x\n", imm, val)

		err = vm.csrWrite(imm, val)

		if rd != 0 {
			vm.reg[rd] = val2
		}

		if err != nil {
			if err == ErrFlushCode {
				vm.pc += 4
				return true, ErrJump
			} else {
				return false, err
			}
		}
	case funct3 == 2 || funct3 == 3: // csrrs
		val2, err := vm.csrRead(imm, rs1 != 0)
		if _, ok := err.(ErrIllegalInstruction); ok {
			return false, ErrIllegalInstruction{insn: insn}
		} else if err != nil {
			return false, err
		}
		vm.log("csrRead(%x)=%x\n", imm, val2)

		if rs1 != 0 {
			if funct3 == 2 {
				val = val2 | val
			} else {
				val = val2 & ^val
			}
			err = vm.csrWrite(imm, val)
			if err != nil {
				return false, err
			}
		} else {
			err = nil
		}

		if rd != 0 {
			vm.reg[rd] = val2
		}
		if err != nil {
			vm.pc = vm.pc + 4
			// if err == 2 {
			// 	return true, ErrJump
			// } else {
			// 	goto doneInterpreting
			// }
			return false, err
		}
	default:
		return false, fmt.Errorf("invalid 0x73: funct3=%d", funct3)
	}

	code.next4()
	return true, nil
}

func stepAtomic(vm *VirtualMachine, code *code, insn uint32) (bool, error) {
	var val uint64
	var err error

	rd := (insn >> 7) & 0x1f
	rs1 := (insn >> 15) & 0x1f
	rs2 := (insn >> 20) & 0x1f
	funct3 := (insn >> 12) & 7

	switch funct3 {
	case 2:
		addr := vm.reg[rs1]
		funct3 := insn >> 27
		switch funct3 {
		case 1: // amiswap.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 2: // lc.w
			if rs2 != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}
			rval, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}
			val = uint64(int32(rval))
			vm.loadRes = addr
		case 3: // sc.w
			if vm.loadRes == addr {
				if err := vm.writeU32(addr, uint32(vm.reg[rs2])); err != nil {
					return false, err
				}
				val = 0
			} else {
				val = 1
			}
		case 0: // amoadd.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			val2 = int32(value) + val2

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 4: // amoxor.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			val2 = int32(value) ^ val2

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 8: // amoor.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			val2 = int32(value) | int32(val2)

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 12: // amoand.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			val2 = int32(value) & val2

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 16: // amomin.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			if int32(val) < val2 {
				val2 = int32(val)
			}

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 20: // amomax.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			if int32(value) > val2 {
				val2 = int32(value)
			}

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 24: // amominu.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			if value < uint32(val2) {
				val2 = int32(value)
			}

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		case 28: // amomaxu.w
			value, err := vm.readU32(addr)
			if err != nil {
				return false, err
			}

			var val2 = int32(vm.reg[rs2])
			if value > uint32(val2) {
				val2 = int32(value)
			}

			if err := vm.writeU32(addr, uint32(val2)); err != nil {
				return false, err
			}

			val = uint64(sext64(int64(value), 32))
		default:
			return false, fmt.Errorf("invalid 0x2f:2: insn= 0x%08X funct3=%d", insn, funct3)
		}
	case 3:
		addr := vm.reg[rs1]
		funct3 := insn >> 27
		switch funct3 {
		case 1: // amiswap.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 2: // lc.w
			if rs2 != 0 {
				return false, ErrInvalidInstruction{insn: insn}
			}

			rval, err := vm.readU64(addr)
			if err != nil {
				return false, err
			}

			val = uint64(int64(rval))
			vm.loadRes = addr
		case 3: // sc.w
			if vm.loadRes == addr {
				if err := vm.writeU64(addr, vm.reg[rs2]); err != nil {
					return false, err
				}
				val = 0
			} else {
				val = 1
			}
		case 0: // amoadd.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			val2 = int64(val) + val2

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 4: // amoxor.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			val2 = int64(val) ^ val2

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 8: // amoor.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			val2 = int64(val) | int64(val2)

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 12: // amoand.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			val2 = int64(val) & int64(val2)

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 16: // amomin.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			if int64(val) < val2 {
				val2 = int64(val)
			}

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 20: // amomax.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			if int64(val) > val2 {
				val2 = int64(val)
			}

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 24: // amominu.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			if val < uint64(val2) {
				val2 = int64(val)
			}

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		case 28: // amomaxu.w
			val, err = vm.readU64(addr)
			if err != nil {
				return false, err
			}

			var val2 = int64(vm.reg[rs2])
			if val > uint64(val2) {
				val2 = int64(val)
			}

			if err := vm.writeU64(addr, uint64(val2)); err != nil {
				return false, err
			}
		default:
			return false, fmt.Errorf("invalid 0x2f:3: insn= 0x%08X funct3=%d", insn, funct3)
		}
	default:
		return false, fmt.Errorf("invalid 0x2f: insn= 0x%08X funct3=%d", insn, funct3)
	}

	if rd != 0 {
		vm.reg[rd] = val
	}

	code.next4()
	return true, nil
}

var stepJumpTable = [128]stepFunc{
	0 + (0 << 2):  stepCQ0,
	0 + (1 << 2):  stepCQ0,
	0 + (2 << 2):  stepCQ0,
	0 + (3 << 2):  stepCQ0,
	0 + (4 << 2):  stepCQ0,
	0 + (5 << 2):  stepCQ0,
	0 + (6 << 2):  stepCQ0,
	0 + (7 << 2):  stepCQ0,
	0 + (8 << 2):  stepCQ0,
	0 + (9 << 2):  stepCQ0,
	0 + (10 << 2): stepCQ0,
	0 + (11 << 2): stepCQ0,
	0 + (12 << 2): stepCQ0,
	0 + (13 << 2): stepCQ0,
	0 + (14 << 2): stepCQ0,
	0 + (15 << 2): stepCQ0,
	0 + (16 << 2): stepCQ0,
	0 + (17 << 2): stepCQ0,
	0 + (18 << 2): stepCQ0,
	0 + (19 << 2): stepCQ0,
	0 + (20 << 2): stepCQ0,
	0 + (21 << 2): stepCQ0,
	0 + (22 << 2): stepCQ0,
	0 + (23 << 2): stepCQ0,
	0 + (24 << 2): stepCQ0,
	0 + (25 << 2): stepCQ0,
	0 + (26 << 2): stepCQ0,
	0 + (27 << 2): stepCQ0,
	0 + (28 << 2): stepCQ0,
	0 + (29 << 2): stepCQ0,
	0 + (30 << 2): stepCQ0,
	0 + (31 << 2): stepCQ0,
	1 + (0 << 2):  stepCQ1,
	1 + (1 << 2):  stepCQ1,
	1 + (2 << 2):  stepCQ1,
	1 + (3 << 2):  stepCQ1,
	1 + (4 << 2):  stepCQ1,
	1 + (5 << 2):  stepCQ1,
	1 + (6 << 2):  stepCQ1,
	1 + (7 << 2):  stepCQ1,
	1 + (8 << 2):  stepCQ1,
	1 + (9 << 2):  stepCQ1,
	1 + (10 << 2): stepCQ1,
	1 + (11 << 2): stepCQ1,
	1 + (12 << 2): stepCQ1,
	1 + (13 << 2): stepCQ1,
	1 + (14 << 2): stepCQ1,
	1 + (15 << 2): stepCQ1,
	1 + (16 << 2): stepCQ1,
	1 + (17 << 2): stepCQ1,
	1 + (18 << 2): stepCQ1,
	1 + (19 << 2): stepCQ1,
	1 + (20 << 2): stepCQ1,
	1 + (21 << 2): stepCQ1,
	1 + (22 << 2): stepCQ1,
	1 + (23 << 2): stepCQ1,
	1 + (24 << 2): stepCQ1,
	1 + (25 << 2): stepCQ1,
	1 + (26 << 2): stepCQ1,
	1 + (27 << 2): stepCQ1,
	1 + (28 << 2): stepCQ1,
	1 + (29 << 2): stepCQ1,
	1 + (30 << 2): stepCQ1,
	1 + (31 << 2): stepCQ1,
	2 + (0 << 2):  stepCQ2,
	2 + (1 << 2):  stepCQ2,
	2 + (2 << 2):  stepCQ2,
	2 + (3 << 2):  stepCQ2,
	2 + (4 << 2):  stepCQ2,
	2 + (5 << 2):  stepCQ2,
	2 + (6 << 2):  stepCQ2,
	2 + (7 << 2):  stepCQ2,
	2 + (8 << 2):  stepCQ2,
	2 + (9 << 2):  stepCQ2,
	2 + (10 << 2): stepCQ2,
	2 + (11 << 2): stepCQ2,
	2 + (12 << 2): stepCQ2,
	2 + (13 << 2): stepCQ2,
	2 + (14 << 2): stepCQ2,
	2 + (15 << 2): stepCQ2,
	2 + (16 << 2): stepCQ2,
	2 + (17 << 2): stepCQ2,
	2 + (18 << 2): stepCQ2,
	2 + (19 << 2): stepCQ2,
	2 + (20 << 2): stepCQ2,
	2 + (21 << 2): stepCQ2,
	2 + (22 << 2): stepCQ2,
	2 + (23 << 2): stepCQ2,
	2 + (24 << 2): stepCQ2,
	2 + (25 << 2): stepCQ2,
	2 + (26 << 2): stepCQ2,
	2 + (27 << 2): stepCQ2,
	2 + (28 << 2): stepCQ2,
	2 + (29 << 2): stepCQ2,
	2 + (30 << 2): stepCQ2,
	2 + (31 << 2): stepCQ2,
	0x03:          stepLoad,
	0x07:          stepFpLoad,
	0x0F:          stepMiscMem,
	0x13:          stepRsi,
	0x17:          stepAuipc,
	0x1B:          stepOpImm32,
	0x23:          stepStore,
	0x27:          stepFpStore,
	0x33:          stepMath,
	0x37:          stepLui,
	0x3B:          stepOp32,
	0x43:          stepFmAdd,
	0x53:          stepFp,
	0x63:          stepBranch,
	0x67:          stepJalr,
	0x6F:          stepJal,
	0x73:          stepSystem,
	0x2f:          stepAtomic,
}

var codeBuffer [4096]byte

func (vm *VirtualMachine) step(cycles int64) error {
	var (
		codeBuf code
		insn    uint32
		err     error
	)

	vm.log("step s->mip=%x s->mie=%x\n", vm.mip, vm.mie)

	if cycles == 0 {
		return nil
	}

	if (vm.mip & vm.mie) != 0 {
		vm.log("raise_interrupt %d\n", cycles)

		ok, err := vm.raiseInterrupt()
		if err != nil {
			return err
		}

		if ok {
			// vm.clockCycles++
			return nil
		}
	}

outer:
	for {
		if codeBuf.code == nil || codeBuf.remaining == 0 {
			if codeBuf.code != nil {
				vm.log("ran out of code\n")
			}

			if cycles <= 0 {
				break outer
			}

			if (vm.mip & vm.mie) != 0 {
				vm.log("raise_interrupt %d\n", cycles)

				ok, err := vm.raiseInterrupt()
				if err != nil {
					return err
				}

				if ok {
					// vm.clockCycles++
					return nil
				}
			}

			err = vm.getCode(codeBuffer[:], &codeBuf, 0)
			if pErr, ok := err.(ErrPageFault); ok {
				pErr.cause = CAUSE_FETCH_PAGE_FAULT
				return pErr
			} else if err != nil {
				return err
			}

			if codeBuf.remaining == 0 {
				err = vm.getCode(codeBuffer[:], &codeBuf, 2)
				if err != nil {
					return err
				}

				codeBuf.remaining = 0

				// instruction is potentially half way between two pages?
				insn = uint32(codeBuf.peek2())
				if (insn & 3) == 3 {
					vm.pc += 2

					specialBuf := make([]byte, 4)

					insnHigh := &code{}

					// instruction is half way between two pages
					err := vm.getCode(specialBuf, insnHigh, 2)
					if err != nil {
						return err
					}

					vm.pc -= 2

					codeBuf.code = append(codeBuf.code[:2], insnHigh.code[:2]...)

					insn = uint32(codeBuf.peek4())
				}
			} else {
				insn = codeBuf.peek4()
			}
		} else {
			if codeBuf.remaining < 4 {
				insn = uint32(codeBuf.peek2())
				if (insn & 3) == 3 {
					// instruction is half way between two pages
					oldPhys := codeBuf.physicalBase
					oldRemaining := codeBuf.remaining
					err = vm.getCode(codeBuffer[:], &codeBuf, 4)
					if err != nil {
						return err
					}
					codeBuf.physicalBase = oldPhys
					codeBuf.remaining = oldRemaining

					insn = codeBuf.peek4()
				}
			} else {
				insn = codeBuf.peek4()
			}
		}

		cycles -= 1

		vm.clockCycles += 1

		if vm.logger != nil && ENABLE_LOGGING {
			printInsn := insn

			if codeBuf.remaining == 2 {
				printInsn = (printInsn & 0xffff)
			}

			vm.log("%d pcVirt=0x%016x insn=%08x priv=%d", vm.clockCycles, vm.pc, printInsn, vm.priv)
			for i := 0; i < 32; i++ {
				vm.log(" r%d=%016x", i, vm.reg[i])
			}
			vm.log(" mstatus=%x", vm.mstatus)
			vm.log(" satp=%x", vm.satp)
			vm.log(" remainingCode=%d", codeBuf.remaining)
			vm.log(" remainingCycles=%d", cycles)
			vm.log(" interupt=%x", vm.mip&vm.mie)
			vm.log(" mip=%x", vm.mip)
			vm.log("\n")
		}

		opcode := insn & 0x7f

		vm.totalCycles += 1
		if vm.maxCycles > 0 && vm.totalCycles > vm.maxCycles {
			return nil
		}

		var ok bool

		if stepFunc := stepJumpTable[opcode]; stepFunc != nil {
			ok, err = stepFunc(vm, &codeBuf, insn)
		} else {
			return fmt.Errorf("invalid instruction: pc= 0x%016X insn= 0x%08X op= 0x%02X", vm.pc, insn, opcode)
		}

		if err == ErrJump {
			codeBuf.code = nil
			continue outer
		} else if ok {
			continue outer
		} else {
			return err
		}
	}

	// doneInterpreting:

	return nil
}
