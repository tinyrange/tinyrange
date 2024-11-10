package ccvm

import "fmt"

type clint struct {
	vm *VirtualMachine
}

func (c *clint) ReadAt(p []byte, off int64) (n int, err error) {
	var val uint32 = 0

	cycles := c.vm.rtcGetTime()

	if off == 0xbff8 {
		val = uint32(cycles)
	} else if off == 0xbffc {
		val = uint32(cycles >> 32)
	} else {
		return -1, fmt.Errorf("invalid clint read at 0x%x", off)
	}

	CpuEndian.PutUint32(p, val)
	return 8, nil
}

func (c *clint) WriteAt(p []byte, off int64) (n int, err error) {
	switch len(p) {
	case 4:
		val := CpuEndian.Uint32(p)

		if off == 0x0000 {
			// noop
		} else if off == 0x4000 {
			c.vm.timecmp = 0 | uint64(val&0xffffffff)
			c.vm.cpuResetMip(MIP_MTIP)
		} else if off == 0x4004 {
			c.vm.timecmp = (c.vm.timecmp & 0xffffffff) | (uint64(val) << 32)
			c.vm.cpuResetMip(MIP_MTIP)
		} else {
			return -1, fmt.Errorf("invalid clint write at 0x%x", off)
		}

		return 4, nil
	case 8:
		val := CpuEndian.Uint64(p)

		if off == 0x4000 {
			c.vm.timecmp = val
			c.vm.cpuResetMip(MIP_MTIP)
		} else {
			return -1, fmt.Errorf("invalid clint write at 0x%x", off)
		}

		return 8, nil
	default:
		return -1, fmt.Errorf("invalid clint write at 0x%x len=%x", off, len(p))
	}
}

func (c *clint) Size() int64 {
	return int64(CLINT_SIZE)
}

var (
	_ memoryRegion = &clint{}
)
