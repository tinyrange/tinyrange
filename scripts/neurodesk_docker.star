def main(args):
    f = args["neurodesktop.tar"]
    neurodesktop = define.build_vm(
        directives = [
            directive.archive(define.read_archive(f, ".tar")),
            directive.run_command("chmod 777 /dev/fuse;NEURODESKTOP_VERSION=2025-03-12 start.sh jupyter lab --ServerApp.password='' --no-browser --expose-app-in-browser --ServerApp.token='jlab:srvr:5dd0c63f33be5d0fdc881461d7d897ca95cc80' --ServerApp.port=57164 --LabApp.quit_button=False "),
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
        storage_size = 4096,
    )
    db.build(neurodesktop, always_rebuild=True)

    pass
