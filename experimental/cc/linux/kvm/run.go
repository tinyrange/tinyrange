package kvm

import "unsafe"

type kvmDebugExitArch [128]byte // Placeholder size
type kvmSyncRegs [512]byte      // Placeholder size

type _kvmRun struct {
	// in
	RequestInterruptWindow uint8
	Padding1               [7]byte

	// out
	ExitReason                 uint32
	ReadyForInterruptInjection uint8
	IfFlag                     uint8
	Padding2                   [2]byte

	// in (pre_kvm_run), out (post_kvm_run)
	Cr8      uint64
	ApicBase uint64

	// Uncomment for s390 arch
	// PswMask uint64
	// PswAddr uint64

	// Union: 256 bytes
	Data [256]byte

	// shared registers
	kvmValidRegs uint64
	kvmDirtyRegs uint64

	// Union: 1024 bytes
	S [1024]byte
}

// Helper structs for union overlays (optional, for convenience)
type kvmRunDataHw struct {
	HardwareExitReason uint64
}

type kvmRunDataFailEntry struct {
	HardwareEntryFailureReason uint64
}

type kvmRunDataEx struct {
	Exception uint32
	ErrorCode uint32
}

type kvmRunDataIo struct {
	Direction  uint8
	Size       uint8
	Port       uint16
	Count      uint32
	DataOffset uint64
}

type kvmRunDataDebug struct {
	Arch kvmDebugExitArch
}

type kvmRunDataMmio struct {
	PhysAddr uint64
	Data     [8]uint8
	Len      uint32
	IsWrite  uint8
	_        [3]byte // Padding to align to 24 bytes
}

type kvmRunDataHypercall struct {
	Nr       uint64
	Args     [6]uint64
	Ret      uint64
	Longmode uint32
	Pad      uint32
}

type kvmRunDataTprAccess struct {
	Rip     uint64
	IsWrite uint32
	Pad     uint32
}

type kvmRunDataS390Sieic struct {
	Icptcode uint8
	Ipa      uint16
	Ipb      uint32
	_        [1]byte // Padding to align to 8 bytes
}

type kvmRunDataS390Reset struct {
	S390ResetFlags uint64
}

type kvmRunDataS390Ucontrol struct {
	TransExcCode uint64
	PgmCode      uint32
	_            [4]byte // Padding to align to 16 bytes
}

type kvmRunDataDcr struct {
	Dcrn    uint32
	Data    uint32
	IsWrite uint8
	_       [3]byte // Padding to align to 12 bytes
}

type kvmRunDataInternal struct {
	Suberror uint32
	Ndata    uint32
	Data     [16]uint64
}

type kvmRunDataOsi struct {
	Gprs [32]uint64
}

type kvmRunDataPaprHcall struct {
	Nr   uint64
	Ret  uint64
	Args [9]uint64
}

type SystemEventType uint32

const (
	SystemEventUnknown  SystemEventType = 0
	SystemEventShutdown SystemEventType = 1
	SystemEventReset    SystemEventType = 2
	SystemEventCrash    SystemEventType = 3
	SystemEventWakeup   SystemEventType = 4
	SystemEventSuspend  SystemEventType = 5
	SystemEventSevTerm  SystemEventType = 6
)

func (t SystemEventType) String() string {
	switch t {
	case SystemEventUnknown:
		return "Unknown"
	case SystemEventShutdown:
		return "Shutdown"
	case SystemEventReset:
		return "Reset"
	case SystemEventCrash:
		return "Crash"
	case SystemEventWakeup:
		return "Wakeup"
	case SystemEventSuspend:
		return "Suspend"
	case SystemEventSevTerm:
		return "SEV Termination"
	default:
		return "Invalid System Event Type"
	}
}

type kvmRunDataSystemEvent struct {
	Type  SystemEventType
	nData uint32
	data  [16]uint64 // Size may vary based on the event type
}

// Overlay helpers (for casting Data to the correct struct)
func (k *_kvmRun) DataHw() *kvmRunDataHw {
	return (*kvmRunDataHw)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataFailEntry() *kvmRunDataFailEntry {
	return (*kvmRunDataFailEntry)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataEx() *kvmRunDataEx {
	return (*kvmRunDataEx)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataIo() *kvmRunDataIo {
	return (*kvmRunDataIo)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataDebug() *kvmRunDataDebug {
	return (*kvmRunDataDebug)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataMmio() *kvmRunDataMmio {
	return (*kvmRunDataMmio)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataHypercall() *kvmRunDataHypercall {
	return (*kvmRunDataHypercall)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataTprAccess() *kvmRunDataTprAccess {
	return (*kvmRunDataTprAccess)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataS390Sieic() *kvmRunDataS390Sieic {
	return (*kvmRunDataS390Sieic)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataS390Reset() *kvmRunDataS390Reset {
	return (*kvmRunDataS390Reset)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataS390Ucontrol() *kvmRunDataS390Ucontrol {
	return (*kvmRunDataS390Ucontrol)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataDcr() *kvmRunDataDcr {
	return (*kvmRunDataDcr)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataInternal() *kvmRunDataInternal {
	return (*kvmRunDataInternal)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataOsi() *kvmRunDataOsi {
	return (*kvmRunDataOsi)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataPaprHcall() *kvmRunDataPaprHcall {
	return (*kvmRunDataPaprHcall)(unsafe.Pointer(&k.Data[0]))
}
func (k *_kvmRun) DataSystemEvent() *kvmRunDataSystemEvent {
	return (*kvmRunDataSystemEvent)(unsafe.Pointer(&k.Data[0]))
}

// For the S union (shared registers)
func (k *_kvmRun) SyncRegs() *kvmSyncRegs {
	return (*kvmSyncRegs)(unsafe.Pointer(&k.S[0]))
}
