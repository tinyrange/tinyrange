import os
import json
from subprocess import Popen, PIPE


def get_sha256_hash(data: str):
    import hashlib

    return hashlib.sha256(data.encode("utf-8")).hexdigest()


def to_byte_array(data: str | bytes) -> list[int]:
    if isinstance(data, str):
        return [ord(c) for c in data]
    else:
        return list(data)


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


class FetchOCIImageDefinition(BuildDefinition):
    def __init__(self, registry="", image="", tag="", architecture=""):
        super().__init__("FetchOciImageDefinition")
        self.registry = registry
        self.image = image
        self.tag = tag
        self.architecture = architecture

    def params(self):
        return {
            "Architecture": self.architecture,
            "Image": self.image,
            "Registry": self.registry,
            "Tag": self.tag,
        }


class BuildVMDefinition(BuildDefinition):
    def __init__(self, cpu_cores=1, memory_mb=1024, storage_size=1024):
        super().__init__("BuildVmDefinition")
        self.cpu_cores = cpu_cores
        self.memory_mb = memory_mb
        self.storage_size = storage_size
        self.output_file = ""
        self.directives = []

    def add_command(self, command):
        self.directives.append(
            {
                "TypeName": "DirectiveRunCommand",
                "Values": {"Command": command, "Raw": False},
            }
        )

    def add_plan_directive(self, plan):
        self.directives.append(plan.pointer())
        self.add_child(plan)

    def add_fetch_oci_image(self, registry, image, tag, architecture=""):
        fetch = FetchOCIImageDefinition(registry, image, tag, architecture)
        self.directives.append(fetch.pointer())
        self.add_child(fetch)

    def add_local_file(self, guest_filename, host_filename):
        self.directives.append(
            {
                "TypeName": "DirectiveLocalFile",
                "Values": {
                    "Filename": guest_filename,
                    "HostFilename": os.path.abspath(host_filename),
                },
            }
        )

    def add_file(self, filename, contents: str | bytes, executable=False):
        self.directives.append(
            {
                "TypeName": "DirectiveAddFile",
                "Values": {
                    "Contents": to_byte_array(contents),
                    "Definition": None,
                    "Executable": executable,
                    "Filename": filename,
                },
            }
        )

    def set_output_file(self, output_file):
        self.output_file = output_file

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
            "OutputFile": self.output_file,
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


class BuildArtifact(object):
    def __init__(self, build_dir: str, hash: str):
        self.build_dir = build_dir
        self.hash = hash
        self.path = os.path.join(self.build_dir, self.hash[:2], self.hash[2:])

    def get_receipt(self):
        with open(os.path.join(self.path, "receipt.json"), "r") as f:
            return json.load(f)

    def open_default(self, mode):
        receipt = self.get_receipt()

        if "default" not in receipt["files"]:
            raise Exception("No default file in receipt")

        return open(os.path.join(self.path, "output.default"), mode)


class TinyRange(object):
    def __init__(self, cmd):
        self.cmd = cmd

    def import_def(self, d: BuildDefinition):
        req = BuildRequest()
        req.add_def(d)
        p = Popen([self.cmd, "import"], stdout=PIPE, stdin=PIPE, text=True)
        p.communicate(input=req.serialize())
        if p.returncode != 0:
            raise Exception("Failed to import definitions")

    def get_build_dir(self):
        p = Popen([self.cmd, "env", "build-dir"], stdout=PIPE, text=True)
        p.wait()
        if p.stdout == None:
            raise Exception("Failed to get build dir")
        return p.stdout.read().strip()

    def build_def(self, d: BuildDefinition, rebuild=False) -> BuildArtifact:
        self.import_def(d)
        top_hash = d.hash()

        args = [self.cmd, "build"]
        if not rebuild:
            args.append("--use-cache")
        args.append(top_hash)

        # call the top level passing stdin, stdout, and stderr
        p = Popen(args)
        p.wait()
        if p.returncode != 0:
            raise Exception("Failed to build definition")
        return BuildArtifact(self.get_build_dir(), top_hash)
