#!/usr/bin/env python3

import os
import hashlib
import urllib.request
from email.message import EmailMessage
from wheel.wheelfile import WheelFile, get_zipinfo_datetime
from zipfile import ZipInfo, ZIP_DEFLATED
import libarchive  # from libarchive-c

# From: https://github.com/ziglang/zig-pypi (MIT License)


class ReproducibleWheelFile(WheelFile):
    def writestr(self, zinfo_or_arcname, data, *args, **kwargs):
        if isinstance(zinfo_or_arcname, ZipInfo):
            zinfo = zinfo_or_arcname
        else:
            assert isinstance(zinfo_or_arcname, str)
            zinfo = ZipInfo(zinfo_or_arcname)
            zinfo.file_size = len(data)
            zinfo.external_attr = 0o0644 << 16
            if zinfo_or_arcname.endswith(".dist-info/RECORD"):
                zinfo.external_attr = 0o0664 << 16

        zinfo.compress_type = ZIP_DEFLATED
        zinfo.date_time = (1980, 1, 1, 0, 0, 0)
        zinfo.create_system = 3
        super().writestr(zinfo, data, *args, **kwargs)


def make_message(headers, payload=None):
    msg = EmailMessage()
    for name, value in headers.items():
        if isinstance(value, list):
            for value_part in value:
                msg[name] = value_part
        else:
            msg[name] = value
    if payload:
        msg.set_payload(payload)
    return msg


def write_wheel_file(filename, contents):
    with ReproducibleWheelFile(filename, "w") as wheel:
        for member_info, member_source in contents.items():
            wheel.writestr(member_info, bytes(member_source))
    return filename


def write_wheel(out_dir, *, name, version, tag, metadata, description, contents):
    wheel_name = f"{name}-{version}-{tag}.whl"
    dist_info = f"{name}-{version}.dist-info"
    return write_wheel_file(
        os.path.join(out_dir, wheel_name),
        {
            **contents,
            f"{dist_info}/METADATA": make_message(
                {
                    "Metadata-Version": "2.4",
                    "Name": name,
                    "Version": version,
                    **metadata,
                },
                description,
            ),
            f"{dist_info}/WHEEL": make_message(
                {
                    "Wheel-Version": "1.0",
                    "Generator": "tinyrange make_wheels.py",
                    "Root-Is-Purelib": "false",
                    "Tag": tag,
                }
            ),
            # Add a entry_points.txt file to the wheel
            f"{dist_info}/entry_points.txt": """[console_scripts]
tinyrange = tinyrange_sys.__main__:main""".encode(
                "ascii"
            ),
        },
    )


def write_tinyrange_wheel(out_dir, *, version, platform, archive):
    contents = {}
    wrote_sys = False

    with libarchive.memory_reader(archive) as archive:
        for entry in archive:
            entry_name = "/".join(entry.name.split("/")[1:])
            if entry.isdir or not entry_name:
                continue

            if entry_name.endswith("tinyrange.portable"):
                continue  # disable the portable version

            zip_info = ZipInfo(f"tinyrange_sys/{entry_name}")
            zip_info.external_attr = (entry.mode & 0xFFFF) << 16
            contents[zip_info] = b"".join(entry.get_blocks())

            if entry_name.endswith("tinyrange") or entry_name.endswith("tinyrange.exe"):
                contents["tinyrange_sys/__main__.py"] = (
                    f"""\
import os, sys, subprocess
sys.exit(subprocess.call([
    os.path.join(os.path.dirname(__file__), "{entry_name}"),
    *sys.argv[1:]
]))
""".encode(
                        "ascii"
                    )
                )

                contents["tinyrange_sys/__init__.py"] = (
                    f"""
import os

TINYRANGE_PATH = os.path.join(os.path.dirname(__file__), "{entry_name}")
"""
                ).encode("ascii")

                wrote_sys = True

    if not wrote_sys:
        raise ValueError("No tinyrange executable found in archive")

    with open("README.pypi.md") as f:
        description = f.read()

    return write_wheel(
        out_dir,
        name="tinyrange_sys",
        version=version,
        tag=f"py3-none-{platform}",
        metadata={
            "Summary": "TinyRange: Next-generation Virtualization for Cyber and beyond.",
            "Description-Content-Type": "text/markdown",
            "License": "Apache 2.0",
            "Classifier": [
                "License :: OSI Approved :: Apache Software License",
            ],
            "Project-URL": [
                "Homepage, https://tinyrange.dev",
                "Source Code, https://github.com/tinyrange/tinyrange",
                "Bug Tracker, https://github.com/tinyrange/tinyrange/issues",
            ],
            "Requires-Python": "~=3.5",
        },
        description=description,
        contents=contents,
    )


tinyrange_version = "0.2.7"
epoch = "0"

for tinyrange_platform, python_platform in {
    "windows-amd64": "win_amd64",
    "darwin-arm64": "macosx_12_0_arm64",
    "linux-amd64": "manylinux_2_12_x86_64.manylinux2010_x86_64.musllinux_1_1_x86_64",
    "linux-arm64": "manylinux_2_17_aarch64.manylinux2014_aarch64.musllinux_1_1_aarch64",
}.items():
    # https://github.com/tinyrange/tinyrange/releases/download/v0.2.6/tinyrange-darwin-arm64.zip

    tinyrange_url = f"https://github.com/tinyrange/tinyrange/releases/download/v{tinyrange_version}/tinyrange-{tinyrange_platform}.zip"
    with urllib.request.urlopen(tinyrange_url) as request:
        tinyrange_archive = request.read()
        print(f"{hashlib.sha256(tinyrange_archive).hexdigest()} {tinyrange_url}")

    wheel_path = write_tinyrange_wheel(
        "dist/",
        version=tinyrange_version + ".post" + epoch,
        platform=python_platform,
        archive=tinyrange_archive,
    )
    with open(wheel_path, "rb") as wheel:
        print(f"  {hashlib.sha256(wheel.read()).hexdigest()} {wheel_path}")
