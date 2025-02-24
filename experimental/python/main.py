import os
import json
from subprocess import Popen, PIPE


def get_sha256_hash(data: str):
    import hashlib

    return hashlib.sha256(data.encode("utf-8")).hexdigest()


def look_path(cmd: str):
    for path in os.environ["PATH"].split(":"):
        if os.path.exists(os.path.join(path, cmd)):
            return os.path.join(path, cmd)
    return None


class BuildDatabase(object):
    def __init__(self, dir: str):
        self.dir = dir


class BuildDefinition(object):
    def __init__(self, typename: str):
        self.typename = typename
        self.child_definitions = []

    def pointer(self):
        return {
            "TypeName": self.typename,
            "Hash": self.hash(),
        }

    def hash(self):
        # return the sha256 hash of the serialized object
        enc = json.JSONEncoder(separators=(",", ":"))
        serialized = enc.encode(self.serialize())
        hsh = get_sha256_hash(serialized)
        return hsh

    def serialize(self):
        return {"TypeName": self.typename, "Params": self.params()}

    def params(self) -> dict:
        raise NotImplementedError("params() must be implemented by subclass")

    def add_child(self, child):
        self.child_definitions.append(child)


class PlanDefinition(BuildDefinition):
    def __init__(self, builder: str):
        super().__init__("PlanDefinition")
        self.builder = builder
        self.search = []

    def params(self):
        return {
            "Architecture": "",
            "Builder": self.builder,
            "Search": self.search,
            "TagList": ["level3", "defaults"],
        }

    def add_search(self, search: str):
        self.search.append(
            {
                "TypeName": "PackageQuery",
                "Values": {
                    "MatchDirect": False,
                    "MatchPartialName": False,
                    "Name": search,
                    "Tags": None,
                    "Version": "",
                },
            }
        )


class BuildVMDefinition(BuildDefinition):
    def __init__(self, cpu_cores=1, memory_mb=1024, storage_size=1024):
        super().__init__("BuildVmDefinition")
        self.cpu_cores = cpu_cores
        self.memory_mb = memory_mb
        self.storage_size = storage_size
        self.directives = []

    def add_command_directive(self, command, raw=False):
        self.directives.append(
            {
                "TypeName": "DirectiveRunCommand",
                "Values": {"Command": command, "Raw": raw},
            }
        )

    def add_plan_directive(self, plan):
        self.directives.append(plan.pointer())
        self.add_child(plan)

    def params(self):
        return {
            "Architecture": "",
            "CpuCores": self.cpu_cores,
            "Debug": False,
            "Directives": self.directives,
            "InitRamFs": None,
            "Interaction": "ssh",
            "Kernel": None,
            "MemoryMB": self.memory_mb,
            "OutputFile": "",
            "RootArchitecture": "",
            "StorageSize": self.storage_size,
        }


class BuildRequest(object):
    def __init__(self):
        self.definitions = set()
        self.insertion_order = []

    def add_def(self, d: BuildDefinition):
        if d in self.definitions:
            return
        for child in d.child_definitions:
            self.add_def(child)
        self.definitions.add(d)
        self.insertion_order.append(d)

    def serialize(self):
        return json.dumps(
            {
                "Definitions": [d.serialize() for d in self.insertion_order],
            }
        )


class TinyRangeExecutor(object):
    def __init__(self, cmd):
        self.cmd = cmd

    def import_def(self, d: BuildDefinition):
        req = BuildRequest()
        req.add_def(d)
        p = Popen([self.cmd, "import"], stdout=PIPE, stdin=PIPE, text=True)
        p.communicate(input=req.serialize())
        if p.returncode != 0:
            raise Exception("Failed to import definitions")

    def build_def(self, d: BuildDefinition):
        self.import_def(d)
        top_hash = d.hash()
        # call the top level passing stdin, stdout, and stderr
        p = Popen(
            [self.cmd, "build", top_hash],
        )
        p.wait()


def main(args):
    tr = TinyRangeExecutor("../../build/tinyrange")

    plan = PlanDefinition("alpine@3.21")
    plan.add_search("bash")
    plan.add_search("build-base")

    vm = BuildVMDefinition()
    vm.add_plan_directive(plan)
    vm.add_command_directive("bash")

    tr.build_def(vm)


if __name__ == "__main__":
    import sys

    main(sys.argv[1:])
