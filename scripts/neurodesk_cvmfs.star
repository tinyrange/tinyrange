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