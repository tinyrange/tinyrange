//go:build linux

package kvm

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

func ioctl(fd uintptr, request uintptr, argp uintptr) (uintptr, syscall.Errno) {
	res, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, argp)
	if errno != 0 {
		return 0, errno
	}
	return res, 0
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

	kvmAPIVersion = 12
)

type kvmOneReg struct {
	Id    uint64
	Value uint64
}

type RegisterId uint64

type KVMVirtualCPU struct {
	fd uintptr
}

func (vcpu *KVMVirtualCPU) SetRegister(id RegisterId, value uint64) error {
	if vcpu.fd == 0 {
		return fmt.Errorf("KVM virtual CPU is not open")
	}
	reg := &kvmOneReg{
		Id:    uint64(id),
		Value: uint64(uintptr(unsafe.Pointer(&value))),
	}
	if _, err := ioctl(vcpu.fd, iiow(kvmSetOneReg, 16), uintptr(unsafe.Pointer(reg))); err != 0 {
		return fmt.Errorf("failed to set register %d: %w", id, err)
	}
	return nil
}

func (vcpu *KVMVirtualCPU) GetRegister(id RegisterId) (uint64, error) {
	if vcpu.fd == 0 {
		return 0, fmt.Errorf("KVM virtual CPU is not open")
	}
	var value uint64
	reg := &kvmOneReg{
		Id:    uint64(id),
		Value: uint64(uintptr(unsafe.Pointer(&value))),
	}
	if _, err := ioctl(vcpu.fd, iiow(kvmGetOneReg, 16), uintptr(unsafe.Pointer(reg))); err != 0 {
		return 0, fmt.Errorf("failed to get register %x: %w(%d)", id, err, err)
	}
	return value, nil
}

func (vcpu *KVMVirtualCPU) Close() error {
	// TODO(joshua): Implement proper VCPU cleanup
	return nil
}

var (
	_ io.Closer = &KVMVirtualCPU{}
)

type KVMVirtualMachine struct {
	fd uintptr
}

func (vm *KVMVirtualMachine) CreateVCPU() (*KVMVirtualCPU, error) {
	if vm.fd == 0 {
		return nil, fmt.Errorf("KVM virtual machine is not open")
	}
	res, err := ioctl(vm.fd, iio(kvmCreateVCPU), 0)
	if err != 0 {
		return nil, fmt.Errorf("failed to create KVM VCPU: %w", err)
	}
	vcpu := &KVMVirtualCPU{
		fd: res,
	}
	if vcpu.fd == 0 {
		return nil, fmt.Errorf("failed to create KVM VCPU: returned fd is 0")
	}
	return vcpu, nil
}

func (vm *KVMVirtualMachine) Close() error {
	// TODO(joshua): Implement proper VM cleanup
	return nil
}

var (
	_ io.Closer = &KVMVirtualMachine{}
)

type KVMDevice struct {
	file *os.File
}

func (k *KVMDevice) CreateVM() (*KVMVirtualMachine, error) {
	if k.file == nil {
		return nil, fmt.Errorf("KVM device is not open")
	}

	res, err := ioctl(k.file.Fd(), iio(kvmCreateVM), 0)
	if err != 0 {
		return nil, fmt.Errorf("failed to create KVM VM: %w", err)
	}

	vm := &KVMVirtualMachine{
		fd: res,
	}

	if vm.fd == 0 {
		return nil, fmt.Errorf("failed to create KVM VM: returned fd is 0")
	}

	return vm, nil
}

func (k *KVMDevice) Close() error {
	if k.file != nil {
		if err := k.file.Close(); err != nil {
			return fmt.Errorf("failed to close KVM device: %w", err)
		}
		k.file = nil
	}
	return nil
}

var (
	_ io.Closer = &KVMDevice{}
)

func OpenKVMDevice() (*KVMDevice, error) {
	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/kvm: %w", err)
	}

	dev := &KVMDevice{
		file: file,
	}

	// Check KVM API version
	version, errno := ioctl(file.Fd(), iio(kvmGetAPIVersion), 0)
	if errno != 0 {
		file.Close()
		return nil, fmt.Errorf("failed to get KVM API version: %w", err)
	}
	if version != kvmAPIVersion {
		file.Close()
		return nil, fmt.Errorf("unsupported KVM API version: got %d, want %d", version, kvmAPIVersion)
	}

	return dev, nil
}
