def main():
    network_interface_up("lo")
    network_interface_up("eth0")
    network_interface_configure("eth0", ip = "10.42.0.2/16", router = "10.42.0.1")

    contents = fetch_http("http://10.42.0.1/hello")

    print(contents)

    set_hostname("login")

    # Mount /proc filesystem.
    mount("proc", "proc", "/proc", ensure_path = True)

    # Mount other filesystems.
    mount("devtmpfs", "devtmpfs", "/dev", ensure_path = True, ignore_error = True)
    mount("sysfs", "none", "/sys", ensure_path = True)
    mount("cgroup2", "cgroup2", "/sys/fs/cgroup")
    mount("bpf", "/bpf", "/sys/fs/bpf")
    mount("debugfs", "debugfs", "/sys/kernel/debug", ignore_error = True)
    mount("devpts", "devpts", "/dev/pts", ensure_path = True)
    mount("tmpfs", "tmpfs", "/dev/shm", ensure_path = True)

    # Symlink /dev/fd to /proc/self/fd
    path_symlink("/proc/self/fd", "/dev/fd")

    run("/bin/login", "-f", "root")
