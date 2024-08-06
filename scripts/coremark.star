test = define.build_vm(
    directives = [
        define.plan(
            builder = "alpine@3.20",
            packages = [
                query("build-base"),
            ],
            tags = ["level3", "defaults"],
        ),
        define.read_archive(define.fetch_http("https://github.com/eembc/coremark/archive/refs/heads/main.tar.gz"), ".tar.gz"),
        directive.run_command("source /etc/profile;cd /coremark-main; make run1.log"),
    ],
    output = "/coremark-main/run1.log",
)
