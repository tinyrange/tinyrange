db.add_mirror("neurodesk", ["http://cvmfs.neurodesk.org/cvmfs"])

def make_test_vm(name, version):
    cvmfs_repo = define.fetch_cvmfs(
        mirror = "mirror://neurodesk",
        repo = "neurodesk.ardc.edu.au",
        path = "/containers/{}_{}/{}_{}.simg".format(name, version, name, version),
    )

    return define.build_vm(
        directives = [
            directive.archive(cvmfs_repo, archive2=True),
            directive.run_command("/.singularity.d/actions/exec /bin/bash"),
        ],
    )

niimath_vm = make_test_vm("niimath", "1.0.0_20240902")
fsl_vm = make_test_vm("fsl", "6.0.7.16_20250131")

neurodesktop = define.build_vm(
    directives = [
        # define.fetch_oci_image(
        #     image = "vnmd/neurodesktop",
        #     tag = "2025-03-12",
        #     arch = "amd64",
        # ),
        directive.archive(define.fetch_cvmfs(
            mirror = "mirror://neurodesk",
            repo = "neurodesk.ardc.edu.au",
            path = "/neurodesktop/2025-03-12",
        ), archive2=True),
        directive.run_command("/init -shell"),
        # directive.run_command("chmod 777 /dev/fuse;NEURODESKTOP_VERSION=2025-03-12 start.sh jupyter lab --ServerApp.password='' --no-browser --expose-app-in-browser --ServerApp.token='jlab:srvr:5dd0c63f33be5d0fdc881461d7d897ca95cc80' --ServerApp.port=57164 --LabApp.quit_button=False "),
        directive.environment({
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
        }),
    ],
    cpu_cores = 8,
    memory_mb = 8192,
    storage_size = 4096
)