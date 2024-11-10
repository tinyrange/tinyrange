package ccvm

import "fmt"

type virtioNetHeader struct {
	flags      uint8
	gsoType    uint8
	hdrLen     uint16
	gsoSize    uint16
	csumStart  uint16
	csumOffset uint16
	numBuffers uint16
}

type virtioNet struct {
	status     uint32
	macAddress [6]byte
}

// implements virtioDevice.
func (v *virtioNet) Status() uint32         { return v.status }
func (v *virtioNet) DeviceId() uint32       { return 1 }
func (v *virtioNet) VendorId() uint32       { return 0xffff }
func (v *virtioNet) DeviceFeatures() uint32 { return (1 << 5) }

// SetStatus implements virtioDevice.
func (v *virtioNet) SetStatus(status uint32) error {
	v.status = status

	if status == 0 {
		// reset
	}

	return nil
}

func (v *virtioNet) writePacket(buf []byte) error {
	return nil
}

// Receive implements virtioDevice.
func (v *virtioNet) Receive(
	virt *virtio,
	queueIdx uint16,
	descIdx uint16,
	readSize uint32,
	writeSize uint32,
) error {
	if queueIdx == 1 {
		buf := make([]byte, readSize)

		if err := virt.readFromQueue(buf, queueIdx, descIdx); err != nil {
			return err
		}

		if err := v.writePacket(buf[12:]); err != nil {
			return nil
		}

		if err := virt.consumeDesc(queueIdx, descIdx, 0); err != nil {
			return err
		}
	}

	return fmt.Errorf("virtioNet.Receive not implemented")
}

// Init implements virtioDevice.
func (vNet *virtioNet) Init(v *virtio) error {
	v.queues[0].manualRecv = true

	// Set the MAC address.
	copy(v.configSpace[:], vNet.macAddress[:])

	return nil
}

var (
	_ virtioDevice = &virtioNet{}
)
