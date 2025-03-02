//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinyrange/tinyrange/experimental/crumblecracker/kvm"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"golang.org/x/arch/x86/x86asm"
	"golang.org/x/term"
)

var START_TIME = time.Now()

// From: https://github.com/bobuhiro11/gokvm

const (
	MagicSignature = 0x53726448

	LoadedHigh   = uint8(1 << 0)
	KeepSegments = uint8(1 << 6)
	CanUseHeap   = uint8(1 << 7)

	EddMbrSigMax = 16
	E820Max      = 128
	E820Ram      = 1
	E820Reserved = 2

	RealModeIvtBegin = 0x00000000
	EBDAStart        = 0x0009fc00
	VGARAMBegin      = 0x000a0000
	MBBIOSBegin      = 0x000f0000
	MBBIOSEnd        = 0x000fffff
)

type HypervisorDevice struct {
	kvm       *kvm.KVMDevice
	wg        sync.WaitGroup
	errorChan chan error
}

func (c *HypervisorDevice) Close() error {
	return c.kvm.Close()
}

func (c *HypervisorDevice) Wait() error {
	doneChan := make(chan struct{})

	go func() {
		c.wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		return nil
	case err := <-c.errorChan:
		return err
	}
}

func (c *HypervisorDevice) error(err error) {
	c.errorChan <- err
}

func (c *HypervisorDevice) CreateVirtualMachine(memSize uint32) (*VirtualMachine, error) {
	vm, err := c.kvm.CreateVirtualMachine()
	if err != nil {
		return nil, fmt.Errorf("failed to create virtual machine: %w", err)
	}

	mem, err := vm.MapMemory(0, memSize)
	if err != nil {
		return nil, fmt.Errorf("failed to map memory: %w", err)
	}

	return &VirtualMachine{hv: c, vm: vm, mem: mem}, nil
}

// Attr implements starlark.HasAttrs.
func (c *HypervisorDevice) Attr(name string) (starlark.Value, error) {
	if name == "new" {
		return starlark.NewBuiltin("HypervisorDevice.new", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				memSize uint32
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"mem_size", &memSize,
			); err != nil {
				return nil, err
			}

			return c.CreateVirtualMachine(memSize)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (c *HypervisorDevice) AttrNames() []string {
	return []string{"new"}
}

func (c *HypervisorDevice) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", c.Type())
}
func (c *HypervisorDevice) Freeze()              {}
func (c *HypervisorDevice) String() string       { return c.Type() }
func (c *HypervisorDevice) Truth() starlark.Bool { return starlark.True }
func (c *HypervisorDevice) Type() string         { return "HypervisorDevice" }

var (
	_ starlark.Value    = (*HypervisorDevice)(nil)
	_ starlark.HasAttrs = (*HypervisorDevice)(nil)
)

type VirtualMachine struct {
	hv       *HypervisorDevice
	vm       *kvm.KVMVirtualMachine
	mem      vm.MemoryRegion
	shutdown atomic.Bool
}

// Shutdown implements VMDevice.
func (vm *VirtualMachine) Shutdown() error {
	vm.shutdown.Store(true)

	return nil
}

// LowerIrq implements CPUDevice.
func (vm *VirtualMachine) LowerIrq(irq byte) error {
	return vm.vm.SetIRQStatus(uint32(irq), 0)
}

// RaiseIrq implements CPUDevice.
func (vm *VirtualMachine) RaiseIrq(irq byte) error {
	return vm.vm.SetIRQStatus(uint32(irq), 1)
}

const (
	KERNEL_PARAMS_ADDR  = 0x10000
	KERNEL_CMDLINE_ADDR = 0x20000
	KERNEL_LOAD_ADDR    = 0x100000
	KERNEL_INITRD_ADDR  = 0x4000000
)

func (vm *VirtualMachine) loadLinux(imagePath string, initrdPath string, cmdline string) error {
	image, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("failed to read image: %w", err)
	}
	defer image.Close()

	imageStat, err := image.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat image: %w", err)
	}

	var hdr BootParams

	// if err := binary.Read(io.NewSectionReader(image, 0, int64(binary.Size(&hdr))), binary.LittleEndian, &hdr); err != nil {
	// 	return fmt.Errorf("failed to read boot param: %w", err)
	// }

	if _, err := io.Copy(
		io.NewOffsetWriter(&hdr, 0),
		io.NewSectionReader(image, 0, int64(binary.Size(&hdr))),
	); err != nil {
		return fmt.Errorf("failed to read boot param: %w", err)
	}

	if hdr.SetupSignature() != MagicSignature {
		return fmt.Errorf("invalid boot param signature: %x", hdr.SetupSignature())
	}

	if hdr.HeaderFormatVersion() < 0x0206 {
		return fmt.Errorf("unsupported boot param version: %x", hdr.HeaderFormatVersion())
	}

	setupSectors := int64(hdr.SetupSects())
	setupSize := int64(setupSectors+1) * 512

	var newHdr BootParams

	var setupHdrStart int64 = 0x1f1
	var setupHdrEnd int64 = 0x202 + int64(hdr[0x201])

	// copy the setup header
	if _, err := io.Copy(
		io.NewOffsetWriter(&newHdr, setupHdrStart), // start of setup_sects
		io.NewSectionReader(hdr, setupHdrStart, (setupHdrEnd)-setupHdrStart),
	); err != nil {
		return fmt.Errorf("failed to copy setup header: %w", err)
	}

	newHdr.SetMountRootRdonly(0)
	newHdr.SetOrigVideoMode(0xff) // VGA
	newHdr.SetCmdLinePtr(KERNEL_CMDLINE_ADDR)
	newHdr.SetAltMemK(uint32(vm.mem.Size()/1024 - 1024))
	newHdr.SetLoaderType(0x01)
	if initrdPath != "" {
		initrd, err := os.Open(initrdPath)
		if err != nil {
			return fmt.Errorf("failed to read initrd: %w", err)
		}
		defer initrd.Close()

		initrdStat, err := initrd.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat initrd: %w", err)
		}

		// slog.Info("", "kernel end", fmt.Sprintf("0x%x", KERNEL_LOAD_ADDR+imageStat.Size()-setupSize))

		// if KERNEL_INITRD_ADDR+uint64(initrdStat.Size()) > 0x100000 {
		// 	return fmt.Errorf("initrd too large: 0x%x", initrdStat.Size())
		// }

		newHdr.SetInitrdStart(KERNEL_INITRD_ADDR)
		newHdr.SetInitrdSize(uint32(initrdStat.Size()))

		if _, err := io.Copy(io.NewOffsetWriter(vm.mem, KERNEL_INITRD_ADDR), initrd); err != nil {
			return fmt.Errorf("failed to write initrd: %w", err)
		}
	}
	newHdr.SetLoadflags(CanUseHeap | LoadedHigh | KeepSegments)
	newHdr.SetSetupSHeapEndPointer(0xfe00)
	// hdr.Hdr.ExtLoaderVer = 0x0

	newHdr.SetGdtTable(2, 0x00cf9b000000ffff) // CS
	newHdr.SetGdtTable(3, 0x00cf93000000ffff) // DS

	// if err := binary.Write(io.NewOffsetWriter(vm.mem, 0x10000), binary.LittleEndian, &newHdr); err != nil {
	// 	return fmt.Errorf("failed to write boot param: %w", err)
	// }
	if _, err := io.Copy(
		io.NewOffsetWriter(vm.mem, KERNEL_PARAMS_ADDR),
		io.NewSectionReader(&newHdr, 0, int64(binary.Size(&newHdr))),
	); err != nil {
		return fmt.Errorf("failed to write boot param: %w", err)
	}
	if _, err := io.Copy(io.NewOffsetWriter(vm.mem, KERNEL_CMDLINE_ADDR), bytes.NewBufferString(cmdline)); err != nil {
		return fmt.Errorf("failed to write command line: %w", err)
	}
	if _, err := io.Copy(io.NewOffsetWriter(vm.mem, KERNEL_LOAD_ADDR), io.NewSectionReader(image, setupSize, imageStat.Size()-setupSize)); err != nil {
		return fmt.Errorf("failed to write kernel: %w", err)
	}

	return nil
}

// Attr implements starlark.HasAttrs.
func (vm *VirtualMachine) Attr(name string) (starlark.Value, error) {
	if name == "load_linux" {
		return starlark.NewBuiltin("VirtualMachine.load_linux", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				imagePath  string
				initrdPath string
				cmdline    string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"image_path", &imagePath,
				"cmdline", &cmdline,
				"initrd_path?", &initrdPath,
			); err != nil {
				return nil, err
			}

			return starlark.None, vm.loadLinux(imagePath, initrdPath, cmdline)
		}), nil
	} else if name == "add_devices" {
		return starlark.NewBuiltin("VirtualMachine.add_devices", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			if err := vm.vm.SetIdentityMapAddr(0xffffc000); err != nil {
				return starlark.None, fmt.Errorf("failed to set identity map address: %w", err)
			}

			if err := vm.vm.SetTSSAddr(0xffffd000); err != nil {
				return starlark.None, fmt.Errorf("failed to set TSS address: %w", err)
			}

			if err := vm.vm.CreateIRQChip(); err != nil {
				return starlark.None, fmt.Errorf("failed to create IRQ chip: %w", err)
			}

			if err := vm.vm.CreatePIT2(false); err != nil {
				return starlark.None, fmt.Errorf("failed to create PIT2: %w", err)
			}

			return starlark.None, nil
		}), nil
	} else if name == "new_cpu" {
		return starlark.NewBuiltin("VirtualMachine.new_cpu", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("missing function argument")
			}

			target, ok := args[0].(starlark.Callable)
			if !ok {
				return nil, fmt.Errorf("invalid function argument")
			}

			cpu, err := vm.vm.CreateCPU()
			if err != nil {
				return nil, fmt.Errorf("failed to create CPU: %w", err)
			}

			cpuInst := &VirtualCPU{vm: vm, cpu: cpu}

			if err := cpuInst.setRegisters(); err != nil {
				return starlark.None, fmt.Errorf("failed to set registers: %w", err)
			}

			if err := cpuInst.setCPUID(); err != nil {
				return starlark.None, fmt.Errorf("failed to set CPUID: %w", err)
			}

			vm.hv.wg.Add(1)
			go func() {
				defer vm.hv.wg.Done()
				thread := &starlark.Thread{
					Name: "VirtualCPU",
				}

				_, err := starlark.Call(thread, target, append(starlark.Tuple{cpuInst}, args[1:]...), nil)
				if err != nil {
					vm.hv.error(err)
					return
				}
			}()

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (vm *VirtualMachine) AttrNames() []string {
	return []string{"load_linux", "add_devices", "new_cpu"}
}

func (c *VirtualMachine) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", c.Type())
}
func (c *VirtualMachine) Freeze()              {}
func (c *VirtualMachine) String() string       { return c.Type() }
func (c *VirtualMachine) Truth() starlark.Bool { return starlark.True }
func (c *VirtualMachine) Type() string         { return "VirtualMachine" }

var (
	_ starlark.Value    = (*VirtualMachine)(nil)
	_ starlark.HasAttrs = (*VirtualMachine)(nil)
	_ VMDevice          = (*VirtualMachine)(nil)
)

type VirtualCPU struct {
	vm  *VirtualMachine
	cpu *kvm.KVMCPU
}

func (vm *VirtualCPU) setRegisters() error {
	sregs, err := vm.cpu.GetSpecialRegisters()
	if err != nil {
		return fmt.Errorf("failed to get special registers: %w", err)
	}

	sregs.CR0 |= (1 << 0) // CR0_PE
	// sregs.gdt.base = KERNEL_PARAMS_ADDR +
	// 				 offsetof(struct linux_params, gdt_table);
	sregs.GDT.Base = KERNEL_PARAMS_ADDR + 4096
	// sregs.gdt.limit = sizeof(params->gdt_table) - 1;
	sregs.GDT.Limit = 32 - 1

	seg := kvm.KVMSegment{}

	seg.Limit = 0xffffffff
	seg.Present = 1
	seg.DB = 1
	seg.S = 1 // code/data
	seg.G = 1 // 4KB granularity

	seg.Type = 0xb // code
	seg.Selector = 2 << 3
	sregs.CS = seg

	seg.Type = 0x3 // data
	seg.Selector = 3 << 3
	sregs.DS = seg
	sregs.ES = seg
	sregs.SS = seg
	sregs.FS = seg
	sregs.GS = seg

	if err := vm.cpu.SetSpecialRegisters(sregs); err != nil {
		return fmt.Errorf("failed to set special registers: %w", err)
	}

	regs, err := vm.cpu.GetRegisters()
	if err != nil {
		return fmt.Errorf("failed to get registers: %w", err)
	}

	regs.RIP = KERNEL_LOAD_ADDR
	regs.RSI = KERNEL_PARAMS_ADDR
	regs.RFLAGS = 2

	if err := vm.cpu.SetRegisters(regs); err != nil {
		return fmt.Errorf("failed to set registers: %w", err)
	}

	return nil
}

func (vm *VirtualCPU) setCPUID() error {
	supported, err := vm.vm.hv.kvm.GetSupportedCPUID()
	if err != nil {
		return fmt.Errorf("failed to get supported CPUID: %w", err)
	}

	for i := 0; i < int(supported.NumEnt); i++ {
		ent := &supported.Ents[i]
		if ent.Function == kvm.KVM_CPUID_SIGNATURE {
			ent.EAX = kvm.KVM_CPUID_FEATURES
			ent.EBX = 0x4b4d564b // "KVMK"
			ent.ECX = 0x564b4d56 // "VMKV"
			ent.EDX = 0x4d       // "M"
		}
	}

	if err := vm.cpu.SetCPUID2(supported); err != nil {
		return fmt.Errorf("failed to set CPUID: %w", err)
	}

	return nil
}

func (cpu *VirtualCPU) Run() error {
	var exit atomic.Bool

	go func() {
		counts := 0
		for {
			time.Sleep(1 * time.Second)

			if counts++; counts > 4 {
				exit.Store(true)

				return
			}
		}
	}()

	var devices []IODevice

	cmos := &CMOSDevice{}

	// RTC
	devices = append(devices, cmos)

	pci := &PciBus{
		devices: make(map[uint8]*PciDevice),
	}

	// add i440fx chipset
	pci.AddDevice(0x00, NewPciDevice(0x8086, 0x1237, 0x02, 0x0600))

	// PCI
	devices = append(devices, pci)

	// Labels from https://bochs.sourceforge.io/techspec/PORTS.LST
	devices = append(devices, &NopDevice{PortList: []uint16{
		// keyboard controller
		0x60, // data port (r/w)
		0x61, // port B control (r/w)
		0x64, // read status/input buffer (r/w)

		// DMA page registers 74612
		0x80, // extra page register (r/w)
		0x87, // DMA channel 0 address byte 2 (r/w)

		// DMA 2 (second Direct Memory Access controller 8237)
		0xde, // DMA channel 4-7 write mask register (w)

		// Serial Port
		0x2e9,

		// Serial Port
		0x2f9,

		// 1st Enhanced Graphics Adapter/VGA
		0x3c0,
		0x3c6,
		0x3c8,
		0x3c9,

		// CGA
		0x3d4,
		0x3d5,
		0x3da,

		// Serial Port
		0x3e9,

		// Intel Pentium motherboard ("Neptune" chipset) ?
		0xcfa,
		0xcfb,
		0xcfe,

		// Intel Pentium motherboard ("Neptune" chipset) ?
		0xc000,
		0xc00a,
		0xc100,
		0xc10a,
		0xc200,
		0xc20a,
		0xc300,
		0xc30a,
		0xc400,
		0xc40a,
		0xc500,
		0xc50a,
		0xc600,
		0xc60a,
		0xc700,
		0xc70a,
		0xc800,
		0xc80a,
		0xc900,
		0xc90a,
		0xca00,
		0xca0a,
		0xcb00,
		0xcb0a,
		0xcc00,
		0xcc0a,
		0xcd00,
		0xcd0a,
		0xce00,
		0xce0a,
		0xcf00,
		0xcf0a,
	}})

	serial := &SerialDevice{
		Base:   0x3f8,
		IRQ:    4,
		Writer: os.Stdout,
	}

	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("failed to make terminal raw: %v", err)
	}
	defer func() { _ = term.Restore(fd, state) }()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				slog.Error("failed to read from stdin", "error", err)
				return
			}

			if _, err := serial.Write(buf[:n]); err != nil {
				slog.Error("failed to write to serial", "error", err)
				return
			}
		}
	}()

	devices = append(devices, serial)

	ioMap := make(map[uint16]IODevice)
	for _, device := range devices {
		if err := device.Init(cpu.vm); err != nil {
			return fmt.Errorf("failed to init device: %w", err)
		}

		for _, port := range device.Ports() {
			ioMap[port] = device
		}
	}

	slog.Info("running", "initTime", time.Since(START_TIME))

	for {
		if cpu.vm.shutdown.Load() || exit.Load() {
			break
		}

		exit, err := cpu.cpu.RunOnce()
		if err != nil {
			return fmt.Errorf("failed to run CPU: %w", err)
		}

		switch exit {
		case kvm.ExitIo:
			io := cpu.cpu.ExitIo()
			device, ok := ioMap[io.Port]
			if !ok {
				slog.Info("unknown io", "port", fmt.Sprintf("0x%x", io.Port), "direction", io.Direction, "size", io.Size)
				continue
			}
			if err := device.IO(io); err != nil {
				return fmt.Errorf("failed to handle io: %w", err)
			}
		case kvm.ExitShutdown:
			slog.Info("shutdown")

			if err := cpu.cpu.DumpRegisters(os.Stderr); err != nil {
				return fmt.Errorf("failed to dump registers: %w", err)
			}

			return nil
		case kvm.ExitIntr:
			continue
		case kvm.ExitDebug:
			// if err := cpu.DumpRegisters(os.Stderr); err != nil {
			// 	return fmt.Errorf("failed to dump registers: %w", err)
			// }

			var insn [16]byte
			regs, err := cpu.cpu.GetRegisters()
			if err != nil {
				return fmt.Errorf("failed to get registers: %w", err)
			}

			if _, err := io.ReadFull(io.NewSectionReader(cpu.vm.mem, int64(regs.RIP), 16), insn[:]); err != nil {
				return fmt.Errorf("failed to read instruction: %w", err)
			}

			inst, err := x86asm.Decode(insn[:], 64)
			if err != nil {
				return fmt.Errorf("failed to decode instruction: %w", err)
			}

			if _, err := fmt.Fprintf(os.Stderr, "instruction: %s\n", inst.String()); err != nil {
				return fmt.Errorf("failed to write instruction: %w", err)
			}
		default:
			return fmt.Errorf("unexpected exit: %s", exit)
		}
	}

	return nil
}

// Attr implements starlark.HasAttrs.
func (c *VirtualCPU) Attr(name string) (starlark.Value, error) {
	if name == "run" {
		return starlark.NewBuiltin("VirtualCPU.run", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return starlark.None, c.Run()
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (c *VirtualCPU) AttrNames() []string {
	return []string{"run"}
}

func (c *VirtualCPU) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", c.Type())
}
func (c *VirtualCPU) Freeze()              {}
func (c *VirtualCPU) String() string       { return c.Type() }
func (c *VirtualCPU) Truth() starlark.Bool { return starlark.True }
func (c *VirtualCPU) Type() string         { return "VirtualCPU" }

var (
	_ starlark.Value    = (*VirtualCPU)(nil)
	_ starlark.HasAttrs = (*VirtualCPU)(nil)
)

type commandLineParams struct {
	filename   string
	cpuprofile string
	keys       map[string]string
}

// Get implements starlark.Mapping.
func (c *commandLineParams) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	keyStr, ok := starlark.AsString(k)
	if !ok {
		return nil, false, fmt.Errorf("key not a string: %s", k.Type())
	}

	value, ok := c.keys[keyStr]
	if !ok {
		return starlark.String(""), true, nil
	}

	return starlark.String(value), true, nil
}

func (c *commandLineParams) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", c.Type())
}
func (c *commandLineParams) Freeze()              {}
func (c *commandLineParams) String() string       { return c.Type() }
func (c *commandLineParams) Truth() starlark.Bool { return starlark.True }
func (c *commandLineParams) Type() string         { return "commandLineParams" }

var (
	_ starlark.Value   = (*commandLineParams)(nil)
	_ starlark.Mapping = (*commandLineParams)(nil)
)

func parseCommandLine(args []string) (*commandLineParams, error) {
	params := &commandLineParams{
		keys: map[string]string{},
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "--cpu-profile=") {
			params.cpuprofile = arg[13:]
		} else if strings.HasPrefix(arg, "--") {
			keyValue := strings.SplitN(arg[2:], "=", 2)
			if len(keyValue) == 2 {
				params.keys[keyValue[0]] = keyValue[1]
			} else {
				params.keys[keyValue[0]] = ""
			}
		} else {
			if params.filename != "" {
				return nil, fmt.Errorf("unexpected argument: %s", arg)
			}
			params.filename = arg
		}
	}

	return params, nil
}

type VMDevice interface {
	RaiseIrq(irq byte) error
	LowerIrq(irq byte) error
	Shutdown() error
}

type IODevice interface {
	Init(cpu VMDevice) error
	Ports() []uint16
	IO(io *kvm.KVMIoEvent) error
}

type NopDevice struct {
	PortList []uint16
}

func (n *NopDevice) Init(cpu VMDevice) error {
	return nil
}

func (n *NopDevice) Ports() []uint16 {
	return n.PortList
}

func (n *NopDevice) IO(io *kvm.KVMIoEvent) error {
	return nil
}

var (
	_ IODevice = (*NopDevice)(nil)
)

type CMOSDevice struct {
	Data [256]byte
	addr uint8
	cpu  VMDevice
}

func (c *CMOSDevice) Init(cpu VMDevice) error {
	c.cpu = cpu

	return nil
}

func (c *CMOSDevice) Ports() []uint16 {
	return []uint16{
		0x70, // address
		0x71, // data
	}
}

func (c *CMOSDevice) IO(io *kvm.KVMIoEvent) error {
	port := io.Port - 0x70

	if port == 0 && io.Direction == kvm.IoDirectionWrite {
		c.addr = uint8(io.Read()[0]) & 0x7f

		return nil
	} else if port == 0 && io.Direction == kvm.IoDirectionRead {
		io.Write([]byte{c.addr})

		return nil
	} else if port == 1 && io.Direction == kvm.IoDirectionWrite {
		data := io.Read()[0]
		slog.Info("CMOS write",
			"addr", fmt.Sprintf("0x%02x", c.addr),
			"data", fmt.Sprintf("0x%02x", data),
		)
		c.Data[c.addr] = io.Read()[0]

		if c.addr == 0x0f {
			switch data {
			case 0x00:
				slog.Info("CMOS shutdown")

				return c.cpu.Shutdown()
			}
		}

		return nil
	} else if port == 1 && io.Direction == kvm.IoDirectionRead {
		slog.Info("CMOS read",
			"addr", fmt.Sprintf("0x%02x", c.addr),
			"data", fmt.Sprintf("0x%02x", c.Data[c.addr]),
		)

		io.Write([]byte{c.Data[c.addr]})

		return nil
	} else {
		return fmt.Errorf("unimplemented: port=0x%04x dir=%s size=%d", io.Port, io.Direction, io.Size)
	}
}

var (
	_ IODevice = (*CMOSDevice)(nil)
)

type newlineReplaceWriter struct {
	w           io.Writer
	replaceWith string
}

func (w *newlineReplaceWriter) Write(p []byte) (n int, err error) {
	return w.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte(w.replaceWith)))
}

func appMain() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(&newlineReplaceWriter{
		w:           os.Stderr,
		replaceWith: "\r\n",
	}, &slog.HandlerOptions{})))

	args, err := parseCommandLine(os.Args[1:])
	if err != nil {
		return fmt.Errorf("failed to parse command line: %w", err)
	}

	if args.cpuprofile != "" {
		f, err := os.Create(args.cpuprofile)
		if err != nil {
			return fmt.Errorf("failed to create CPU profile: %w", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("failed to start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if args.filename == "" {
		return fmt.Errorf("missing filename")
	}

	dev, err := kvm.OpenKVMDevice("/dev/kvm")
	if err != nil {
		return fmt.Errorf("failed to open KVM device: %w", err)
	}

	hv := &HypervisorDevice{
		kvm:       dev,
		errorChan: make(chan error),
	}

	globals := starlark.StringDict{}

	opts := syntax.FileOptions{}

	thread := &starlark.Thread{
		Name: "main",
	}

	defs, err := starlark.ExecFileOptions(&opts, thread, args.filename, nil, globals)
	if err != nil {
		return fmt.Errorf("failed to execute main.star: %w", err)
	}

	main, ok := defs["main"]
	if !ok {
		return fmt.Errorf("main.star does not define main function")
	}

	if _, err := starlark.Call(thread, main, starlark.Tuple{hv, args}, nil); err != nil {
		return fmt.Errorf("failed to call main function: %w", err)
	}

	if err := hv.Wait(); err != nil {
		return fmt.Errorf("failed to wait for hypervisor: %w", err)
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
