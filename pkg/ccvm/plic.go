package ccvm

type plic struct {
	vm *VirtualMachine

	irqs    [33]*irq
	lastIrq int
}

func ctz32(a uint32) uint32 {
	if a == 0 {
		return 32
	}
	for i := 0; i < 32; i++ {
		if (a>>i)&1 != 0 {
			return uint32(i)
		}
	}
	return 32
}

func (plic *plic) getIrq() *irq {
	if plic.lastIrq > len(plic.irqs) {
		panic("ran out of irqs")
	}

	plic.lastIrq += 1

	irq := &irq{vm: plic.vm, id: plic.lastIrq, setFunc: plic.SetIrq}

	plic.irqs[plic.lastIrq] = irq

	return irq
}

func (plic *plic) SetIrq(irqNum int, state int) {
	var mask uint32 = 1 << (irqNum - 1)
	if state != 0 {
		plic.vm.plicPendingIrq |= mask
	} else {
		plic.vm.plicPendingIrq &= ^mask
	}

	plic.vm.plicUpdateMip()
}

const PLIC_HART_BASE = 0x200000

// ReadAt implements memoryRegion.
func (plic *plic) ReadAt(p []byte, off int64) (n int, err error) {
	var val uint32 = 0

	if off == PLIC_HART_BASE { // PLIC_HART_BASE
		val = 0
	} else if off == PLIC_HART_BASE+4 { // PLIC_HART_BASE + 4
		mask := plic.vm.plicPendingIrq & ^plic.vm.plicServedIrq
		if mask != 0 {
			i := ctz32(mask)
			plic.vm.plicServedIrq |= 1 << i
			plic.vm.plicUpdateMip()
			val = i + 1
		} else {
			val = 0
		}
	} else {
		val = 0
	}

	CpuEndian.PutUint32(p, val)

	return 4, nil
}

// Size implements memoryRegion.
func (p *plic) Size() int64 {
	return int64(PLIC_SIZE)
}

// WriteAt implements memoryRegion.
func (plic *plic) WriteAt(p []byte, off int64) (n int, err error) {
	val := CpuEndian.Uint32(p)

	if off == PLIC_HART_BASE+4 {
		val--
		if val < 32 {
			plic.vm.plicServedIrq &= ^(1 << val)
			plic.vm.plicUpdateMip()
		}
	}

	return 4, nil
}

var (
	_ memoryRegion = &plic{}
)
