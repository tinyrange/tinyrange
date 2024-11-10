package ccvm

import "fmt"

type virtioDevice interface {
	DeviceId() uint32
	VendorId() uint32
	Status() uint32
	SetStatus(status uint32) error
	DeviceFeatures() uint32
	Init(v *virtio) error

	Receive(
		v *virtio,
		queueIdx uint16,
		descIdx uint16,
		readSize uint32,
		writeSize uint32,
	) error
}

type virtioQueue struct {
	queueIdx      uint16
	ready         uint32
	num           uint32
	descAddr      uint64
	descAvailable uint64
	descUsed      uint64
	lastAvailIdx  uint16
	manualRecv    bool
}

type virtioDesc struct {
	addr  uint64
	len   uint32
	flags uint16
	next  uint16
}

func (v *virtioQueue) getDesc(vm *VirtualMachine, descIdx uint16) (virtioDesc, error) {
	descStart := v.descAddr + uint64(descIdx)*16

	descBuf := make([]byte, 16)

	m, _, vAddr, err := vm.getMapAtPhysAddr(descStart)
	if err != nil {
		return virtioDesc{}, fmt.Errorf("get map failed: %w", err)
	}

	if _, err := m.ReadAt(descBuf, vAddr); err != nil {
		return virtioDesc{}, fmt.Errorf("read desc failed: %w", err)
	}

	var desc virtioDesc

	desc.addr = CpuEndian.Uint64(descBuf[:8])
	desc.len = CpuEndian.Uint32(descBuf[8:12])
	desc.flags = CpuEndian.Uint16(descBuf[12:14])
	desc.next = CpuEndian.Uint16(descBuf[14:16])

	return desc, nil
}

const (
	VRING_DESC_F_NEXT     = 1
	VRING_DESC_F_WRITE    = 2
	VRING_DESC_F_INDIRECT = 4
)

func (v *virtioQueue) getDescReadWriteSize(vm *VirtualMachine, descIdx uint16) (uint32, uint32, error) {
	var readSize, writeSize uint32

	for {
		desc, err := v.getDesc(vm, descIdx)
		if err != nil {
			return 0, 0, fmt.Errorf("get desc failed: %w", err)
		}

		if desc.flags&VRING_DESC_F_WRITE != 0 {
			break
		}
		readSize += desc.len
		if desc.flags&VRING_DESC_F_NEXT == 0 {
			goto done
		}
		descIdx = desc.next
	}

	for {
		desc, err := v.getDesc(vm, descIdx)
		if err != nil {
			return 0, 0, fmt.Errorf("get desc failed: %w", err)
		}

		if desc.flags&VRING_DESC_F_WRITE == 0 {
			return 0, 0, fmt.Errorf("invalid descriptor")
		}
		writeSize += desc.len
		if desc.flags&VRING_DESC_F_NEXT == 0 {
			break
		}
		descIdx = desc.next
	}

done:
	return readSize, writeSize, nil
}

func (v *virtioQueue) notify(dev *virtio) error {
	if v.manualRecv {
		return nil
	}

	availIdx, err := dev.readU16(v.descAvailable + 2)
	if err != nil {
		return fmt.Errorf("read availIdx failed: %w", err)
	}

	dev.vm.ulog("queue_notify: idx=%d avail_idx=%d\n", v.queueIdx, availIdx)

	for v.lastAvailIdx != availIdx {
		dev.vm.ulog("queue_notify: avail_addr=%x last_avail_idx=%d num=%d phys=%x\n",
			v.descAvailable, v.lastAvailIdx, v.num,
			v.descAvailable+4+uint64(uint32(v.lastAvailIdx)&(v.num-1))*2,
		)

		descIdx, err := dev.readU16(v.descAvailable + 4 + uint64(
			uint32(v.lastAvailIdx)&(v.num-1),
		)*2)
		if err != nil {
			return fmt.Errorf("read descIdx failed: %w", err)
		}

		dev.vm.ulog("queue_notify: idx=%d desc_idx=%d last_avail_idx=%d\n", v.queueIdx, descIdx, v.lastAvailIdx)

		readSize, writeSize, err := v.getDescReadWriteSize(dev.vm, descIdx)
		if err != nil {
			return fmt.Errorf("get desc read write size failed: %w", err)
		}

		err = dev.dev.Receive(dev, v.queueIdx, descIdx, readSize, writeSize)
		if err != nil {
			return fmt.Errorf("receive failed: %w", err)
		}

		v.lastAvailIdx++
	}

	return nil
}

type virtio struct {
	vm          *VirtualMachine
	dev         virtioDevice
	featuresSel uint32
	queueSel    uint32
	queues      [8]*virtioQueue
	irq         *irq
	intStatus   uint32
	configSpace [256]byte
}

func (v *virtio) copyFromRam(buf []byte, addr uint64) error {
	m, _, vAddr, err := v.vm.getMapAtPhysAddr(addr)
	if err != nil {
		return fmt.Errorf("get map failed: %w", err)
	}

	if _, err := m.ReadAt(buf, vAddr); err != nil {
		return fmt.Errorf("read desc failed: %w", err)
	}

	return nil
}

func (v *virtio) copyToRam(buf []byte, addr uint64) error {
	m, _, vAddr, err := v.vm.getMapAtPhysAddr(addr)
	if err != nil {
		return fmt.Errorf("get map failed: %w", err)
	}

	if _, err := m.WriteAt(buf, vAddr); err != nil {
		return fmt.Errorf("read desc failed: %w", err)
	}

	return nil
}

func (v *virtio) consumeDesc(queueIdx uint16, descIdx uint16, size uint64) error {
	queue := v.queues[queueIdx]

	addr := queue.descUsed + 2
	index, err := v.readU16(addr)
	if err != nil {
		return err
	}
	if err := v.writeU16(addr, index+1); err != nil {
		return err
	}

	addr = queue.descUsed + 4 + (uint64(index)&(uint64(queue.num)-1))*8
	if err := v.writeU32(addr, uint32(descIdx)); err != nil {
		return err
	}
	if err := v.writeU32(addr+4, uint32(size)); err != nil {
		return err
	}

	v.intStatus |= 1
	if err := v.irq.set(1); err != nil {
		return err
	}

	return nil
}

func (v *virtio) writeToQueue(buf []byte, queueIdx uint16, descIdx uint16) error {
	queue := v.queues[queueIdx]

	desc, err := queue.getDesc(v.vm, descIdx)
	if err != nil {
		return fmt.Errorf("get desc failed: %w", err)
	}

	var fWriteFlag uint16 = VRING_DESC_F_WRITE

	for {
		if (desc.flags & VRING_DESC_F_WRITE) == fWriteFlag {
			break
		}
		if (desc.flags & VRING_DESC_F_NEXT) == 0 {
			return fmt.Errorf("invalid descriptor: %d", desc.flags)
		}

		descIdx = desc.next

		desc, err = queue.getDesc(v.vm, descIdx)
		if err != nil {
			return fmt.Errorf("get desc failed: %w", err)
		}
	}

	// assume offset = 0
	var offset int32 = 0

	for {
		l := min(int32(len(buf)), int32(desc.len)-offset)

		if err := v.copyToRam(buf[:l], desc.addr+uint64(offset)); err != nil {
			return err
		}

		buf = buf[l:]
		if len(buf) == 0 {
			break
		}

		offset += l

		if offset == int32(desc.len) {
			if desc.flags&VRING_DESC_F_NEXT == 0 {
				return fmt.Errorf("descriptor does not have a next item")
			}

			descIdx = desc.next

			desc, err = queue.getDesc(v.vm, descIdx)
			if err != nil {
				return fmt.Errorf("get desc failed: %w", err)
			}

			if (desc.flags & VRING_DESC_F_WRITE) != fWriteFlag {
				return fmt.Errorf("invalid descriptor: %d", desc.flags)
			}

			offset = 0
		}
	}

	return nil
}

func (v *virtio) readFromQueue(buf []byte, queueIdx uint16, descIdx uint16) error {
	queue := v.queues[queueIdx]

	desc, err := queue.getDesc(v.vm, descIdx)
	if err != nil {
		return fmt.Errorf("get desc failed: %w", err)
	}

	if desc.flags&VRING_DESC_F_WRITE != 0 {
		return fmt.Errorf("bad descriptor")
	}

	var offset int32 = 0

	for {
		l := min(int32(len(buf)), int32(desc.len)-offset)

		if err := v.copyFromRam(buf[:l], desc.addr+uint64(offset)); err != nil {
			return err
		}

		buf = buf[l:]
		if len(buf) == 0 {
			break
		}

		offset += l

		if offset == int32(desc.len) {
			if desc.flags&VRING_DESC_F_NEXT == 0 {
				return fmt.Errorf("descriptor does not have a next item")
			}

			descIdx = desc.next

			desc, err = queue.getDesc(v.vm, descIdx)
			if err != nil {
				return fmt.Errorf("get desc failed: %w", err)
			}

			if (desc.flags & VRING_DESC_F_WRITE) != 0 {
				return fmt.Errorf("invalid descriptor")
			}

			offset = 0
		}
	}

	return nil
}

func (v *virtio) configChangeNotify() error {
	v.intStatus |= 2
	return v.irq.set(1)
}

func (v *virtio) reset() error {
	for i, queue := range v.queues {
		queue.queueIdx = uint16(i)
		queue.ready = 0
		queue.descAddr = 0
		queue.descAvailable = 0
		queue.descUsed = 0
		queue.lastAvailIdx = 0
	}

	return nil
}

func (v *virtio) readU16(addr uint64) (uint16, error) {
	var val [2]byte

	m, _, vAddr, err := v.vm.getMapAtPhysAddr(addr)
	if err != nil {
		return 0, err
	}

	if _, err := m.ReadAt(val[:], vAddr); err != nil {
		return 0, err
	}

	return CpuEndian.Uint16(val[:]), nil
}

func (v *virtio) writeU16(addr uint64, value uint16) error {
	var val [2]byte

	CpuEndian.PutUint16(val[:], value)

	m, _, vAddr, err := v.vm.getMapAtPhysAddr(addr)
	if err != nil {
		return err
	}

	if _, err := m.WriteAt(val[:], vAddr); err != nil {
		return err
	}

	return nil
}

func (v *virtio) writeU32(addr uint64, value uint32) error {
	var val [4]byte

	CpuEndian.PutUint32(val[:], value)

	m, _, vAddr, err := v.vm.getMapAtPhysAddr(addr)
	if err != nil {
		return err
	}

	if _, err := m.WriteAt(val[:], vAddr); err != nil {
		return err
	}

	return nil
}

// ReadAt implements memoryRegion.
func (v *virtio) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= 0x100 {
		// config space read.
		v.vm.log("# config space read off=%x len=%x\n", off-0x100, len(p))
		return copy(p, v.configSpace[off-0x100:]), nil
	}

	if len(p) != 4 {
		return -1, fmt.Errorf("invalid virtio read size=%d", len(p))
	}

	var val uint32 = 0

	switch off {
	case 0x000: // magic
		val = 0x74726976
	case 0x004: // version
		val = 2
	case 0x008: // device id
		val = v.dev.DeviceId()
	case 0x00c: // vendor id
		val = v.dev.VendorId()
	case 0x010: // device features
		switch v.featuresSel {
		case 0:
			val = v.dev.DeviceFeatures()
		case 1:
			val = 1 // version
		default:
			return -1, fmt.Errorf("invalid virtio read featuresSel=%d", v.featuresSel)
		}
	case 0x034: // queue num max
		val = 0x10
	case 0x044: // queue ready
		val = v.queues[v.queueSel].ready
	case 0x060: // interrupt status
		val = v.intStatus
	case 0x070: // status
		val = v.dev.Status()
	case 0x0fc: // config generation
		val = 0
	default:
		return -1, fmt.Errorf("invalid virtio read off=%x", off)
	}

	CpuEndian.PutUint32(p, val)

	return 4, nil
}

// Size implements memoryRegion.
func (v *virtio) Size() int64 {
	return int64(VIRTIO_SIZE)
}

// WriteAt implements memoryRegion.
func (v *virtio) WriteAt(p []byte, off int64) (n int, err error) {
	if len(p) != 4 {
		return -1, fmt.Errorf("invalid virtio write size=%d", len(p))
	}

	val := CpuEndian.Uint32(p)

	v.vm.ulog("virtio write: off=%x val=%x\n", off, val)

	switch off {
	case 0x014: // device features sel
		v.featuresSel = val
	case 0x020: // driver features (not implemented)
		return 4, nil
	case 0x024: // driver features sel (not implemented)
		return 4, nil
	case 0x030: // queue sel
		if val < 8 {
			v.queueSel = val
		}
	case 0x038: // queue num
		if (val&(val-1)) == 0 && val > 0 {
			v.queues[v.queueSel].num = val
		}
	case 0x044: // queue ready
		v.queues[v.queueSel].ready = val & 1
	case 0x050: // queue notify
		if val < 8 {
			if err := v.queues[val].notify(v); err != nil {
				return -1, err
			}
		}
	case 0x064: // interrupt act
		v.intStatus &= ^val
		if v.intStatus == 0 {
			if err := v.irq.set(0); err != nil {
				return -1, err
			}
		}
	case 0x070: // status
		if val == 0 {
			if err := v.reset(); err != nil {
				return -1, err
			}
		}

		if err := v.dev.SetStatus(val); err != nil {
			return -1, err
		}
	case 0x080: // queue desc low
		v.queues[v.queueSel].descAddr = uint64(val) & 0xffff_ffff
	case 0x084: // queue desc high
		v.queues[v.queueSel].descAddr |= uint64(val) << 32
	case 0x090: // queue available low
		v.queues[v.queueSel].descAvailable = uint64(val) & 0xffff_ffff
	case 0x094: // queue available high
		v.queues[v.queueSel].descAvailable |= uint64(val) << 32
	case 0x0a0: // queue used low
		v.queues[v.queueSel].descUsed = uint64(val) & 0xffff_ffff
	case 0x0a4: // queue used high
		v.queues[v.queueSel].descUsed |= uint64(val) << 32
	default:
		return -1, fmt.Errorf("invalid virtio write off=%x", off)
	}

	return 4, nil
}

var (
	_ memoryRegion = &virtio{}
)

func newVirtio(vm *VirtualMachine, dev virtioDevice, irq *irq) *virtio {
	return &virtio{
		vm:  vm,
		dev: dev,
		irq: irq,
		queues: [8]*virtioQueue{
			{queueIdx: 0},
			{queueIdx: 1},
			{queueIdx: 2},
			{queueIdx: 3},
			{queueIdx: 4},
			{queueIdx: 5},
			{queueIdx: 6},
			{queueIdx: 7},
		},
	}
}
