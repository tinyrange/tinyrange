//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

// ioctl is a convenience function to call ioctl.
// Its main purpose is to format arguments
// and return values to make things easier for
// programmers.
func ioctl(fd, op, arg uintptr) (uintptr, error) {
	res, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, op, arg)
	if errno != 0 {
		return res, errno
	}

	return res, nil
}

const (
	nrbits   = 8
	typebits = 8
	sizebits = 14
	dirbits  = 2

	nrmask   = (1 << nrbits) - 1
	sizemask = (1 << sizebits) - 1
	dirmask  = (1 << dirbits) - 1

	none      = 0
	write     = 1
	read      = 2
	readwrite = 3

	nrshift   = 0
	typeshift = nrshift + nrbits
	sizeshift = typeshift + typebits
	dirshift  = sizeshift + sizebits
)

// kvmIo is for the kvmIo ioctl.
const kvmIo = 0xAE

// iiowr creates an iiowr ioctl.
func iiowr(nr, size uintptr) uintptr {
	return iioc(readwrite, nr, size)
}

// iior creates an iior ioctl.
func iior(nr, size uintptr) uintptr {
	return iioc(read, nr, size)
}

// iiow creates an iiow ioctl.
func iiow(nr, size uintptr) uintptr {
	return iioc(write, nr, size)
}

// iio creates an IIOC ioctl from a number.
func iio(nr uintptr) uintptr {
	return iioc(none, nr, 0)
}

// iioc creates an iioc ioctl from a direction, nr, and size.
func iioc(dir, nr, size uintptr) uintptr {
	// This is another case of forced wrapping which is considered an anti-pattern in Google.
	return ((dir & dirmask) << dirshift) | (kvmIo << typeshift) |
		((nr & nrmask) << nrshift) | ((size & sizemask) << sizeshift)
}

const (
	// 	/*
	//  * ioctls for /dev/kvm fds:
	//  */
	// #define KVM_GET_API_VERSION       _IO(KVMIO,   0x00)
	kvmGetAPIVersion = 0x00
	// #define KVM_CREATE_VM             _IO(KVMIO,   0x01) /* returns a VM fd */
	kvmCreateVM = 0x01
	// #define KVM_GET_MSR_INDEX_LIST    _IOWR(KVMIO, 0x02, struct kvm_msr_list)
	kvmGetMSRIndexList = 0x02
	// #define KVM_S390_ENABLE_SIE       _IO(KVMIO,   0x06)
	kvmS390EnableSIE = 0x06
	// /*
	//  * Check if a kvm extension is available.  Argument is extension number,
	//  * return is 1 (yes) or 0 (no, sorry).
	//  */
	// #define KVM_CHECK_EXTENSION       _IO(KVMIO,   0x03)
	kvmCheckExtension = 0x03
	// /*
	//  * Get size for mmap(vcpu_fd)
	//  */
	// #define KVM_GET_VCPU_MMAP_SIZE    _IO(KVMIO,   0x04) /* in bytes */
	kvmGetVCPUMMapSize = 0x04
	// #define KVM_GET_SUPPORTED_CPUID   _IOWR(KVMIO, 0x05, struct kvm_cpuid2)
	kvmGetSupportedCPUID = 0x05
	// #define KVM_GET_EMULATED_CPUID	  _IOWR(KVMIO, 0x09, struct kvm_cpuid2)
	kvmGetEmulatedCPUID = 0x09
	// #define KVM_GET_MSR_FEATURE_INDEX_LIST    _IOWR(KVMIO, 0x0a, struct kvm_msr_list)
	kvmGetMSRFeatureIndexList = 0x0a
	// #define KVM_CREATE_VCPU           _IO(KVMIO,   0x41)
	kvmCreateVCPU = 0x41
	// #define KVM_GET_DIRTY_LOG         _IOW(KVMIO,  0x42, struct kvm_dirty_log)
	kvmGetDirtyLog = 0x42
	// #define KVM_SET_NR_MMU_PAGES      _IO(KVMIO,   0x44)
	kvmSetNrMMUPages = 0x44
	// #define KVM_GET_NR_MMU_PAGES      _IO(KVMIO,   0x45)  /* deprecated */
	kvmGetNrMMUPages = 0x45
	//	#define KVM_SET_USER_MEMORY_REGION _IOW(KVMIO, 0x46, \
	//						struct kvm_userspace_memory_region)
	kvmSetUserMemoryRegion = 0x46
	// #define KVM_SET_TSS_ADDR          _IO(KVMIO,   0x47)
	kvmSetTSSAddr = 0x47
	// #define KVM_SET_IDENTITY_MAP_ADDR _IOW(KVMIO,  0x48, __u64)
	kvmSetIdentityMapAddr = 0x48
	//	#define KVM_SET_USER_MEMORY_REGION2 _IOW(KVMIO, 0x49, \
	//						 struct kvm_userspace_memory_region2)
	kvmSetUserMemoryRegion2 = 0x49
	// /* enable ucontrol for s390 */
	// #define KVM_S390_UCAS_MAP        _IOW(KVMIO, 0x50, struct kvm_s390_ucas_mapping)
	kvmS390UCASMap = 0x50
	// #define KVM_S390_UCAS_UNMAP      _IOW(KVMIO, 0x51, struct kvm_s390_ucas_mapping)
	kvmS390UCASUnmap = 0x51
	// #define KVM_S390_VCPU_FAULT	 _IOW(KVMIO, 0x52, unsigned long)
	kvmS390VCPUFault = 0x52
	// /* Device model IOC */
	// #define KVM_CREATE_IRQCHIP        _IO(KVMIO,   0x60)
	kvmCreateIRQChip = 0x60
	// #define KVM_IRQ_LINE              _IOW(KVMIO,  0x61, struct kvm_irq_level)
	kvmIRQLine = 0x61
	// #define KVM_GET_IRQCHIP           _IOWR(KVMIO, 0x62, struct kvm_irqchip)
	kvmGetIRQChip = 0x62
	// #define KVM_SET_IRQCHIP           _IOR(KVMIO,  0x63, struct kvm_irqchip)
	kvmSetIRQChip = 0x63
	// #define KVM_CREATE_PIT            _IO(KVMIO,   0x64)
	kvmCreatePIT = 0x64
	// #define KVM_GET_PIT               _IOWR(KVMIO, 0x65, struct kvm_pit_state)
	kvmGetPIT = 0x65
	// #define KVM_SET_PIT               _IOR(KVMIO,  0x66, struct kvm_pit_state)
	kvmSetPIT = 0x66
	// #define KVM_IRQ_LINE_STATUS       _IOWR(KVMIO, 0x67, struct kvm_irq_level)
	kvmIRQLineStatus = 0x67
	//	#define KVM_REGISTER_COALESCED_MMIO \
	//				_IOW(KVMIO,  0x67, struct kvm_coalesced_mmio_zone)
	kvmRegisterCoalescedMMIO = 0x67
	//	#define KVM_UNREGISTER_COALESCED_MMIO \
	//				_IOW(KVMIO,  0x68, struct kvm_coalesced_mmio_zone)
	kvmUnregisterCoalescedMMIO = 0x68
	// #define KVM_SET_GSI_ROUTING       _IOW(KVMIO,  0x6a, struct kvm_irq_routing)
	kvmSetGSIRouting = 0x6a
	// #define KVM_REINJECT_CONTROL      _IO(KVMIO,   0x71)
	kvmReinjectControl = 0x71
	// #define KVM_IRQFD                 _IOW(KVMIO,  0x76, struct kvm_irqfd)
	kvmIRQFD = 0x76
	// #define KVM_CREATE_PIT2		  _IOW(KVMIO,  0x77, struct kvm_pit_config)
	kvmCreatePIT2 = 0x77
	// #define KVM_SET_BOOT_CPU_ID       _IO(KVMIO,   0x78)
	kvmSetBootCPUID = 0x78
	// #define KVM_IOEVENTFD             _IOW(KVMIO,  0x79, struct kvm_ioeventfd)
	kvmIOEventFD = 0x79
	// #define KVM_XEN_HVM_CONFIG        _IOW(KVMIO,  0x7a, struct kvm_xen_hvm_config)
	kvmXenHVMConfig = 0x7a
	// #define KVM_SET_CLOCK             _IOW(KVMIO,  0x7b, struct kvm_clock_data)
	kvmSetClock = 0x7b
	// #define KVM_GET_CLOCK             _IOR(KVMIO,  0x7c, struct kvm_clock_data)
	kvmGetClock = 0x7c
	// /* Available with KVM_CAP_PIT_STATE2 */
	// #define KVM_GET_PIT2              _IOR(KVMIO,  0x9f, struct kvm_pit_state2)
	kvmGetPIT2 = 0x9f
	// #define KVM_SET_PIT2              _IOW(KVMIO,  0xa0, struct kvm_pit_state2)
	kvmSetPIT2 = 0xa0
	// /* Available with KVM_CAP_PPC_GET_PVINFO */
	// #define KVM_PPC_GET_PVINFO	  _IOW(KVMIO,  0xa1, struct kvm_ppc_pvinfo)
	kvmPPCGetPVInfo = 0xa1
	// /* Available with KVM_CAP_TSC_CONTROL for a vCPU, or with
	// *  KVM_CAP_VM_TSC_CONTROL to set defaults for a VM */
	// #define KVM_SET_TSC_KHZ           _IO(KVMIO,  0xa2)
	kvmSetTSCKHz = 0xa2
	// #define KVM_GET_TSC_KHZ           _IO(KVMIO,  0xa3)
	kvmGetTSCKHz = 0xa3
	// /* Available with KVM_CAP_SIGNAL_MSI */
	// #define KVM_SIGNAL_MSI            _IOW(KVMIO,  0xa5, struct kvm_msi)
	kvmSignalMSI = 0xa5
	// /* Available with KVM_CAP_PPC_GET_SMMU_INFO */
	// #define KVM_PPC_GET_SMMU_INFO	  _IOR(KVMIO,  0xa6, struct kvm_ppc_smmu_info)
	kvmPPCGetSMMUInfo = 0xa6
	// /* Available with KVM_CAP_PPC_ALLOC_HTAB */
	// #define KVM_PPC_ALLOCATE_HTAB	  _IOWR(KVMIO, 0xa7, __u32)
	kvmPPCAllocateHTAB = 0xa7
	// #define KVM_CREATE_SPAPR_TCE	  _IOW(KVMIO,  0xa8, struct kvm_create_spapr_tce)
	kvmCreateSPAPRTCE = 0xa8
	//	#define KVM_CREATE_SPAPR_TCE_64	  _IOW(KVMIO,  0xa8, \
	//					       struct kvm_create_spapr_tce_64)
	kvmCreateSPAPRTCE64 = 0xa8
	// /* Available with KVM_CAP_RMA */
	// #define KVM_ALLOCATE_RMA	  _IOR(KVMIO,  0xa9, struct kvm_allocate_rma)
	kvmAllocateRMA = 0xa9
	// /* Available with KVM_CAP_PPC_HTAB_FD */
	// #define KVM_PPC_GET_HTAB_FD	  _IOW(KVMIO,  0xaa, struct kvm_get_htab_fd)
	kvmPPCGetHTABFD = 0xaa
	// /* Available with KVM_CAP_ARM_SET_DEVICE_ADDR */
	// #define KVM_ARM_SET_DEVICE_ADDR	  _IOW(KVMIO,  0xab, struct kvm_arm_device_addr)
	kvmARMSetDeviceAddr = 0xab
	// /* Available with KVM_CAP_PPC_RTAS */
	// #define KVM_PPC_RTAS_DEFINE_TOKEN _IOW(KVMIO,  0xac, struct kvm_rtas_token_args)
	kvmPPCRTASDefineToken = 0xac
	// /* Available with KVM_CAP_SPAPR_RESIZE_HPT */
	// #define KVM_PPC_RESIZE_HPT_PREPARE _IOR(KVMIO, 0xad, struct kvm_ppc_resize_hpt)
	kvmPPCResizeHPTPrepare = 0xad
	// #define KVM_PPC_RESIZE_HPT_COMMIT  _IOR(KVMIO, 0xae, struct kvm_ppc_resize_hpt)
	kvmPPCResizeHPTCommit = 0xae
	// /* Available with KVM_CAP_PPC_MMU_RADIX or KVM_CAP_PPC_MMU_HASH_V3 */
	// #define KVM_PPC_CONFIGURE_V3_MMU  _IOW(KVMIO,  0xaf, struct kvm_ppc_mmuv3_cfg)
	kvmPPCConfigureV3MMU = 0xaf
	// /* Available with KVM_CAP_PPC_MMU_RADIX */
	// #define KVM_PPC_GET_RMMU_INFO	  _IOW(KVMIO,  0xb0, struct kvm_ppc_rmmu_info)
	kvmPPCGetRMMUInfo = 0xb0
	// /* Available with KVM_CAP_PPC_GET_CPU_CHAR */
	// #define KVM_PPC_GET_CPU_CHAR	  _IOR(KVMIO,  0xb1, struct kvm_ppc_cpu_char)
	kvmPPCGetCPUChar = 0xb1
	// /* Available with KVM_CAP_PMU_EVENT_FILTER */
	// #define KVM_SET_PMU_EVENT_FILTER  _IOW(KVMIO,  0xb2, struct kvm_pmu_event_filter)
	kvmSetPMUEventFilter = 0xb2
	// #define KVM_PPC_SVM_OFF		  _IO(KVMIO,  0xb3)
	kvmPPCSVMOff = 0xb3
	// #define KVM_ARM_MTE_COPY_TAGS	  _IOR(KVMIO,  0xb4, struct kvm_arm_copy_mte_tags)
	kvmARMMTECopyTags = 0xb4
	// /* Available with KVM_CAP_COUNTER_OFFSET */
	// #define KVM_ARM_SET_COUNTER_OFFSET _IOW(KVMIO,  0xb5, struct kvm_arm_counter_offset)
	kvmARMSetCounterOffset = 0xb5
	// #define KVM_ARM_GET_REG_WRITABLE_MASKS _IOR(KVMIO,  0xb6, struct reg_mask_range)
	kvmARMGetRegWritableMasks = 0xb6
	// /* ioctl for vm fd */
	// #define KVM_CREATE_DEVICE	  _IOWR(KVMIO,  0xe0, struct kvm_create_device)
	kvmCreateDevice = 0xe0
	// /* ioctls for fds returned by KVM_CREATE_DEVICE */
	// #define KVM_SET_DEVICE_ATTR	  _IOW(KVMIO,  0xe1, struct kvm_device_attr)
	kvmSetDeviceAttr = 0xe1
	// #define KVM_GET_DEVICE_ATTR	  _IOW(KVMIO,  0xe2, struct kvm_device_attr)
	kvmGetDeviceAttr = 0xe2
	// #define KVM_HAS_DEVICE_ATTR	  _IOW(KVMIO,  0xe3, struct kvm_device_attr)
	kvmHasDeviceAttr = 0xe3
	// /*
	//   - ioctls for vcpu fds
	//     */
	//
	// #define KVM_RUN                   _IO(KVMIO,   0x80)
	kvmRun = 0x80
	// #define KVM_GET_REGS              _IOR(KVMIO,  0x81, struct kvm_regs)
	kvmGetRegs = 0x81
	// #define KVM_SET_REGS              _IOW(KVMIO,  0x82, struct kvm_regs)
	kvmSetRegs = 0x82
	// #define KVM_GET_SREGS             _IOR(KVMIO,  0x83, struct kvm_sregs)
	kvmGetSRegs = 0x83
	// #define KVM_SET_SREGS             _IOW(KVMIO,  0x84, struct kvm_sregs)
	kvmSetSRegs = 0x84
	// #define KVM_TRANSLATE             _IOWR(KVMIO, 0x85, struct kvm_translation)
	kvmTranslate = 0x85
	// #define KVM_INTERRUPT             _IOW(KVMIO,  0x86, struct kvm_interrupt)
	kvmInterrupt = 0x86
	// #define KVM_GET_MSRS              _IOWR(KVMIO, 0x88, struct kvm_msrs)
	kvmGetMSRs = 0x88
	// #define KVM_SET_MSRS              _IOW(KVMIO,  0x89, struct kvm_msrs)
	kvmSetMSRs = 0x89
	// #define KVM_SET_CPUID             _IOW(KVMIO,  0x8a, struct kvm_cpuid)
	kvmSetCPUID = 0x8a
	// #define KVM_SET_SIGNAL_MASK       _IOW(KVMIO,  0x8b, struct kvm_signal_mask)
	kvmSetSignalMask = 0x8b
	// #define KVM_GET_FPU               _IOR(KVMIO,  0x8c, struct kvm_fpu)
	kvmGetFPU = 0x8c
	// #define KVM_SET_FPU               _IOW(KVMIO,  0x8d, struct kvm_fpu)
	kvmSetFPU = 0x8d
	// #define KVM_GET_LAPIC             _IOR(KVMIO,  0x8e, struct kvm_lapic_state)
	kvmGetLAPIC = 0x8e
	// #define KVM_SET_LAPIC             _IOW(KVMIO,  0x8f, struct kvm_lapic_state)
	kvmSetLAPIC = 0x8f
	// #define KVM_SET_CPUID2            _IOW(KVMIO,  0x90, struct kvm_cpuid2)
	kvmSetCPUID2 = 0x90
	// #define KVM_GET_CPUID2            _IOWR(KVMIO, 0x91, struct kvm_cpuid2)
	kvmGetCPUID2 = 0x91
	// /* Available with KVM_CAP_VAPIC */
	// #define KVM_TPR_ACCESS_REPORTING  _IOWR(KVMIO, 0x92, struct kvm_tpr_access_ctl)
	kvmTPRAccessReporting = 0x92
	// /* Available with KVM_CAP_VAPIC */
	// #define KVM_SET_VAPIC_ADDR        _IOW(KVMIO,  0x93, struct kvm_vapic_addr)
	kvmSetVAPICAddr = 0x93
	// /* valid for virtual machine (for floating interrupt)_and_ vcpu */
	// #define KVM_S390_INTERRUPT        _IOW(KVMIO,  0x94, struct kvm_s390_interrupt)
	kvmS390Interrupt = 0x94
	// #define KVM_S390_STORE_STATUS	  _IOW(KVMIO,  0x95, unsigned long)
	kvmS390StoreStatus = 0x95
	// /* initial ipl psw for s390 */
	// #define KVM_S390_SET_INITIAL_PSW  _IOW(KVMIO,  0x96, struct kvm_s390_psw)
	kvmS390SetInitialPSW = 0x96
	// /* initial reset for s390 */
	// #define KVM_S390_INITIAL_RESET    _IO(KVMIO,   0x97)
	kvmS390InitialReset = 0x97
	// #define KVM_GET_MP_STATE          _IOR(KVMIO,  0x98, struct kvm_mp_state)
	kvmGetMPState = 0x98
	// #define KVM_SET_MP_STATE          _IOW(KVMIO,  0x99, struct kvm_mp_state)
	kvmSetMPState = 0x99
	// /* Available with KVM_CAP_USER_NMI */
	// #define KVM_NMI                   _IO(KVMIO,   0x9a)
	kvmNMI = 0x9a
	// /* Available with KVM_CAP_SET_GUEST_DEBUG */
	// #define KVM_SET_GUEST_DEBUG       _IOW(KVMIO,  0x9b, struct kvm_guest_debug)
	kvmSetGuestDebug = 0x9b
	// /* MCE for x86 */
	// #define KVM_X86_SETUP_MCE         _IOW(KVMIO,  0x9c, __u64)
	kvmX86SetupMCE = 0x9c
	// #define KVM_X86_GET_MCE_CAP_SUPPORTED _IOR(KVMIO,  0x9d, __u64)
	kvmX86GetMCECapSupported = 0x9d
	// #define KVM_X86_SET_MCE           _IOW(KVMIO,  0x9e, struct kvm_x86_mce)
	kvmX86SetMCE = 0x9e
	// /* Available with KVM_CAP_VCPU_EVENTS */
	// #define KVM_GET_VCPU_EVENTS       _IOR(KVMIO,  0x9f, struct kvm_vcpu_events)
	kvmGetVCPUEvents = 0x9f
	// #define KVM_SET_VCPU_EVENTS       _IOW(KVMIO,  0xa0, struct kvm_vcpu_events)
	kvmSetVCPUEvents = 0xa0
	// /* Available with KVM_CAP_DEBUGREGS */
	// #define KVM_GET_DEBUGREGS         _IOR(KVMIO,  0xa1, struct kvm_debugregs)
	kvmGetDebugRegs = 0xa1
	// #define KVM_SET_DEBUGREGS         _IOW(KVMIO,  0xa2, struct kvm_debugregs)
	kvmSetDebugRegs = 0xa2
	// /*
	//   - vcpu version available with KVM_CAP_ENABLE_CAP
	//   - vm version available with KVM_CAP_ENABLE_CAP_VM
	//     */
	//
	// #define KVM_ENABLE_CAP            _IOW(KVMIO,  0xa3, struct kvm_enable_cap)
	kvmEnableCap = 0xa3
	// /* Available with KVM_CAP_XSAVE */
	// #define KVM_GET_XSAVE		  _IOR(KVMIO,  0xa4, struct kvm_xsave)
	kvmGetXSAVE = 0xa4
	// #define KVM_SET_XSAVE		  _IOW(KVMIO,  0xa5, struct kvm_xsave)
	kvmSetXSAVE = 0xa5
	// /* Available with KVM_CAP_XCRS */
	// #define KVM_GET_XCRS		  _IOR(KVMIO,  0xa6, struct kvm_xcrs)
	kvmGetXCRS = 0xa6
	// #define KVM_SET_XCRS		  _IOW(KVMIO,  0xa7, struct kvm_xcrs)
	kvmSetXCRS = 0xa7
	// /* Available with KVM_CAP_SW_TLB */
	// #define KVM_DIRTY_TLB		  _IOW(KVMIO,  0xaa, struct kvm_dirty_tlb)
	kvmDirtyTLB = 0xaa
	// /* Available with KVM_CAP_ONE_REG */
	// #define KVM_GET_ONE_REG		  _IOW(KVMIO,  0xab, struct kvm_one_reg)
	kvmGetOneReg = 0xab
	// #define KVM_SET_ONE_REG		  _IOW(KVMIO,  0xac, struct kvm_one_reg)
	kvmSetOneReg = 0xac
	// /* VM is being stopped by host */
	// #define KVM_KVMCLOCK_CTRL	  _IO(KVMIO,   0xad)
	kvmKVMClockCtrl = 0xad
	// #define KVM_ARM_VCPU_INIT	  _IOW(KVMIO,  0xae, struct kvm_vcpu_init)
	kvmARMVCPUInit = 0xae
	// #define KVM_ARM_PREFERRED_TARGET  _IOR(KVMIO,  0xaf, struct kvm_vcpu_init)
	kvmARMPreferredTarget = 0xaf
	// #define KVM_GET_REG_LIST	  _IOWR(KVMIO, 0xb0, struct kvm_reg_list)
	kvmGetRegList = 0xb0
	// /* Available with KVM_CAP_S390_MEM_OP */
	// #define KVM_S390_MEM_OP		  _IOW(KVMIO,  0xb1, struct kvm_s390_mem_op)
	kvmS390MemOp = 0xb1
	// /* Available with KVM_CAP_S390_SKEYS */
	// #define KVM_S390_GET_SKEYS      _IOW(KVMIO, 0xb2, struct kvm_s390_skeys)
	kvmS390GetSKEYS = 0xb2
	// #define KVM_S390_SET_SKEYS      _IOW(KVMIO, 0xb3, struct kvm_s390_skeys)
	kvmS390SetSKEYS = 0xb3
	// /* Available with KVM_CAP_S390_INJECT_IRQ */
	// #define KVM_S390_IRQ              _IOW(KVMIO,  0xb4, struct kvm_s390_irq)
	kvmS390IRQ = 0xb4
	// /* Available with KVM_CAP_S390_IRQ_STATE */
	// #define KVM_S390_SET_IRQ_STATE	  _IOW(KVMIO, 0xb5, struct kvm_s390_irq_state)
	kvmS390SetIRQState = 0xb5
	// #define KVM_S390_GET_IRQ_STATE	  _IOW(KVMIO, 0xb6, struct kvm_s390_irq_state)
	kvmS390GetIRQState = 0xb6
	// /* Available with KVM_CAP_X86_SMM */
	// #define KVM_SMI                   _IO(KVMIO,   0xb7)
	kvmSMI = 0xb7
	// /* Available with KVM_CAP_S390_CMMA_MIGRATION */
	// #define KVM_S390_GET_CMMA_BITS      _IOWR(KVMIO, 0xb8, struct kvm_s390_cmma_log)
	kvmS390GetCMMABits = 0xb8
	// #define KVM_S390_SET_CMMA_BITS      _IOW(KVMIO, 0xb9, struct kvm_s390_cmma_log)
	kvmS390SetCMMABits = 0xb9
	// /* Memory Encryption Commands */
	// #define KVM_MEMORY_ENCRYPT_OP      _IOWR(KVMIO, 0xba, unsigned long)
	kvmMemoryEncryptOp = 0xba
	// /* Available with KVM_CAP_HYPERV_EVENTFD */
	// #define KVM_HYPERV_EVENTFD        _IOW(KVMIO,  0xbd, struct kvm_hyperv_eventfd)
	kvmHyperVEventFD = 0xbd
	// /* Available with KVM_CAP_NESTED_STATE */
	// #define KVM_GET_NESTED_STATE         _IOWR(KVMIO, 0xbe, struct kvm_nested_state)
	// #define KVM_SET_NESTED_STATE         _IOW(KVMIO,  0xbf, struct kvm_nested_state)
	kvmGetNestedState = 0xbe
	kvmSetNestedState = 0xbf
	// /* Available with KVM_CAP_MANUAL_DIRTY_LOG_PROTECT_2 */
	// #define KVM_CLEAR_DIRTY_LOG          _IOWR(KVMIO, 0xc0, struct kvm_clear_dirty_log)
	kvmClearDirtyLog = 0xc0
	// /* Available with KVM_CAP_HYPERV_CPUID (vcpu) / KVM_CAP_SYS_HYPERV_CPUID (system) */
	// #define KVM_GET_SUPPORTED_HV_CPUID _IOWR(KVMIO, 0xc1, struct kvm_cpuid2)
	kvmGetSupportedHVCPUID = 0xc1
	// /* Available with KVM_CAP_ARM_SVE */
	// #define KVM_ARM_VCPU_FINALIZE	  _IOW(KVMIO,  0xc2, int)
	kvmARMVCPUFinalize = 0xc2
	// /* Available with  KVM_CAP_S390_VCPU_RESETS */
	// #define KVM_S390_NORMAL_RESET	_IO(KVMIO,   0xc3)
	// #define KVM_S390_CLEAR_RESET	_IO(KVMIO,   0xc4)
	kvmS390NormalReset = 0xc3
	// /* Available with KVM_CAP_S390_PROTECTED */
	// #define KVM_S390_PV_COMMAND		_IOWR(KVMIO, 0xc5, struct kvm_pv_cmd)
	kvmS390PVCommand = 0xc5
	// /* Available with KVM_CAP_X86_MSR_FILTER */
	// #define KVM_X86_SET_MSR_FILTER	_IOW(KVMIO,  0xc6, struct kvm_msr_filter)
	kvmX86SetMSRFilter = 0xc6
	// /* Available with KVM_CAP_DIRTY_LOG_RING */
	// #define KVM_RESET_DIRTY_RINGS		_IO(KVMIO, 0xc7)
	kvmResetDirtyRings = 0xc7
	// /* Per-VM Xen attributes */
	// #define KVM_XEN_HVM_GET_ATTR	_IOWR(KVMIO, 0xc8, struct kvm_xen_hvm_attr)
	kvmXenHVMGetAttr = 0xc8
	// #define KVM_XEN_HVM_SET_ATTR	_IOW(KVMIO,  0xc9, struct kvm_xen_hvm_attr)
	kvmXenHVMSetAttr = 0xc8
	// /* Per-vCPU Xen attributes */
	// #define KVM_XEN_VCPU_GET_ATTR	_IOWR(KVMIO, 0xca, struct kvm_xen_vcpu_attr)
	kvmXenVCPUGetAttr = 0xca
	// #define KVM_XEN_VCPU_SET_ATTR	_IOW(KVMIO,  0xcb, struct kvm_xen_vcpu_attr)
	kvmXenVCPUSetAttr = 0xcb
	// /* Available with KVM_CAP_XEN_HVM / KVM_XEN_HVM_CONFIG_EVTCHN_SEND */
	// #define KVM_XEN_HVM_EVTCHN_SEND	_IOW(KVMIO,  0xd0, struct kvm_irq_routing_xen_evtchn)
	kvmXenHVMEvtchnSend = 0xd0
	// #define KVM_GET_SREGS2             _IOR(KVMIO,  0xcc, struct kvm_sregs2)
	kvmGetSRegs2 = 0xcc
	// #define KVM_SET_SREGS2             _IOW(KVMIO,  0xcd, struct kvm_sregs2)
	kvmSetSRegs2 = 0xcd
)

const (
	KVM_CPUID_SIGNATURE = 0x40000000
	KVM_CPUID_FEATURES  = 0x40000001
)

type ExitType uint32

const (
	ExitUnknown        ExitType = 0
	ExitException      ExitType = 1
	ExitIo             ExitType = 2
	ExitHypercall      ExitType = 3
	ExitDebug          ExitType = 4
	ExitHlt            ExitType = 5
	ExitMmio           ExitType = 6
	ExitIrqWindowOpen  ExitType = 7
	ExitShutdown       ExitType = 8
	ExitFailEntry      ExitType = 9
	ExitIntr           ExitType = 10
	ExitSetTpr         ExitType = 11
	ExitTprAccess      ExitType = 12
	ExitS390Sieic      ExitType = 13
	ExitS390Reset      ExitType = 14
	ExitDcr            ExitType = 15 /* deprecated */
	ExitNmi            ExitType = 16
	ExitInternalError  ExitType = 17
	ExitOsi            ExitType = 18
	ExitPaprHcall      ExitType = 19
	ExitS390Ucontrol   ExitType = 20
	ExitWatchdog       ExitType = 21
	ExitS390Tsch       ExitType = 22
	ExitEpr            ExitType = 23
	ExitSystemEvent    ExitType = 24
	ExitS390Stsi       ExitType = 25
	ExitIoapicEoi      ExitType = 26
	ExitHyperv         ExitType = 27
	ExitArmNisv        ExitType = 28
	ExitX86Rdmsr       ExitType = 29
	ExitX86Wrmsr       ExitType = 30
	ExitDirtyRingFull  ExitType = 31
	ExitApResetHold    ExitType = 32
	ExitX86BusLock     ExitType = 33
	ExitXen            ExitType = 34
	ExitRiscvSbi       ExitType = 35
	ExitRiscvCsr       ExitType = 36
	ExitNotify         ExitType = 37
	ExitLoongarchIocsr ExitType = 38
	ExitMemoryFault    ExitType = 39
)

func (exit ExitType) String() string {
	switch exit {
	case ExitUnknown:
		return "Unknown"
	case ExitException:
		return "Exception"
	case ExitIo:
		return "IO"
	case ExitHypercall:
		return "Hypercall"
	case ExitDebug:
		return "Debug"
	case ExitHlt:
		return "Halt"
	case ExitMmio:
		return "MMIO"
	case ExitIrqWindowOpen:
		return "IRQ Window Open"
	case ExitShutdown:
		return "Shutdown"
	case ExitFailEntry:
		return "Fail Entry"
	case ExitIntr:
		return "Interrupt"
	case ExitSetTpr:
		return "Set TPR"
	case ExitTprAccess:
		return "TPR Access"
	case ExitS390Sieic:
		return "S390 SIEIC"
	case ExitS390Reset:
		return "S390 Reset"
	case ExitDcr:
		return "DCR"
	case ExitNmi:
		return "NMI"
	case ExitInternalError:
		return "Internal Error"
	case ExitOsi:
		return "OSI"
	case ExitPaprHcall:
		return "PAPR Hypercall"
	case ExitS390Ucontrol:
		return "S390 Ucontrol"
	case ExitWatchdog:
		return "Watchdog"
	case ExitS390Tsch:
		return "S390 TSCH"
	case ExitEpr:
		return "EPR"
	case ExitSystemEvent:
		return "System Event"
	case ExitS390Stsi:
		return "S390 STSI"
	case ExitIoapicEoi:
		return "IOAPIC EOI"
	case ExitHyperv:
		return "Hyper-V"
	case ExitArmNisv:
		return "ARM NISV"
	case ExitX86Rdmsr:
		return "X86 RDMSR"
	case ExitX86Wrmsr:
		return "X86 WRMSR"
	case ExitDirtyRingFull:
		return "Dirty Ring Full"
	case ExitApResetHold:
		return "AP Reset Hold"
	case ExitX86BusLock:
		return "X86 Bus Lock"
	case ExitXen:
		return "Xen"
	case ExitRiscvSbi:
		return "RISC-V SBI"
	case ExitRiscvCsr:
		return "RISC-V CSR"
	case ExitNotify:
		return "Notify"
	case ExitLoongarchIocsr:
		return "Loongarch IOCSR"
	case ExitMemoryFault:
		return "Memory Fault"
	default:
		return "Unknown"
	}
}

type KVMRegisters struct {
	RAX, RBX, RCX, RDX uint64
	RSI, RDI, RSP, RBP uint64
	R8, R9, R10, R11   uint64
	R12, R13, R14, R15 uint64
	RIP, RFLAGS        uint64
}

type KVMSegment struct {
	Base                           uint64
	Limit                          uint32
	Selector                       uint16
	Type                           uint8
	Present, DPL, DB, S, L, G, AVL uint8
	_                              uint8
	_                              uint8
}

type KVMDTable struct {
	Base  uint64
	Limit uint16
	_     [3]uint16
}

type KVMSpecialRegisters struct {
	CS, DS, ES, FS, GS, SS        KVMSegment
	TR, LDT                       KVMSegment
	GDT, IDT                      KVMDTable
	CR0, CR2, CR3, CR4, CR8, EFER uint64
	APICBase                      uint64
	InterruptBitmap               [(256 + 63) / 64]uint64
}

type KVMDevice struct {
	fh *os.File
}

func (dev *KVMDevice) Close() error {
	return dev.fh.Close()
}

func (dev *KVMDevice) getAPIVersion() (uint32, error) {
	version, err := ioctl(uintptr(dev.fh.Fd()), iio(kvmGetAPIVersion), uintptr(0))
	if err != nil {
		return 0, fmt.Errorf("failed to get API version: %w", err)
	}

	return uint32(version), nil
}

func (dev *KVMDevice) getVCPUMMapSize() (uintptr, error) {
	size, err := ioctl(uintptr(dev.fh.Fd()), iio(kvmGetVCPUMMapSize), uintptr(0))
	if err != nil {
		return 0, fmt.Errorf("failed to get VCPU mmap size: %w", err)
	}

	return size, nil
}

type KVMCPUIDEntry struct {
	Function uint32
	Index    uint32
	Flags    uint32
	EAX      uint32
	EBX      uint32
	ECX      uint32
	EDX      uint32
	_        [3]uint32
}

type KVMCPUID struct {
	NumEnt uint32
	_      uint32
	Ents   [100]KVMCPUIDEntry
}

func (dev *KVMDevice) GetSupportedCPUID() (*KVMCPUID, error) {
	cpuid := KVMCPUID{}
	cpuid.NumEnt = 100

	_, err := ioctl(
		uintptr(dev.fh.Fd()),
		iiowr(kvmGetSupportedCPUID, unsafe.Sizeof(&cpuid)),
		uintptr(unsafe.Pointer(&cpuid)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get supported CPUID: %w", err)
	}

	return &cpuid, nil
}

func (dev *KVMDevice) CreateVirtualMachine() (*KVMVirtualMachine, error) {
	vmfd, err := ioctl(uintptr(dev.fh.Fd()), iio(kvmCreateVM), uintptr(0))
	if err != nil {
		return nil, fmt.Errorf("failed to create virtual machine: %w", err)
	}

	return &KVMVirtualMachine{dev: dev, vmFd: vmfd}, nil
}

func OpenKVMDevice(filename string) (*KVMDevice, error) {
	fh, err := os.OpenFile(filename, os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	dev := &KVMDevice{fh: fh}

	version, err := dev.getAPIVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get API version: %w", err)
	}

	if version != 12 {
		return nil, fmt.Errorf("unsupported API version: %d", version)
	}

	size, err := dev.getVCPUMMapSize()
	if err != nil {
		return nil, fmt.Errorf("failed to get VCPU mmap size: %w", err)
	}

	if size < uintptr(unsafe.Sizeof(runData{})) {
		return nil, fmt.Errorf("unexpected VCPU mmap size %d expected %d", size, unsafe.Sizeof(runData{}))
	}

	return &KVMDevice{fh: fh}, nil
}

type KVMVirtualMachine struct {
	dev         *KVMDevice
	vmFd        uintptr
	cpus        []*KVMCPU
	memorySlots []*memoryRegion
}

// UserSpaceMemoryRegion defines Memory Regions.
type userSpaceMemoryRegion struct {
	Slot          uint32
	Flags         uint32
	GuestPhysAddr uint64
	MemorySize    uint64
	UserSpaceAddr uint64
}

type memoryRegion struct {
	us  userSpaceMemoryRegion
	mem []byte
}

type kvmIRQLevel struct {
	IRQ   uint32
	Level uint32
}

func (vm *KVMVirtualMachine) SetIRQStatus(irq uint32, status uint32) error {
	irqLevel := kvmIRQLevel{IRQ: irq, Level: status}

	_, err := ioctl(
		vm.vmFd,
		iiow(kvmIRQLine, unsafe.Sizeof(irqLevel)),
		uintptr(unsafe.Pointer(&irqLevel)),
	)
	if err != nil {
		return fmt.Errorf("failed to raise IRQ: %w", err)
	}

	return nil
}

func (vm *KVMVirtualMachine) SetIdentityMapAddr(addr uint64) error {
	_, err := ioctl(
		uintptr(vm.vmFd),
		iiow(kvmSetIdentityMapAddr, unsafe.Sizeof(addr)),
		uintptr(unsafe.Pointer(&addr)),
	)
	if err != nil {
		return fmt.Errorf("failed to set identity map address: %w", err)
	}

	return nil
}

func (vm *KVMVirtualMachine) SetTSSAddr(addr uint64) error {
	_, err := ioctl(
		uintptr(vm.vmFd),
		iio(kvmSetTSSAddr),
		uintptr(addr),
	)
	if err != nil {
		return fmt.Errorf("failed to set TSS address: %w", err)
	}

	return nil
}

func (vm *KVMVirtualMachine) CreateIRQChip() error {
	_, err := ioctl(
		uintptr(vm.vmFd),
		iio(kvmCreateIRQChip),
		uintptr(0),
	)
	if err != nil {
		return fmt.Errorf("failed to create IRQ chip: %w", err)
	}

	return nil
}

type kvmPitConfig struct {
	flags uint32
	pad   [15]uint32
}

const kvmPitSpeakerDummy = 1

func (vm *KVMVirtualMachine) CreatePIT2(speakerDummy bool) error {
	config := kvmPitConfig{}

	if speakerDummy {
		config.flags = kvmPitSpeakerDummy
	}

	_, err := ioctl(
		uintptr(vm.vmFd),
		iiow(kvmCreatePIT2, unsafe.Sizeof(config)),
		uintptr(unsafe.Pointer(&config)),
	)
	if err != nil {
		return fmt.Errorf("failed to create PIT2: %w", err)
	}

	return nil
}

func (vm *KVMVirtualMachine) MapMemory(physOffset uint64, size uint32) (vm.RawRegion, error) {
	// Map the memory.
	mem, err := syscall.Mmap(
		-1, 0, int(size),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED|syscall.MAP_ANONYMOUS,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to map memory: %w", err)
	}

	// Define the user memory region.
	region := userSpaceMemoryRegion{
		Slot:          uint32(len(vm.memorySlots)),
		Flags:         0,
		GuestPhysAddr: physOffset,
		MemorySize:    uint64(size),
		UserSpaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
	}

	// Set the user memory region.
	if _, err = ioctl(
		uintptr(vm.vmFd),
		iiow(kvmSetUserMemoryRegion, unsafe.Sizeof(userSpaceMemoryRegion{})),
		uintptr(unsafe.Pointer(&region)),
	); err != nil {
		return nil, fmt.Errorf("failed to set user memory region: %w", err)
	}

	// Append the memory region.
	vm.memorySlots = append(vm.memorySlots, &memoryRegion{us: region, mem: mem})

	return mem, nil
}

func (vm *KVMVirtualMachine) CreateCPU() (*KVMCPU, error) {
	vcpuFd, err := ioctl(uintptr(vm.vmFd), iio(kvmCreateVCPU), uintptr(len(vm.cpus)))
	if err != nil {
		return nil, fmt.Errorf("failed to create CPU: %w", err)
	}

	cpu := &KVMCPU{vm: vm, vcpuFd: vcpuFd}
	vm.cpus = append(vm.cpus, cpu)

	// Map the run data.
	cpuRunData, err := syscall.Mmap(
		int(vcpuFd), 0, int(unsafe.Sizeof(runData{})),
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to map run data: %w", err)
	}

	cpu.runData = (*runData)(unsafe.Pointer(&cpuRunData[0]))

	return cpu, nil
}

type KVMIoEvent struct {
	*kvmIoEvent
	runData *runData
}

func (evt *KVMIoEvent) Read() []byte {
	return unsafe.Slice(
		(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(evt.runData))+uintptr(evt.dataOffset))),
		evt.Size,
	)
}

func (evt *KVMIoEvent) Write(data []byte) {
	copy(
		unsafe.Slice((*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(evt.runData))+uintptr(evt.dataOffset))), evt.Size),
		data,
	)
}

type IoDirection uint8

const (
	IoDirectionRead  IoDirection = 0
	IoDirectionWrite IoDirection = 1
)

func (dir IoDirection) String() string {
	switch dir {
	case IoDirectionRead:
		return "Read"
	case IoDirectionWrite:
		return "Write"
	default:
		return "Unknown"
	}
}

type kvmIoEvent struct {
	Direction  IoDirection
	Size       uint8
	Port       uint16
	Count      uint32
	dataOffset uint64
}

// runData defines the data used to run a VM.
type runData struct {
	RequestInterruptWindow     uint8
	ImmediateExit              uint8
	_                          [6]uint8
	ExitReason                 uint32
	ReadyForInterruptInjection uint8
	IfFlag                     uint8
	_                          [2]uint8
	CR8                        uint64
	ApicBase                   uint64
	Data                       [1024]uint64
}

func (runData *runData) getExitReason() ExitType {
	return ExitType(runData.ExitReason)
}

func (runData *runData) ExitIo() *KVMIoEvent {
	evt := (*kvmIoEvent)(unsafe.Pointer(&runData.Data[0]))
	return &KVMIoEvent{kvmIoEvent: evt, runData: runData}
}

type internalErrorKind uint32

const (
	internalErrorEmulation      internalErrorKind = 1 // Emulate instruction failed
	internalErrorSimulEx        internalErrorKind = 2 // Simultaneous exceptions
	internalErrorDeliveryEv     internalErrorKind = 3 // Unpexpted vm exit due to delivery event
	internalErrorUnexpectedExit internalErrorKind = 4 // Unexpected vm exit
)

func (iek internalErrorKind) String() string {
	switch iek {
	case internalErrorEmulation:
		return "Emulation"
	case internalErrorSimulEx:
		return "Simultaneous Exceptions"
	case internalErrorDeliveryEv:
		return "Unexpected VM Exit due to Delivery Event"
	case internalErrorUnexpectedExit:
		return "Unexpected VM Exit"
	default:
		return "Unknown"
	}
}

func (runData *runData) getInternalErrorData() (internalErrorKind, []uint64) {
	val := runData.Data[0]
	val1, val2 := uint32(val), uint32(val>>32)
	return internalErrorKind(val1), runData.Data[1 : 1+val2]
}

type KVMCPU struct {
	vm      *KVMVirtualMachine
	vcpuFd  uintptr
	runData *runData
}

func (cpu *KVMCPU) ExitIo() *KVMIoEvent {
	return cpu.runData.ExitIo()
}

func (cpu *KVMCPU) GetRegisters() (KVMRegisters, error) {
	var regs KVMRegisters

	_, err := ioctl(
		cpu.vcpuFd,
		iior(kvmGetRegs, unsafe.Sizeof(regs)),
		uintptr(unsafe.Pointer(&regs)),
	)
	if err != nil {
		return KVMRegisters{}, fmt.Errorf("failed to get registers: %w", err)
	}

	return regs, nil
}

func (cpu *KVMCPU) SetRegisters(regs KVMRegisters) error {
	_, err := ioctl(
		cpu.vcpuFd,
		iiow(kvmSetRegs, unsafe.Sizeof(regs)),
		uintptr(unsafe.Pointer(&regs)),
	)
	if err != nil {
		return fmt.Errorf("failed to set registers: %w", err)
	}

	return nil
}

func (cpu *KVMCPU) GetSpecialRegisters() (KVMSpecialRegisters, error) {
	var sregs KVMSpecialRegisters

	_, err := ioctl(
		cpu.vcpuFd,
		iior(kvmGetSRegs, unsafe.Sizeof(sregs)),
		uintptr(unsafe.Pointer(&sregs)),
	)
	if err != nil {
		return KVMSpecialRegisters{}, fmt.Errorf("failed to get special registers: %w", err)
	}

	return sregs, nil
}

func (cpu *KVMCPU) SetSpecialRegisters(sregs KVMSpecialRegisters) error {
	_, err := ioctl(
		cpu.vcpuFd,
		iiow(kvmSetSRegs, unsafe.Sizeof(sregs)),
		uintptr(unsafe.Pointer(&sregs)),
	)
	if err != nil {
		return fmt.Errorf("failed to set special registers: %w", err)
	}

	return nil
}

func (cpu *KVMCPU) RaiseNMI() error {
	_, err := ioctl(
		cpu.vcpuFd,
		iio(kvmNMI),
		uintptr(0),
	)
	if err != nil {
		return fmt.Errorf("failed to raise NMI: %w", err)
	}

	return nil
}

func (cpu *KVMCPU) Pause() error {
	_, err := ioctl(
		cpu.vcpuFd,
		iio(kvmKVMClockCtrl),
		uintptr(0),
	)
	if err != nil {
		return fmt.Errorf("failed to pause CPU: %w", err)
	}

	return nil
}

type kvmGuestDebug struct {
	Control  uint32
	_        uint32
	DebugReg [8]uint64
}

const (
	kvmGuestDebugEnable     = 0x00000001
	kvmGuestDebugSingleStep = 0x00000002
)

func (cpu *KVMCPU) SetSingleStep(enable bool) error {
	dbg := kvmGuestDebug{Control: 0}
	if enable {
		dbg.Control = kvmGuestDebugEnable | kvmGuestDebugSingleStep
	}

	if _, err := ioctl(
		cpu.vcpuFd,
		iiow(kvmSetGuestDebug, unsafe.Sizeof(kvmGuestDebug{})),
		uintptr(unsafe.Pointer(&dbg)),
	); err != nil {
		return fmt.Errorf("failed to set debug registers: %w", err)
	}

	return nil
}

func (cpu *KVMCPU) SetCPUID2(cpuid *KVMCPUID) error {
	_, err := ioctl(
		cpu.vcpuFd,
		iiow(kvmSetCPUID2, unsafe.Sizeof(cpuid)),
		uintptr(unsafe.Pointer(cpuid)),
	)
	if err != nil {
		return fmt.Errorf("failed to set CPUID: %w", err)
	}

	return nil
}

func (cpu *KVMCPU) RunOnce() (ExitType, error) {
	_, err := ioctl(cpu.vcpuFd, iio(kvmRun), uintptr(0))
	if err != nil {
		if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
			return ExitType(cpu.runData.ExitReason), nil
		} else {
			return ExitInternalError, fmt.Errorf("failed to run CPU: %w", err)
		}
	}

	if cpu.runData.getExitReason() == ExitInternalError {
		reason, data := cpu.runData.getInternalErrorData()
		if reason == internalErrorEmulation {
			flags := data[0]
			// transform data[1:] from []uint64 to []byte
			dataBytes := make([]byte, 0, 16)
			for _, v := range data[1:] {
				dataBytes = binary.NativeEndian.AppendUint64(dataBytes, v)
			}

			len := dataBytes[0]

			return ExitInternalError, fmt.Errorf("emulation error: flags=%d data=%x", flags, dataBytes[1:len+1])
		} else {
			return ExitInternalError, fmt.Errorf("internal error: %s %v", reason, data)
		}
	}

	return ExitType(cpu.runData.ExitReason), nil
}

func (cpu *KVMCPU) DumpRegisters(w io.Writer) error {
	regs, err := cpu.GetRegisters()
	if err != nil {
		return fmt.Errorf("failed to get registers: %w", err)
	}

	if _, err := fmt.Fprintf(w,
		"RAX=%016x RBX   =%016x RCX=%016x RDX=%016x RSI=%016x RDI=%016x RSP=%016x RBP=%016x\n"+
			"R8 =%016x R9    =%016x R10=%016x R11=%016x R12=%016x R13=%016x R14=%016x R15=%016x\n"+
			"RIP=%016x RFLAGS=%016x\n",
		regs.RAX, regs.RBX, regs.RCX, regs.RDX,
		regs.RSI, regs.RDI, regs.RSP, regs.RBP,
		regs.R8, regs.R9, regs.R10, regs.R11,
		regs.R12, regs.R13, regs.R14, regs.R15,
		regs.RIP, regs.RFLAGS); err != nil {
		return fmt.Errorf("failed to write registers: %w", err)
	}

	return nil
}
