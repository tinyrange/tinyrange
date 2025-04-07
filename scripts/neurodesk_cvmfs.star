db.add_mirror("neurodesk", ["http://cvmfs.neurodesk.org/cvmfs"])

def make_test_vm(name, version):
    cvmfs_repo = define.fetch_cvmfs(
        mirror = "mirror://neurodesk",
        repo = "neurodesk.ardc.edu.au",
        path = "/containers/{}_{}/{}_{}.simg".format(name, version, name, version),
    )

    return define.build_vm(
        directives = [
            directive.archive(cvmfs_repo, archive2 = True),
            directive.run_command("/.singularity.d/actions/exec /bin/bash"),
        ],
    )

niimath_vm = make_test_vm("niimath", "1.0.0_20240902")
fsl_vm = make_test_vm("fsl", "6.0.7.16_20250131")

run_command = directive.run_command(
    "chmod 777 /dev/fuse;" +
    "NEURODESKTOP_VERSION=2025-03-12 " +
    "start.sh jupyter lab " +
    "--ServerApp.password='' " +
    "--no-browser " +
    "--expose-app-in-browser " +
    "--ServerApp.token='jlab:srvr:5dd0c63f33be5d0fdc881461d7d897ca95cc80' " +
    "--ServerApp.port=57164 " +
    "--LabApp.quit_button=False ",
)

neurodesktop_docker_dir = define.fetch_oci_image(
    image = "vnmd/neurodesktop",
    tag = "2025-03-12",
    arch = "amd64",
)

neurodesktop_cvmfs_base = define.fetch_cvmfs(
    mirror = "mirror://neurodesk",
    repo = "neurodesk.ardc.edu.au",
    path = "/neurodesktop/2025-03-12",
)

neurodesktop_cvmfs_dir = directive.archive(neurodesktop_cvmfs_base, archive2 = True)

neurodesktop_cvmfs_env = directive.environment({
    "PATH": "/opt/conda/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
    "DEBIAN_FRONTEND": "noninteractive",
    "CONDA_DIR": "/opt/conda",
    "SHELL": "/bin/bash",
    "NB_USER": "jovyan",
    "NB_UID": "1000",
    "NB_GID": "100",
    "LC_ALL": "",
    "LANG": "",
    "LANGUAGE": "",
    "HOME": "/home/jovyan",
    "JUPYTER_PORT": "8888",
    "DONT_PROMPT_WSL_INSTALL": "1",
    "LMOD_CMD": "/usr/share/lmod/lmod/libexec/lmod",
    "APPTAINER_BINDPATH": "/data,/mnt,/neurodesktop-storage,/tmp,/cvmfs",
    "MODULEPATH": "/neurodesktop-storage/containers/modules/:/cvmfs/neurodesk.ardc.edu.au/containers/modules/",
    "neurodesk_singularity_opts": " --overlay /tmp/apptainer_overlay ",
})

neurodesktop_docker_file_info = define.build_vm(
    directives = [
        neurodesktop_docker_dir,
        directive.run_command("/init -dump-fs /dev/null -dump-fs-hash"),
    ],
)

neurodesktop_docker = define.build_vm(
    directives = [
        neurodesktop_docker_dir,
        run_command,
    ],
    cpu_cores = 8,
    memory_mb = 8192,
    storage_size = 4096,
)

neurodesktop_cvmfs_file_info = define.build_vm(
    directives = [
        neurodesktop_cvmfs_dir,
        directive.run_command("/init -dump-fs /dev/null -dump-fs-hash"),
    ],
)

neurodesktop_cvmfs_shell = define.build_vm(
    directives = [
        neurodesktop_cvmfs_dir,
        directive.run_command("/init -shell"),
    ],
)

neurodesktop_cvmfs = define.build_vm(
    directives = [
        neurodesktop_cvmfs_dir,
        run_command,
        neurodesktop_cvmfs_env,
    ],
    cpu_cores = 8,
    memory_mb = 8192,
    storage_size = 4096,
)
