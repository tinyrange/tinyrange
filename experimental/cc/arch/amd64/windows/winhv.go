// Copyright 2025 Microsoft
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

//go:build windows && amd64

package main

import (
	"syscall"
	"unsafe"
)

var (
	winhvplatform = syscall.NewLazyDLL("WinHVPlatform.dll")

	// Platform capabilities
	procWHvGetCapability = winhvplatform.NewProc("WHvGetCapability")

	// Partition management
	procWHvCreatePartition      = winhvplatform.NewProc("WHvCreatePartition")
	procWHvSetupPartition       = winhvplatform.NewProc("WHvSetupPartition")
	procWHvResetPartition       = winhvplatform.NewProc("WHvResetPartition")
	procWHvDeletePartition      = winhvplatform.NewProc("WHvDeletePartition")
	procWHvGetPartitionProperty = winhvplatform.NewProc("WHvGetPartitionProperty")
	procWHvSetPartitionProperty = winhvplatform.NewProc("WHvSetPartitionProperty")
	procWHvSuspendPartitionTime = winhvplatform.NewProc("WHvSuspendPartitionTime")
	procWHvResumePartitionTime  = winhvplatform.NewProc("WHvResumePartitionTime")

	// Memory management
	procWHvMapGpaRange    = winhvplatform.NewProc("WHvMapGpaRange")
	procWHvMapGpaRange2   = winhvplatform.NewProc("WHvMapGpaRange2")
	procWHvUnmapGpaRange  = winhvplatform.NewProc("WHvUnmapGpaRange")
	procWHvTranslateGva   = winhvplatform.NewProc("WHvTranslateGva")
	procWHvAdviseGpaRange = winhvplatform.NewProc("WHvAdviseGpaRange")
	procWHvReadGpaRange   = winhvplatform.NewProc("WHvReadGpaRange")
	procWHvWriteGpaRange  = winhvplatform.NewProc("WHvWriteGpaRange")

	// Virtual processors
	procWHvCreateVirtualProcessor           = winhvplatform.NewProc("WHvCreateVirtualProcessor")
	procWHvCreateVirtualProcessor2          = winhvplatform.NewProc("WHvCreateVirtualProcessor2")
	procWHvDeleteVirtualProcessor           = winhvplatform.NewProc("WHvDeleteVirtualProcessor")
	procWHvRunVirtualProcessor              = winhvplatform.NewProc("WHvRunVirtualProcessor")
	procWHvCancelRunVirtualProcessor        = winhvplatform.NewProc("WHvCancelRunVirtualProcessor")
	procWHvGetVirtualProcessorRegisters     = winhvplatform.NewProc("WHvGetVirtualProcessorRegisters")
	procWHvSetVirtualProcessorRegisters     = winhvplatform.NewProc("WHvSetVirtualProcessorRegisters")
	procWHvGetVirtualProcessorState         = winhvplatform.NewProc("WHvGetVirtualProcessorState")
	procWHvSetVirtualProcessorState         = winhvplatform.NewProc("WHvSetVirtualProcessorState")
	procWHvGetVirtualProcessorCpuidOutput   = winhvplatform.NewProc("WHvGetVirtualProcessorCpuidOutput")
	procWHvRequestInterrupt                 = winhvplatform.NewProc("WHvRequestInterrupt")
	procWHvGetInterruptTargetVpSet          = winhvplatform.NewProc("WHvGetInterruptTargetVpSet")
	procWHvSignalVirtualProcessorSynicEvent = winhvplatform.NewProc("WHvSignalVirtualProcessorSynicEvent")
	procWHvPostVirtualProcessorSynicMessage = winhvplatform.NewProc("WHvPostVirtualProcessorSynicMessage")

	// Deprecated functions (AMD64 only)
	procWHvGetVirtualProcessorInterruptControllerState  = winhvplatform.NewProc("WHvGetVirtualProcessorInterruptControllerState")
	procWHvSetVirtualProcessorInterruptControllerState  = winhvplatform.NewProc("WHvSetVirtualProcessorInterruptControllerState")
	procWHvGetVirtualProcessorInterruptControllerState2 = winhvplatform.NewProc("WHvGetVirtualProcessorInterruptControllerState2")
	procWHvSetVirtualProcessorInterruptControllerState2 = winhvplatform.NewProc("WHvSetVirtualProcessorInterruptControllerState2")
	procWHvGetVirtualProcessorXsaveState                = winhvplatform.NewProc("WHvGetVirtualProcessorXsaveState")
	procWHvSetVirtualProcessorXsaveState                = winhvplatform.NewProc("WHvSetVirtualProcessorXsaveState")

	// Counters and monitoring
	procWHvQueryGpaRangeDirtyBitmap    = winhvplatform.NewProc("WHvQueryGpaRangeDirtyBitmap")
	procWHvGetPartitionCounters        = winhvplatform.NewProc("WHvGetPartitionCounters")
	procWHvGetVirtualProcessorCounters = winhvplatform.NewProc("WHvGetVirtualProcessorCounters")

	// Deprecated doorbell events
	procWHvRegisterPartitionDoorbellEvent   = winhvplatform.NewProc("WHvRegisterPartitionDoorbellEvent")
	procWHvUnregisterPartitionDoorbellEvent = winhvplatform.NewProc("WHvUnregisterPartitionDoorbellEvent")

	// Virtual PCI
	procWHvAllocateVpciResource         = winhvplatform.NewProc("WHvAllocateVpciResource")
	procWHvCreateVpciDevice             = winhvplatform.NewProc("WHvCreateVpciDevice")
	procWHvDeleteVpciDevice             = winhvplatform.NewProc("WHvDeleteVpciDevice")
	procWHvGetVpciDeviceProperty        = winhvplatform.NewProc("WHvGetVpciDeviceProperty")
	procWHvGetVpciDeviceNotification    = winhvplatform.NewProc("WHvGetVpciDeviceNotification")
	procWHvMapVpciDeviceMmioRanges      = winhvplatform.NewProc("WHvMapVpciDeviceMmioRanges")
	procWHvUnmapVpciDeviceMmioRanges    = winhvplatform.NewProc("WHvUnmapVpciDeviceMmioRanges")
	procWHvSetVpciDevicePowerState      = winhvplatform.NewProc("WHvSetVpciDevicePowerState")
	procWHvReadVpciDeviceRegister       = winhvplatform.NewProc("WHvReadVpciDeviceRegister")
	procWHvWriteVpciDeviceRegister      = winhvplatform.NewProc("WHvWriteVpciDeviceRegister")
	procWHvMapVpciDeviceInterrupt       = winhvplatform.NewProc("WHvMapVpciDeviceInterrupt")
	procWHvUnmapVpciDeviceInterrupt     = winhvplatform.NewProc("WHvUnmapVpciDeviceInterrupt")
	procWHvRetargetVpciDeviceInterrupt  = winhvplatform.NewProc("WHvRetargetVpciDeviceInterrupt")
	procWHvRequestVpciDeviceInterrupt   = winhvplatform.NewProc("WHvRequestVpciDeviceInterrupt")
	procWHvGetVpciDeviceInterruptTarget = winhvplatform.NewProc("WHvGetVpciDeviceInterruptTarget")

	// Triggers
	procWHvCreateTrigger           = winhvplatform.NewProc("WHvCreateTrigger")
	procWHvUpdateTriggerParameters = winhvplatform.NewProc("WHvUpdateTriggerParameters")
	procWHvDeleteTrigger           = winhvplatform.NewProc("WHvDeleteTrigger")

	// Notification ports
	procWHvCreateNotificationPort      = winhvplatform.NewProc("WHvCreateNotificationPort")
	procWHvSetNotificationPortProperty = winhvplatform.NewProc("WHvSetNotificationPortProperty")
	procWHvDeleteNotificationPort      = winhvplatform.NewProc("WHvDeleteNotificationPort")

	// Migration
	procWHvStartPartitionMigration    = winhvplatform.NewProc("WHvStartPartitionMigration")
	procWHvCancelPartitionMigration   = winhvplatform.NewProc("WHvCancelPartitionMigration")
	procWHvCompletePartitionMigration = winhvplatform.NewProc("WHvCompletePartitionMigration")
	procWHvAcceptPartitionMigration   = winhvplatform.NewProc("WHvAcceptPartitionMigration")
)

// Type definitions
type (
	WHVPartitionHandle              uintptr
	WHVTriggerHandle                uintptr
	WHVNotificationPortHandle       uintptr
	WHVGuestPhysicalAddress         uint64
	WHVGuestVirtualAddress          uint64
	WHVCapabilityCode               uint32
	WHVPartitionPropertyCode        uint32
	WHVRegisterName                 uint32
	WHVVirtualProcessorStateType    uint32
	WHVMapGpaRangeFlags             uint32
	WHVTranslateGvaFlags            uint32
	WHVAccessGpaControls            uint32
	WHVPartitionCounterSet          uint32
	WHVProcessorCounterSet          uint32
	WHVAdviseGpaRangeCode           uint32
	WHVInterruptDestinationMode     uint32
	WHVAllocateVpciResourceFlags    uint32
	WHVCreateVpciDeviceFlags        uint32
	WHVVpciDevicePropertyCode       uint32
	WHVNotificationPortPropertyCode uint32
)

const (
	WHvPartitionPropertyCodeExtendedVmExits                 WHVPartitionPropertyCode = 0x00000001
	WHvPartitionPropertyCodeExceptionExitBitmap             WHVPartitionPropertyCode = 0x00000002
	WHvPartitionPropertyCodeSeparateSecurityDomain          WHVPartitionPropertyCode = 0x00000003
	WHvPartitionPropertyCodeNestedVirtualization            WHVPartitionPropertyCode = 0x00000004
	WHvPartitionPropertyCodeX64MsrExitBitmap                WHVPartitionPropertyCode = 0x00000005
	WHvPartitionPropertyCodePrimaryNumaNode                 WHVPartitionPropertyCode = 0x00000006
	WHvPartitionPropertyCodeCpuReserve                      WHVPartitionPropertyCode = 0x00000007
	WHvPartitionPropertyCodeCpuCap                          WHVPartitionPropertyCode = 0x00000008
	WHvPartitionPropertyCodeCpuWeight                       WHVPartitionPropertyCode = 0x00000009
	WHvPartitionPropertyCodeCpuGroupId                      WHVPartitionPropertyCode = 0x0000000a
	WHvPartitionPropertyCodeProcessorFrequencyCap           WHVPartitionPropertyCode = 0x0000000b
	WHvPartitionPropertyCodeAllowDeviceAssignment           WHVPartitionPropertyCode = 0x0000000c
	WHvPartitionPropertyCodeDisableSmt                      WHVPartitionPropertyCode = 0x0000000d
	WHvPartitionPropertyCodeProcessorFeatures               WHVPartitionPropertyCode = 0x00001001
	WHvPartitionPropertyCodeProcessorClFlushSize            WHVPartitionPropertyCode = 0x00001002
	WHvPartitionPropertyCodeCpuidExitList                   WHVPartitionPropertyCode = 0x00001003
	WHvPartitionPropertyCodeCpuidResultList                 WHVPartitionPropertyCode = 0x00001004
	WHvPartitionPropertyCodeLocalApicEmulationMode          WHVPartitionPropertyCode = 0x00001005
	WHvPartitionPropertyCodeProcessorXsaveFeatures          WHVPartitionPropertyCode = 0x00001006
	WHvPartitionPropertyCodeProcessorClockFrequency         WHVPartitionPropertyCode = 0x00001007
	WHvPartitionPropertyCodeInterruptClockFrequency         WHVPartitionPropertyCode = 0x00001008
	WHvPartitionPropertyCodeApicRemoteReadSupport           WHVPartitionPropertyCode = 0x00001009
	WHvPartitionPropertyCodeProcessorFeaturesBanks          WHVPartitionPropertyCode = 0x0000100A
	WHvPartitionPropertyCodeReferenceTime                   WHVPartitionPropertyCode = 0x0000100B
	WHvPartitionPropertyCodeSyntheticProcessorFeaturesBanks WHVPartitionPropertyCode = 0x0000100C
	WHvPartitionPropertyCodeCpuidResultList2                WHVPartitionPropertyCode = 0x0000100D
	WHvPartitionPropertyCodeProcessorPerfmonFeatures        WHVPartitionPropertyCode = 0x0000100E
	WHvPartitionPropertyCodeMsrActionList                   WHVPartitionPropertyCode = 0x0000100F
	WHvPartitionPropertyCodeUnimplementedMsrAction          WHVPartitionPropertyCode = 0x00001010
	WHvPartitionPropertyCodePhysicalAddressWidth            WHVPartitionPropertyCode = 0x00001011
	WHvPartitionPropertyCodeProcessorCount                  WHVPartitionPropertyCode = 0x00001fff
)

// Register value union
type WHVRegisterValue struct {
	Data [16]byte
}

// CPUID output structure
type WHVCpuidOutput struct {
	Eax uint32
	Ebx uint32
	Ecx uint32
	Edx uint32
}

// Translate GVA result
type WHVTranslateGvaResult struct {
	ResultCode uint32
	Reserved   uint32
}

// Interrupt control structure
type WHVInterruptControl struct {
	Type            uint64
	DestinationMode uint32
	TriggerMode     uint32
	Vector          uint32
	Destination     uint64
}

// SYNIC event parameters
type WHVSynicEventParameters struct {
	VpIndex    uint32
	SintIndex  uint32
	Reserved   uint16
	FlagNumber uint16
	Flags      uint32
}

// Memory range entry
type WHVMemoryRangeEntry struct {
	GuestAddress WHVGuestPhysicalAddress
	SizeInBytes  uint64
}

// Virtual processor property
type WHVVirtualProcessorProperty struct {
	PropertyCode  uint32
	Reserved      uint32
	PropertyValue uint64
}

// VPCI device register
type WHVVpciDeviceRegister struct {
	Location    uint64
	SizeInBytes uint32
	Reserved    uint32
}

// VPCI interrupt target
type WHVVpciInterruptTarget struct {
	Vector       uint32
	Flags        uint32
	ProcessorSet uint64
}

// VPCI MMIO mapping
type WHVVpciMmioMapping struct {
	Location uint64
	Length   uint64
	Flags    uint32
	Reserved uint32
}

// VPCI device notification
type WHVVpciDeviceNotification struct {
	NotificationType uint32
	Reserved1        uint32
	Reserved2        uint64
}

// Doorbell match data
type WHVDoorbellMatchData struct {
	GuestAddress WHVGuestPhysicalAddress
	Value        uint64
	Length       uint32
	MatchType    uint32
}

// Trigger parameters
type WHVTriggerParameters struct {
	TriggerType uint32
	Reserved    uint32
	Parameters  [32]byte
}

// Notification port parameters
type WHVNotificationPortParameters struct {
	NotificationType uint32
	Reserved         uint32
	Parameters       [32]byte
}

// Notification port property
type WHVNotificationPortProperty struct {
	PropertyValue uint64
}

// Helper function to check if HRESULT indicates failure
func hresultFailed(hr uintptr) bool {
	return int32(hr) < 0
}

// Helper function to convert HRESULT to error
func hresultToError(hr uintptr) error {
	if hresultFailed(hr) {
		return syscall.Errno(hr)
	}
	return nil
}

// Platform capabilities
func WHvGetCapability(
	capabilityCode WHVCapabilityCode,
	capabilityBuffer unsafe.Pointer,
	capabilityBufferSizeInBytes uint32,
) (writtenSizeInBytes uint32, err error) {
	r1, _, _ := procWHvGetCapability.Call(
		uintptr(capabilityCode),
		uintptr(capabilityBuffer),
		uintptr(capabilityBufferSizeInBytes),
		uintptr(unsafe.Pointer(&writtenSizeInBytes)),
	)
	err = hresultToError(r1)
	return
}

// Partition management
func WHvCreatePartition() (partition WHVPartitionHandle, err error) {
	r1, _, _ := procWHvCreatePartition.Call(
		uintptr(unsafe.Pointer(&partition)),
	)
	err = hresultToError(r1)
	return
}

func WHvSetupPartition(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvSetupPartition.Call(uintptr(partition))
	return hresultToError(r1)
}

func WHvResetPartition(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvResetPartition.Call(uintptr(partition))
	return hresultToError(r1)
}

func WHvDeletePartition(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvDeletePartition.Call(uintptr(partition))
	return hresultToError(r1)
}

func WHvGetPartitionProperty(
	partition WHVPartitionHandle,
	propertyCode WHVPartitionPropertyCode,
	propertyBuffer unsafe.Pointer,
	propertyBufferSizeInBytes uint32,
) (writtenSizeInBytes uint32, err error) {
	r1, _, _ := procWHvGetPartitionProperty.Call(
		uintptr(partition),
		uintptr(propertyCode),
		uintptr(propertyBuffer),
		uintptr(propertyBufferSizeInBytes),
		uintptr(unsafe.Pointer(&writtenSizeInBytes)),
	)
	err = hresultToError(r1)
	return
}

func WHvSetPartitionProperty(
	partition WHVPartitionHandle,
	propertyCode WHVPartitionPropertyCode,
	propertyBuffer unsafe.Pointer,
	propertyBufferSizeInBytes uint32,
) error {
	r1, _, _ := procWHvSetPartitionProperty.Call(
		uintptr(partition),
		uintptr(propertyCode),
		uintptr(propertyBuffer),
		uintptr(propertyBufferSizeInBytes),
	)
	return hresultToError(r1)
}

func WHvSuspendPartitionTime(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvSuspendPartitionTime.Call(uintptr(partition))
	return hresultToError(r1)
}

func WHvResumePartitionTime(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvResumePartitionTime.Call(uintptr(partition))
	return hresultToError(r1)
}

// Memory management
func WHvMapGpaRange(
	partition WHVPartitionHandle,
	sourceAddress unsafe.Pointer,
	guestAddress WHVGuestPhysicalAddress,
	sizeInBytes uint64,
	flags WHVMapGpaRangeFlags,
) error {
	r1, _, _ := procWHvMapGpaRange.Call(
		uintptr(partition),
		uintptr(sourceAddress),
		uintptr(guestAddress),
		uintptr(sizeInBytes),
		uintptr(flags),
	)
	return hresultToError(r1)
}

func WHvMapGpaRange2(
	partition WHVPartitionHandle,
	process syscall.Handle,
	sourceAddress unsafe.Pointer,
	guestAddress WHVGuestPhysicalAddress,
	sizeInBytes uint64,
	flags WHVMapGpaRangeFlags,
) error {
	r1, _, _ := procWHvMapGpaRange2.Call(
		uintptr(partition),
		uintptr(process),
		uintptr(sourceAddress),
		uintptr(guestAddress),
		uintptr(sizeInBytes),
		uintptr(flags),
	)
	return hresultToError(r1)
}

func WHvUnmapGpaRange(
	partition WHVPartitionHandle,
	guestAddress WHVGuestPhysicalAddress,
	sizeInBytes uint64,
) error {
	r1, _, _ := procWHvUnmapGpaRange.Call(
		uintptr(partition),
		uintptr(guestAddress),
		uintptr(sizeInBytes),
	)
	return hresultToError(r1)
}

func WHvTranslateGva(
	partition WHVPartitionHandle,
	vpIndex uint32,
	gva WHVGuestVirtualAddress,
	translateFlags WHVTranslateGvaFlags,
) (translationResult WHVTranslateGvaResult, gpa WHVGuestPhysicalAddress, err error) {
	r1, _, _ := procWHvTranslateGva.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(gva),
		uintptr(translateFlags),
		uintptr(unsafe.Pointer(&translationResult)),
		uintptr(unsafe.Pointer(&gpa)),
	)
	err = hresultToError(r1)
	return
}

// Virtual processors
func WHvCreateVirtualProcessor(
	partition WHVPartitionHandle,
	vpIndex uint32,
	flags uint32,
) error {
	r1, _, _ := procWHvCreateVirtualProcessor.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(flags),
	)
	return hresultToError(r1)
}

func WHvCreateVirtualProcessor2(
	partition WHVPartitionHandle,
	vpIndex uint32,
	properties []WHVVirtualProcessorProperty,
) error {
	var propertiesPtr unsafe.Pointer
	if len(properties) > 0 {
		propertiesPtr = unsafe.Pointer(&properties[0])
	}

	r1, _, _ := procWHvCreateVirtualProcessor2.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(propertiesPtr),
		uintptr(len(properties)),
	)
	return hresultToError(r1)
}

func WHvDeleteVirtualProcessor(
	partition WHVPartitionHandle,
	vpIndex uint32,
) error {
	r1, _, _ := procWHvDeleteVirtualProcessor.Call(
		uintptr(partition),
		uintptr(vpIndex),
	)
	return hresultToError(r1)
}

func WHvRunVirtualProcessor(
	partition WHVPartitionHandle,
	vpIndex uint32,
	exitContext unsafe.Pointer,
	exitContextSizeInBytes uint32,
) error {
	r1, _, _ := procWHvRunVirtualProcessor.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(exitContext),
		uintptr(exitContextSizeInBytes),
	)
	return hresultToError(r1)
}

func WHvCancelRunVirtualProcessor(
	partition WHVPartitionHandle,
	vpIndex uint32,
	flags uint32,
) error {
	r1, _, _ := procWHvCancelRunVirtualProcessor.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(flags),
	)
	return hresultToError(r1)
}

func WHvGetVirtualProcessorRegisters(
	partition WHVPartitionHandle,
	vpIndex uint32,
	registerNames []WHVRegisterName,
	registerValues []WHVRegisterValue,
) error {
	if len(registerNames) != len(registerValues) {
		return syscall.EINVAL
	}

	var registerNamesPtr, registerValuesPtr unsafe.Pointer
	if len(registerNames) > 0 {
		registerNamesPtr = unsafe.Pointer(&registerNames[0])
		registerValuesPtr = unsafe.Pointer(&registerValues[0])
	}

	r1, _, _ := procWHvGetVirtualProcessorRegisters.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(registerNamesPtr),
		uintptr(len(registerNames)),
		uintptr(registerValuesPtr),
	)
	return hresultToError(r1)
}

func WHvSetVirtualProcessorRegisters(
	partition WHVPartitionHandle,
	vpIndex uint32,
	registerNames []WHVRegisterName,
	registerValues []WHVRegisterValue,
) error {
	if len(registerNames) != len(registerValues) {
		return syscall.EINVAL
	}

	var registerNamesPtr, registerValuesPtr unsafe.Pointer
	if len(registerNames) > 0 {
		registerNamesPtr = unsafe.Pointer(&registerNames[0])
		registerValuesPtr = unsafe.Pointer(&registerValues[0])
	}

	r1, _, _ := procWHvSetVirtualProcessorRegisters.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(registerNamesPtr),
		uintptr(len(registerNames)),
		uintptr(registerValuesPtr),
	)
	return hresultToError(r1)
}

func WHvGetVirtualProcessorCpuidOutput(
	partition WHVPartitionHandle,
	vpIndex uint32,
	eax uint32,
	ecx uint32,
) (cpuidOutput WHVCpuidOutput, err error) {
	r1, _, _ := procWHvGetVirtualProcessorCpuidOutput.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(eax),
		uintptr(ecx),
		uintptr(unsafe.Pointer(&cpuidOutput)),
	)
	err = hresultToError(r1)
	return
}

func WHvGetInterruptTargetVpSet(
	partition WHVPartitionHandle,
	destination uint64,
	destinationMode WHVInterruptDestinationMode,
	targetVps []uint32,
) (targetVpCount uint32, err error) {
	var targetVpsPtr unsafe.Pointer
	if len(targetVps) > 0 {
		targetVpsPtr = unsafe.Pointer(&targetVps[0])
	}

	r1, _, _ := procWHvGetInterruptTargetVpSet.Call(
		uintptr(partition),
		uintptr(destination),
		uintptr(destinationMode),
		uintptr(targetVpsPtr),
		uintptr(len(targetVps)),
		uintptr(unsafe.Pointer(&targetVpCount)),
	)
	err = hresultToError(r1)
	return
}

func WHvRequestInterrupt(
	partition WHVPartitionHandle,
	interrupt *WHVInterruptControl,
	interruptControlSize uint32,
) error {
	r1, _, _ := procWHvRequestInterrupt.Call(
		uintptr(partition),
		uintptr(unsafe.Pointer(interrupt)),
		uintptr(interruptControlSize),
	)
	return hresultToError(r1)
}

func WHvGetVirtualProcessorState(
	partition WHVPartitionHandle,
	vpIndex uint32,
	stateType WHVVirtualProcessorStateType,
	buffer unsafe.Pointer,
	bufferSizeInBytes uint32,
) (bytesWritten uint32, err error) {
	r1, _, _ := procWHvGetVirtualProcessorState.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(stateType),
		uintptr(buffer),
		uintptr(bufferSizeInBytes),
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	err = hresultToError(r1)
	return
}

func WHvSetVirtualProcessorState(
	partition WHVPartitionHandle,
	vpIndex uint32,
	stateType WHVVirtualProcessorStateType,
	buffer unsafe.Pointer,
	bufferSizeInBytes uint32,
) error {
	r1, _, _ := procWHvSetVirtualProcessorState.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(stateType),
		uintptr(buffer),
		uintptr(bufferSizeInBytes),
	)
	return hresultToError(r1)
}

// Migration functions
func WHvStartPartitionMigration(
	partition WHVPartitionHandle,
) (migrationHandle syscall.Handle, err error) {
	r1, _, _ := procWHvStartPartitionMigration.Call(
		uintptr(partition),
		uintptr(unsafe.Pointer(&migrationHandle)),
	)
	err = hresultToError(r1)
	return
}

func WHvCancelPartitionMigration(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvCancelPartitionMigration.Call(uintptr(partition))
	return hresultToError(r1)
}

func WHvCompletePartitionMigration(partition WHVPartitionHandle) error {
	r1, _, _ := procWHvCompletePartitionMigration.Call(uintptr(partition))
	return hresultToError(r1)
}

func WHvAcceptPartitionMigration(
	migrationHandle syscall.Handle,
) (partition WHVPartitionHandle, err error) {
	r1, _, _ := procWHvAcceptPartitionMigration.Call(
		uintptr(migrationHandle),
		uintptr(unsafe.Pointer(&partition)),
	)
	err = hresultToError(r1)
	return
}

// Additional utility functions for common operations
func WHvSignalVirtualProcessorSynicEvent(
	partition WHVPartitionHandle,
	synicEvent WHVSynicEventParameters,
) (newlySignaled bool, err error) {
	var newlySignaledInt int32
	r1, _, _ := procWHvSignalVirtualProcessorSynicEvent.Call(
		uintptr(partition),
		uintptr(unsafe.Pointer(&synicEvent)),
		uintptr(unsafe.Pointer(&newlySignaledInt)),
	)
	err = hresultToError(r1)
	newlySignaled = newlySignaledInt != 0
	return
}

func WHvPostVirtualProcessorSynicMessage(
	partition WHVPartitionHandle,
	vpIndex uint32,
	sintIndex uint32,
	message unsafe.Pointer,
	messageSizeInBytes uint32,
) error {
	r1, _, _ := procWHvPostVirtualProcessorSynicMessage.Call(
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(sintIndex),
		uintptr(message),
		uintptr(messageSizeInBytes),
	)
	return hresultToError(r1)
}
