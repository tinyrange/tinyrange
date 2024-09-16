def ssh_connect(ctx):
    if "ssh_command" in args:
        return ctx.run(args["ssh_command"])
    else:
        return ctx.run(["/bin/login", "-pf", "root"])

def main():
    network_interface_up("lo")
    network_interface_up("eth0")
    network_interface_configure("eth0", ip = "10.42.0.2/16", router = "10.42.0.1")

    # print(fetch_http("http://1.1.1.1"))

    # Set the hostname.
    set_hostname("tinyrange")

    # Mount /proc filesystem.
    mount("proc", "proc", "/proc", ensure_path = True)

    parse_commandline(file_read("/proc/cmdline"))

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

    # Write /etc/resolv.conf
    path_ensure("/etc")
    file_write("/etc/resolv.conf", "nameserver 10.42.0.1\n")

    # Write a custom MOTD since the default one might link to distribution
    # documentation which may not work inside TinyRange.
    file_write("/etc/motd", "")

    set_env("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
    set_env("HOME", "/root")

    if get_env("TINYRANGE_INTERACTION") == "serial":
        if "ssh_command" in args:
            exec(*args["ssh_command"])
        else:
            exec("/bin/login", "-pf", "root")
    else:
        run_ssh_server(ssh_connect)
