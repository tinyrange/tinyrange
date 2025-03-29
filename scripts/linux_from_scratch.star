# LFS Instructions from https://www.linuxfromscratch.org/lfs/view/stable (12.3)
# Licensed under the MIT License

VERSION_CHECK = """
#!/bin/bash
# A script to list version numbers of critical development tools

# If you have tools installed in other directories, adjust PATH here AND
# in ~lfs/.bashrc (section 4.4) as well.

LC_ALL=C 
PATH=/usr/bin:/bin

bail() { echo "FATAL: $1"; exit 1; }
grep --version > /dev/null 2> /dev/null || bail "grep does not work"
sed '' /dev/null || bail "sed does not work"
sort   /dev/null || bail "sort does not work"

ver_check()
{
   if ! type -p $2 &>/dev/null
   then 
     echo "ERROR: Cannot find $2 ($1)"; return 1; 
   fi
   v=$($2 --version 2>&1 | grep -E -o '[0-9]+\\.[0-9\\.]+[a-z]*' | head -n1)
   if printf '%s\\n' $3 $v | sort --version-sort --check &>/dev/null
   then 
     printf "OK:    %-9s %-6s >= $3\\n" "$1" "$v"; return 0;
   else 
     printf "ERROR: %-9s is TOO OLD ($3 or later required)\\n" "$1"; 
     return 1; 
   fi
}

ver_kernel()
{
   kver=$(uname -r | grep -E -o '^[0-9\\.]+')
   if printf '%s\\n' $1 $kver | sort --version-sort --check &>/dev/null
   then 
     printf "OK:    Linux Kernel $kver >= $1\\n"; return 0;
   else 
     printf "ERROR: Linux Kernel ($kver) is TOO OLD ($1 or later required)\\n" "$kver"; 
     return 1; 
   fi
}

# Coreutils first because --version-sort needs Coreutils >= 7.0
ver_check Coreutils      sort     8.1 || bail "Coreutils too old, stop"
ver_check Bash           bash     3.2
ver_check Binutils       ld       2.13.1
ver_check Bison          bison    2.7
ver_check Diffutils      diff     2.8.1
ver_check Findutils      find     4.2.31
ver_check Gawk           gawk     4.0.1
ver_check GCC            gcc      5.2
ver_check "GCC (C++)"    g++      5.2
ver_check Grep           grep     2.5.1a
ver_check Gzip           gzip     1.3.12
ver_check M4             m4       1.4.10
ver_check Make           make     4.0
ver_check Patch          patch    2.5.4
ver_check Perl           perl     5.8.8
ver_check Python         python3  3.4
ver_check Sed            sed      4.1.5
ver_check Tar            tar      1.22
ver_check Texinfo        texi2any 5.0
ver_check Xz             xz       5.0.0
ver_kernel 5.4 

if mount | grep -q 'devpts on /dev/pts' && [ -e /dev/ptmx ]
then echo "OK:    Linux Kernel supports UNIX 98 PTY";
else echo "ERROR: Linux Kernel does NOT support UNIX 98 PTY"; fi

alias_check() {
   if $1 --version 2>&1 | grep -qi $2
   then printf "OK:    %-4s is $2\\n" "$1";
   else printf "ERROR: %-4s is NOT $2\\n" "$1"; fi
}
echo "Aliases:"
alias_check awk GNU
alias_check yacc Bison
alias_check sh Bash

echo "Compiler check:"
if printf "int main(){}" | g++ -x c++ -
then echo "OK:    g++ works";
else echo "ERROR: g++ does NOT work"; fi
rm -f a.out

if [ "$(nproc)" = "" ]; then
   echo "ERROR: nproc is not available or it produces empty output"
else
   echo "OK: nproc reports $(nproc) logical cores are available"
fi
"""

build_base = define.plan(
    builder = "alpine@3.21",
    packages = [
        query("bash"),
        query("grep"),
        query("coreutils"),
        query("build-base"),
        query("diffutils"),
        query("findutils"),
        query("bison"),
        query("gawk"),
        query("perl"),
        query("python3"),
        query("sed"),
        query("texinfo"),
        query("xz"),
        query("shadow"),
    ],
    tags = ["level3", "defaults"],
)

base_directives = [
    # directive.add_volume("lfs", "/mnt/lfs", 32 * 1024),
    directive.environment({"LFS": "/mnt/lfs"}),
    build_base,
    directive.run_command("rm /bin/sh;ln -s /bin/bash /bin/sh"),
]

version_check = define.build_vm(
    directives = base_directives + [
        directive.add_file("/root/version_check.sh", file(VERSION_CHECK, executable = True)),
        directive.run_command("bash /root/version_check.sh"),
    ],
)

# Section 3.2 Download packages

PACKAGE_VERSIONS = [
    {
        "Name": "Acl",
        "Version": "2.3.2",
        "DownloadSize": "363 KB",
        "Homepage": "https://savannah.nongnu.org/projects/acl",
        "Download": "https://download.savannah.gnu.org/releases/acl/acl-2.3.2.tar.xz",
        "MD5Sum": "590765dee95907dbc3c856f7255bd669",
    },
    {
        "Name": "Attr",
        "Version": "2.5.2",
        "DownloadSize": "484 KB",
        "Homepage": "https://savannah.nongnu.org/projects/attr",
        "Download": "https://download.savannah.gnu.org/releases/attr/attr-2.5.2.tar.gz",
        "MD5Sum": "227043ec2f6ca03c0948df5517f9c927",
    },
    {
        "Name": "Autoconf",
        "Version": "2.72",
        "DownloadSize": "1,360 KB",
        "Homepage": "https://www.gnu.org/software/autoconf/",
        "Download": "https://ftp.gnu.org/gnu/autoconf/autoconf-2.72.tar.xz",
        "MD5Sum": "1be79f7106ab6767f18391c5e22be701",
    },
    {
        "Name": "Automake",
        "Version": "1.17",
        "DownloadSize": "1,614 KB",
        "Homepage": "https://www.gnu.org/software/automake/",
        "Download": "https://ftp.gnu.org/gnu/automake/automake-1.17.tar.xz",
        "MD5Sum": "7ab3a02318fee6f5bd42adfc369abf10",
    },
    {
        "Name": "Bash",
        "Version": "5.2.37",
        "DownloadSize": "10,868 KB",
        "Homepage": "https://www.gnu.org/software/bash/",
        "Download": "https://ftp.gnu.org/gnu/bash/bash-5.2.37.tar.gz",
        "MD5Sum": "9c28f21ff65de72ca329c1779684a972",
    },
    {
        "Name": "Bc",
        "Version": "7.0.3",
        "DownloadSize": "464 KB",
        "Homepage": "https://git.gavinhoward.com/gavin/bc",
        "Download": "https://github.com/gavinhoward/bc/releases/download/7.0.3/bc-7.0.3.tar.xz",
        "MD5Sum": "ad4db5a0eb4fdbb3f6813be4b6b3da74",
    },
    {
        "Name": "Binutils",
        "Version": "2.44",
        "DownloadSize": "26,647 KB",
        "Homepage": "https://www.gnu.org/software/binutils/",
        "Download": "https://sourceware.org/pub/binutils/releases/binutils-2.44.tar.xz",
        "MD5Sum": "49912ce774666a30806141f106124294",
    },
    {
        "Name": "Bison",
        "Version": "3.8.2",
        "DownloadSize": "2,752 KB",
        "Homepage": "https://www.gnu.org/software/bison/",
        "Download": "https://ftp.gnu.org/gnu/bison/bison-3.8.2.tar.xz",
        "MD5Sum": "c28f119f405a2304ff0a7ccdcc629713",
    },
    {
        "Name": "Bzip2",
        "Version": "1.0.8",
        "DownloadSize": "792 KB",
        "Download": "https://www.sourceware.org/pub/bzip2/bzip2-1.0.8.tar.gz",
        "MD5Sum": "67e051268d0c475ea773822f7500d0e5",
    },
    {
        "Name": "Check",
        "Version": "0.15.2",
        "DownloadSize": "760 KB",
        "Homepage": "https://libcheck.github.io/check",
        "Download": "https://github.com/libcheck/check/releases/download/0.15.2/check-0.15.2.tar.gz",
        "MD5Sum": "50fcafcecde5a380415b12e9c574e0b2",
    },
    {
        "Name": "Coreutils",
        "Version": "9.6",
        "DownloadSize": "5,991 KB",
        "Homepage": "https://www.gnu.org/software/coreutils/",
        "Download": "https://ftp.gnu.org/gnu/coreutils/coreutils-9.6.tar.xz",
        "MD5Sum": "0ed6cc983fe02973bc98803155cc1733",
    },
    {
        "Name": "DejaGNU",
        "Version": "1.6.3",
        "DownloadSize": "608 KB",
        "Homepage": "https://www.gnu.org/software/dejagnu/",
        "Download": "https://ftp.gnu.org/gnu/dejagnu/dejagnu-1.6.3.tar.gz",
        "MD5Sum": "68c5208c58236eba447d7d6d1326b821",
    },
    {
        "Name": "Diffutils",
        "Version": "3.11",
        "DownloadSize": "1,881 KB",
        "Homepage": "https://www.gnu.org/software/diffutils/",
        "Download": "https://ftp.gnu.org/gnu/diffutils/diffutils-3.11.tar.xz",
        "MD5Sum": "75ab2bb7b5ac0e3e10cece85bd1780c2",
    },
    {
        "Name": "E2fsprogs",
        "Version": "1.47.2",
        "DownloadSize": "9,763 KB",
        "Homepage": "https://e2fsprogs.sourceforge.net/",
        "Download": "https://downloads.sourceforge.net/project/e2fsprogs/e2fsprogs/v1.47.2/e2fsprogs-1.47.2.tar.gz",
        "MD5Sum": "752e5a3ce19aea060d8a203f2fae9baa",
    },
    {
        "Name": "Elfutils",
        "Version": "0.192",
        "DownloadSize": "11,635 KB",
        "Homepage": "https://sourceware.org/elfutils/",
        "Download": "https://sourceware.org/ftp/elfutils/0.192/elfutils-0.192.tar.bz2",
        "MD5Sum": "a6bb1efc147302cfc15b5c2b827f186a",
    },
    {
        "Name": "Expat",
        "Version": "2.6.4",
        "DownloadSize": "476 KB",
        "Homepage": "https://libexpat.github.io/",
        "Download": "https://prdownloads.sourceforge.net/expat/expat-2.6.4.tar.xz",
        "MD5Sum": "101fe3e320a2800f36af8cf4045b45c7",
    },
    {
        "Name": "Expect",
        "Version": "5.45.4",
        "DownloadSize": "618 KB",
        "Homepage": "https://core.tcl.tk/expect/",
        "Download": "https://prdownloads.sourceforge.net/expect/expect5.45.4.tar.gz",
        "MD5Sum": "00fce8de158422f5ccd2666512329bd2",
    },
    {
        "Name": "File",
        "Version": "5.46",
        "DownloadSize": "1,283 KB",
        "Homepage": "https://www.darwinsys.com/file/",
        "Download": "https://astron.com/pub/file/file-5.46.tar.gz",
        "MD5Sum": "459da2d4b534801e2e2861611d823864",
    },
    {
        "Name": "Findutils",
        "Version": "4.10.0",
        "DownloadSize": "2,189 KB",
        "Homepage": "https://www.gnu.org/software/findutils/",
        "Download": "https://ftp.gnu.org/gnu/findutils/findutils-4.10.0.tar.xz",
        "MD5Sum": "870cfd71c07d37ebe56f9f4aaf4ad872",
    },
    {
        "Name": "Flex",
        "Version": "2.6.4",
        "DownloadSize": "1,386 KB",
        "Homepage": "https://github.com/westes/flex",
        "Download": "https://github.com/westes/flex/releases/download/v2.6.4/flex-2.6.4.tar.gz",
        "MD5Sum": "2882e3179748cc9f9c23ec593d6adc8d",
    },
    {
        "Name": "Flit-core",
        "Version": "3.11.0",
        "DownloadSize": "51 KB",
        "Homepage": "https://pypi.org/project/flit-core/",
        "Download": "https://pypi.org/packages/source/f/flit-core/flit_core-3.11.0.tar.gz",
        "MD5Sum": "6d677b1acef1769c4c7156c7508e0dbd",
    },
    {
        "Name": "Gawk",
        "Version": "5.3.1",
        "DownloadSize": "3,428 KB",
        "Homepage": "https://www.gnu.org/software/gawk/",
        "Download": "https://ftp.gnu.org/gnu/gawk/gawk-5.3.1.tar.xz",
        "MD5Sum": "4e9292a06b43694500e0620851762eec",
    },
    {
        "Name": "GCC",
        "Version": "14.2.0",
        "DownloadSize": "90,144 KB",
        "Homepage": "https://gcc.gnu.org/",
        "Download": "https://ftp.gnu.org/gnu/gcc/gcc-14.2.0/gcc-14.2.0.tar.xz",
        "MD5Sum": "2268420ba02dc01821960e274711bde0",
    },
    {
        "Name": "GDBM",
        "Version": "1.24",
        "DownloadSize": "1,168 KB",
        "Homepage": "https://www.gnu.org/software/gdbm/",
        "Download": "https://ftp.gnu.org/gnu/gdbm/gdbm-1.24.tar.gz",
        "MD5Sum": "c780815649e52317be48331c1773e987",
    },
    {
        "Name": "Gettext",
        "Version": "0.24",
        "DownloadSize": "8,120 KB",
        "Homepage": "https://www.gnu.org/software/gettext/",
        "Download": "https://ftp.gnu.org/gnu/gettext/gettext-0.24.tar.xz",
        "MD5Sum": "87aea3013802a3c60fa3feb5c7164069",
    },
    {
        "Name": "Glibc",
        "Version": "2.41",
        "DownloadSize": "18,892 KB",
        "Homepage": "https://www.gnu.org/software/libc/",
        "Download": "https://ftp.gnu.org/gnu/glibc/glibc-2.41.tar.xz",
        "MD5Sum": "19862601af60f73ac69e067d3e9267d4",
    },
    {
        "Name": "GMP",
        "Version": "6.3.0",
        "DownloadSize": "2,046 KB",
        "Homepage": "https://www.gnu.org/software/gmp/",
        "Download": "https://ftp.gnu.org/gnu/gmp/gmp-6.3.0.tar.xz",
        "MD5Sum": "956dc04e864001a9c22429f761f2c283",
    },
    {
        "Name": "Gperf",
        "Version": "3.1",
        "DownloadSize": "1,188 KB",
        "Homepage": "https://www.gnu.org/software/gperf/",
        "Download": "https://ftp.gnu.org/gnu/gperf/gperf-3.1.tar.gz",
        "MD5Sum": "9e251c0a618ad0824b51117d5d9db87e",
    },
    {
        "Name": "Grep",
        "Version": "3.11",
        "DownloadSize": "1,664 KB",
        "Homepage": "https://www.gnu.org/software/grep/",
        "Download": "https://ftp.gnu.org/gnu/grep/grep-3.11.tar.xz",
        "MD5Sum": "7c9bbd74492131245f7cdb291fa142c0",
    },
    {
        "Name": "Groff",
        "Version": "1.23.0",
        "DownloadSize": "7,259 KB",
        "Homepage": "https://www.gnu.org/software/groff/",
        "Download": "https://ftp.gnu.org/gnu/groff/groff-1.23.0.tar.gz",
        "MD5Sum": "5e4f40315a22bb8a158748e7d5094c7d",
    },
    {
        "Name": "GRUB",
        "Version": "2.12",
        "DownloadSize": "6,524 KB",
        "Homepage": "https://www.gnu.org/software/grub/",
        "Download": "https://ftp.gnu.org/gnu/grub/grub-2.12.tar.xz",
        "MD5Sum": "60c564b1bdc39d8e43b3aab4bc0fb140",
    },
    {
        "Name": "Gzip",
        "Version": "1.13",
        "DownloadSize": "819 KB",
        "Homepage": "https://www.gnu.org/software/gzip/",
        "Download": "https://ftp.gnu.org/gnu/gzip/gzip-1.13.tar.xz",
        "MD5Sum": "d5c9fc9441288817a4a0be2da0249e29",
    },
    {
        "Name": "Iana-Etc",
        "Version": "20250123",
        "DownloadSize": "591 KB",
        "Homepage": "https://www.iana.org/protocols",
        "Download": "https://github.com/Mic92/iana-etc/releases/download/20250123/iana-etc-20250123.tar.gz",
        "MD5Sum": "f8a0ebdc19a5004cf42d8bdcf614fa5d",
    },
    {
        "Name": "Inetutils",
        "Version": "2.6",
        "DownloadSize": "1,724 KB",
        "Homepage": "https://www.gnu.org/software/inetutils/",
        "Download": "https://ftp.gnu.org/gnu/inetutils/inetutils-2.6.tar.xz",
        "MD5Sum": "401d7d07682a193960bcdecafd03de94",
    },
    {
        "Name": "Intltool",
        "Version": "0.51.0",
        "DownloadSize": "159 KB",
        "Homepage": "https://freedesktop.org/wiki/Software/intltool",
        "Download": "https://launchpad.net/intltool/trunk/0.51.0/+download/intltool-0.51.0.tar.gz",
        "MD5Sum": "12e517cac2b57a0121cda351570f1e63",
    },
    {
        "Name": "IPRoute2",
        "Version": "6.13.0",
        "DownloadSize": "906 KB",
        "Homepage": "https://www.kernel.org/pub/linux/utils/net/iproute2/",
        "Download": "https://www.kernel.org/pub/linux/utils/net/iproute2/iproute2-6.13.0.tar.xz",
        "MD5Sum": "1603d25120d03feeaba9b360d03ffaec",
    },
    {
        "Name": "Jinja2",
        "Verison": "3.1.5",
        "DownloadSize": "239 KB",
        "Homepage": "https://jinja.palletsprojects.com/en/3.1.x/",
        "Download": "https://pypi.org/packages/source/J/Jinja2/jinja2-3.1.5.tar.gz",
        "MD5Sum": "083d64f070f6f1b5f75971ae60240785",
    },
    {
        "Name": "Kbd",
        "Version": "2.7.1",
        "DownloadSize": "1,438 KB",
        "Homepage": "https://kbd-project.org/",
        "Download": "https://www.kernel.org/pub/linux/utils/kbd/kbd-2.7.1.tar.xz",
        "MD5Sum": "f15673d9f748e58f82fa50cff0d0fd20",
    },
    {
        "Name": "Kmod",
        "Version": "34",
        "DownloadSize": "331 KB",
        "Homepage": "https://github.com/kmod-project/kmod",
        "Download": "https://www.kernel.org/pub/linux/utils/kernel/kmod/kmod-34.tar.xz",
        "MD5Sum": "3e6c5c9ad9c7367ab9c3cc4f08dfde62",
    },
    {
        "Name": "Less",
        "Version": "668",
        "DownloadSize": "635 KB",
        "Homepage": "https://www.greenwoodsoftware.com/less/",
        "Download": "https://www.greenwoodsoftware.com/less/less-668.tar.gz",
        "MD5Sum": "d72760386c5f80702890340d2f66c302",
    },
    {
        "Name": "LFS-Bootscripts",
        "Version": "20240825",
        "DownloadSize": "33 KB",
        "Download": "https://www.linuxfromscratch.org/lfs/downloads/12.3/lfs-bootscripts-20240825.tar.xz",
        "MD5Sum": "7b078c594a77e0f9cd53a0027471c3bc",
    },
    {
        "Name": "Libcap",
        "Version": "2.73",
        "DownloadSize": "191 KB",
        "Homepage": "https://sites.google.com/site/fullycapable/",
        "Download": "https://www.kernel.org/pub/linux/libs/security/linux-privs/libcap2/libcap-2.73.tar.xz",
        "MD5Sum": "0e186df9de9b1e925593a96684fe2e32",
    },
    {
        "Name": "Libffi",
        "Version": "3.4.7",
        "DownloadSize": "1,362 KB",
        "Homepage": "https://sourceware.org/libffi/",
        "Download": "https://github.com/libffi/libffi/releases/download/v3.4.7/libffi-3.4.7.tar.gz",
        "MD5Sum": "696a1d483a1174ce8a477575546a5284",
    },
    {
        "Name": "Libpipeline",
        "Version": "1.5.8",
        "DownloadSize": "1046 KB",
        "Homepage": "https://libpipeline.nongnu.org/",
        "Download": "https://download.savannah.gnu.org/releases/libpipeline/libpipeline-1.5.8.tar.gz",
        "MD5Sum": "17ac6969b2015386bcb5d278a08a40b5",
    },
    {
        "Name": "Libtool",
        "Version": "2.5.4",
        "DownloadSize": "1,033 KB",
        "Homepage": "https://www.gnu.org/software/libtool/",
        "Download": "https://ftp.gnu.org/gnu/libtool/libtool-2.5.4.tar.xz",
        "MD5Sum": "22e0a29df8af5fdde276ea3a7d351d30",
    },
    {
        "Name": "Libxcrypt",
        "Version": "4.4.38",
        "DownloadSize": "612 KB",
        "Homepage": "https://github.com/besser82/libxcrypt/",
        "Download": "https://github.com/besser82/libxcrypt/releases/download/v4.4.38/libxcrypt-4.4.38.tar.xz",
        "MD5Sum": "1796a5d20098e9dd9e3f576803c83000",
    },
    {
        "Name": "Linux",
        "Version": "6.13.4",
        "DownloadSize": "145,015 KB",
        "Homepage": "https://www.kernel.org/",
        "Download": "https://www.kernel.org/pub/linux/kernel/v6.x/linux-6.13.4.tar.xz",
        "MD5Sum": "13b9e6c29105a34db4647190a43d1810",
    },
    {
        "Name": "Lz4",
        "Version": "1.10.0",
        "DownloadSize": "379 KB",
        "Homepage": "https://lz4.org/",
        "Download": "https://github.com/lz4/lz4/releases/download/v1.10.0/lz4-1.10.0.tar.gz",
        "MD5Sum": "dead9f5f1966d9ae56e1e32761e4e675",
    },
    {
        "Name": "M4",
        "Version": "1.4.19",
        "DownloadSize": "1,617 KB",
        "Homepage": "https://www.gnu.org/software/m4/",
        "Download": "https://ftp.gnu.org/gnu/m4/m4-1.4.19.tar.xz",
        "MD5Sum": "0d90823e1426f1da2fd872df0311298d",
    },
    {
        "Name": "Make",
        "Version": "4.4.1",
        "DownloadSize": "2,300 KB",
        "Homepage": "https://www.gnu.org/software/make/",
        "Download": "https://ftp.gnu.org/gnu/make/make-4.4.1.tar.gz",
        "MD5Sum": "c8469a3713cbbe04d955d4ae4be23eeb",
    },
    {
        "Name": "Man-DB",
        "Version": "2.13.0",
        "DownloadSize": "2,023 KB",
        "Homepage": "https://www.nongnu.org/man-db/",
        "Download": "https://download.savannah.gnu.org/releases/man-db/man-db-2.13.0.tar.xz",
        "MD5Sum": "97ab5f9f32914eef2062d867381d8cee",
    },
    {
        "Name": "Man-pages",
        "Version": "6.12",
        "DownloadSize": "1,838 KB",
        "Homepage": "https://www.kernel.org/doc/man-pages/",
        "Download": "https://www.kernel.org/pub/linux/docs/man-pages/man-pages-6.12.tar.xz",
        "MD5Sum": "44de430a598605eaba3e36dd43f24298",
    },
    {
        "Name": "MarkupSafe",
        "Version": "3.0.2",
        "DownloadSize": "21 KB",
        "Homepage": "https://palletsprojects.com/p/markupsafe/",
        "Download": "https://pypi.org/packages/source/M/MarkupSafe/markupsafe-3.0.2.tar.gz",
        "MD5Sum": "cb0071711b573b155cc8f86e1de72167",
    },
    {
        "Name": "Meson",
        "Version": "1.7.0",
        "DownloadSize": "2,241 KB",
        "Homepage": "https://mesonbuild.com",
        "Download": "https://github.com/mesonbuild/meson/releases/download/1.7.0/meson-1.7.0.tar.gz",
        "MD5Sum": "c20f3e5ebbb007352d22f4fd6ceb925c",
    },
    {
        "Name": "MPC",
        "Version": "1.3.1",
        "DownloadSize": "756 KB",
        "Homepage": "https://www.multiprecision.org/",
        "Download": "https://ftp.gnu.org/gnu/mpc/mpc-1.3.1.tar.gz",
        "MD5Sum": "5c9bc658c9fd0f940e8e3e0f09530c62",
    },
    {
        "Name": "MPFR",
        "Version": "4.2.1",
        "DownloadSize": "1,459 KB",
        "Homepage": "https://www.mpfr.org/",
        "Download": "https://ftp.gnu.org/gnu/mpfr/mpfr-4.2.1.tar.xz",
        "MD5Sum": "523c50c6318dde6f9dc523bc0244690a",
    },
    {
        "Name": "Ncurses",
        "Version": "6.5",
        "DownloadSize": "2,156 KB",
        "Homepage": "https://www.gnu.org/software/ncurses/",
        "Download": "https://invisible-mirror.net/archives/ncurses/ncurses-6.5.tar.gz",
        "MD5Sum": "ac2d2629296f04c8537ca706b6977687",
    },
    {
        "Name": "Ninja",
        "Version": "1.12.1",
        "DownloadSize": "235 KB",
        "Homepage": "https://ninja-build.org/",
        "Download": "https://github.com/ninja-build/ninja/archive/v1.12.1/ninja-1.12.1.tar.gz",
        "MD5Sum": "6288992b05e593a391599692e2f7e490",
    },
    {
        "Name": "OpenSSL",
        "Version": "3.4.1",
        "DownloadSize": "17,917 KB",
        "Homepage": "https://www.openssl-library.org/",
        "Download": "https://github.com/openssl/openssl/releases/download/openssl-3.4.1/openssl-3.4.1.tar.gz",
        "MD5Sum": "fb7a747ac6793a7ad7118eaba45db379",
    },
    {
        "Name": "Patch",
        "Version": "2.7.6",
        "DownloadSize": "766 KB",
        "Homepage": "https://savannah.gnu.org/projects/patch/",
        "Download": "https://ftp.gnu.org/gnu/patch/patch-2.7.6.tar.xz",
        "MD5Sum": "78ad9937e4caadcba1526ef1853730d5",
    },
    {
        "Name": "Perl",
        "Version": "5.40.1",
        "DownloadSize": "13,605 KB",
        "Homepage": "https://www.perl.org/",
        "Download": "https://www.cpan.org/src/5.0/perl-5.40.1.tar.xz",
        "MD5Sum": "bab3547a5cdf2302ee0396419d74a42e",
    },
    {
        "Name": "Pkgconf",
        "Version": "2.3.0",
        "DownloadSize": "309 KB",
        "Homepage": "https://github.com/pkgconf/pkgconf",
        "Download": "https://distfiles.ariadne.space/pkgconf/pkgconf-2.3.0.tar.xz",
        "MD5Sum": "833363e77b5bed0131c7bc4cc6f7747b",
    },
    {
        "Name": "Procps",
        "Version": "4.0.5",
        "DownloadSize": "1,483 KB",
        "Homepage": "https://gitlab.com/procps-ng/procps/",
        "Download": "https://sourceforge.net/projects/procps-ng/files/Production/procps-ng-4.0.5.tar.xz",
        "MD5Sum": "90803e64f51f192f3325d25c3335d057",
    },
    {
        "Name": "Psmisc",
        "Version": "23.7",
        "DownloadSize": "423 KB",
        "Homepage": "https://gitlab.com/psmisc/psmisc",
        "Download": "https://sourceforge.net/projects/psmisc/files/psmisc/psmisc-23.7.tar.xz",
        "MD5Sum": "53eae841735189a896d614cba440eb10",
    },
    {
        "Name": "Python",
        "Version": "3.13.2",
        "DownloadSize": "22,091 KB",
        "Homepage": "https://www.python.org/",
        "Download": "https://www.python.org/ftp/python/3.13.2/Python-3.13.2.tar.xz",
        "MD5Sum": "4c2d9202ab4db02c9d0999b14655dfe5",
    },
    {
        "Name": "Python Documentation",
        "Version": "3.13.2",
        "DownloadSize": "10,102 KB",
        "Download": "https://www.python.org/ftp/python/doc/3.13.2/python-3.13.2-docs-html.tar.bz2",
        "MD5Sum": "d6aede88f480a018d26b3206f21654ae",
    },
    {
        "Name": "Readline",
        "Version": "8.2.13",
        "DownloadSize": "2,974 KB",
        "Homepage": "https://tiswww.case.edu/php/chet/readline/rltop.html",
        "Download": "https://ftp.gnu.org/gnu/readline/readline-8.2.13.tar.gz",
        "MD5Sum": "05080bf3801e6874bb115cd6700b708f",
    },
    {
        "Name": "Sed",
        "Version": "4.9",
        "DownloadSize": "1,365 KB",
        "Homepage": "https://www.gnu.org/software/sed/",
        "Download": "https://ftp.gnu.org/gnu/sed/sed-4.9.tar.xz",
        "MD5Sum": "6aac9b2dbafcd5b7a67a8a9bcb8036c3",
    },
    {
        "Name": "Setuptools",
        "Version": "75.8.1",
        "DownloadSize": "1,313 KB",
        "Homepage": "https://pypi.org/project/setuptools/",
        "Download": "https://pypi.org/packages/source/s/setuptools/setuptools-75.8.1.tar.gz",
        "MD5Sum": "7dc3d3f529b76b10e35326e25c676b30",
    },
    {
        "Name": "Shadow",
        "Version": "4.17.3",
        "DownloadSize": "2,274 KB",
        "Homepage": "https://github.com/shadow-maint/shadow/",
        "Download": "https://github.com/shadow-maint/shadow/releases/download/4.17.3/shadow-4.17.3.tar.xz",
        "MD5Sum": "0da190e53ecee76237e4c8f3f39531ed",
    },
    {
        "Name": "Sysklogd",
        "Version": "2.7.0",
        "DownloadSize": "465 KB",
        "Homepage": "https://www.infodrom.org/projects/sysklogd/",
        "Download": "https://github.com/troglobit/sysklogd/releases/download/v2.7.0/sysklogd-2.7.0.tar.gz",
        "MD5Sum": "611c0fa5c138eb7a532f3c13bdf11ebc",
    },
    {
        "Name": "Systemd",
        "Version": "257.3",
        "DownloadSize": "15,847 KB",
        "Homepage": "https://www.freedesktop.org/wiki/Software/systemd/",
        "Download": "https://github.com/systemd/systemd/archive/v257.3/systemd-257.3.tar.gz",
        "MD5Sum": "8e4fc90c7aead651fa5c50bd1b34abc2",
    },
    {
        "Name": "Systemd Man Pages",
        "Version": "257.3",
        "DownloadSize": "733 KB",
        "Homepage": "https://www.freedesktop.org/wiki/Software/systemd/",
        "Download": "https://anduin.linuxfromscratch.org/LFS/systemd-man-pages-257.3.tar.xz",
        "MD5Sum": "9b77c3b066723d490cb10aed4fb05696",
    },
    {
        "Name": "SysVinit",
        "Version": "3.14",
        "DownloadSize": "236 KB",
        "Homepage": "https://savannah.nongnu.org/projects/sysvinit",
        "Download": "https://github.com/slicer69/sysvinit/releases/download/3.14/sysvinit-3.14.tar.xz",
        "MD5Sum": "bc6890b975d19dc9db42d0c7364dd092",
    },
    {
        "Name": "Tar",
        "Version": "1.35",
        "DownloadSize": "2,263 KB",
        "Homepage": "https://www.gnu.org/software/tar/",
        "Download": "https://ftp.gnu.org/gnu/tar/tar-1.35.tar.xz",
        "MD5Sum": "a2d8042658cfd8ea939e6d911eaf4152",
    },
    {
        "Name": "Tcl",
        "Version": "8.6.16",
        "DownloadSize": "11,406 KB",
        "Homepage": "https://tcl.sourceforge.net/",
        "Download": "https://downloads.sourceforge.net/tcl/tcl8.6.16-src.tar.gz",
        "MD5Sum": "eaef5d0a27239fb840f04af8ec608242",
    },
    {
        "Name": "Tcl Documentation",
        "Version": "8.6.16",
        "DownloadSize": "1,169 KB",
        "Download": "https://downloads.sourceforge.net/tcl/tcl8.6.16-html.tar.gz",
        "MD5Sum": "750c221bcb6f8737a6791c1fbe98b684",
    },
    {
        "Name": "Texinfo",
        "Version": "7.2",
        "DownloadSize": "6,259 KB",
        "Homepage": "https://www.gnu.org/software/texinfo/",
        "Download": "https://ftp.gnu.org/gnu/texinfo/texinfo-7.2.tar.xz",
        "MD5Sum": "11939a7624572814912a18e76c8d8972",
    },
    {
        "Name": "Time Zone Data",
        "Version": "2025a",
        "DownloadSize": "453 KB",
        "Homepage": "https://www.iana.org/time-zones",
        "Download": "https://www.iana.org/time-zones/repository/releases/tzdata2025a.tar.gz",
        "MD5Sum": "404229390c06b7440f5e48d12c1a3251",
    },
    {
        "Name": "Udev-lfs Tarball",
        "Version": "udev-lfs-20230818",
        "DownloadSize": "10 KB",
        "Download": "https://anduin.linuxfromscratch.org/LFS/udev-lfs-20230818.tar.xz",
        "MD5Sum": "acd4360d8a5c3ef320b9db88d275dae6",
    },
    {
        "Name": "Util-linux",
        "Version": "2.40.4",
        "DownloadSize": "8,641 KB",
        "Homepage": "https://git.kernel.org/pub/scm/utils/util-linux/util-linux.git/",
        "Download": "https://www.kernel.org/pub/linux/utils/util-linux/v2.40/util-linux-2.40.4.tar.xz",
        "MD5Sum": "f9cbb1c8315d8ccbeb0ec36d10350304",
    },
    {
        "Name": "Vim",
        "Version": "9.1.1166",
        "DownloadSize": "18,077 KB",
        "Homepage": "https://www.vim.org",
        "Download": "https://github.com/vim/vim/archive/v9.1.1166/vim-9.1.1166.tar.gz",
        "MD5Sum": "718d43ce957ab7c81071793de176c2eb",
    },
    {
        "Name": "Wheel",
        "Version": "0.45.1",
        "DownloadSize": "106 KB",
        "Homepage": "https://pypi.org/project/wheel/",
        "Download": "https://pypi.org/packages/source/w/wheel/wheel-0.45.1.tar.gz",
        "MD5Sum": "dddc505d0573d03576c7c6c5a4fe0641",
    },
    {
        "Name": "XML::Parser",
        "Verison": "2.47",
        "DownloadSize": "276 KB",
        "Homepage": "https://github.com/chorny/XML-Parser",
        "Download": "https://cpan.metacpan.org/authors/id/T/TO/TODDR/XML-Parser-2.47.tar.gz",
        "MD5Sum": "89a8e82cfd2ad948b349c0a69c494463",
    },
    {
        "Name": "Xz Utils",
        "Version": "5.6.4",
        "DownloadSize": "1,310 KB",
        "Homepage": "https://tukaani.org/xz",
        "Download": "https://github.com//tukaani-project/xz/releases/download/v5.6.4/xz-5.6.4.tar.xz",
        "MD5Sum": "4b1cf07d45ec7eb90a01dd3c00311a3e",
    },
    {
        "Name": "Zlib",
        "Version": "1.3.1",
        "DownloadSize": "1,478 KB",
        "Homepage": "https://zlib.net/",
        "Download": "https://zlib.net/fossils/zlib-1.3.1.tar.gz",
        "MD5Sum": "9855b6d802d7fe5b7bd5b196a2271655",
    },
    {
        "Name": "Zstd",
        "Version": "1.5.7",
        "DownloadSize": "2,378 KB",
        "Homepage": "https://facebook.github.io/zstd/",
        "Download": "https://github.com/facebook/zstd/releases/download/v1.5.7/zstd-1.5.7.tar.gz",
        "MD5Sum": "780fc1896922b1bc52a4e90980cdda48",
    },
]

def get_package(name):
    for package in PACKAGE_VERSIONS:
        if package["Name"].lower() == name.lower():
            return package
    return None

"""
Bzip2 Documentation Patch - 1.6 KB:
Download: https://www.linuxfromscratch.org/patches/lfs/12.3/bzip2-1.0.8-install_docs-1.patch

MD5 sum: 6a5ac7e89b791aae556de0f745916f7f

Coreutils Internationalization Fixes Patch - 164 KB:
Download: https://www.linuxfromscratch.org/patches/lfs/12.3/coreutils-9.6-i18n-1.patch

MD5 sum: 6aee45dd3e05b7658971c321d92f44b7

Expect GCC14 Patch - 7.8 KB:
Download: https://www.linuxfromscratch.org/patches/lfs/12.3/expect-5.45.4-gcc14-1.patch

MD5 sum: 0b8b5ac411d011263ad40b0664c669f0

Glibc FHS Patch - 2.8 KB:
Download: https://www.linuxfromscratch.org/patches/lfs/12.3/glibc-2.41-fhs-1.patch

MD5 sum: 9a5997c3452909b1769918c759eff8a2

Kbd Backspace/Delete Fix Patch - 12 KB:
Download: https://www.linuxfromscratch.org/patches/lfs/12.3/kbd-2.7.1-backspace-1.patch

MD5 sum: f75cca16a38da6caa7d52151f7136895

SysVinit Consolidated Patch - 2.5 KB:
Download: https://www.linuxfromscratch.org/patches/lfs/12.3/sysvinit-3.14-consolidated-1.patch

MD5 sum: 3af8fd8e13cad481eeeaa48be4247445
"""

def hash_string(s):
    return "s" + str(abs(hash(s)))

def run_script(script):
    h = hash_string(script)
    return [
        directive.add_file("/tmp/{}.sh".format(h), file(script, executable = True)),
        directive.run_command("bash /tmp/{}.sh".format(h)),
    ]

limited_directory_layout = base_directives + run_script("""
mkdir -v $LFS/sources

chmod -v a+wt $LFS/sources

mkdir -pv $LFS/{etc,var} $LFS/usr/{bin,lib,sbin}

for i in bin lib sbin; do
  ln -sv usr/$i $LFS/$i
done

case $(uname -m) in
  x86_64) mkdir -pv $LFS/lib64 ;;
esac

mkdir -v $LFS/tools
""")

limited_directory_layout_test = define.build_vm(
    directives = limited_directory_layout,
)

setup_user = limited_directory_layout + run_script("""
groupadd lfs
useradd -s /bin/bash -g lfs -m -k /dev/null lfs

chown -v lfs $LFS/{usr{,/*},var,etc,tools}
case $(uname -m) in
  x86_64) chown -v lfs $LFS/lib64 ;;
esac
""")

setup_user_test = define.build_vm(
    directives = setup_user,
)

def run_script_as_user(script, user):
    h = hash_string(script)
    return [
        directive.add_file("/tmp/{}.sh".format(h), file(script, executable = True)),
        directive.run_command("su {} /tmp/{}.sh".format(user, h)),
    ]

setup_lfs_profile = setup_user + run_script_as_user("""
cat > ~/.bash_profile << "EOF"
exec env -i HOME=$HOME TERM=$TERM PS1='\\u:\\w\\$ ' /bin/bash
EOF

cat > ~/.bashrc << "EOF"
set +h
umask 022
LFS=/mnt/lfs
LC_ALL=POSIX
LFS_TGT=$(uname -m)-lfs-linux-gnu
PATH=/usr/bin
if [ ! -L /bin ]; then PATH=/bin:$PATH; fi
PATH=$LFS/tools/bin:$PATH
CONFIG_SITE=$LFS/usr/share/config.site
export LFS LC_ALL LFS_TGT PATH CONFIG_SITE
EOF

cat >> ~/.bashrc << "EOF"
export MAKEFLAGS=-j$(nproc)
EOF
""", "lfs")

setup_lfs_profile_test = define.build_vm(
    directives = setup_lfs_profile,
)

def get_package_file(name):
    info = get_package(name)
    if info == None:
        return error("Package {} not found".format(name))

    download = define.fetch_http(info["Download"])
    target = "/mnt/lfs/sources/{}".format(path.base(info["Download"]))

    return directive.add_file(target, download)

def get_package_directive(name):
    info = get_package(name)
    if info == None:
        return error("Package {} not found".format(name))

    dir_name = "{}-{}".format(info["Name"].lower(), info["Version"].lower())

    target = "/mnt/lfs/sources/{}".format(dir_name)

    # download and extract the package
    download = define.fetch_http(info["Download"])
    ark = define.read_archive(download, info["Download"], strip_components = 1)

    # Make an archive directive
    ark_directive = directive.archive(ark, target)

    return ark_directive, dir_name, target

def compile_package(name, script, additional_packages = []):
    ark_directive, dir_name, target = get_package_directive(name)

    # Get additional packages
    additional_directives = []
    for package in additional_packages:
        additional_directives.append(get_package_file(package))

    # Create the full script
    full_script = "source ~/.bashrc\nset -ex\ncd $LFS/sources/{}\n".format(dir_name) + script

    return [
        ark_directive,
        # Make sure the user is the owner of the directory
        directive.run_command("chown -v lfs:lfs {}".format(target)),
    ] + additional_directives + run_script_as_user(full_script, "lfs")

binutils_pass1 = setup_lfs_profile + compile_package(
    "Binutils",
    """
mkdir -v build
cd       build
../configure --prefix=$LFS/tools \\
    --with-sysroot=$LFS \\
    --target=$LFS_TGT   \\
    --disable-nls       \\
    --enable-gprofng=no \\
    --disable-werror    \\
    --enable-new-dtags  \\
    --enable-default-hash-style=gnu
make
make install
    """,
)

binutils_pass1_test = define.build_vm(
    directives = binutils_pass1,
    auto_scale = True,
)

gcc_pass1 = binutils_pass1 + compile_package(
    "GCC",
    """
tar -xf ../mpfr-4.2.1.tar.xz
mv -v mpfr-4.2.1 mpfr
tar -xf ../gmp-6.3.0.tar.xz
mv -v gmp-6.3.0 gmp
tar -xf ../mpc-1.3.1.tar.gz
mv -v mpc-1.3.1 mpc

case $(uname -m) in
  x86_64)
    sed -e '/m64=/s/lib64/lib/' \\
        -i.orig gcc/config/i386/t-linux64
 ;;
esac

mkdir -v build
cd       build

../configure                  \\
    --target=$LFS_TGT         \\
    --prefix=$LFS/tools       \\
    --with-glibc-version=2.41 \\
    --with-sysroot=$LFS       \\
    --with-newlib             \\
    --without-headers         \\
    --enable-default-pie      \\
    --enable-default-ssp      \\
    --disable-nls             \\
    --disable-shared          \\
    --disable-multilib        \\
    --disable-threads         \\
    --disable-libatomic       \\
    --disable-libgomp         \\
    --disable-libquadmath     \\
    --disable-libssp          \\
    --disable-libvtv          \\
    --disable-libstdcxx       \\
    --enable-languages=c,c++

make

make install

cd ..
cat gcc/limitx.h gcc/glimits.h gcc/limity.h > \\
  `dirname $($LFS_TGT-gcc -print-libgcc-file-name)`/include/limits.h
    """,
    additional_packages = [
        "mpfr",
        "gmp",
        "mpc",
    ],
)

gcc_pass1_test = define.build_vm(
    directives = gcc_pass1,
    auto_scale = True,
    storage_size = 32 * 1024,
)