def ssh_connect(ctx):
    if "ssh_command" in args:
        return ctx.run(args["ssh_command"])
    else:
        return ctx.run(["/bin/login", "-pf", "root"])

def main():
    parse_commandline(file_read("/proc/cmdline"))

    nonet = False

    # Only configure the network if the TINYRANGE_NONET environment variable is not set.
    if get_env("TINYRANGE_NONET") == "":
        network_interface_up("lo")
        network_interface_up("eth0")
        guest_cidr = get_env("TINYRANGE_GUEST_CIDR") or "10.42.0.2/16"
        host_ip = get_env("TINYRANGE_HOST_IP") or "10.42.0.1"
        network_interface_configure("eth0", ip = guest_cidr, router = host_ip)
        print("configured network interface eth0 with IP {}".format(guest_cidr))
    else:
        nonet = True

    # Set the hostname.
    set_hostname("tinyrange")

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
    if not nonet:
        path_ensure("/etc", make_symlink_target = True)
        host_ip = get_env("TINYRANGE_HOST_IP") or "10.42.0.1"
        file_write("/etc/resolv.conf", "nameserver {}\n".format(host_ip), remove_symlink = True)

    # Write a custom MOTD since the default one might link to distribution
    # documentation which may not work inside TinyRange.
    file_write("/etc/motd", "")

    set_env("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
    set_env("HOME", "/root")

    # Run additional scripts.
    if "additional_scripts" in args:
        for script in args["additional_scripts"]:
            run_starlark(script)

    # If the nbd_test flag is set then mount a test filesystem at /mnt.
    if has_experimental_flag("nbd_test"):
        path_ensure("/mnt")
        host_ip = get_env("TINYRANGE_HOST_IP") or "10.42.0.1"
        dev = connect_nbd(host_ip, 10809, "nbd_test")
        mount("ext4", dev, "/mnt")

    interaction = get_env("TINYRANGE_INTERACTION")
    if interaction == "serial" or nonet:
        if "ssh_command" in args:
            exec(*args["ssh_command"])
        else:
            exec("/bin/login", "-pf", "root")
    elif interaction != "":
        # Run the SSH server.
        password = ""
        host_key = ""

        if "ssh_password" in args:
            password = args["ssh_password"]

        if "ssh_host_key" in args:
            host_key = args["ssh_host_key"]
        
        run_ssh_server(ssh_connect, host_key = host_key, password = password)
    else:
        print("detected unhosted environment, dropping to shell")
        run_shell()
