package ccvm

import (
	"fmt"
	"os"
)

type blockRequestHeader struct {
	typ       uint32
	ioprio    uint32
	sectorNum uint64
}

type blockRequest struct {
	typ       uint32
	buf       []byte
	writeSize int
	queueIdx  int
	descIdx   int
}

type virtioBlock struct {
	status uint32

	contents []byte

	filename string

	req blockRequest
}

// implements virtioDevice.
func (v *virtioBlock) Status() uint32         { return v.status }
func (v *virtioBlock) DeviceId() uint32       { return 2 }
func (v *virtioBlock) VendorId() uint32       { return 0xffff }
func (v *virtioBlock) DeviceFeatures() uint32 { return 0x0 }

// SetStatus implements virtioDevice.
func (v *virtioBlock) SetStatus(status uint32) error {
	v.status = status

	if status == 0 {
		// reset
	}

	return nil
}

const (
	SECTOR_SIZE = 512
)

func (v *virtioBlock) readSectors(buf []byte, sector uint64, count uint32) error {
	// TODO(joshua): Handle out of bounds read.

	off := sector * SECTOR_SIZE
	length := count * SECTOR_SIZE

	copy(buf, v.contents[off:off+uint64(length)])

	return nil
}

func (v *virtioBlock) writeSectors(buf []byte, sector uint64, count uint32) error {
	// TODO(joshua): Handle out of bounds read.

	off := sector * SECTOR_SIZE
	length := count * SECTOR_SIZE

	copy(v.contents[off:off+uint64(length)], buf)

	return nil
}

// Receive implements virtioDevice.
func (v *virtioBlock) Receive(
	virt *virtio,
	queueIdx uint16,
	descIdx uint16,
	readSize uint32,
	writeSize uint32,
) error {
	var buf [16]byte

	if err := virt.readFromQueue(buf[:], queueIdx, descIdx); err != nil {
		return err
	}

	var hdr blockRequestHeader

	hdr.typ = CpuEndian.Uint32(buf[:4])
	// hdr.ioprio = CpuEndian.Uint32(buf[4:8])
	hdr.sectorNum = CpuEndian.Uint64(buf[8:16])

	v.req.typ = hdr.typ
	v.req.queueIdx = int(queueIdx)
	v.req.descIdx = int(descIdx)

	switch hdr.typ {
	case 0: // VIRTIO_BLK_T_IN
		v.req.buf = make([]byte, writeSize)
		v.req.writeSize = int(writeSize)
		if err := v.readSectors(v.req.buf, hdr.sectorNum, (writeSize-1)/SECTOR_SIZE); err != nil {
			return err
		}

		// Read successful
		v.req.buf[writeSize-1] = 0

		// Write the result to the queue.
		if err := virt.writeToQueue(v.req.buf, queueIdx, descIdx); err != nil {
			return fmt.Errorf("failed to write back to queue: %w", err)
		}

		// Consume the descriptor.
		if err := virt.consumeDesc(queueIdx, descIdx, uint64(len(v.req.buf))); err != nil {
			return fmt.Errorf("failed to consume descriptor: %w", err)
		}

		return nil
	case 1: // VIRTIO_BLK_T_OUT
		buf := make([]byte, readSize)

		if err := virt.readFromQueue(buf, queueIdx, descIdx); err != nil {
			return fmt.Errorf("failed to read from queue: %w", err)
		}

		buf = buf[16:] // skip the header at the start of queue.

		// Write the sectors.
		if err := v.writeSectors(buf, hdr.sectorNum, readSize-16/SECTOR_SIZE); err != nil {
			return err
		}

		var buf1 [1]byte

		// Write successful
		buf1[0] = 0

		// Write the result to the queue.
		if err := virt.writeToQueue(buf1[:], queueIdx, descIdx); err != nil {
			return fmt.Errorf("failed to write back to queue: %w", err)
		}

		// Consume the descriptor.
		if err := virt.consumeDesc(queueIdx, descIdx, 1); err != nil {
			return fmt.Errorf("failed to consume descriptor: %w", err)
		}

		return nil
	default:
		return nil
	}
}

// Init implements virtioDevice.
func (blk *virtioBlock) Init(v *virtio) error {
	if blk.contents == nil {
		var err error

		blk.contents, err = os.ReadFile(blk.filename)
		if err != nil {
			return err
		}
	}

	CpuEndian.PutUint64(v.configSpace[:], uint64(len(blk.contents))/512)

	return nil
}

var (
	_ virtioDevice = &virtioBlock{}
)
