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
	"time"
	"unsafe"

	"github.com/tinyrange/tinyrange/experimental/crumblecracker/kvm"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"golang.org/x/arch/x86/x86asm"
)

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

type E820Entry struct {
	Addr uint64
	Size uint64
	Type uint32
}

// The so-called "zeropage"
// https://www.kernel.org/doc/html/latest/x86/boot.html
// https://github.com/torvalds/linux/blob/master/arch/x86/include/uapi/asm/bootparam.h
type BootParam struct {
	Padding             [0x1e8]uint8
	E820Entries         uint8
	EddbufEntries       uint8
	EddMbrSigBufEntries uint8
	KdbStatus           uint8
	Padding2            [5]uint8
	Hdr                 SetupHeader
	Padding3            [0x290 - 0x1f1 - unsafe.Sizeof(SetupHeader{})]uint8

	// Required to adjust the offset of E820Map to 0x2D0.
	Padding4 [0x3d]uint8

	EddMbrSigBuffer [EddMbrSigMax]uint8
	E820Map         [E820Max]E820Entry
}

type SetupHeader struct {
	SetupSects          uint8
	RootFlags           uint16
	SysSize             uint32
	RAMSize             uint16
	VidMode             uint16
	RootDev             uint16
	BootFlag            uint16
	Jump                uint16
	Header              uint32
	Version             uint16
	ReadModeSwitch      uint32
	StartSysSeg         uint16
	KernelVersion       uint16
	TypeOfLoader        uint8
	LoadFlags           uint8
	SetupMoveSize       uint16
	Code32Start         uint32
	RamdiskImage        uint32
	RamdiskSize         uint32
	BootsectKludge      uint32
	HeapEndPtr          uint16
	ExtLoaderVer        uint8
	ExtLoaderType       uint8
	CmdlinePtr          uint32
	InitrdAddrMax       uint32
	KernelAlignment     uint32
	RelocatableKernel   uint8
	MinAlignment        uint8
	XloadFlags          uint16
	CmdlineSize         uint32
	HardwareSubarch     uint32
	HardwareSubarchData uint64
	PayloadOffset       uint32
	PayloadLength       uint32
	SetupData           uint64
	PrefAddress         uint64
	InitSize            uint32
	HandoverOffset      uint32
	KernelInfoOffset    uint32
}

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
	hv  *HypervisorDevice
	vm  *kvm.KVMVirtualMachine
	mem vm.MemoryRegion
}

// LowerIrq implements CPUDevice.
func (vm *VirtualMachine) LowerIrq(irq byte) error {
	return vm.vm.SetIRQStatus(uint32(irq), 0)
}

// RaiseIrq implements CPUDevice.
func (vm *VirtualMachine) RaiseIrq(irq byte) error {
	return vm.vm.SetIRQStatus(uint32(irq), 1)
}

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

	var hdr BootParam

	if err := binary.Read(io.NewSectionReader(image, 0, int64(binary.Size(&hdr))), binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("failed to read boot param: %w", err)
	}

	if hdr.Hdr.Header != MagicSignature {
		return fmt.Errorf("invalid boot param signature: %x", hdr.Hdr.Header)
	}

	if hdr.Hdr.Version < 0x0206 {
		return fmt.Errorf("unsupported boot param version: %x", hdr.Hdr.Version)
	}

	setupSectors := int64(hdr.Hdr.SetupSects)
	setupSize := int64(setupSectors+1) * 512

	hdr.Hdr.VidMode = 0xffff // VGA
	hdr.Hdr.TypeOfLoader = 0xff
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

		hdr.Hdr.RamdiskImage = 0x30000
		hdr.Hdr.RamdiskSize = uint32(initrdStat.Size())

		if _, err := io.Copy(io.NewOffsetWriter(vm.mem, 0x30000), initrd); err != nil {
			return fmt.Errorf("failed to write initrd: %w", err)
		}
	} else {
		hdr.Hdr.RamdiskImage = 0x0
		hdr.Hdr.RamdiskSize = 0x0
	}
	hdr.Hdr.LoadFlags = CanUseHeap | LoadedHigh | KeepSegments
	hdr.Hdr.HeapEndPtr = 0xfe00
	hdr.Hdr.ExtLoaderVer = 0x0
	hdr.Hdr.CmdlinePtr = 0x20000

	if err := binary.Write(io.NewOffsetWriter(vm.mem, 0x10000), binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("failed to write boot param: %w", err)
	}
	if _, err := io.Copy(io.NewOffsetWriter(vm.mem, 0x20000), bytes.NewBufferString(cmdline)); err != nil {
		return fmt.Errorf("failed to write command line: %w", err)
	}
	if _, err := io.Copy(io.NewOffsetWriter(vm.mem, 0x100000), io.NewSectionReader(image, setupSize, imageStat.Size()-setupSize)); err != nil {
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

	sregs.CS.Base = 0
	sregs.CS.Limit = ^uint32(0)
	sregs.CS.G = 1

	sregs.DS.Base = 0
	sregs.DS.Limit = ^uint32(0)
	sregs.DS.G = 1

	sregs.FS.Base = 0
	sregs.FS.Limit = ^uint32(0)
	sregs.FS.G = 1

	sregs.GS.Base = 0
	sregs.GS.Limit = ^uint32(0)
	sregs.GS.G = 1

	sregs.ES.Base = 0
	sregs.ES.Limit = ^uint32(0)
	sregs.ES.G = 1

	sregs.SS.Base = 0
	sregs.SS.Limit = ^uint32(0)
	sregs.SS.G = 1

	sregs.CS.DB = 1
	sregs.DS.DB = 1
	sregs.CR0 |= 1 // Enable protected mode.

	if err := vm.cpu.SetSpecialRegisters(sregs); err != nil {
		return fmt.Errorf("failed to set special registers: %w", err)
	}

	regs, err := vm.cpu.GetRegisters()
	if err != nil {
		return fmt.Errorf("failed to get registers: %w", err)
	}

	regs.RFLAGS = 2
	regs.RIP = 0x100000
	regs.RSI = 0x10000

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
	go func() {
		for {
			os.WriteFile("dump.mem", cpu.vm.mem.(vm.RawRegion), os.ModePerm)

			slog.Info("dumped memory")

			time.Sleep(1 * time.Second)
		}
	}()

	var devices []IODevice

	devices = append(devices, &NopDevice{PortList: []uint16{
		0x61,
		0x64,

		0x70,
		0x71,

		0x80,

		0x2e9,

		0x3c0,
		0x3c6,
		0x3c8,
		0x3c9,

		0x3d4,
		0x3d5,
		0x3da,

		0x3e9,

		0x2f9,
	}})

	serial := &SerialDevice{
		Base:   0x3f8,
		Writer: os.Stdout,
	}

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

	for {
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
				slog.Error("failed to handle io", "error", err)
				continue
			}
		case kvm.ExitShutdown:
			slog.Info("shutdown")

			if err := cpu.cpu.DumpRegisters(os.Stderr); err != nil {
				return fmt.Errorf("failed to dump registers: %w", err)
			}

			os.WriteFile("dump.mem", cpu.vm.mem.(vm.RawRegion), os.ModePerm)

			return nil
		case kvm.ExitIntr:
			// slog.Info("interrupt")
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

func appMain() error {
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
