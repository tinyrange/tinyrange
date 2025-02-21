def cpu_thread(cpu):
    cpu.run()

def main(hv, params):
    vm = hv.new(1 << 30)  # 1GB of memory
    vm.load_linux(
        params["kernel"],
        params["cmdline"] or "console=ttyS0 init=/init noapic",
        initrd_path = params["initrd"],
    )
    vm.add_devices()
    vm.new_cpu(cpu_thread)
