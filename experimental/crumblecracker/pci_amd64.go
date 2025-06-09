//go:build linux && amd64

package main

import (
	"encoding/binary"
	"fmt"

	"github.com/tinyrange/tinyrange/experimental/crumblecracker/kvm"
	"github.com/tinyrange/tinyrange/pkg/log"
)

// Based on: tinyemu-2019-12-21/pci.h and tinyemu-2019-12-21/pci.c

const (
	PCI_VENDOR_ID           = 0x00 /* 16 bits */
	PCI_DEVICE_ID           = 0x02 /* 16 bits */
	PCI_COMMAND             = 0x04 /* 16 bits */
	PCI_COMMAND_IO          = (1 << 0)
	PCI_COMMAND_MEMORY      = (1 << 1)
	PCI_STATUS              = 0x06 /* 16 bits */
	PCI_STATUS_CAP_LIST     = (1 << 4)
	PCI_CLASS_PROG          = 0x09
	PCI_SUBSYSTEM_VENDOR_ID = 0x2c /* 16 bits */
	PCI_SUBSYSTEM_ID        = 0x2e /* 16 bits */
	PCI_CAPABILITY_LIST     = 0x34 /* 8 bits */
	PCI_INTERRUPT_LINE      = 0x3c /* 8 bits */
	PCI_INTERRUPT_PIN       = 0x3d /* 8 bits */

	PCI_ROM_SLOT = 6

	// bar type
	PCI_ADDRESS_SPACE_MEM          = 0x00
	PCI_ADDRESS_SPACE_IO           = 0x01
	PCI_ADDRESS_SPACE_MEM_PREFETCH = 0x08
)

type pciBarSetFunc func(index uint8, addr uint32, enabled bool) error
type pciIOReadFunc func(port uint16, size int) []byte
type pciIOWriteFunc func(port uint16, data []byte)

type pciIORegion struct {
	size     uint32
	typ      uint8
	enabled  bool
	barSet   pciBarSetFunc
	baseAddr uint32
}

func (p *pciIORegion) tryBarSet(index uint8, addr uint32, enabled bool) error {
	if p.barSet != nil {
		return p.barSet(index, addr, enabled)
	}

	return fmt.Errorf("barSet not set")
}

type PciDevice struct {
	name          string
	config        [256]byte
	nextCapOffset uint8
	ioRegions     [7]pciIORegion

	// I/O port handlers for devices that need them (like virtio)
	ioReadHandler  pciIOReadFunc
	ioWriteHandler pciIOWriteFunc
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

func (p *PciDevice) readU32(addr uint32) uint32 {
	return binary.LittleEndian.Uint32(p.config[addr : addr+4])
}

func (p *PciDevice) setIOHandler(
	readHandler pciIOReadFunc,
	writeHandler pciIOWriteFunc,
) {
	p.ioReadHandler = readHandler
	p.ioWriteHandler = writeHandler
}

func (p *PciDevice) addCapability(cap []byte) error {
	offset := p.nextCapOffset
	if offset+uint8(len(cap)) > 255 { // TODO(joshua): not sure if this overflows
		return fmt.Errorf("capability too large")
	}
	p.nextCapOffset += uint8(len(cap))
	p.config[PCI_STATUS] |= PCI_STATUS_CAP_LIST
	copy(p.config[offset:], cap)
	p.config[offset+1] = p.config[PCI_CAPABILITY_LIST]
	p.config[PCI_CAPABILITY_LIST] = offset
	return nil
}

func (p *PciDevice) updateMappings() error {
	cmd := binary.LittleEndian.Uint16(p.config[PCI_COMMAND : PCI_COMMAND+2])

	log.Default().Info(
		"PCI updateMappings",
		"device",
		p.name,
		"command",
		fmt.Sprintf("0x%04x", cmd),
	)

	for i := range p.ioRegions {
		region := &p.ioRegions[i]
		var offset uint32
		if i == PCI_ROM_SLOT {
			offset = 0x30
		} else {
			offset = 0x10 + uint32(i)*4
		}

		newAddr := p.readU32(offset)
		newEnabled := false
		if region.size != 0 {
			if (region.typ&PCI_ADDRESS_SPACE_IO != 0) &&
				(cmd&PCI_COMMAND_IO != 0) {
				newEnabled = true
				log.Default().Info(
					"PCI I/O region will be enabled",
					"device",
					p.name,
					"bar",
					i,
					"addr",
					fmt.Sprintf("0x%08x", newAddr),
				)
			} else {
				if cmd&PCI_COMMAND_MEMORY != 0 {
					if i == PCI_ROM_SLOT {
						newEnabled = (newAddr & 1) != 0
					} else {
						newEnabled = true
					}
				}
			}
		}
		if newEnabled {
			// new address
			newAddr = p.readU32(offset) & ^(region.size - 1)
			region.baseAddr = newAddr
			log.Default().Info(
				"PCI region enabled",
				"device",
				p.name,
				"bar",
				i,
				"baseAddr",
				fmt.Sprintf("0x%08x", newAddr),
				"size",
				region.size,
			)
			if err := region.tryBarSet(uint8(i), newAddr, true); err != nil {
				return err
			}
			region.enabled = true
		} else if region.enabled {
			log.Default().Info("PCI region disabled", "device", p.name, "bar", i)
			if err := region.tryBarSet(uint8(i), 0, false); err != nil {
				return err
			}
			region.enabled = false
			region.baseAddr = 0
		}
	}

	return nil
}

func (p *PciDevice) registerBar(
	index uint8,
	size uint32,
	typ uint8,
	barSet pciBarSetFunc,
) error {
	p.ioRegions[index] = pciIORegion{
		size:    size,
		typ:     typ,
		enabled: false,
		barSet:  barSet,
	}

	var configAddr uint8
	var val uint32 = 0
	if index == PCI_ROM_SLOT {
		configAddr = 0x30
	} else {
		val |= uint32(typ)
		configAddr = 0x10 + index*4
	}
	p.writeU32(uint32(configAddr), val)

	return nil
}

func (p *PciDevice) writeBar(addr uint32, val uint32) (bool, error) {
	var reg uint8
	if addr == 0x30 {
		reg = PCI_ROM_SLOT
	} else {
		reg = uint8((addr - 0x10) >> 2)
	}

	log.Default().Info(
		"pci write bar",
		"name",
		p.name,
		"addr",
		fmt.Sprintf("0x%02x", addr),
		"val",
		fmt.Sprintf("0x%08x", val),
		"reg",
		reg,
	)

	r := &p.ioRegions[reg]
	if r.size == 0 {
		log.Default().Warn("pci write bar: region not registered", "reg", reg)
		return false, nil // don't handle the write
	}

	// Handle size probing
	if val == 0xffffffff {
		if reg == PCI_ROM_SLOT {
			val = (^(r.size - 1)) | 1
		} else {
			val = (^(r.size - 1)) | uint32(r.typ)
		}
	} else {
		if reg == PCI_ROM_SLOT {
			val = val & ((^(r.size - 1)) | 1)
		} else {
			val = (val & ^(r.size - 1)) | uint32(r.typ)
		}
	}

	log.Default().Info(
		"pci write bar: setting bar",
		"reg",
		reg,
		"val",
		fmt.Sprintf("0x%08x", val),
	)
	p.writeU32(addr, val)
	return true, p.updateMappings()
}

// Check if a port falls within any of this device's I/O BARs
func (p *PciDevice) handlesIOPort(port uint16) bool {
	for i, region := range p.ioRegions {
		if region.enabled && (region.typ&PCI_ADDRESS_SPACE_IO != 0) {
			base := uint16(region.baseAddr)
			if port >= base && port < base+uint16(region.size) {
				log.Default().Info(
					"PCI device handles I/O port",
					"device",
					p.name,
					"port",
					fmt.Sprintf("0x%04x", port),
					"bar",
					i,
					"base",
					fmt.Sprintf("0x%04x", base),
					"size",
					region.size,
				)
				return true
			}
		}
	}
	return false
}

// Handle I/O port access for this device
func (p *PciDevice) handleIOPort(io *kvm.KVMIoEvent) error {
	if p.ioReadHandler == nil || p.ioWriteHandler == nil {
		return fmt.Errorf("device %s has no I/O handlers", p.name)
	}

	switch io.Direction {
	case kvm.IoDirectionRead:
		data := p.ioReadHandler(io.Port, int(io.Size))
		io.Write(data)
		return nil
	case kvm.IoDirectionWrite:
		data := io.Read()
		p.ioWriteHandler(io.Port, data)
		return nil
	default:
		return fmt.Errorf("invalid I/O direction %d", io.Direction)
	}
}

func (p *PciDevice) ConfigIO(io *kvm.KVMIoEvent, addr uint32) error {
	switch io.Direction {
	case kvm.IoDirectionRead:
		// Add special logging for IRQ line reads
		if addr == PCI_INTERRUPT_LINE {
			log.Default().Info("pci config io read IRQ LINE",
				"name", p.name,
				"addr", fmt.Sprintf("0x%02x", addr),
				"size", io.Size,
				"value", fmt.Sprintf("0x%02x", p.config[addr]),
			)
		} else {
			log.Default().Info("pci config io read",
				"name", p.name,
				"addr", fmt.Sprintf("0x%02x", addr),
				"size", io.Size,
				"value", fmt.Sprintf("%x", p.config[addr:addr+uint32(io.Size)]),
			)
		}
		io.Write(p.config[addr : addr+uint32(io.Size)])
		return nil
	case kvm.IoDirectionWrite:
		data := io.Read()

		log.Default().Info(
			"pci config io write",
			"name",
			p.name,
			"addr",
			fmt.Sprintf("0x%02x", addr),
			"size",
			io.Size,
			"value",
			fmt.Sprintf("%x", data),
		)

		if io.Size == 4 &&
			((addr >= 0x10 && addr < 0x10+4*6) || addr == 0x30) {
			ok, err := p.writeBar(addr, binary.LittleEndian.Uint32(data))
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}

		copy(p.config[addr:addr+uint32(io.Size)], data)

		if PCI_COMMAND >= addr && PCI_COMMAND < addr+uint32(io.Size) {
			return p.updateMappings()
		}

		return nil
	default:
		return fmt.Errorf("invalid direction %d", io.Direction)
	}
}

func NewPciDevice(
	name string,
	vendorId uint16,
	deviceId uint16,
	revision uint8,
	classId uint32,
) *PciDevice {
	dev := &PciDevice{
		name:          name,
		nextCapOffset: 0x40,
	}

	binary.LittleEndian.PutUint16(dev.config[0x00:0x02], vendorId)
	binary.LittleEndian.PutUint16(dev.config[0x02:0x04], deviceId)
	dev.config[0x08] = revision

	// Fix class code writing - it's 3 bytes starting at 0x09
	dev.config[0x09] = uint8(classId)       // prog interface
	dev.config[0x0A] = uint8(classId >> 8)  // subclass
	dev.config[0x0B] = uint8(classId >> 16) // class

	dev.config[0x0E] = 0x00 // header type

	return dev
}

type PciBus struct {
	cpu VMDevice

	busNum uint8

	addr uint32

	devices map[uint8]*PciDevice
}

func (p *PciBus) AllocateDeviceNumber() (uint8, error) {
	for i := 0; i < 256; i += 8 {
		if _, ok := p.devices[uint8(i)]; !ok {
			return uint8(i), nil
		}
	}
	return 0, fmt.Errorf("no available device number")
}

// AddDevice adds a PCI device to the bus.
func (p *PciBus) AddDevice(devfn uint8, dev *PciDevice) {
	p.devices[devfn] = dev
}

// Add this method to your PciBus struct in the PCI code
func (p *PciBus) HandleIOPort(io *kvm.KVMIoEvent) error {
	for _, dev := range p.devices {
		if dev.handlesIOPort(io.Port) {
			return dev.handleIOPort(io)
		}
	}
	return fmt.Errorf("no PCI device handles port 0x%04x", io.Port)
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
	return []uint16{0xCF8, 0xCFC, 0xCFD, 0xCFE, 0xCFF}
}

// handleConfigAccess handles PCI configuration space access for both 0xCFC and byte-level ports
func (p *PciBus) handleConfigAccess(
	io *kvm.KVMIoEvent,
	byteOffset uint8,
) error {
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
		log.Default().Info("pci device not found", "devfn", devfn)
		return p.valOnes(io)
	}
	config_addr := (addr & 0xfc) + uint32(byteOffset)

	return dev.ConfigIO(io, config_addr)
}

// IO implements IODevice.
func (p *PciBus) IO(io *kvm.KVMIoEvent) error {
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
	} else if io.Port >= 0xCFC && io.Port <= 0xCFF {
		// Handle byte-level access to configuration data
		byteOffset := uint8(io.Port - 0xCFC)
		return p.handleConfigAccess(io, byteOffset)
	} else {
		return fmt.Errorf("unknown port 0x%04x", io.Port)
	}
}

var (
	_ IODevice = &PciBus{}
)
