package main

import (
	"encoding/binary"
	"fmt"
	"log/slog"

	"github.com/tinyrange/tinyrange/experimental/crumblecracker/kvm"
)

type PciDevice struct {
	config [256]byte
}

func (p *PciDevice) writeU32(addr uint32, val uint32) {
	binary.LittleEndian.PutUint32(p.config[addr:addr+4], val)
}

func (p *PciDevice) writeU16(addr uint32, val uint16) {
	binary.LittleEndian.PutUint16(p.config[addr:addr+2], val)
}

func (p *PciDevice) writeU8(addr uint32, val uint8) {
	p.config[addr] = val
}

func (p *PciDevice) ConfigIO(io *kvm.KVMIoEvent, addr uint32) error {
	switch io.Direction {
	case kvm.IoDirectionRead:
		io.Write(p.config[addr : addr+uint32(io.Size)])
		return nil
	case kvm.IoDirectionWrite:
		data := io.Read()
		copy(p.config[addr:addr+uint32(io.Size)], data)
		return nil
	default:
		return fmt.Errorf("invalid direction %d", io.Direction)
	}
}

func NewPciDevice(vendorId uint16, deviceId uint16, revision uint8, classId uint16) *PciDevice {
	dev := &PciDevice{}

	binary.LittleEndian.PutUint16(dev.config[0x00:0x02], vendorId)
	binary.LittleEndian.PutUint16(dev.config[0x02:0x04], deviceId)
	dev.config[0x08] = revision
	binary.LittleEndian.PutUint16(dev.config[0x0A:0x0C], classId)
	dev.config[0x0E] = 0x00

	return dev
}

type PciBus struct {
	cpu VMDevice

	busNum uint8

	addr uint32

	devices map[uint8]*PciDevice
}

// AddDevice adds a PCI device to the bus.
func (p *PciBus) AddDevice(devfn uint8, dev *PciDevice) {
	p.devices[devfn] = dev
}

func (p *PciBus) valOnes(io *kvm.KVMIoEvent) error {
	val := make([]byte, io.Size)
	for i := range val {
		val[i] = 0xff
	}

	io.Write(val)

	return nil
}

// Init implements IODevice.
func (p *PciBus) Init(cpu VMDevice) error {
	p.cpu = cpu

	p.busNum = 0

	return nil
}

// Ports implements IODevice.
func (p *PciBus) Ports() []uint16 {
	return []uint16{0xCF8, 0xCFC}
}

// IO implements IODevice.
func (p *PciBus) IO(io *kvm.KVMIoEvent) error {
	slog.Warn("pci access",
		"port", fmt.Sprintf("0x%04x", io.Port),
		"count", io.Count,
		"size", io.Size,
		"dir", io.Direction,
	)

	if io.Port == 0xCF8 {
		if io.Size != 4 {
			return fmt.Errorf("invalid size %d", io.Size)
		}

		if io.Direction == kvm.IoDirectionRead {
			io.Write(binary.LittleEndian.AppendUint32(nil, p.addr))
		} else {
			data := io.Read()

			p.addr = binary.LittleEndian.Uint32(data)
		}

		return nil
	} else if io.Port == 0xCFC {
		if p.addr&0x80000000 == 0 {
			if io.Direction == kvm.IoDirectionWrite {
				return nil
			}

			return p.valOnes(io)
		}

		addr := p.addr & 0x7fffffff

		bus_num := uint8((addr >> 16) & 0xff)
		if bus_num != p.busNum {
			return p.valOnes(io)
		}
		devfn := uint8((addr >> 8) & 0xff)
		dev, ok := p.devices[devfn]
		if !ok {
			slog.Info("pci device not found", "devfn", devfn)
			return p.valOnes(io)
		}
		config_addr := addr & 0xff

		return dev.ConfigIO(io, config_addr)
	} else {
		return fmt.Errorf("unknown port 0x%04x", io.Port)
	}
}

var (
	_ IODevice = &PciBus{}
)
