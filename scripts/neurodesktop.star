VERSION = "2024-05-25"

HOSTS = """127.0.0.1       localhost
::1     localhost ip6-localhost ip6-loopback
fe00::0 ip6-localnet
ff00::0 ip6-mcastprefix
ff02::1 ip6-allnodes
ff02::2 ip6-allrouters
"""

neurodesktop = define.build_vm(
    [
        directive.add_file("/etc/hosts", file(HOSTS)),
        define.fetch_oci_image("vnmd/neurodesktop", tag = VERSION),
        directive.environment({"NB_UID": str(1000), "NB_GID": str(1000)}),
        directive.run_command("chmod 666 /dev/fuse"),
        directive.run_command("cd /home/jovyan;bash -lc \"start-notebook.py\""),
        directive.export_port("jupyterlab", 8888),
    ],
    storage_size = 8192,
    memory_mb = 4096,
    cpu_cores = 4,
)
