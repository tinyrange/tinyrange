GO_ARCH_MAP = {
    "x86_64": "amd64",
    "aarch64": "arm64",
}

def extract_go_impl(ctx, arch, version):
    ark = ctx.build(define.read_archive(
        define.fetch_http("https://go.dev/dl/go{}.linux-amd64.tar.gz".format(version)),
        ".tar.gz",
    ))

    ark = filesystem(ark)

    ret = filesystem()

    ret["/usr/local/go"] = ark["/go"]

    return ctx.archive(ret)

def extract_go(arch, version):
    return define.build(extract_go_impl, arch, version)

def go(arch, version):
    "#macro variable,arch string"
    return directive.list([
        extract_go(arch, version),
        directive.environment({
            "GOROOT": "/usr/local/go",
            "PATH": "/usr/local/go/bin:$PATH",
        }),
    ])