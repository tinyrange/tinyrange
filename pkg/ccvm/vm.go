package ccvm

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"runtime"
	"slices"
	"syscall"
	"time"
)

const ENABLE_LOGGING = false

//go:embed bbl64.bin
var RISCV_BIOS []byte

var (
	ErrFlushCode  = errors.New("flush code")
	ErrStopOnZero = errors.New("stop on zero")
)

type ErrIllegalInstruction struct {
	insn uint32
}

// Error implements error.
func (e ErrIllegalInstruction) Error() string {
	return fmt.Sprintf("illegal instruction: %08x", e.insn)
}

var (
	_ error = ErrIllegalInstruction{}
)

type ErrPageFault struct {
	cause    uint32
	addr     uint64
	physical bool
}

// Error implements error.
func (e ErrPageFault) Error() string {
	return fmt.Sprintf("page fault: 0x%08x", e.addr)
}

var (
	_ error = ErrPageFault{}
)

type ErrUserECall struct {
}

// Error implements error.
func (e ErrUserECall) Error() string {
	return "user ecall"
}

var (
	_ error = ErrUserECall{}
)

type ErrBreakpoint struct {
}

// Error implements error.
func (e ErrBreakpoint) Error() string {
	return "breakpoint"
}

var (
	_ error = ErrBreakpoint{}
)

type ErrInvalidInstruction struct {
	insn uint32
}

// Error implements error.
func (e ErrInvalidInstruction) Error() string {
	return fmt.Sprintf("invalid instruction: %08x", e.insn)
}

var (
	_ error = ErrInvalidInstruction{}
)

const (
	MCPUID_SUPER = (1 << ('S' - 'A'))
	MCPUID_USER  = (1 << ('U' - 'A'))
	MCPUID_I     = (1 << ('I' - 'A'))
	MCPUID_M     = (1 << ('M' - 'A'))
	MCPUID_A     = (1 << ('A' - 'A'))
	MCPUID_F     = (1 << ('F' - 'A'))
	MCPUID_D     = (1 << ('D' - 'A'))
	MCPUID_Q     = (1 << ('Q' - 'A'))
	MCPUID_C     = (1 << ('C' - 'A'))

	CAUSE_INTERRUPT = uint32(1) << 31

	PRV_U = 0
	PRV_S = 1
	PRV_H = 2
	PRV_M = 3

	MSTATUS_SPIE_SHIFT = 5
	MSTATUS_MPIE_SHIFT = 7
	MSTATUS_SPP_SHIFT  = 8
	MSTATUS_MPP_SHIFT  = 11
	MSTATUS_FS_SHIFT   = 13
	MSTATUS_UXL_SHIFT  = 32
	MSTATUS_SXL_SHIFT  = 34

	MSTATUS_UIE      uint64 = (1 << 0)
	MSTATUS_SIE      uint64 = (1 << 1)
	MSTATUS_HIE      uint64 = (1 << 2)
	MSTATUS_MIE      uint64 = (1 << 3)
	MSTATUS_UPIE     uint64 = (1 << 4)
	MSTATUS_SPIE     uint64 = (1 << MSTATUS_SPIE_SHIFT)
	MSTATUS_HPIE     uint64 = (1 << 6)
	MSTATUS_MPIE     uint64 = (1 << MSTATUS_MPIE_SHIFT)
	MSTATUS_SPP      uint64 = (1 << MSTATUS_SPP_SHIFT)
	MSTATUS_HPP      uint64 = (3 << 9)
	MSTATUS_MPP      uint64 = (3 << MSTATUS_MPP_SHIFT)
	MSTATUS_FS       uint64 = (3 << MSTATUS_FS_SHIFT)
	MSTATUS_XS       uint64 = (3 << 15)
	MSTATUS_MPRV     uint64 = (1 << 17)
	MSTATUS_SUM      uint64 = (1 << 18)
	MSTATUS_MXR      uint64 = (1 << 19)
	MSTATUS_UXL_MASK uint64 = (uint64(3) << MSTATUS_UXL_SHIFT)
	MSTATUS_SXL_MASK uint64 = (uint64(3) << MSTATUS_SXL_SHIFT)

	F32_HIGH = 0xffff_ffff
	F64_HIGH = 0
)

var CpuEndian = binary.LittleEndian

type code struct {
	code         []byte
	physicalBase uint64
	remaining    int64
	vm           *VirtualMachine
}

func (c *code) peek2() uint16 {
	return CpuEndian.Uint16(c.code[:2])
}

func (c *code) peek4() uint32 {
	if len(c.code) < 4 {
		return 0
	}

	return CpuEndian.Uint32(c.code[:4])
}

func (c *code) next2() {
	c.vm.pc += 2
	c.code = c.code[2:]
	c.remaining -= 2
	if c.remaining < 0 {
		c.remaining = 0
	}
}

func (c *code) next4() {
	c.vm.pc += 4
	c.code = c.code[4:]
	c.remaining -= 4
	if c.remaining < 0 {
		c.remaining = 0
	}
}

// Sign-extend a value
func sext(val int32, n int) int32 {
	return (val << (32 - n)) >> (32 - n)
}

func sext64(val int64, n int64) int64 {
	return (val << (64 - n)) >> (64 - n)
}

// Get a field from a bitfield
func getField1(val uint32, srcPos int, dstPos int, dstPosMax int) uint32 {
	if dstPosMax < dstPos {
		panic("dstPosMax must be greater than or equal to dstPos")
	}

	mask := ((1 << (dstPosMax - dstPos + 1)) - 1) << dstPos

	if dstPos >= srcPos {
		return (val << (dstPos - srcPos)) & uint32(mask)
	} else {
		return (val >> (srcPos - dstPos)) & uint32(mask)
	}
}

const (
	RAM_BASE    uint64 = 0x8000_0000
	PLIC_BASE   uint64 = 0x4010_0000
	PLIC_SIZE   uint64 = 0x0040_0000
	CLINT_BASE  uint64 = 0x0200_0000
	CLINT_SIZE  uint64 = 0x000c_0000
	VIRTIO_BASE uint64 = 0x4001_0000
	VIRTIO_SIZE uint64 = 0x0000_1000
	VIRTIO_IRQ         = 1
)

type rawRegion []byte

// Size implements memoryRegion.
func (r rawRegion) Size() int64 {
	return int64(len(r))
}

func (r rawRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off >= int64(len(r)) {
		return 0, fmt.Errorf("read out of bounds at off:%x size:%x", off, len(p))
	}
	n = copy(p, r[off:])
	if n < len(p) {
		err = fmt.Errorf("short read: %d < %d", n, len(p))
	}
	return
}

func (r rawRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off >= int64(len(r)) {
		return 0, fmt.Errorf("write out of bounds at off:%x size:%x", off, len(p))
	}
	n = copy(r[off:], p)
	if n < len(p) {
		err = fmt.Errorf("short read: %d < %d", n, len(p))
	}
	return
}

func (r rawRegion) getSlice(off int64, size int) ([]byte, error) {
	if off < 0 || off+int64(size) > int64(len(r)) {
		return nil, fmt.Errorf("getSlice out of bounds")
	}
	return r[off : off+int64(size)], nil
}

var (
	_ memoryRegion = rawRegion(nil)
)

type memoryMap struct {
	memoryRegion
	base     uint64
	readonly bool
}

type irq struct {
	vm *VirtualMachine

	id int

	setFunc func(irqNum int, state int)
}

func (i *irq) set(val int) error {
	i.setFunc(i.id, val)

	return nil
}

const (
	PG_SHIFT   uint64 = 12
	PTE_V_MASK        = (1 << 0)
	PTE_U_MASK        = (1 << 4)
	PTE_A_MASK        = (1 << 6)
	PTE_D_MASK        = (1 << 7)
)

type AccessMode uint8

const (
	ACCESS_READ AccessMode = iota
	ACCESS_WRITE
	ACCESS_CODE
)

const (
	TLB_SIZE = 256
)

type TLBEntry struct {
	vaddr uint64
	mMap  *memoryMap
	base  uint64
}

type VirtualMachine struct {
	logger io.Writer

	maps    []*memoryMap
	ramMap  *memoryMap
	ramSize uint64

	reg   [32]uint64
	fpReg [32]uint64
	pc    uint64

	fs   uint8
	priv uint8

	totalCycles uint64
	clockCycles uint64
	maxCycles   uint64
	skipCycles  uint64

	curXlen    uint64
	scounteren uint32
	satp       uint64

	lastTVal uint64

	stvec    uint64
	sscratch uint64
	sepc     uint64
	scause   uint64
	stval    uint64

	mhartid    uint64
	mscratch   uint64
	misa       uint64
	mxl        uint64
	mie        uint32
	mtvec      uint64
	mstatus    uint64
	mepc       uint64
	mcause     uint64
	mtval      uint64
	mideleg    uint32
	medeleg    uint32
	mip        uint32
	mcounteren uint32

	fflags uint32
	frm    uint8

	loadRes uint64

	plic           *plic
	plicPendingIrq uint32
	plicServedIrq  uint32

	clint   *clint
	timecmp uint64

	console              *virtio
	consoleResizePending bool
	inputBytes           chan byte

	virtio []*virtio

	stopOnZero bool

	tlbRead  [TLB_SIZE]TLBEntry
	tlbWrite [TLB_SIZE]TLBEntry
	tlbCode  [TLB_SIZE]TLBEntry

	tlbHits   uint64
	tlbMisses uint64
}

func (vm *VirtualMachine) rtcGetTime() uint64 {
	return vm.totalCycles / 16
}

func (vm *VirtualMachine) plicUpdateMip() {
	// vm.log("# plicUpdateMip: pending=%d served=%d\n", vm.plicPendingIrq, vm.plicServedIrq)

	mask := vm.plicPendingIrq & ^vm.plicServedIrq
	if mask != 0 {
		vm.cpuSetMip(MIP_MEIP | MIP_SEIP)
	} else {
		vm.cpuResetMip(MIP_MEIP | MIP_SEIP)
	}
}

func (vm *VirtualMachine) cpuSetMip(mask uint32) {
	vm.mip |= mask
	/* exit from power down if an interrupt is pending */
	// if (s->power_down_flag && (s->mip & s->mie) != 0)
	//     s->power_down_flag = FALSE;
}

func (vm *VirtualMachine) cpuResetMip(mask uint32) {
	vm.mip &= ^mask
}

func (vm *VirtualMachine) registerRam(base uint64, region memoryRegion) *memoryMap {
	m := &memoryMap{
		memoryRegion: region,
		base:         base,
	}
	vm.maps = append(vm.maps, m)

	slices.SortFunc(vm.maps, func(i, j *memoryMap) int {
		return int(i.base - j.base)
	})

	return m
}

func (vm *VirtualMachine) getMapAtPhysAddr(addr uint64) (*memoryMap, uint64, int64, error) {
	if addr > RAM_BASE && addr < RAM_BASE+vm.ramSize {
		return vm.ramMap, addr, int64(addr - RAM_BASE), nil
	}

	// Use binary search for faster lookup
	left, right := 0, len(vm.maps)-1
	for left <= right {
		mid := (left + right) / 2
		m := vm.maps[mid]
		if addr < m.base {
			right = mid - 1
		} else if addr >= m.base+uint64(m.Size()) {
			left = mid + 1
		} else {
			return m, addr, int64(addr - m.base), nil
		}
	}

	return nil, 0, 0, ErrPageFault{addr: addr, physical: true}
}

func (vm *VirtualMachine) translatePhysicalAddress(vaddr uint64, access AccessMode) (uint64, error) {
	var priv uint8

	if (vm.mstatus&MSTATUS_MPRV != 0) && access != ACCESS_CODE {
		// use previous privilege
		priv = uint8(vm.mstatus>>MSTATUS_MPP_SHIFT) & 3
	} else {
		priv = vm.priv
	}

	if vm.priv == PRV_M {
		return vaddr, nil
	}

	mode := (vm.satp >> 60) & 0xf
	if mode == 0 {
		/* bare: no translation */
		return vaddr, nil
	}

	levels := mode - 8 + 3
	pteSizeLog2 := uint64(3)
	vaddrShift := 64 - (PG_SHIFT + levels*9)
	if uint64((int64(vaddr)<<vaddrShift)>>vaddrShift) != vaddr {
		vm.ulog("# uint64((int64(vaddr)<<vaddrShift)>>vaddrShift) != vaddr\n")
		return 0, ErrPageFault{addr: vaddr}
	}
	pteAddrBits := 44

	pteAddr := (vm.satp & ((uint64(1) << pteAddrBits) - 1)) << PG_SHIFT
	pteBits := 12 - pteSizeLog2
	pteMask := uint64(1<<pteBits) - 1
	for i := uint64(0); i < levels; i++ {
		vaddrShift = PG_SHIFT + pteBits*(levels-1-i)
		pteIdx := (vaddr >> vaddrShift) & pteMask
		pteAddr += pteIdx << pteSizeLog2
		pte, err := vm.readPhysU64(pteAddr)
		if err != nil {
			return 0, fmt.Errorf("read pte failed: %w", err)
		}
		// vm.log("pte=%x\n", pte)
		if (pte & PTE_V_MASK) == 0 {
			vm.ulog("# (pte & PTE_V_MASK) == 0 (pteAddr=%x, pte=%x)\n", pteAddr, pte)
			return 0, ErrPageFault{addr: vaddr}
		}
		paddr := (pte >> 10) << PG_SHIFT
		xwr := (pte >> 1) & 7
		if xwr != 0 {
			if xwr == 2 || xwr == 6 {
				vm.ulog("# xwr == 2 || xwr == 6\n")
				return 0, ErrPageFault{addr: vaddr}
			}
			// privilege check
			if priv == PRV_S {
				if (pte&PTE_U_MASK) != 0 && (vm.mstatus&MSTATUS_SUM) == 0 {
					vm.ulog("# (pte&PTE_U_MASK) != 0 && (vm.mstatus&MSTATUS_SUM) == 0\n")
					return 0, ErrPageFault{addr: vaddr}
				}
			} else {
				if (pte & PTE_U_MASK) == 0 {
					vm.ulog("# (pte & PTE_U_MASK) == 0\n")
					return 0, ErrPageFault{addr: vaddr}
				}
			}
			// protection check
			// MXR allows read access to execute-only pages
			if (vm.mstatus & MSTATUS_MXR) != 0 {
				xwr |= (xwr >> 2)
			}

			if ((xwr >> access) & 1) == 0 {
				vm.ulog("# ((xwr >> access) & 1) == 0\n")
				return 0, ErrPageFault{addr: vaddr}
			}
			need_write := (pte&PTE_A_MASK) == 0 ||
				((pte&PTE_D_MASK) == 0 && access == ACCESS_WRITE)
			pte |= PTE_A_MASK
			if access == ACCESS_WRITE {
				pte |= PTE_D_MASK
			}
			if need_write {
				if pteSizeLog2 == 2 {
					// phys_write_u32(pte_addr, pte)
				} else {
					// phys_write_u64(pte_addr, pte)
				}
			}
			vaddr_mask := (uint64(1) << vaddrShift) - 1
			return (vaddr & vaddr_mask) | (paddr & ^vaddr_mask), nil
		} else {
			pteAddr = paddr
		}
	}

	vm.ulog("# default\n")
	return 0, ErrPageFault{addr: vaddr}
}

var physU64 [8]byte

func (vm *VirtualMachine) readPhysU64(addr uint64) (uint64, error) {
	m, _, vAddr, err := vm.getMapAtPhysAddr(addr)
	if err != nil {
		return 0, err
	}

	if _, err := m.ReadAt(physU64[:], vAddr); err != nil {
		return 0, err
	}

	value := CpuEndian.Uint64(physU64[:])

	return value, nil
}

func (vm *VirtualMachine) getTlb(addr uint64, length int, access AccessMode) (*memoryMap, uint64, uint64, bool) {
	tlbIdx := (addr >> PG_SHIFT) & (TLB_SIZE - 1)

	expected := (addr & ^(PG_MASK & ^((uint64(length) / 8) - 1)))

	switch access {
	case ACCESS_READ:
		if vm.tlbRead[tlbIdx].vaddr == expected {
			return vm.tlbRead[tlbIdx].mMap, addr, vm.tlbRead[tlbIdx].base, true
		}
	case ACCESS_WRITE:
		if vm.tlbWrite[tlbIdx].vaddr == expected {
			return vm.tlbWrite[tlbIdx].mMap, addr, vm.tlbWrite[tlbIdx].base, true
		}
	case ACCESS_CODE:
		if vm.tlbCode[tlbIdx].vaddr == expected {
			return vm.tlbCode[tlbIdx].mMap, addr, vm.tlbCode[tlbIdx].base, true
		}
	}

	return nil, 0, 0, false
}

func (vm *VirtualMachine) updateTlb(addr uint64, virtBase uint64, access AccessMode, mmap *memoryMap) {
	tlbIdx := (addr >> PG_SHIFT) & (TLB_SIZE - 1)

	switch access {
	case ACCESS_READ:
		vm.tlbRead[tlbIdx].vaddr = addr & ^PG_MASK
		vm.tlbRead[tlbIdx].base = virtBase
		vm.tlbRead[tlbIdx].mMap = mmap
	case ACCESS_WRITE:
		vm.tlbWrite[tlbIdx].vaddr = addr & ^PG_MASK
		vm.tlbWrite[tlbIdx].base = virtBase
		vm.tlbWrite[tlbIdx].mMap = mmap
	case ACCESS_CODE:
		vm.tlbCode[tlbIdx].vaddr = addr & ^PG_MASK
		vm.tlbCode[tlbIdx].base = virtBase
		vm.tlbCode[tlbIdx].mMap = mmap
	}
}

func (vm *VirtualMachine) getMap(addr uint64, length int, access AccessMode) (*memoryMap, uint64, int64, error) {
	var virtOffset int64

	physMap, virt, virtBase, ok := vm.getTlb(addr, length, access)
	if !ok {
		physAddr, err := vm.translatePhysicalAddress(addr, access)
		if err != nil {
			switch access {
			case ACCESS_READ:
				vm.log("get_phys_addr failed in read\n")
			case ACCESS_WRITE:
				vm.log("get_phys_addr failed in write\n")
			case ACCESS_CODE:
				vm.log("get_phys_addr failed in code\n")
			}
			return nil, 0, 9, err
		}

		physMap, virt, virtOffset, err = vm.getMapAtPhysAddr(physAddr)
		if err != nil {
			return nil, 0, 0, err
		}

		// TLB only affects raw memory regions
		if _, ok := physMap.memoryRegion.(rawRegion); ok {
			vm.updateTlb(addr, addr-uint64(virtOffset), access, physMap)
			// vm.log("# tlb miss virt=%x virtOffset=%x\n", virt, virtOffset)
		}
		vm.tlbMisses += 1
		// vm.log("# tlb miss addr=%x virtOffset=%x\n", addr, virtOffset)
	} else {
		virtOffset = int64(addr - virtBase)
		// vm.log("# tlb hit addr=%x virtOffset=%x addr=%x mapBase=%x\n", addr, virtOffset, addr, virtBase)
		vm.tlbHits += 1
		if virtOffset < 0 {
			return nil, 0, 0, fmt.Errorf("virtOffset < 0")
		}
	}

	if vm.totalCycles == 38214730 {
		vm.log("# getMap: addr=%x length=%x access=%d physMap=%T\n", addr, length, access, physMap.memoryRegion)
	}

	return physMap, virt, virtOffset, nil
}

func (vm *VirtualMachine) log(fmtS string, args ...any) {
	if !ENABLE_LOGGING {
		return
	}

	if vm.logger == nil {
		return
	}

	if vm.totalCycles < vm.skipCycles {
		return
	}

	fmt.Fprintf(vm.logger, fmtS, args...)
}

func (vm *VirtualMachine) ulog(fmtS string, args ...any) {
	if vm.logger == nil {
		return
	}

	fmt.Fprintf(vm.logger, fmtS, args...)
}

func (vm *VirtualMachine) copyTo(src []byte, off uint64) (int, error) {
	m, _, vAddr, err := vm.getMap(off, len(src), ACCESS_WRITE)
	if err != nil {
		if pf, ok := err.(ErrPageFault); ok {
			pf.cause = CAUSE_FAULT_STORE
			return -1, pf
		}

		return -1, err
	}

	if m.readonly {
		return -1, fmt.Errorf("copy to readonly memory")
	}

	return m.WriteAt(src, vAddr)
}

func (vm *VirtualMachine) readAt(p []byte, off uint64) (int, uint64, error) {
	m, pAddr, vAddr, err := vm.getMap(off, len(p), ACCESS_READ)
	if err != nil {
		if pf, ok := err.(ErrPageFault); ok {
			pf.cause = CAUSE_LOAD_PAGE_FAULT
			return -1, 0, pf
		}

		return -1, 0, err
	}

	n, err := m.ReadAt(p, vAddr)
	return n, pAddr, err
}

func (vm *VirtualMachine) writeAt(p []byte, off uint64) (int, uint64, error) {
	m, pAddr, vAddr, err := vm.getMap(off, len(p), ACCESS_WRITE)
	if err != nil {
		if pf, ok := err.(ErrPageFault); ok {
			pf.cause = CAUSE_STORE_PAGE_FAULT
			return -1, 0, pf
		}

		return -1, 0, err
	}

	if m.readonly {
		return -1, 0, fmt.Errorf("copy to readonly memory")
	}

	n, err := m.WriteAt(p, vAddr)
	return n, pAddr, err
}

const (
	PG_MASK uint64 = ((1 << PG_SHIFT) - 1)
)

func (vm *VirtualMachine) getCode(buf []byte, code *code, size uint64) error {
	var addr = vm.pc

	userSize := true

	if size == 0 {
		size = (PG_MASK - 1 - (addr & PG_MASK))
		userSize = false
	}

	changedSize := false

	// we don't handle unaligned access the same way as TinyEMU
	if size == 2 {
		size = 4
		changedSize = true
	}

	if len(buf) < int(size) {
		return fmt.Errorf("buf is too small")
	}

	code.vm = vm
	code.code = buf
	code.remaining = int64(size)

	if changedSize {
		code.remaining -= 2
	}

	m, pAddr, vAddr, err := vm.getMap(addr, len(code.code), ACCESS_CODE)
	if pErr, ok := err.(ErrPageFault); ok {
		pErr.cause = CAUSE_FETCH_PAGE_FAULT
		return pErr
	} else if err != nil {
		return fmt.Errorf("get map failed: %w", err)
	}

	code.physicalBase = pAddr

	var n int
	if raw, ok := m.memoryRegion.(rawRegion); ok {
		code.code, err = raw.getSlice(vAddr, len(code.code))
		n = int(size)
	} else {
		n, err = m.ReadAt(code.code, vAddr)
	}
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}
	if n < int(size) {
		// maybe an unaligned read.
		return fmt.Errorf("unaligned read")
	}

	if !userSize {
		if changedSize {
			vm.log("get_code(%x, %d)\n", addr, size-2)
		} else {
			vm.log("get_code(%x, %d)\n", addr, size)
		}
	}

	return nil
}

func (vm *VirtualMachine) getMstatus(mask uint64) uint64 {
	// target_ulong val;
	// BOOL sd;
	vm.log("mstatus mask=%x", mask)
	val := vm.mstatus | (uint64(vm.fs) << 13)
	vm.log(" fs=%d", vm.fs)
	val &= mask
	vm.log(" val=%x", val)
	sd := ((val & (3 << 13)) == (3 << 13)) ||
		((val & (3 << 15)) == (3 << 15))
	if sd {
		vm.log(" sd")
		val |= uint64(1) << (vm.curXlen - 1)
	}
	vm.log(" result=%x\n", val)
	return val
}

func (vm *VirtualMachine) setMstatus(val uint64) {
	var mask uint64

	mod := vm.mstatus ^ val
	if (mod&((1<<17)|(1<<18)|(1<<19))) != 0 ||
		((vm.mstatus&(1<<17)) != 0 && ((mod & (3 << 11)) != 0)) {
		vm.tlbFlushAll()
	}
	vm.fs = uint8((val >> 13) & 3)

	mask = ((1 << 0) | (1 << 1) | (1 << 3) | (1 << 4) | (1 << 5) | (1 << 7) | (1 << 8) | (3 << 11) | (3 << 13) | (1 << 17) | (1 << 18) | (1 << 19)) & ^(3 << 13)

	uxl := (val >> 32) & 3
	if uxl >= 1 && uxl <= 2 {
		mask |= (uint64(3) << 32)
	}
	sxl := (val >> 32) & 3
	if sxl >= 1 && sxl <= 2 {
		mask |= (uint64(3) << 34)
	}

	vm.mstatus = (vm.mstatus & ^mask) | (val & mask)
}

// cycle and insn counters
const COUNTEREN_MASK = ((1 << 0) | (1 << 2))

const (
	MIP_USIP = (1 << 0)
	MIP_SSIP = (1 << 1)
	MIP_HSIP = (1 << 2)
	MIP_MSIP = (1 << 3)
	MIP_UTIP = (1 << 4)
	MIP_STIP = (1 << 5)
	MIP_HTIP = (1 << 6)
	MIP_MTIP = (1 << 7)
	MIP_UEIP = (1 << 8)
	MIP_SEIP = (1 << 9)
	MIP_HEIP = (1 << 10)
	MIP_MEIP = (1 << 11)
)

const (
	CAUSE_MISALIGNED_FETCH    = 0x0
	CAUSE_FAULT_FETCH         = 0x1
	CAUSE_ILLEGAL_INSTRUCTION = 0x2
	CAUSE_BREAKPOINT          = 0x3
	CAUSE_MISALIGNED_LOAD     = 0x4
	CAUSE_FAULT_LOAD          = 0x5
	CAUSE_MISALIGNED_STORE    = 0x6
	CAUSE_FAULT_STORE         = 0x7
	CAUSE_USER_ECALL          = 0x8
	CAUSE_SUPERVISOR_ECALL    = 0x9
	CAUSE_HYPERVISOR_ECALL    = 0xa
	CAUSE_MACHINE_ECALL       = 0xb
	CAUSE_FETCH_PAGE_FAULT    = 0xc
	CAUSE_LOAD_PAGE_FAULT     = 0xd
	CAUSE_STORE_PAGE_FAULT    = 0xf
)

func (vm *VirtualMachine) setFrm(val uint32) {
	if val >= 5 {
		val = 0
	}
	vm.frm = uint8(val)
}

const (
	SSTATUS_MASK0 = (MSTATUS_UIE | MSTATUS_SIE |
		MSTATUS_UPIE | MSTATUS_SPIE |
		MSTATUS_SPP |
		MSTATUS_FS | MSTATUS_XS |
		MSTATUS_SUM | MSTATUS_MXR)
	SSTATUS_MASK = (SSTATUS_MASK0 | MSTATUS_UXL_MASK)
)

func (vm *VirtualMachine) tlbInit() {
	vm.log("tlb_init\n")

	for i := 0; i < TLB_SIZE; i++ {
		vm.tlbRead[i].vaddr = math.MaxUint64
		vm.tlbWrite[i].vaddr = math.MaxUint64
		vm.tlbCode[i].vaddr = math.MaxUint64
	}
}

func (vm *VirtualMachine) tlbFlushAll() {
	vm.tlbInit()
}

func (vm *VirtualMachine) tlbFlushVaddr(addr uint64) {
	vm.tlbFlushAll()
}

func (vm *VirtualMachine) csrRead(csr uint32, willWrite bool) (uint64, error) {
	switch csr {
	case 0x003:
		if vm.fs == 0 {
			return 0, fmt.Errorf("vm.fs == 0")
		}

		return uint64(vm.fflags | uint32(vm.frm<<5)), nil
	case 0x100:
		return vm.getMstatus(SSTATUS_MASK), nil
	case 0x104: // sie
		return uint64(vm.mie & vm.mideleg), nil
	case 0x105: // stvec
		return vm.stvec, nil
	case 0x106:
		return uint64(vm.scounteren), nil
	case 0x140: // sscratch
		return vm.sscratch, nil
	case 0x141: // sepc
		return vm.sepc, nil
	case 0x142: // scause
		return vm.scause, nil
	case 0x143: // stval
		return vm.stval, nil
	case 0x180:
		return vm.satp, nil
	case 0x300:
		val := vm.getMstatus(^uint64(0))
		return val, nil
	case 0x301:
		val := vm.misa
		val |= uint64(vm.mxl) << (vm.curXlen - 2)
		vm.log("mxl=%x misa=%x cur_xlen=%d val=%x\n", vm.mxl, vm.misa, vm.curXlen, val)

		return val, nil
	case 0x302:
		return uint64(vm.medeleg), nil
	case 0x303:
		return uint64(vm.mideleg), nil
	case 0x304:
		return uint64(vm.mie), nil
	case 0x305:
		return vm.mtvec, nil
	case 0x306:
		return uint64(vm.mcounteren), nil
	case 0x340:
		return vm.mscratch, nil
	case 0x341:
		return vm.mepc, nil
	case 0x342: // mcause
		return vm.mcause, nil
	case 0x343: // mtval
		return vm.mtval, nil
	case 0x344:
		return uint64(vm.mip), nil
	case 0xc01: // time
		return vm.rtcGetTime(), nil
	case 0xf14:
		return vm.mhartid, nil
	default:
		vm.log("# [warn:%d] invalid csr read: 0x%x\n", vm.totalCycles, csr)
		return 0, ErrIllegalInstruction{}
	}
}

func (vm *VirtualMachine) csrWrite(csr uint32, value uint64) error {
	switch csr {
	case 0x003:
		vm.setFrm((uint32(value) >> 5) & 7)
		vm.fflags = uint32(value & 0x1f)
		vm.fs = 3
	case 0x100:
		vm.setMstatus((vm.mstatus & ^SSTATUS_MASK) | (value & SSTATUS_MASK))
	case 0x104: // sie
		mask := vm.mideleg
		vm.mie = (vm.mie & ^mask) | (uint32(value) & mask)
	case 0x105:
		vm.stvec = value & ^uint64(3)
	case 0x106:
		vm.scounteren = uint32(value) & COUNTEREN_MASK
	case 0x140: // sscratch
		vm.sscratch = value
	case 0x141:
		vm.sepc = value & 0xffffffff_fffffffe
	case 0x142: // scause
		vm.scause = value
	case 0x143: // stval
		vm.stval = value
	case 0x180:
		mode := vm.satp >> 60
		new_mode := (value >> 60) & 0xf
		if new_mode == 0 || (new_mode >= 8 && new_mode <= 9) {
			mode = new_mode
		}
		vm.satp = (value & ((uint64(1) << 44) - 1)) |
			(uint64(mode) << 60)

		vm.tlbFlushAll()
		return ErrFlushCode
	case 0x300:
		vm.setMstatus(value)
	case 0x302:
		var mask uint32 = (1 << (CAUSE_STORE_PAGE_FAULT + 1)) - 1
		vm.medeleg = (vm.medeleg & ^mask) | (uint32(value) & mask)
	case 0x303:
		var mask uint32 = MIP_SSIP | MIP_STIP | MIP_SEIP
		vm.mideleg = (vm.mideleg & ^mask) | (uint32(value) & mask)
	case 0x304:
		var mask uint32 = MIP_MSIP | MIP_MTIP | MIP_SSIP | MIP_STIP | MIP_SEIP
		vm.mie = (vm.mie & ^mask) | (uint32(value) & mask)
	case 0x305:
		vm.mtvec = value & ^uint64(3)
	case 0x306:
		vm.mcounteren = uint32(value & COUNTEREN_MASK)
	case 0x340:
		vm.mscratch = value
	case 0x341:
		vm.mepc = value & ^uint64(1)
	case 0x342: // mcause
		vm.mcause = value
	case 0x343: // mtval
		vm.mtval = value
	case 0x344:
		mask := vm.mideleg
		vm.mip = (vm.mip & ^mask) | (uint32(value) & mask)
	default:
		return fmt.Errorf("invalid csr write: 0x%x", csr)
	}

	return nil
}

var u8Buf [1]byte
var u16Buf [2]byte
var u32Buf [4]byte
var u64Buf [8]byte

func (vm *VirtualMachine) readU8(addr uint64) (uint8, error) {
	_, _, err := vm.readAt(u8Buf[:], addr)
	if err != nil {
		vm.log("read(%x)=0\n", addr)
		return 0, err
	}

	value := u8Buf[0]

	vm.log("read(%x)=%x\n", addr, value)

	return value, nil
}

func (vm *VirtualMachine) readU16(addr uint64) (uint16, error) {
	_, _, err := vm.readAt(u16Buf[:], addr)
	if err != nil {
		vm.log("read(%x)=0\n", addr)
		return 0, err
	}

	value := CpuEndian.Uint16(u16Buf[:])

	vm.log("read(%x)=%x\n", addr, value)

	return value, nil
}

func (vm *VirtualMachine) readU32(addr uint64) (uint32, error) {
	_, _, err := vm.readAt(u32Buf[:], addr)
	if err != nil {
		vm.log("read(%x)=0\n", addr)
		return 0, err
	}

	value := CpuEndian.Uint32(u32Buf[:])

	vm.log("read(%x)=%x\n", addr, value)

	return value, nil
}

func (vm *VirtualMachine) readU64(addr uint64) (uint64, error) {
	_, _, err := vm.readAt(u64Buf[:], addr)
	if err != nil {
		vm.log("read(%x)=0\n", addr)
		return 0, err
	}

	value := CpuEndian.Uint64(u64Buf[:])

	vm.log("read(%x)=%x\n", addr, value)

	return value, nil
}

func (vm *VirtualMachine) writeU8(addr uint64, value uint8) error {
	vm.log("write(%x)=%x\n", addr, value)

	_, _, err := vm.writeAt([]byte{value}, addr)
	if err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachine) writeU16(addr uint64, value uint16) error {
	CpuEndian.PutUint16(u16Buf[:], value)

	vm.log("write(%x)=%x\n", addr, value)

	_, _, err := vm.writeAt(u16Buf[:], addr)
	if err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachine) writeU32(addr uint64, value uint32) error {
	CpuEndian.PutUint32(u32Buf[:], value)

	vm.log("write(%x)=%x\n", addr, value)

	_, _, err := vm.writeAt(u32Buf[:], addr)
	if err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachine) writeU64(addr uint64, value uint64) error {
	CpuEndian.PutUint64(u64Buf[:], value)

	vm.log("write(%x)=%x\n", addr, value)

	_, _, err := vm.writeAt(u64Buf[:], addr)
	if err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachine) setPriv(priv uint8) {
	if vm.priv != priv {
		vm.tlbFlushAll()

		// int mxl;
		// if (priv == PRV_S)
		//     mxl = (s->mstatus >> MSTATUS_SXL_SHIFT) & 3;
		// else if (priv == PRV_U)
		//     mxl = (s->mstatus >> MSTATUS_UXL_SHIFT) & 3;
		// else
		//     mxl = s->mxl;
		// s->cur_xlen = 1 << (4 + mxl);

		vm.priv = priv
	}
}

func (vm *VirtualMachine) raiseException2(cause uint32, tVal uint64) {
	deleg := false

	vm.log("raise_exception2 cause=%x tval=%x", cause, tVal)

	vm.lastTVal = tVal

	if vm.priv <= PRV_S {
		// delegate the exception to the supervisor priviledge
		if uint32(cause)&CAUSE_INTERRUPT != 0 {
			deleg = (vm.mideleg>>(cause&(64-1)))&1 != 0
		} else {
			deleg = (vm.medeleg>>cause)&1 != 0
		}
	} else {
		deleg = false
	}

	var causel uint64 = uint64(cause) & 0x7fffffff
	if uint32(cause)&CAUSE_INTERRUPT != 0 {
		causel |= uint64(1) << (vm.curXlen - 1)
	}

	if deleg {
		vm.log(" delag\n")
		vm.scause = causel
		vm.sepc = vm.pc
		vm.stval = tVal
		vm.mstatus = (vm.mstatus & ^MSTATUS_SPIE) |
			(((vm.mstatus >> vm.priv) & 1) << MSTATUS_SPIE_SHIFT)
		vm.mstatus = (vm.mstatus & ^MSTATUS_SPP) |
			(uint64(vm.priv) << MSTATUS_SPP_SHIFT)
		vm.mstatus &= ^MSTATUS_SIE
		vm.setPriv(PRV_S)
		vm.pc = vm.stvec
	} else {
		vm.log("\n")
		vm.mcause = causel
		vm.mepc = vm.pc
		vm.mtval = tVal
		vm.mstatus = (vm.mstatus & ^MSTATUS_MPIE) |
			(((vm.mstatus >> vm.priv) & 1) << MSTATUS_MPIE_SHIFT)
		vm.mstatus = (vm.mstatus & ^MSTATUS_MPP) |
			(uint64(vm.priv) << MSTATUS_MPP_SHIFT)
		vm.mstatus &= ^MSTATUS_MIE
		vm.setPriv(PRV_M)
		vm.pc = vm.mtvec
	}
}

func (vm *VirtualMachine) handleMret() {
	vm.log("handle_mret\n")
	mpp := (vm.mstatus >> MSTATUS_MPP_SHIFT) & 3
	// set the IE state to previous IE state
	mpie := (vm.mstatus >> MSTATUS_MPIE_SHIFT) & 1
	vm.mstatus = (vm.mstatus & ^(1 << mpp)) |
		(mpie << mpp)
	// set MPIE to 1
	vm.mstatus |= MSTATUS_MPIE
	// set MPP to U
	vm.mstatus &= ^MSTATUS_MPP
	vm.setPriv(uint8(mpp))
	vm.pc = vm.mepc
}

func (vm *VirtualMachine) handleSret() {
	vm.log("handle_sret\n")
	spp := (vm.mstatus >> MSTATUS_SPP_SHIFT) & 1
	// set the IE state to previous IE state
	spie := (vm.mstatus >> MSTATUS_SPIE_SHIFT) & 1
	vm.mstatus = (vm.mstatus & ^(1 << spp)) |
		(spie << spp)
	// set SPIE to 1
	vm.mstatus |= MSTATUS_SPIE
	// set SPP to U
	vm.mstatus &= ^MSTATUS_SPP
	vm.setPriv(uint8(spp))
	vm.pc = vm.sepc
}

func (vm *VirtualMachine) getPendingIrqMask() uint32 {
	pending_ints := vm.mip & vm.mie
	if pending_ints == 0 {
		return 0
	}

	var enabled_ints int = 0
	switch vm.priv {
	case PRV_M:
		if (vm.mstatus & MSTATUS_MIE) != 0 {
			enabled_ints = int(^vm.mideleg)
		}
	case PRV_S:
		enabled_ints = int(^vm.mideleg)
		if (vm.mstatus & MSTATUS_SIE) != 0 {
			enabled_ints |= int(vm.mideleg)
		}
	case PRV_U:
		enabled_ints = -1
	default:
		enabled_ints = -1
	}

	return pending_ints & uint32(enabled_ints)
}

func (vm *VirtualMachine) raiseInterrupt() (bool, error) {
	vm.log("raise_interrupt s->mip=%x s->mie=%x s->mip&s->mie=%d\n", vm.mip, vm.mie, vm.mip&vm.mie)

	mask := vm.getPendingIrqMask()
	if mask == 0 {
		return false, nil
	}

	irq_num := ctz32(mask)
	vm.raiseException2(irq_num|CAUSE_INTERRUPT, 0)

	return true, nil
}

const RTC_FREQ = 10000000

func (vm *VirtualMachine) getSleepDuration(delay int) int {
	// wait for an event: the only asynchronous event is the RTC timer
	if (vm.mip & MIP_MTIP) == 0 {
		var delay1 int64 = int64(vm.timecmp - vm.rtcGetTime())
		// vm.log("# sleep: timecmp=%d delay1=%d\n", vm.timecmp, delay1)
		if delay1 <= 0 {
			vm.cpuSetMip(MIP_MTIP)
			delay = 0
		} else {
			/* convert delay to ms */
			delay1 = delay1 / (RTC_FREQ / 1000)
			if int(delay1) < delay {
				delay = int(delay1)
			}
		}
	}

	if !false {
		delay = 0
	}

	return delay
}

func (vm *VirtualMachine) Step(cycles int64) error {
	sleepDuration := vm.getSleepDuration(10)

	canWrite, err := vm.consoleCanWrite()
	if err != nil {
		return err
	}
	if canWrite {
		if vm.consoleResizePending {
			if err := vm.consoleResize(80, 50); err != nil {
				return err
			}

			vm.consoleResizePending = false
		}

		writeLen, err := vm.consoleGetWriteLen()
		if err != nil {
			return err
		}
		if writeLen > 0 {
			buf := make([]byte, writeLen)
			// read from vm.inputBytes until buf is filled.
			i := 0
		outer:
			for ; i < writeLen; i++ {
				// non-blocking read fron vm.inputBytes
				select {
				case b := <-vm.inputBytes:
					buf[i] = b
				default:
					break outer
				}
			}

			if i > 0 {
				buf = buf[:i]

				if err := vm.consoleWriteData(buf); err != nil {
					return err
				}
			}
		}
	}

	_ = sleepDuration

	timeout := vm.clockCycles + uint64(cycles)
	for {
		if int(timeout-vm.clockCycles) <= 0 {
			return nil
		}

		nCycles := timeout - vm.clockCycles
		err := vm.step(int64(nCycles))
		if iErr, ok := err.(ErrIllegalInstruction); ok {
			vm.raiseException2(CAUSE_ILLEGAL_INSTRUCTION, uint64(iErr.insn))
		} else if _, ok := err.(ErrUserECall); ok {
			vm.raiseException2(uint32(CAUSE_USER_ECALL+vm.priv), vm.lastTVal)
		} else if _, ok := err.(ErrBreakpoint); ok {
			vm.raiseException2(CAUSE_BREAKPOINT, vm.pc)
		} else if _, ok := err.(ErrInvalidInstruction); ok {
			vm.raiseException2(CAUSE_ILLEGAL_INSTRUCTION, vm.pc)
		} else if pErr, ok := err.(ErrPageFault); ok {
			if pErr.cause == 0 {
				return fmt.Errorf("page fault without cause")
			}
			oldPc := vm.pc
			vm.raiseException2(pErr.cause, pErr.addr)
			if vm.pc == oldPc {
				return fmt.Errorf("page fault did not change PC (triple fault?)")
			}
		} else if err != nil {
			return err
		}
	}
}

func (vm *VirtualMachine) buildFdt(
	cmdLine string,
	kernelStart uint64, kernelSize uint64,
) ([]byte, error) {
	s := newFdt()

	var cur_phandle = 1

	s.beginNode("")
	s.propU32("#address-cells", 2)
	s.propU32("#size-cells", 2)
	s.propStr("compatible", "ucbbar,riscvemu-bar_dev")
	s.propStr("model", "ucbbar,riscvemu-bare")

	/* CPU list */
	s.beginNode("cpus")
	s.propU32("#address-cells", 1)
	s.propU32("#size-cells", 0)
	s.propU32("timebase-frequency", RTC_FREQ)

	/* cpu */
	s.beginNodeNum("cpu", 0)
	s.propStr("device_type", "cpu")
	s.propU32("reg", 0)
	s.propStr("status", "okay")
	s.propStr("compatible", "riscv")

	isaString := "rv64"
	for i := 0; i < 26; i++ {
		if vm.misa&(1<<i) != 0 {
			isaString += string('a' + i)
		}
	}
	s.propStr("riscv,isa", isaString)

	s.propStr("mmu-type", "riscv,sv48")
	s.propU32("clock-frequency", 2000000000)

	s.beginNode("interrupt-controller")
	s.propU32("#interrupt-cells", 1)
	s.prop("interrupt-controller", []byte{})
	s.propStr("compatible", "riscv,cpu-intc")
	intc_phandle := cur_phandle
	s.propU32("phandle", uint32(intc_phandle))
	cur_phandle += 1
	s.endNode() /* interrupt-controller */

	s.endNode() /* cpu */

	s.endNode() /* cpus */

	s.beginNodeNum("memory", RAM_BASE)
	s.propStr("device_type", "memory")
	s.propTabU32("reg", []uint32{
		uint32(RAM_BASE >> 32),
		uint32(RAM_BASE),
		uint32(vm.ramSize >> 32),
		uint32(vm.ramSize),
	})

	s.endNode() /* memory */

	s.beginNode("htif")
	s.propStr("compatible", "ucb,htif0")
	s.endNode() /* htif */

	s.beginNode("soc")
	s.propU32("#address-cells", 2)
	s.propU32("#size-cells", 2)
	s.propTabStr("compatible", "ucbbar,riscvemu-bar-soc", "simple-bus")
	s.prop("ranges", []byte{})

	s.beginNodeNum("clint", CLINT_BASE)
	s.propStr("compatible", "riscv,clint0")

	s.propTabU32("interrupts-extended", []uint32{
		uint32(intc_phandle),
		3, // M IPI irq
		uint32(intc_phandle),
		7, // M timer irq
	})

	s.propTabU64_2("reg", CLINT_BASE, CLINT_SIZE)

	s.endNode() /* clint */

	s.beginNodeNum("plic", PLIC_BASE)
	s.propU32("#interrupt-cells", 1)
	s.prop("interrupt-controller", []byte{})
	s.propStr("compatible", "riscv,plic0")
	s.propU32("riscv,ndev", 31)
	s.propTabU64_2("reg", PLIC_BASE, PLIC_SIZE)

	s.propTabU32("interrupts-extended", []uint32{
		uint32(intc_phandle),
		9, // S ext irq
		uint32(intc_phandle),
		11, // M ext irq
	})

	plic_phandle := cur_phandle
	s.propU32("phandle", uint32(plic_phandle))
	cur_phandle += 1

	s.endNode() /* plic */

	for i := 0; i < len(vm.virtio); i++ {
		s.beginNodeNum("virtio", VIRTIO_BASE+uint64(i)*VIRTIO_SIZE)
		s.propStr("compatible", "virtio,mmio")
		s.propTabU64_2("reg", VIRTIO_BASE+uint64(i)*VIRTIO_SIZE, VIRTIO_SIZE)
		s.propTabU32("interrupts-extended", []uint32{
			uint32(plic_phandle),
			VIRTIO_IRQ + uint32(i),
		})
		s.endNode() /* virtio */
	}

	s.endNode() /* soc */

	s.beginNode("chosen")
	s.propStr("bootargs", cmdLine)
	if kernelSize > 0 {
		s.propTabU64("riscv,kernel-start", kernelStart)
		s.propTabU64("riscv,kernel-end", kernelStart+kernelSize)
	}
	// if s.initrd_size > 0 {
	// 	s.fdt_prop_tab_u64(s, "linux,initrd-start", initrd_start)
	// 	s.fdt_prop_tab_u64(s, "linux,initrd-end", initrd_start+initrd_size)
	// }

	s.endNode() /* chosen */

	s.endNode() /* / */

	return s.output()
}

func instrToBytes(instr []uint32) []byte {
	ret := make([]byte, len(instr)*4)

	for i := 0; i < len(instr); i++ {
		CpuEndian.PutUint32(ret[i*4:(i+1)*4], instr[i])
	}

	return ret
}

type memoryRegion interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}

//go:embed linux.bin
var LINUX_KERNEL []byte

//go:embed disk.img
var DISK_IMAGE []byte

func RunVirtualMachine(memSize uint64, diskContents []byte, kernelContents []byte, kernelCmdline string) error {
	var logger io.Writer = nil
	if ENABLE_LOGGING {
		logger = os.Stdout
	}

	vm := &VirtualMachine{
		logger:  logger,
		pc:      0x1000,
		priv:    PRV_M,
		misa:    MCPUID_SUPER | MCPUID_USER | MCPUID_I | MCPUID_M | MCPUID_A | MCPUID_F | MCPUID_D | MCPUID_C,
		curXlen: 64,
		mxl:     2,
		mstatus: 0xa00000000,
		ramSize: memSize,
	}

	vm.plic = &plic{vm: vm}

	vm.clint = &clint{vm: vm}

	// if !ENABLE_LOGGING && runtime.GOOS != "wasip1" {
	// 	// set the stdin to raw mode.
	// 	oldState, err := term.MakeRaw(0)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	defer term.Restore(0, oldState)
	// }

	virtConsole := &virtioConsole{}
	vm.console = newVirtio(vm, virtConsole, vm.plic.getIrq())
	vm.console.queues[0].manualRecv = true
	vm.consoleResizePending = true
	vm.inputBytes = make(chan byte, 32)
	if runtime.GOOS != "linux" && runtime.GOOS != "wasip1" {
		go func() {
			for {
				buf := make([]byte, 1024)
				n, err := os.Stdin.Read(buf)
				if err != nil {
					slog.Error("error", "err", err)
					return
				}

				for i := 0; i < n; i++ {
					vm.inputBytes <- buf[i]
				}
			}
		}()
	}

	vm.virtio = append(vm.virtio, vm.console)
	vm.virtio = append(vm.virtio, newVirtio(vm, &virtioNet{
		macAddress: [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
	}, vm.plic.getIrq()))
	if diskContents != nil {
		vm.virtio = append(vm.virtio, newVirtio(vm, &virtioBlock{
			contents: diskContents,
		}, vm.plic.getIrq()))
	}

	vm.registerRam(0, rawRegion(make([]byte, 64*1024)))
	vm.registerRam(CLINT_BASE, vm.clint)
	vm.registerRam(PLIC_BASE, vm.plic)

	var base = VIRTIO_BASE

	for _, dev := range vm.virtio {
		if err := dev.dev.Init(dev); err != nil {
			return err
		}

		vm.registerRam(base, dev)

		base += 0x1000
	}

	vm.ramMap = vm.registerRam(RAM_BASE, rawRegion(make([]byte, vm.ramSize)))

	// var sigStart uint64
	// var sigEnd uint64

	// if *elfFile != "" {
	// 	// load the elf file into memory.

	// 	f, err := os.Open(*elfFile)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	elfF, err := elf.NewFile(f)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	for _, sect := range elfF.Sections {
	// 		if sect.Type != elf.SHT_PROGBITS {
	// 			continue
	// 		}

	// 		data, err := sect.Data()
	// 		if err != nil {
	// 			return err
	// 		}
	// 		if _, err := vm.copyTo(data, uint64(sect.Addr)); err != nil {
	// 			return err
	// 		}
	// 	}

	// 	if *signature != "" {
	// 		syms, err := elfF.Symbols()
	// 		if err != nil {
	// 			return err
	// 		}

	// 		for _, sym := range syms {
	// 			if sym.Name == "begin_signature" {
	// 				sigStart = sym.Value
	// 			} else if sym.Name == "end_signature" {
	// 				sigEnd = sym.Value
	// 			}
	// 		}

	// 		vm.stopOnZero = true

	// 		// write a empty file to the signature so if we crash it still exists.
	// 		if err := os.WriteFile(*signature, []byte{}, 0644); err != nil {
	// 			return err
	// 		}
	// 	}

	// 	vm.pc = uint64(elfF.Entry)
	// } else {
	var ramAddr uint32 = uint32(RAM_BASE)

	if _, err := vm.copyTo(RISCV_BIOS, uint64(ramAddr)); err != nil {
		return err
	}

	var fdtAddr uint32 = 0x1000 + 8*8

	var kernelStart uint64 = 0
	var kernelSize uint64 = 0

	// if kernelFilename != "" {
	// 	kernelContents, err := os.ReadFile(kernelFilename)
	// 	if err != nil {
	// 		return err
	// 	}
	kernelStart = RAM_BASE + 2*1024*1024
	kernelSize = uint64(len(kernelContents))

	if _, err := vm.copyTo(kernelContents, kernelStart); err != nil {
		return err
	}
	// } else {
	// 	kernelStart = RAM_BASE + 2*1024*1024
	// 	kernelSize = uint64(len(LINUX_KERNEL))

	// 	if _, err := vm.copyTo(LINUX_KERNEL, kernelStart); err != nil {
	// 		return err
	// 	}
	// }

	if kernelCmdline == "" {
		kernelCmdline = "console=hvc0 root=/dev/vda rw"
	}
	fdt, err := vm.buildFdt(kernelCmdline, kernelStart, kernelSize)
	if err != nil {
		return err
	}

	if _, err := vm.copyTo(fdt, uint64(fdtAddr)); err != nil {
		return err
	}

	// Weite the jump code to the start of the ram.
	if _, err := vm.copyTo(instrToBytes([]uint32{
		0x297 + ramAddr - 0x1000,        /* auipc t0, jump_addr */
		0x597,                           /* auipc a1, dtb */
		0x58593 + ((fdtAddr - 4) << 20), /* addi a1, a1, dtb */
		0xf1402573,                      /* csrr a0, mhartid */
		0x00028067,                      /* jalr zero, t0, jump_addr */
	}), 0x1000); err != nil {
		return err
	}
	// }

	vm.tlbInit()

	start := time.Now()

	if runtime.GOOS == "linux" {
		if err := syscall.SetNonblock(int(os.Stdin.Fd()), true); err != nil {
			return err
		}
	}

	buf := make([]byte, 128)

	for {
		if runtime.GOOS == "linux" || runtime.GOOS == "wasip1" {
			n, err := syscall.Read(int(os.Stdin.Fd()), buf)
			if err == nil {
				for i := 0; i < n; i++ {
					select {
					case vm.inputBytes <- buf[i]:
					default:
					}
				}
			}
		}

		if err := vm.Step(500000); err != nil {
			if err == ErrStopOnZero {
				break
			}

			slog.Info("error", "time", time.Since(start))
			return err
		}

		if vm.maxCycles > 0 && vm.totalCycles >= vm.maxCycles {
			// if *signature != "" {
			// 	return fmt.Errorf("maxCycles reached in signature mode")
			// }
			slog.Info("complete", "time", time.Since(start), "tlbHits", vm.tlbHits, "tlbMisses", vm.tlbMisses)
			break
		}
	}

	// if *signature != "" {
	// 	data := make([]byte, sigEnd-sigStart)

	// 	if len(data)%8 != 0 {
	// 		return fmt.Errorf("signature is not a multiple of 4 bytes")
	// 	}

	// 	if _, _, err := vm.readAt(data, sigStart); err != nil {
	// 		return err
	// 	}

	// 	sigFile, err := os.Create(*signature)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	defer sigFile.Close()

	// 	for i := 0; i < len(data); i += 8 {
	// 		fmt.Fprintf(sigFile, "%016x\n", binary.LittleEndian.Uint64(data[i:i+8]))
	// 	}
	// }

	return nil
}
