//go:build linux && amd64

package main

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/pkg/log"
)

type VirtIOKind string

const (
	VirtIOKindConsole VirtIOKind = "console"
)

type VirtIODevice interface {
	Kind() VirtIOKind
	GetHostFeatures() uint32
	SetGuestFeatures(features uint32)
	GetStatus() uint8
	SetStatus(status uint8)
	Reset()
	GetConfigSize() int
	ReadConfig(offset int, size int) []byte
	WriteConfig(offset int, data []byte)
}

// Virtio legacy register offsets
const (
	VIRTIO_PCI_HOST_FEATURES  = 0x00
	VIRTIO_PCI_GUEST_FEATURES = 0x04
	VIRTIO_PCI_QUEUE_PFN      = 0x08
	VIRTIO_PCI_QUEUE_NUM      = 0x0C
	VIRTIO_PCI_QUEUE_SEL      = 0x0E
	VIRTIO_PCI_QUEUE_NOTIFY   = 0x10
	VIRTIO_PCI_STATUS         = 0x12
	VIRTIO_PCI_ISR            = 0x13
	VIRTIO_PCI_CONFIG_START   = 0x14
)

// Virtio status bits
const (
	VIRTIO_CONFIG_S_ACKNOWLEDGE = 1
	VIRTIO_CONFIG_S_DRIVER      = 2
	VIRTIO_CONFIG_S_DRIVER_OK   = 4
	VIRTIO_CONFIG_S_FEATURES_OK = 8
	VIRTIO_CONFIG_S_FAILED      = 0x80
)

// Virtio console features
const (
	VIRTIO_CONSOLE_F_SIZE      = 0
	VIRTIO_CONSOLE_F_MULTIPORT = 1
	VIRTIO_F_INDIRECT_DESC     = 28
	VIRTIO_F_EVENT_IDX         = 29
	VIRTIO_F_VERSION_1         = 32
)

// ISR status bits
const (
	VIRTIO_PCI_ISR_QUEUE  = 0x1 // Queue interrupt
	VIRTIO_PCI_ISR_CONFIG = 0x2 // Configuration change interrupt
)

type virtIODevice struct {
	dev VirtIODevice
	cpu VMDevice

	// Legacy interface state
	ioBase        uint16
	hostFeatures  uint32
	guestFeatures uint32
	queueSel      uint16
	status        uint8
	isr           uint8

	// Queue state
	queues    [16]virtQueue
	numQueues uint16
	irqLine   uint8
}

type virtQueue struct {
	pfn       uint32
	size      uint16
	enabled   bool
	lastAvail uint16
}

func (v *virtIODevice) initializeQueues() {
	// Console typically has 2 queues: receiveq (0) and transmitq (1)
	for i := 0; i < 2; i++ {
		v.queues[i] = virtQueue{
			size:    256,
			enabled: false,
		}
	}
	v.numQueues = 2
}

func (v *virtIODevice) deliverInterrupt() error {
	if v.irqLine != 0xFF && v.irqLine != 0 {
		// Set ISR register
		v.isr |= VIRTIO_PCI_ISR_QUEUE

		// Deliver interrupt to guest
		log.Default().Info("virtio delivering interrupt", "irq", v.irqLine)
		return v.cpu.RaiseIrq(v.irqLine)
	}
	return nil
}

func (v *virtIODevice) handleQueueNotify(queueIndex uint16) {
	if queueIndex >= uint16(len(v.queues)) {
		log.Default().Warn("virtio queue notify for invalid queue", "queue", queueIndex)
		return
	}

	queue := &v.queues[queueIndex]
	if !queue.enabled {
		log.Default().Warn("virtio queue notify for disabled queue", "queue", queueIndex)
		return
	}

	log.Default().Info("virtio processing queue", "queue", queueIndex, "pfn", queue.pfn)

	// Process the queue and then deliver interrupt
	if err := v.deliverInterrupt(); err != nil {
		log.Default().Error("failed to deliver virtio interrupt", "error", err)
	}
}

func (v *virtIODevice) handleIORead(port uint16, size int) []byte {
	if v.ioBase == 0 {
		log.Default().Warn("virtio IO read before BAR configured", "port", port)
		return make([]byte, size)
	}

	offset := port - v.ioBase
	log.Default().Info(
		"virtio IO read",
		"port", fmt.Sprintf("0x%04x", port),
		"offset", fmt.Sprintf("0x%02x", offset),
		"size", size,
		"register", v.getRegisterName(offset),
	)

	switch offset {
	case VIRTIO_PCI_HOST_FEATURES:
		features := v.dev.GetHostFeatures()
		data := make([]byte, size)
		if size >= 4 {
			binary.LittleEndian.PutUint32(data, features)
		} else {
			for i := 0; i < size; i++ {
				data[i] = byte(features >> (i * 8))
			}
		}
		log.Default().Info("virtio returning host features", "features", fmt.Sprintf("0x%08x", features))
		return data

	case VIRTIO_PCI_GUEST_FEATURES:
		data := make([]byte, size)
		if size >= 4 {
			binary.LittleEndian.PutUint32(data, v.guestFeatures)
		} else {
			for i := 0; i < size; i++ {
				data[i] = byte(v.guestFeatures >> (i * 8))
			}
		}
		log.Default().Info("virtio returning guest features", "features", fmt.Sprintf("0x%08x", v.guestFeatures))
		return data

	case VIRTIO_PCI_QUEUE_PFN:
		data := make([]byte, size)
		if v.queueSel < uint16(len(v.queues)) && size >= 4 {
			binary.LittleEndian.PutUint32(data, v.queues[v.queueSel].pfn)
		}
		log.Default().Info("virtio returning queue PFN", "queue", v.queueSel, "pfn", v.queues[v.queueSel].pfn)
		return data

	case VIRTIO_PCI_QUEUE_NUM:
		data := make([]byte, size)
		var queueSize uint16
		if v.queueSel < 2 { // Console has 2 queues: receiveq and transmitq
			queueSize = 256
		} else {
			queueSize = 0 // Invalid queue
		}
		if size >= 2 {
			binary.LittleEndian.PutUint16(data, queueSize)
		} else if size == 1 {
			data[0] = byte(queueSize)
		}
		log.Default().Info("virtio returning queue size", "queue", v.queueSel, "size", queueSize)
		return data

	case VIRTIO_PCI_QUEUE_SEL:
		data := make([]byte, size)
		if size >= 2 {
			binary.LittleEndian.PutUint16(data, v.queueSel)
		} else if size == 1 {
			data[0] = byte(v.queueSel)
		}
		log.Default().Info("virtio returning queue sel", "queue", v.queueSel)
		return data

	case VIRTIO_PCI_STATUS:
		status := v.dev.GetStatus()
		log.Default().Info("virtio returning status", "status", fmt.Sprintf("0x%02x", status))
		return []byte{status}

	case VIRTIO_PCI_ISR:
		// Reading ISR clears it
		isr := v.isr
		v.isr = 0
		log.Default().Info("virtio ISR read and cleared", "value", fmt.Sprintf("0x%02x", isr))
		return []byte{isr}

	default:
		if offset >= VIRTIO_PCI_CONFIG_START {
			configOffset := int(offset - VIRTIO_PCI_CONFIG_START)
			data := v.dev.ReadConfig(configOffset, size)
			log.Default().Info("virtio config read", "offset", configOffset, "size", size, "data", fmt.Sprintf("%x", data))
			return data
		} else {
			log.Default().Warn("virtio IO read from unknown register", "port", fmt.Sprintf("0x%04x", port), "offset", fmt.Sprintf("0x%02x", offset))
		}
	}

	// Return zeros for unhandled reads
	data := make([]byte, size)
	log.Default().Info("virtio returning zeros", "size", size)
	return data
}

func (v *virtIODevice) getRegisterName(offset uint16) string {
	switch offset {
	case 0x00:
		return "HOST_FEATURES"
	case 0x04:
		return "GUEST_FEATURES"
	case 0x08:
		return "QUEUE_PFN"
	case 0x0C:
		return "QUEUE_NUM"
	case 0x0E:
		return "QUEUE_SEL"
	case 0x10:
		return "QUEUE_NOTIFY"
	case 0x12:
		return "STATUS"
	case 0x13:
		return "ISR"
	default:
		if offset >= VIRTIO_PCI_CONFIG_START {
			return fmt.Sprintf("CONFIG(0x%02x)", offset-VIRTIO_PCI_CONFIG_START)
		}
		return fmt.Sprintf("UNKNOWN(0x%02x)", offset)
	}
}

func (v *virtIODevice) handleIOWrite(port uint16, data []byte) {
	if v.ioBase == 0 {
		log.Default().Warn("virtio IO write before BAR configured", "port", port)
		return
	}

	offset := port - v.ioBase
	log.Default().Info(
		"virtio IO write",
		"port",
		port,
		"offset",
		offset,
		"data",
		fmt.Sprintf("%x", data),
		"register",
		v.getRegisterName(offset),
	)

	switch offset {
	case VIRTIO_PCI_GUEST_FEATURES:
		if len(data) >= 4 {
			v.guestFeatures = binary.LittleEndian.Uint32(data)
		} else if len(data) == 2 {
			v.guestFeatures = uint32(binary.LittleEndian.Uint16(data))
		} else if len(data) == 1 {
			v.guestFeatures = uint32(data[0])
		}
		v.dev.SetGuestFeatures(v.guestFeatures)
		log.Default().Info("virtio guest features set", "features", fmt.Sprintf("0x%08x", v.guestFeatures))

	case VIRTIO_PCI_QUEUE_SEL:
		if len(data) >= 2 {
			v.queueSel = binary.LittleEndian.Uint16(data)
		} else if len(data) == 1 {
			v.queueSel = uint16(data[0])
		}
		log.Default().Info("virtio queue selected", "queue", v.queueSel)

	case VIRTIO_PCI_QUEUE_PFN:
		if len(data) >= 4 {
			pfn := binary.LittleEndian.Uint32(data)
			log.Default().Info("virtio queue PFN set", "queue", v.queueSel, "pfn", pfn)
			if v.queueSel < uint16(len(v.queues)) {
				v.queues[v.queueSel].pfn = pfn
				v.queues[v.queueSel].enabled = (pfn != 0)
				if pfn != 0 {
					log.Default().Info(
						"virtio queue enabled",
						"queue",
						v.queueSel,
						"pfn",
						pfn,
						"addr",
						fmt.Sprintf("0x%x", pfn*4096),
					)
				}
			}
		}

	case VIRTIO_PCI_QUEUE_NOTIFY:
		if len(data) >= 2 {
			queue := binary.LittleEndian.Uint16(data)
			log.Default().Info("virtio queue notify", "queue", queue)
			v.handleQueueNotify(queue)
		} else if len(data) == 1 {
			queue := uint16(data[0])
			log.Default().Info("virtio queue notify", "queue", queue)
			v.handleQueueNotify(queue)
		}

	case VIRTIO_PCI_STATUS:
		if len(data) >= 1 {
			v.dev.SetStatus(data[0])
		}

	default:
		if offset >= VIRTIO_PCI_CONFIG_START {
			configOffset := int(offset - VIRTIO_PCI_CONFIG_START)
			v.dev.WriteConfig(configOffset, data)
		}
	}
}

func (v *virtIODevice) barSet(index uint8, addr uint32, enabled bool) error {
	log.Default().Info(
		"virtio barSet",
		"index",
		index,
		"addr",
		fmt.Sprintf("0x%08x", addr),
		"enabled",
		enabled,
	)

	if index == 0 && enabled { // Legacy I/O BAR
		v.ioBase = uint16(addr & 0xFFFC)
		log.Default().Info("virtio I/O base set", "base", v.ioBase)

		if v.handleIORead == nil || v.handleIOWrite == nil {
			log.Default().Error("virtio I/O handlers not set!")
			return fmt.Errorf("I/O handlers not configured")
		}
		log.Default().Info(
			"virtio I/O handlers confirmed",
			"read",
			v.handleIORead != nil,
			"write",
			v.handleIOWrite != nil,
		)
	} else if index == 0 && !enabled {
		log.Default().Info("virtio I/O disabled")
		v.ioBase = 0
	}

	return nil
}

func (v *virtIODevice) toPCIDevice() (*PciDevice, error) {
	var dev *PciDevice
	switch kind := v.dev.Kind(); kind {
	case VirtIOKindConsole:
		// Use revision 0 for legacy virtio devices
		dev = NewPciDevice("virtio_console", 0x1af4, 0x1003, 0x00, 0x078000)
		dev.writeU16(0x2e, 0x1000) // subsystem device ID
	default:
		return nil, fmt.Errorf("unsupported virtio device kind %s", kind)
	}

	dev.writeU16(0x2c, 0x1af4) // subsystem vendor ID

	// Set up proper IRQ configuration
	dev.writeU8(PCI_INTERRUPT_LINE, v.irqLine)
	dev.writeU8(PCI_INTERRUPT_PIN, 1) // INT A

	// Make sure revision is 0 for legacy
	dev.writeU8(0x08, 0x00)

	// Register legacy I/O BAR (BAR0)
	if err := dev.registerBar(0, 64, PCI_ADDRESS_SPACE_IO, v.barSet); err != nil {
		return nil, err
	}

	// Set up I/O handlers
	dev.setIOHandler(v.handleIORead, v.handleIOWrite)

	log.Default().Info(
		"virtio device created",
		"kind", v.dev.Kind(),
		"revision", dev.config[0x08],
		"irq_line", dev.config[PCI_INTERRUPT_LINE],
		"irq_pin", dev.config[PCI_INTERRUPT_PIN],
		"vendor_id", binary.LittleEndian.Uint16(dev.config[0x00:0x02]),
		"device_id", binary.LittleEndian.Uint16(dev.config[0x02:0x04]),
	)

	return dev, nil
}

type VirtIOBus struct {
	cpu     VMDevice
	pci     *PciBus
	devices []*virtIODevice
}

func (v *VirtIOBus) AddDevice(dev VirtIODevice) error {
	newDev := &virtIODevice{
		dev:       dev,
		cpu:       v.cpu,
		numQueues: 2,
		irqLine:   11,
	}
	newDev.initializeQueues()

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

	status        uint8
	hostFeatures  uint32
	guestFeatures uint32
}

func (v *VirtIOConsole) Kind() VirtIOKind {
	return VirtIOKindConsole
}

func (v *VirtIOConsole) GetHostFeatures() uint32 {
	// Only advertise basic console features for legacy compatibility
	features := uint32(1 << VIRTIO_CONSOLE_F_SIZE)
	log.Default().Info("virtio console host features", "features", fmt.Sprintf("0x%08x", features))
	return features
}

func (v *VirtIOConsole) SetGuestFeatures(features uint32) {
	v.guestFeatures = features
	log.Default().Info("virtio console guest features", "features", fmt.Sprintf("0x%08x", features))
}

func (v *VirtIOConsole) GetStatus() uint8 {
	return v.status
}

func (v *VirtIOConsole) SetStatus(status uint8) {
	log.Default().Info("virtio console status change", "old", fmt.Sprintf("0x%02x", v.status), "new", fmt.Sprintf("0x%02x", status))
	oldStatus := v.status
	v.status = status

	if status == 0 {
		v.Reset()
		return
	}

	// Handle status transitions
	if (status&VIRTIO_CONFIG_S_ACKNOWLEDGE) != 0 &&
		(oldStatus&VIRTIO_CONFIG_S_ACKNOWLEDGE) == 0 {
		log.Default().Info("virtio console: device acknowledged")
	}
	if (status&VIRTIO_CONFIG_S_DRIVER) != 0 &&
		(oldStatus&VIRTIO_CONFIG_S_DRIVER) == 0 {
		log.Default().Info("virtio console: driver loaded")
	}
	if (status&VIRTIO_CONFIG_S_FEATURES_OK) != 0 &&
		(oldStatus&VIRTIO_CONFIG_S_FEATURES_OK) == 0 {
		log.Default().Info("virtio console: features negotiated")
	}
	if (status&VIRTIO_CONFIG_S_DRIVER_OK) != 0 &&
		(oldStatus&VIRTIO_CONFIG_S_DRIVER_OK) == 0 {
		log.Default().Info("virtio console: driver ready - device is now operational")
	}
	if (status & VIRTIO_CONFIG_S_FAILED) != 0 {
		log.Default().Error("virtio console: device failed")
	}
}

func (v *VirtIOConsole) Reset() {
	log.Default().Info("virtio console reset")
	v.status = 0
	v.guestFeatures = 0
}

func (v *VirtIOConsole) GetConfigSize() int {
	return 8 // cols(2) + rows(2) + max_nr_ports(4)
}

func (v *VirtIOConsole) ReadConfig(offset int, size int) []byte {
	data := make([]byte, size)

	// Default console config: 80x25, 1 port
	config := make([]byte, 8)
	binary.LittleEndian.PutUint16(config[0:], 80) // cols
	binary.LittleEndian.PutUint16(config[2:], 25) // rows
	binary.LittleEndian.PutUint32(config[4:], 1)  // max_nr_ports

	for i := 0; i < size && offset+i < len(config); i++ {
		data[i] = config[offset+i]
	}

	log.Default().Info(
		"virtio console config read",
		"offset",
		offset,
		"size",
		size,
		"data",
		fmt.Sprintf("%x", data),
	)

	return data
}

func (v *VirtIOConsole) WriteConfig(offset int, data []byte) {
	log.Default().Info(
		"virtio console config write",
		"offset",
		offset,
		"data",
		fmt.Sprintf("%x", data),
	)
	// Console config is typically read-only
}

var (
	_ VirtIODevice = &VirtIOConsole{}
)

func NewVirtioConsole(stdout io.Writer, stdin io.Reader) *VirtIOConsole {
	return &VirtIOConsole{
		stdout:       stdout,
		stdin:        stdin,
		hostFeatures: (1 << VIRTIO_CONSOLE_F_SIZE),
	}
}
