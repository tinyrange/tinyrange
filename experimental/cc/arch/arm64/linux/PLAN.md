## Phase 1: Foundation & Basic KVM Integration

### Milestone 1.1: KVM Device Access

**Test Cases:**

- [x] Open `/dev/kvm` successfully
- [x] Query KVM API version and verify compatibility

### Milestone 1.2: VM Creation & Basic vCPU Management

**Test Cases:**

- [x] Create a KVM VM instance
- [x] Create a single vCPU for the VM
- [x] Set basic vCPU registers (PC, SP, CPSR)

### Milestone 1.3: Guest Physical Memory Management

**Test Cases:**

- [x] Allocate host memory for guest RAM
- [x] Map guest physical address ranges to host virtual memory
- [x] Test different memory sizes (1MB, 16MB, 64MB, 128MB)
- [x] Verify vCPU enters and exits correctly
- [x] Test memory read/write operations from vCPU
- [x] Execute a simple instruction sequence (e.g., infinite loop)

## Phase 2: Basic Execution Environment

### Milestone 2.1: Simple Payload Execution

**Test Cases:**

- [x] Load and execute hand-crafted ARM64 assembly that writes to memory
- [x] Execute payload that performs arithmetic operations
- [x] Test branching and basic control flow
- [x] Verify register state preservation across VM exits

### Milestone 2.2: Interrupt Controller Foundation

**Test Cases:**

- [ ] Initialize basic GICv2 (Generic Interrupt Controller)
- [ ] Test timer interrupt generation and handling
- [ ] Verify interrupt masking and unmasking
- [ ] Test interrupt priority handling

## Phase 3: Serial Console & Basic I/O

### Milestone 3.1: UART Emulation

**Test Cases:**

- [ ] Implement PL011 UART emulation
- [ ] Test character output from guest to host console
- [ ] Verify UART register read/write operations
- [ ] Test interrupt-driven and polling modes
- [ ] Execute simple "Hello World" payload via UART

### Milestone 3.2: Memory-Mapped I/O Framework

**Test Cases:**

- [ ] Implement MMIO trap and emulation framework
- [ ] Test device registration and address space management
- [ ] Verify MMIO read/write routing to correct devices
- [ ] Test overlapping and conflicting device mappings

## Phase 4: Linux Kernel Boot Preparation

### Milestone 4.1: Device Tree Support

**Test Cases:**

- [ ] Generate minimal ARM64 device tree blob
- [ ] Include CPU, memory, and UART descriptions
- [ ] Test device tree parsing and validation
- [ ] Verify device tree loaded at correct guest physical address

### Milestone 4.2: Linux Boot Protocol

**Test Cases:**

- [ ] Load Linux kernel Image at correct address (0x40080000 typically)
- [ ] Set up proper boot arguments in x0-x3 registers
- [ ] Test with minimal initramfs
- [ ] Verify kernel entry point execution

### Milestone 4.3: Timer and Clock Support

**Test Cases:**

- [ ] Implement ARM Generic Timer support
- [ ] Test timer interrupt generation at regular intervals
- [ ] Verify timer frequency and accuracy
- [ ] Test timer masking and configuration

## Phase 5: Linux Kernel Boot

### Milestone 5.1: Early Kernel Boot

**Test Cases:**

- [ ] Boot Linux kernel to early printk output
- [ ] Verify kernel decompression (if using compressed image)
- [ ] Test kernel command line parsing
- [ ] Validate initial memory layout detection

### Milestone 5.2: Core Kernel Initialization

**Test Cases:**

- [ ] Boot through kernel initialization
- [ ] Verify CPU online sequences
- [ ] Test interrupt controller initialization
- [ ] Validate timer subsystem initialization

### Milestone 5.3: Console and Basic Userspace

**Test Cases:**

- [ ] Achieve kernel console output via UART
- [ ] Boot to initramfs or simple init process
- [ ] Test basic shell commands
- [ ] Verify process creation and scheduling

## Phase 6: Virtio Device Framework

### Milestone 6.1: Virtio Transport Layer

**Test Cases:**

- [ ] Implement virtio-mmio transport
- [ ] Test virtqueue setup and management
- [ ] Verify descriptor chain processing
- [ ] Test interrupt notification mechanisms

### Milestone 6.2: Virtio-Console Device

**Test Cases:**

- [ ] Implement basic virtio-console device
- [ ] Test bidirectional communication
- [ ] Verify proper virtqueue handling
- [ ] Test console resize and configuration

### Milestone 6.3: Advanced Virtio Devices

**Test Cases:**

- [ ] Implement virtio-net device with basic networking
- [ ] Implement virtio-block device with file backing
- [ ] Test device hotplug/unplug scenarios
- [ ] Verify multi-queue support where applicable

## Phase 7: Performance and Stability

### Milestone 7.1: Multi-vCPU Support

**Test Cases:**

- [ ] Create and manage multiple vCPUs
- [ ] Test SMP Linux kernel boot
- [ ] Verify CPU hotplug operations
- [ ] Test inter-CPU interrupt delivery

### Milestone 7.2: Advanced Memory Management

**Test Cases:**

- [ ] Implement memory ballooning
- [ ] Test large memory configurations (>1GB)
- [ ] Verify memory hotplug support
- [ ] Test memory overcommitment scenarios

### Milestone 7.3: Backend Abstraction Validation

**Test Cases:**

- [ ] Verify device backend swapping works correctly
- [ ] Test different storage backends for virtio-block
- [ ] Test different network backends for virtio-net
- [ ] Validate clean separation between frontend and backend
