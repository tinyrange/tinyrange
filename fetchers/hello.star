version = "3.13.0"
local_version_minor_etc = version.split(".")[1:]
local_version_minor_etc += ["0" * (3 - len(local_version_minor_etc))]
local_version_str = "%(version_major)s" + "".join(["{}".format(int(x)) for x in local_version_minor_etc])
print(local_version_str)
