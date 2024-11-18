# Starlark functions to handle building the Linux kernel.

db.add_mirror("kernel", ["https://cdn.kernel.org/pub/linux/kernel/"])

kernel_builder = define.plan(
    builder = "alpine@3.20",
    packages = [
        query("build-base"),
        query("flex"),
        query("bison"),
        query("linux-headers"),
        query("xz"),
        query("bash"),
        query("openssl-dev"),
        query("perl"),
        query("findutils"),
    ],
    tags = ["level3", "defaults"],
)

def make_tinyconfig_impl(ctx, source):
    return define.build_vm(
        directives = [
            kernel_builder,
            source,
            directive.run_command("cd /linux-*; make tinyconfig; mv .config /"),
        ],
        output = "/.config",
    )

def make_tinyconfig(source):
    return define.build(make_tinyconfig_impl, source)

def make_defconfig_impl(ctx, source):
    return define.build_vm(
        directives = [
            kernel_builder,
            source,
            directive.run_command("cd /linux-*; make defconfig; mv .config /"),
        ],
        output = "/.config",
    )

def make_defconfig(source):
    return define.build(make_defconfig_impl, source)

def modify_config_impl(ctx, config, changes):
    lines = config.read().split("\n")

    out = []

    needs_edit = {k: True for k in changes}

    for line in lines:
        key = None
        value = None

        if line.startswith("#") or not line:
            if line.endswith("is not set"):
                key = line.removeprefix("# ").removesuffix(" is not set")
                value = "n"
            else:
                out.append(line)
                continue
        else:
            key, value = line.split("=", 1)

        if key in changes:
            value = "n"
            needs_edit[key] = False

        if value == "n":
            out.append("# {} is not set".format(key))
        else:
            out.append("{}={}".format(key, value))

    output = filesystem()

    output[".oldconfig"] = file("\n".join(out))

    return ctx.archive(output)

def modify_config(config, changes):
    return define.build(modify_config_impl, config, changes)

def make_oldconfig_impl(ctx, source, config, changes):
    edited_config = ctx.build(modify_config(config, changes))

    return define.build_vm(
        directives = [
            kernel_builder,
            source,
            edited_config,
            directive.run_command("cd /linux-*; cp /.oldconfig .config; make olddefconfig; diff /.oldconfig .config; mv .config /"),
        ],
        output = "/.config",
    )

def make_oldconfig(source, changes):
    return define.build(make_oldconfig_impl, source, make_defconfig(source), changes)

def validate_config_impl(ctx, config, changes):
    lines = config.read().split("\n")

    found_changes = {k: False for k in changes}

    for line in lines:
        key = None
        value = None

        if line.startswith("#") or not line:
            if line.endswith("is not set"):
                key = line.removeprefix("# ").removesuffix(" is not set")
                value = "n"
            else:
                continue
        else:
            key, value = line.split("=", 1)

        if key in changes:
            if value != "n":
                return error("Configuration option {} is enabled".format(key))

            found_changes[key] = True

    return config

def validate_config(config, changes):
    return define.build(validate_config_impl, config, changes)

def build_kernel_impl(ctx, source, config):
    return define.build_vm(
        directives = [
            kernel_builder,
            source,
            directive.add_file("/.config", config),
            directive.run_command("cd /linux-*; mv /.config .; make -j$(nproc) Image"),
            directive.run_command("cp /linux-*/arch/arm64/boot/Image /Image"),
        ],
        output = "/Image",
        cpu_cores = 4,
        memory_mb = 8192,
        storage_size = 4096,
    )

def build_kernel(source, config):
    return define.build(build_kernel_impl, source, config)


kernel_stable = define.read_archive(define.fetch_http("mirror://kernel/v6.x/linux-6.11.9.tar.xz"), ".tar.xz")

kernel_stable_vm = define.build_vm(
    directives = [
        kernel_builder,
        kernel_stable,
        directive.run_command("interactive"),
    ],
)

kernel_stable_tinyconfig = make_tinyconfig(kernel_stable)

base_changes = [
    "CONFIG_USB_SUPPORT",
    "CONFIG_USB",
    "CONFIG_LEDS_CLASS",
    "CONFIG_ACPI",
    "CONFIG_FPGA",
    "CONFIG_SERIAL_STM32",
    "CONFIG_MTD",
    "CONFIG_NET_VENDOR_INTEL",
    "CONFIG_MMC_SDHCI",
    "CONFIG_ARM_PMU",
    "CONFIG_SKY2",
    "CONFIG_XEN",
    "CONFIG_SOUND",
    "CONFIG_FB",
    "CONFIG_PWM",
    # Disable all device-specific configurations.
    "CONFIG_ARCH_ACTIONS",
    "CONFIG_ARCH_AIROHA",
    "CONFIG_ARCH_SUNXI",
    "CONFIG_ARCH_ALPINE",
    "CONFIG_ARCH_APPLE",
    "CONFIG_ARCH_BCM",
    "CONFIG_ARCH_BCM2835",
    "CONFIG_ARCH_BCM_IPROC",
    "CONFIG_ARCH_BCMBCA",
    "CONFIG_ARCH_BRCMSTB",
    "CONFIG_ARCH_BERLIN",
    "CONFIG_ARCH_EXYNOS",
    "CONFIG_ARCH_SPARX5",
    "CONFIG_ARCH_K3",
    "CONFIG_ARCH_LG1K",
    "CONFIG_ARCH_HISI",
    "CONFIG_ARCH_KEEMBAY",
    "CONFIG_ARCH_MEDIATEK",
    "CONFIG_ARCH_MESON",
    "CONFIG_ARCH_MVEBU",
    "CONFIG_ARCH_NXP",
    "CONFIG_ARCH_LAYERSCAPE",
    "CONFIG_ARCH_MXC",
    "CONFIG_ARCH_S32",
    "CONFIG_ARCH_MA35",
    "CONFIG_ARCH_NPCM",
    "CONFIG_ARCH_QCOM",
    "CONFIG_ARCH_REALTEK",
    "CONFIG_ARCH_RENESAS",
    "CONFIG_ARCH_ROCKCHIP",
    "CONFIG_ARCH_SEATTLE",
    "CONFIG_ARCH_INTEL_SOCFPGA",
    "CONFIG_ARCH_STM32",
    "CONFIG_ARCH_SYNQUACER",
    "CONFIG_ARCH_TEGRA",
    "CONFIG_ARCH_TESLA_FSD",
    "CONFIG_ARCH_SPRD",
    "CONFIG_ARCH_THUNDER",
    "CONFIG_ARCH_THUNDER2",
    "CONFIG_ARCH_UNIPHIER",
    "CONFIG_ARCH_VEXPRESS",
    "CONFIG_ARCH_VISCONTI",
    "CONFIG_ARCH_XGENE",
    "CONFIG_ARCH_ZYNQMP",
]

kernel_stable_config = make_oldconfig(kernel_stable, base_changes)

kernel_stable_test_config = validate_config(kernel_stable_config, base_changes)

kernel_stable_config_vm = define.build_vm(
    directives = [
        kernel_builder,
        kernel_stable,
        directive.add_file("/.config", kernel_stable_config),
        directive.run_command("interactive"),
    ],
)

kernel_stable_build = build_kernel(kernel_stable, kernel_stable_config)

kernel_stable_boot = define.build_vm(
    directives = [
        kernel_builder,
        directive.run_command("interactive"),
    ],
    kernel = kernel_stable_build,
)

def kernel(arch):
    "#macro variable,arch"
    return directive.kernel(kernel_stable_build)