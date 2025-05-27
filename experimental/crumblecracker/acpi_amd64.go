package main

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

// ACPI table header (common to all tables)
type ACPITableHeader struct {
	Signature       [4]byte
	Length          uint32
	Revision        uint8
	Checksum        uint8
	OEMID           [6]byte
	OEMTableID      [8]byte
	OEMRevision     uint32
	CreatorID       [4]byte
	CreatorRevision uint32
}

// Root System Description Pointer
type ACPIRSDP struct {
	Signature   [8]byte // "RSD PTR "
	Checksum    uint8
	OEMID       [6]byte
	Revision    uint8
	RSDTAddress uint32
	Length      uint32
	XSDTAddress uint64
	XChecksum   uint8
	Reserved    [3]uint8
}

// APIC structure types
const (
	ACPI_MADT_TYPE_LOCAL_APIC         = 0
	ACPI_MADT_TYPE_IO_APIC            = 1
	ACPI_MADT_TYPE_INTERRUPT_OVERRIDE = 2
	ACPI_MADT_TYPE_LOCAL_APIC_NMI     = 4
)

type ACPIBuilder struct {
	memory vm.MemoryRegion
}

func NewACPIBuilder(memory vm.MemoryRegion) *ACPIBuilder {
	return &ACPIBuilder{
		memory: memory,
	}
}

func (a *ACPIBuilder) calculateChecksum(data []byte) uint8 {
	var sum uint8
	for _, b := range data {
		sum += b
	}
	return uint8(-int8(sum))
}

func (a *ACPIBuilder) createTableHeader(signature string, length uint32) ACPITableHeader {
	var sig [4]byte
	copy(sig[:], signature)

	var oemid [6]byte
	copy(oemid[:], "TINYVM")

	var oemtableid [8]byte
	copy(oemtableid[:], "TINYVM01")

	var creatorid [4]byte
	copy(creatorid[:], "TINY")

	return ACPITableHeader{
		Signature:       sig,
		Length:          length,
		Revision:        1,
		Checksum:        0, // Will be calculated later
		OEMID:           oemid,
		OEMTableID:      oemtableid,
		OEMRevision:     1,
		CreatorID:       creatorid,
		CreatorRevision: 1,
	}
}

func (a *ACPIBuilder) serializeTableHeader(header ACPITableHeader) []byte {
	var buf []byte
	buf = append(buf, header.Signature[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, header.Length)
	buf = append(buf, header.Revision)
	buf = append(buf, header.Checksum)
	buf = append(buf, header.OEMID[:]...)
	buf = append(buf, header.OEMTableID[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, header.OEMRevision)
	buf = append(buf, header.CreatorID[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, header.CreatorRevision)
	return buf
}

func (a *ACPIBuilder) createRSDP(rsdtAddr uint32) []byte {
	rsdp := ACPIRSDP{
		Signature:   [8]byte{'R', 'S', 'D', ' ', 'P', 'T', 'R', ' '},
		Checksum:    0, // Will be calculated
		OEMID:       [6]byte{'T', 'I', 'N', 'Y', 'V', 'M'},
		Revision:    0, // ACPI 1.0
		RSDTAddress: rsdtAddr,
		Length:      20, // ACPI 1.0 RSDP length
		XSDTAddress: 0,
		XChecksum:   0,
		Reserved:    [3]uint8{0, 0, 0},
	}

	// Serialize first 20 bytes for ACPI 1.0
	var buf []byte
	buf = append(buf, rsdp.Signature[:]...)
	buf = append(buf, 0) // Checksum placeholder
	buf = append(buf, rsdp.OEMID[:]...)
	buf = append(buf, rsdp.Revision)
	buf = binary.LittleEndian.AppendUint32(buf, rsdp.RSDTAddress)

	// Calculate checksum for first 20 bytes
	buf[8] = a.calculateChecksum(buf)

	return buf
}

func (a *ACPIBuilder) createRSDT(tableAddresses []uint32) []byte {
	headerSize := int(unsafe.Sizeof(ACPITableHeader{}))
	totalSize := headerSize + len(tableAddresses)*4

	header := a.createTableHeader("RSDT", uint32(totalSize))
	buf := a.serializeTableHeader(header)

	// Add table addresses
	for _, addr := range tableAddresses {
		buf = binary.LittleEndian.AppendUint32(buf, addr)
	}

	// Calculate and set checksum
	buf[9] = a.calculateChecksum(buf)

	return buf
}

func (a *ACPIBuilder) createFACP(dsdtAddr uint32) []byte {
	headerSize := int(unsafe.Sizeof(ACPITableHeader{}))
	// FADT structure size (we'll use a minimal version)
	totalSize := headerSize + 80 // Minimal FADT size

	header := a.createTableHeader("FACP", uint32(totalSize))
	buf := a.serializeTableHeader(header)

	// FADT fields (minimal set)
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // FIRMWARE_CTRL
	buf = binary.LittleEndian.AppendUint32(buf, dsdtAddr) // DSDT
	buf = append(buf, 0)                                  // Reserved1
	buf = append(buf, 0)                                  // Preferred_PM_Profile
	buf = binary.LittleEndian.AppendUint16(buf, 9)        // SCI_INT
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // SMI_CMD
	buf = append(buf, 0)                                  // ACPI_ENABLE
	buf = append(buf, 0)                                  // ACPI_DISABLE
	buf = append(buf, 0)                                  // S4BIOS_REQ
	buf = append(buf, 0)                                  // PSTATE_CNT
	buf = binary.LittleEndian.AppendUint32(buf, 0x600)    // PM1a_EVT_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // PM1b_EVT_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0x604)    // PM1a_CNT_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // PM1b_CNT_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // PM2_CNT_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0x608)    // PM_TMR_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // GPE0_BLK
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // GPE1_BLK
	buf = append(buf, 4)                                  // PM1_EVT_LEN
	buf = append(buf, 2)                                  // PM1_CNT_LEN
	buf = append(buf, 0)                                  // PM2_CNT_LEN
	buf = append(buf, 4)                                  // PM_TMR_LEN
	buf = append(buf, 0)                                  // GPE0_BLK_LEN
	buf = append(buf, 0)                                  // GPE1_BLK_LEN
	buf = append(buf, 0)                                  // GPE1_BASE
	buf = append(buf, 0)                                  // CST_CNT
	buf = binary.LittleEndian.AppendUint16(buf, 0)        // P_LVL2_LAT
	buf = binary.LittleEndian.AppendUint16(buf, 0)        // P_LVL3_LAT
	buf = binary.LittleEndian.AppendUint16(buf, 0)        // FLUSH_SIZE
	buf = binary.LittleEndian.AppendUint16(buf, 0)        // FLUSH_STRIDE
	buf = append(buf, 0)                                  // DUTY_OFFSET
	buf = append(buf, 0)                                  // DUTY_WIDTH
	buf = append(buf, 0)                                  // DAY_ALRM
	buf = append(buf, 0)                                  // MON_ALRM
	buf = append(buf, 0)                                  // CENTURY
	buf = binary.LittleEndian.AppendUint16(buf, 0)        // BOOT_ARCH
	buf = append(buf, 0)                                  // Reserved2
	buf = binary.LittleEndian.AppendUint32(buf, 0)        // FLAGS

	// Calculate and set checksum
	buf[9] = a.calculateChecksum(buf)

	return buf
}

func (a *ACPIBuilder) createMADT() []byte {
	headerSize := int(unsafe.Sizeof(ACPITableHeader{}))

	// Calculate total size: header + MADT header + APIC structures
	apicStructuresSize := 8 + 12 + 10*4 // Local APIC + IO APIC + 4 interrupt overrides
	totalSize := headerSize + 8 + apicStructuresSize

	header := a.createTableHeader("APIC", uint32(totalSize))
	buf := a.serializeTableHeader(header)

	// MADT header
	buf = binary.LittleEndian.AppendUint32(buf, 0xFEE00000) // Local APIC Address
	buf = binary.LittleEndian.AppendUint32(buf, 0)          // Flags

	// Local APIC structure
	buf = append(buf, ACPI_MADT_TYPE_LOCAL_APIC)   // Type
	buf = append(buf, 8)                           // Length
	buf = append(buf, 0)                           // Processor ID
	buf = append(buf, 0)                           // APIC ID
	buf = binary.LittleEndian.AppendUint32(buf, 1) // Flags (enabled)

	// IO APIC structure
	buf = append(buf, ACPI_MADT_TYPE_IO_APIC)               // Type
	buf = append(buf, 12)                                   // Length
	buf = append(buf, 0)                                    // IO APIC ID
	buf = append(buf, 0)                                    // Reserved
	buf = binary.LittleEndian.AppendUint32(buf, 0xFEC00000) // IO APIC Address
	buf = binary.LittleEndian.AppendUint32(buf, 0)          // Global System Interrupt Base

	// Interrupt Source Override structures
	overrides := []struct {
		source, gsi uint32
		flags       uint16
	}{
		{0, 2, 0},     // Timer
		{5, 5, 0xD},   // IRQ 5 (active high, level triggered)
		{9, 9, 0xD},   // IRQ 9 (active high, level triggered)
		{10, 10, 0xD}, // IRQ 10 (active high, level triggered)
		{11, 11, 0xD}, // IRQ 11 (active high, level triggered)
	}

	for _, override := range overrides {
		buf = append(buf, ACPI_MADT_TYPE_INTERRUPT_OVERRIDE)        // Type
		buf = append(buf, 10)                                       // Length
		buf = append(buf, 0)                                        // Bus (ISA)
		buf = append(buf, uint8(override.source))                   // Source
		buf = binary.LittleEndian.AppendUint32(buf, override.gsi)   // Global System Interrupt
		buf = binary.LittleEndian.AppendUint16(buf, override.flags) // Flags
	}

	// Calculate and set checksum
	buf[9] = a.calculateChecksum(buf)

	return buf
}

func (a *ACPIBuilder) createDSDT() []byte {
	// This is a minimal DSDT with PCI IRQ routing
	// We'll create the AML bytecode manually for the essential parts

	amlCode := []byte{
		// DefinitionBlock("", "DSDT", 1, "TINYVM", "TINYVM01", 1)
		// Scope(\_SB) {
		0x10, 0x41, 0x1E, // Scope, length will be patched
		'\\', '_', 'S', 'B', '_',

		// Device(PCI0) {
		0x5B, 0x82, 0x41, 0x1A, // Device, length
		'P', 'C', 'I', '0',

		// Name(_HID, EISAID("PNP0A03"))
		0x08, '_', 'H', 'I', 'D', // Name _HID
		0x0C, 0x41, 0xD0, 0x0A, 0x03, // EISAID("PNP0A03")

		// Name(_ADR, 0x00000000)
		0x08, '_', 'A', 'D', 'R', // Name _ADR
		0x00, // 0x00000000

		// Name(_BBN, 0x00)
		0x08, '_', 'B', 'B', 'N', // Name _BBN
		0x00, // 0x00

		// Name(_CRS, ResourceTemplate() {
		0x08, '_', 'C', 'R', 'S', // Name _CRS
		0x11, 0x42, 0x04, 0x0A, 0x3F, // Buffer
		// WordBusNumber(ResourceProducer, MinFixed, MaxFixed, PosDecode, 0x0000, 0x0000, 0x00FF, 0x0000, 0x0100)
		0x88, 0x0D, 0x00, 0x02, 0x0C, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x01,
		// IO(Decode16, 0x0CF8, 0x0CF8, 0x01, 0x08)
		0x47, 0x01, 0xF8, 0x0C, 0xF8, 0x0C, 0x01, 0x08,
		// WordIO(ResourceProducer, MinFixed, MaxFixed, PosDecode, EntireRange, 0x0000, 0x0000, 0x0CF7, 0x0000, 0x0CF8)
		0x88, 0x0D, 0x00, 0x01, 0x0C, 0x03, 0x00, 0x00, 0x00, 0x00, 0xF7, 0x0C, 0x00, 0x00, 0xF8, 0x0C,
		// WordIO(ResourceProducer, MinFixed, MaxFixed, PosDecode, EntireRange, 0x0000, 0x0D00, 0xFFFF, 0x0000, 0xF300)
		0x88, 0x0D, 0x00, 0x01, 0x0C, 0x03, 0x00, 0x00, 0x00, 0x0D, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0xF3,
		// End tag
		0x79, 0x00,
		// }) // End _CRS

		// Name(_PRT, Package() {
		0x08, '_', 'P', 'R', 'T', // Name _PRT
		0x12, 0x41, 0x08, 0x10, // Package, 16 elements

		// Package(){0x0002FFFF, 0, LNKA, 0},  // Device 2 (our Virtio console)
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x02, 0x00, // 0x0002FFFF
		0x00,               // 0
		'L', 'N', 'K', 'A', // LNKA
		0x00, // 0

		// Package(){0x0002FFFF, 1, LNKB, 0},
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x02, 0x00,
		0x01,
		'L', 'N', 'K', 'B',
		0x00,

		// Package(){0x0002FFFF, 2, LNKC, 0},
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x02, 0x00,
		0x0A, 0x02,
		'L', 'N', 'K', 'C',
		0x00,

		// Package(){0x0002FFFF, 3, LNKD, 0},
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x02, 0x00,
		0x0A, 0x03,
		'L', 'N', 'K', 'D',
		0x00,

		// Add more device entries for other slots...
		// Package(){0x0003FFFF, 0, LNKB, 0},  // Device 3
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x03, 0x00,
		0x00,
		'L', 'N', 'K', 'B',
		0x00,

		// Package(){0x0004FFFF, 0, LNKC, 0},  // Device 4
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x04, 0x00,
		0x00,
		'L', 'N', 'K', 'C',
		0x00,

		// Package(){0x0005FFFF, 0, LNKD, 0},  // Device 5
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x05, 0x00,
		0x00,
		'L', 'N', 'K', 'D',
		0x00,

		// Package(){0x0001FFFF, 0, LNKA, 0},  // Device 1 (PIIX3)
		0x12, 0x0D, 0x04,
		0x0C, 0xFF, 0xFF, 0x01, 0x00,
		0x00,
		'L', 'N', 'K', 'A',
		0x00,

		// Remaining 8 empty packages
		0x12, 0x03, 0x00, // Empty package
		0x12, 0x03, 0x00,
		0x12, 0x03, 0x00,
		0x12, 0x03, 0x00,
		0x12, 0x03, 0x00,
		0x12, 0x03, 0x00,
		0x12, 0x03, 0x00,
		0x12, 0x03, 0x00,
		// }) // End _PRT

		// } // End PCI0

		// PCI IRQ Link devices
		// Device(LNKA) {
		0x5B, 0x82, 0x41, 0x0C, // Device
		'L', 'N', 'K', 'A',

		// Name(_HID, EISAID("PNP0C0F"))
		0x08, '_', 'H', 'I', 'D',
		0x0C, 0x41, 0xD0, 0x0C, 0x0F,

		// Name(_UID, 0x01)
		0x08, '_', 'U', 'I', 'D',
		0x01,

		// Name(_STA, 0x0B)
		0x08, '_', 'S', 'T', 'A',
		0x0A, 0x0B,

		// Name(_PRS, ResourceTemplate() {
		0x08, '_', 'P', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		// IRQ(Level, ActiveLow, Shared, ) {10}
		0x23, 0x00, 0x04, 0x18,
		0x79, 0x00, // End tag
		// })

		// Name(_CRS, ResourceTemplate() {
		0x08, '_', 'C', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		// IRQ(Level, ActiveLow, Shared, ) {10}
		0x23, 0x00, 0x04, 0x18,
		0x79, 0x00, // End tag
		// })

		// Method(_SRS, 1, NotSerialized) {}
		0x14, 0x06, '_', 'S', 'R', 'S', 0x01,
		// } // End LNKA

		// Device(LNKB) {
		0x5B, 0x82, 0x41, 0x0C,
		'L', 'N', 'K', 'B',
		0x08, '_', 'H', 'I', 'D',
		0x0C, 0x41, 0xD0, 0x0C, 0x0F,
		0x08, '_', 'U', 'I', 'D',
		0x0A, 0x02,
		0x08, '_', 'S', 'T', 'A',
		0x0A, 0x0B,
		0x08, '_', 'P', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		0x23, 0x00, 0x04, 0x18, // IRQ 10
		0x79, 0x00,
		0x08, '_', 'C', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		0x23, 0x00, 0x04, 0x18,
		0x79, 0x00,
		0x14, 0x06, '_', 'S', 'R', 'S', 0x01,
		// } // End LNKB

		// Device(LNKC) {
		0x5B, 0x82, 0x41, 0x0C,
		'L', 'N', 'K', 'C',
		0x08, '_', 'H', 'I', 'D',
		0x0C, 0x41, 0xD0, 0x0C, 0x0F,
		0x08, '_', 'U', 'I', 'D',
		0x0A, 0x03,
		0x08, '_', 'S', 'T', 'A',
		0x0A, 0x0B,
		0x08, '_', 'P', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		0x23, 0x00, 0x08, 0x18, // IRQ 11
		0x79, 0x00,
		0x08, '_', 'C', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		0x23, 0x00, 0x08, 0x18,
		0x79, 0x00,
		0x14, 0x06, '_', 'S', 'R', 'S', 0x01,
		// } // End LNKC

		// Device(LNKD) {
		0x5B, 0x82, 0x41, 0x0C,
		'L', 'N', 'K', 'D',
		0x08, '_', 'H', 'I', 'D',
		0x0C, 0x41, 0xD0, 0x0C, 0x0F,
		0x08, '_', 'U', 'I', 'D',
		0x0A, 0x04,
		0x08, '_', 'S', 'T', 'A',
		0x0A, 0x0B,
		0x08, '_', 'P', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		0x23, 0x00, 0x08, 0x18, // IRQ 11
		0x79, 0x00,
		0x08, '_', 'C', 'R', 'S',
		0x11, 0x09, 0x0A, 0x06,
		0x23, 0x00, 0x08, 0x18,
		0x79, 0x00,
		0x14, 0x06, '_', 'S', 'R', 'S', 0x01,
		// } // End LNKD

		// } // End \_SB
	}

	// Create DSDT table
	headerSize := int(unsafe.Sizeof(ACPITableHeader{}))
	totalSize := headerSize + len(amlCode)

	header := a.createTableHeader("DSDT", uint32(totalSize))
	buf := a.serializeTableHeader(header)
	buf = append(buf, amlCode...)

	// Calculate and set checksum
	buf[9] = a.calculateChecksum(buf)

	return buf
}

func (a *ACPIBuilder) writeTable(addr uint32, data []byte) error {
	_, err := a.memory.WriteAt(data, int64(addr))
	return err
}

func (a *ACPIBuilder) CreateMinimalACPI() error {
	// Base addresses for ACPI tables (in BIOS area)
	const (
		RSDP_ADDR = 0x000F5F40
		RSDT_ADDR = 0x000F5000
		FACP_ADDR = 0x000F4000
		DSDT_ADDR = 0x000F0000
		MADT_ADDR = 0x000F3000
	)

	// Create MADT
	madt := a.createMADT()
	if err := a.writeTable(MADT_ADDR, madt); err != nil {
		return fmt.Errorf("failed to write MADT: %w", err)
	}

	// Create DSDT
	dsdt := a.createDSDT()
	if err := a.writeTable(DSDT_ADDR, dsdt); err != nil {
		return fmt.Errorf("failed to write DSDT: %w", err)
	}

	// Create FACP
	facp := a.createFACP(DSDT_ADDR)
	if err := a.writeTable(FACP_ADDR, facp); err != nil {
		return fmt.Errorf("failed to write FACP: %w", err)
	}

	// Create RSDT
	rsdt := a.createRSDT([]uint32{FACP_ADDR, MADT_ADDR})
	if err := a.writeTable(RSDT_ADDR, rsdt); err != nil {
		return fmt.Errorf("failed to write RSDT: %w", err)
	}

	// Create RSDP
	rsdp := a.createRSDP(RSDT_ADDR)
	if err := a.writeTable(RSDP_ADDR, rsdp); err != nil {
		return fmt.Errorf("failed to write RSDP: %w", err)
	}

	return nil
}

// Integration function - add this to your VM setup
func (vm *VirtualMachine) setupACPI() error {
	builder := NewACPIBuilder(vm.mem)
	return builder.CreateMinimalACPI()
}
