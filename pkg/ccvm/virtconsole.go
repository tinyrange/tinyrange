package ccvm

import (
	"fmt"
	"os"
)

type virtioConsole struct {
	status uint32
}

func (v *virtioConsole) write(virt *virtio, buf []byte) error {
	return virt.vm.writeConsole(buf)
}

// Receive implements virtioDevice.
func (v *virtioConsole) Receive(
	virt *virtio,
	queueIdx uint16,
	descIdx uint16,
	readSize uint32,
	writeSize uint32,
) error {
	virt.vm.ulog("virtioConsole.Receive queueIdx=%d descIdx=%d readSize=%d writeSize=%d\n",
		queueIdx, descIdx, readSize, writeSize)

	if queueIdx == 1 {
		// send to console

		buf := make([]byte, readSize)

		if err := virt.readFromQueue(buf, queueIdx, descIdx); err != nil {
			return err
		}

		if err := v.write(virt, buf); err != nil {
			return err
		}

		if err := virt.consumeDesc(queueIdx, descIdx, 0); err != nil {
			return err
		}
	}

	return nil
}

// implements virtioDevice.
func (v *virtioConsole) Status() uint32         { return v.status }
func (v *virtioConsole) DeviceId() uint32       { return 3 }
func (v *virtioConsole) VendorId() uint32       { return 0xffff }
func (v *virtioConsole) DeviceFeatures() uint32 { return 1 }

// SetStatus implements virtioDevice.
func (v *virtioConsole) SetStatus(status uint32) error {
	v.status = status

	if status == 0 {
		// reset
	}

	return nil
}

// Init implements virtioDevice.
func (*virtioConsole) Init(v *virtio) error {
	return nil
}

var (
	_ virtioDevice = &virtioConsole{}
)

func (vm *VirtualMachine) consoleCanWrite() (bool, error) {
	queue := vm.console.queues[0]

	if queue.ready == 0 {
		return false, nil
	}

	availIdx, err := vm.console.readU16(queue.descAvailable + 2)
	if err != nil {
		return false, err
	}

	return availIdx != queue.lastAvailIdx, nil
}

func (vm *VirtualMachine) consoleGetWriteLen() (int, error) {
	queue := vm.console.queues[0]

	if queue.ready == 0 {
		return 0, nil
	}

	availIdx, err := vm.console.readU16(queue.descAvailable + 2)
	if err != nil {
		return 0, err
	}

	if queue.lastAvailIdx == availIdx {
		return 0, nil
	}

	descIdx, err := vm.console.readU16(queue.descAvailable + 4 + uint64(uint32(queue.lastAvailIdx)&(queue.num-1))*2)
	if err != nil {
		return 0, err
	}

	_, writeSize, err := queue.getDescReadWriteSize(vm, descIdx)
	if err != nil {
		return 0, err
	}

	return int(writeSize), nil
}

func (vm *VirtualMachine) consoleWriteData(data []byte) error {
	queue := vm.console.queues[0]

	if queue.ready == 0 {
		return fmt.Errorf("queue not ready")
	}

	availIdx, err := vm.console.readU16(queue.descAvailable + 2)
	if err != nil {
		return err
	}

	if queue.lastAvailIdx == availIdx {
		return fmt.Errorf("no available index")
	}

	descIdx, err := vm.console.readU16(queue.descAvailable + 4 + uint64(uint32(queue.lastAvailIdx)&(queue.num-1))*2)
	if err != nil {
		return err
	}

	if err := vm.console.writeToQueue(data, 0, descIdx); err != nil {
		return err
	}

	if err := vm.console.consumeDesc(0, descIdx, uint64(len(data))); err != nil {
		return err
	}

	queue.lastAvailIdx++

	return nil
}

func (vm *VirtualMachine) consoleResize(cols, rows uint16) error {
	CpuEndian.PutUint16(vm.console.configSpace[:2], cols)
	CpuEndian.PutUint16(vm.console.configSpace[2:4], rows)

	return vm.console.configChangeNotify()
}

func (vm *VirtualMachine) writeConsole(buf []byte) error {
	// vm.log("console_write: len=%d\n", len(buf))

	if _, err := os.Stdout.Write(buf); err != nil {
		return err
	}

	return nil
}
