package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
)

type VirtIOKind string

const (
	VirtIOKindConsole VirtIOKind = "console"
)

type VirtIODevice interface {
	Kind() VirtIOKind
}

type virtIODevice struct {
	dev VirtIODevice
}

const VIRTIO_PCI_CFG_OFFSET = 0x0000
const VIRTIO_PCI_ISR_OFFSET = 0x1000
const VIRTIO_PCI_CONFIG_OFFSET = 0x2000
const VIRTIO_PCI_NOTIFY_OFFSET = 0x3000

func (v *virtIODevice) addPciCapability(
	dev *PciDevice,
	cfgType uint8,
	barNum uint8,
	offset int,
	size int,
	mult int,
) error {
	var cap []byte
	var capLen uint8
	if cfgType == 2 {
		cap = make([]byte, 20)
		capLen = 20
	} else {
		cap = make([]byte, 16)
		capLen = 16
	}
	cap[0] = 0x09   // vendor specific */
	cap[2] = capLen // set by pci_add_capability() */
	cap[3] = cfgType
	cap[4] = barNum
	binary.LittleEndian.PutUint32(cap[8:], uint32(offset))
	binary.LittleEndian.PutUint32(cap[12:], uint32(size))
	if cfgType == 2 {
		binary.LittleEndian.PutUint32(cap[16:], uint32(mult))
	}
	return dev.addCapability(cap)
}

func (v *virtIODevice) barSet(index uint8, addr uint32, enabled bool) error {
	slog.Info(
		"barSet",
		"index", index,
		"addr", addr,
		"enabled", enabled,
	)
	return fmt.Errorf("barSet not implemented")
}

func (v *virtIODevice) toPCIDevice() (*PciDevice, error) {
	var dev *PciDevice
	switch kind := v.dev.Kind(); kind {
	case VirtIOKindConsole:
		dev = NewPciDevice("virtio_console", 0x1af4, 0x1003, 0x00, 0x0780)
		dev.writeU16(0x2e, 3)
	default:
		panic(fmt.Sprintf("unsupported virtio device kind %s", kind))
	}

	dev.writeU16(0x2c, 0x1af4)
	dev.writeU8(0x3d, 1) // interrupt pin

	var barNum uint8 = 4
	if err := v.addPciCapability(dev, 1, barNum, VIRTIO_PCI_CFG_OFFSET, 0x1000, 0); err != nil { // common
		return nil, err
	}
	if err := v.addPciCapability(dev, 3, barNum+1, VIRTIO_PCI_ISR_OFFSET, 0x1000, 0); err != nil { // isr
		return nil, err
	}
	if err := v.addPciCapability(dev, 4, barNum+2, VIRTIO_PCI_CONFIG_OFFSET, 0x1000, 0); err != nil { // config
		return nil, err
	}
	if err := v.addPciCapability(dev, 2, barNum+3, VIRTIO_PCI_NOTIFY_OFFSET, 0x1000, 0); err != nil { // notify
		return nil, err
	}
	if err := dev.registerBar(barNum, 0x4000, PCI_ADDRESS_SPACE_MEM, v.barSet); err != nil {
		return nil, err
	}

	return dev, nil
}

type VirtIOBus struct {
	cpu     VMDevice
	pci     *PciBus
	devices []*virtIODevice
}

func (v *VirtIOBus) AddDevice(dev VirtIODevice) error {
	newDev := &virtIODevice{dev: dev}
	v.devices = append(v.devices, newDev)
	pciDev, err := newDev.toPCIDevice()
	if err != nil {
		return err
	}

	devfn, err := v.pci.AllocateDeviceNumber()
	if err != nil {
		return err
	}

	v.pci.AddDevice(devfn, pciDev)
	return nil
}

func NewVirtioBus(cpu VMDevice, pci *PciBus) *VirtIOBus {
	return &VirtIOBus{
		cpu: cpu,
		pci: pci,
	}
}

type VirtIOConsole struct {
	stdout io.Writer
	stdin  io.Reader
}

func (v *VirtIOConsole) Kind() VirtIOKind { return VirtIOKindConsole }

var (
	_ VirtIODevice = &VirtIOConsole{}
)

func NewVirtioConsole(stdout io.Writer, stdin io.Reader) *VirtIOConsole {
	return &VirtIOConsole{
		stdout: stdout,
		stdin:  stdin,
	}
}
