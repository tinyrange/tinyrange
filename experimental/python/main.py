import tinyrange


def main(args):
    tr = tinyrange.TinyRange("../../build/tinyrange")

    plan = tinyrange.PlanDefinition("alpine@3.21")
    plan.add_search("py3-matplotlib")

    vm = tinyrange.BuildVMDefinition()
    vm.add_plan_directive(plan)
    vm.add_file(
        "test.py",
        'import matplotlib.pyplot as plt;plt.plot([1,2,3,4,5]);plt.savefig("test.png")',
    )
    vm.add_command("python test.py")
    vm.set_output_file("test.png")

    artifact = tr.build_def(vm)

    with open("out.png", "wb") as f:
        f.write(artifact.open_default("rb").read())


if __name__ == "__main__":
    import sys

    main(sys.argv[1:])
