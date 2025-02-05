BLOCK_SIZE = 512

def mbr(f):
    f.skip(440)
    return {
        "disk_id": f.u32le(),
        "reserved": assert(f.u16le(), 0),
        "partitions": [
            {
                "status": f.u8(),
                "chs_start": f.u24le(),
                "type": f.u8(),
                "chs_end": f.u24le(),
                "lba_start": f.u32le(),
                "sectors": f.u32le(),
            }
            for _ in range(4)
        ],
        "signature": assert(f.u16le(), 0xaa55),
    }

FS_TYPES = {
    0: "unused",
    1: "swap",  # swap
    2: "v6",  # Sixth Edition
    3: "v7",  # Seventh Edition
    4: "sysv",  # System V
    5: "v71k",  # V7 with 1K blocks (4.1, 2.9)
    6: "v8",  # Eighth Edition, 4K blocks
    7: "bsdffs",  # 4.2BSD fast file system
    8: "msdos",  # MSDOS file system
    9: "bsdlfs",  # 4.4BSD log-structured file system
    10: "other",  # in use, but unknown/unsupported
    11: "hpfs",  # OS/2 high-performance file system
    12: "iso9660",  # ISO 9660, normally CD-ROM
    13: "boot",  # partition contains bootstrap
    14: "ados",  # AmigaDOS fast file system
    15: "hfs",  # Macintosh HFS
    16: "adfs",  # Acorn Disk Filing System
    17: "ext2fs",  # ext2fs
    18: "ccd",  # ccd component
    19: "raid",  # RAIDframe or softraid
    20: "ntfs",  # Windows/NT file system
    21: "udf",  # UDF (DVD) filesystem
}

def disklabel(f):
    return {
        "magic": assert(f.u32le(), 0x82564557),
        "type": f.u16le(),  # drive type
        "subtype": f.u16le(),  # controller/d_type specific
        "typename": f.ascii(16).strip("\x00"),  # type name, e.g. "eagle"
        "packname": f.ascii(16).strip("\x00"),  # pack identifier
        "secsize": f.u32le(),  # # of bytes per sector
        "nsectors": f.u32le(),  # # of data sectors per track
        "ntracks": f.u32le(),  # # of tracks per cylinder
        "ncylinders": f.u32le(),  # # of data cylinders per unit
        "secpercyl": f.u32le(),  # # of data sectors per cylinder
        "secperunit": f.u32le(),  # # of data sectors (low part)
        "uid": f.bytes(8),  # Unique label identifier.
        "acylinders": f.u32le(),  # # of alt. cylinders per unit
        "bstarth": f.u16le(),  # start of useable region (high part)
        "bendh": f.u16le(),  # size of useable region (high part)
        "bstart": f.u32le(),  # start of useable region
        "bend": f.u32le(),  # end of useable region
        "flags": f.u32le(),  # generic flags
        "spare4": [f.u32le() for _ in range(5)],  # reserved for future use
        "secperunith": f.u16le(),  # # of data sectors (high part)
        "version": f.u16le(),  # version # (1=48 bit addressing)
        "spare": [f.u32le() for _ in range(4)],  # reserved for future use
        "magic2": assert(f.u32le(), 0x82564557),  # the magic number (again)
        "checksum": f.u16le(),  # xor of data incl. partitions
        "npartitions": f.u16le(),  # number of partitions in following
        "spare2": f.u32le(),  # reserved for future use
        "spare3": f.u32le(),  # reserved for future use
        "partitions": [
            # the partition table
            {
                "size": f.u32le(),  # number of sectors (low part)
                "offset": f.u32le(),  # starting sector (low part)
                "offseth": f.u16le(),  # starting sector (high part)
                "sizeh": f.u16le(),  # number of sectors (high part)
                "fstype": FS_TYPES[f.u8()],  # filesystem type, see below
                "fragblock": f.u8(),  # encoded filesystem frag/block
                "cpg": f.u16le(),  # UFS: FS cylinders per group
            }
            for _ in range(8)
        ],
    }

def csum(f):
    return {
        "cs_ndir": f.i32le(),
        "cs_nbfree": f.i32le(),
        "cs_nifree": f.i32le(),
        "cs_nffree": f.i32le(),
    }

def csum_total(f):
    return {
        "cs_ndir": f.i64le(),
        "cs_nbfree": f.i64le(),
        "cs_nifree": f.i64le(),
        "cs_nffree": f.i64le(),
        "cs_spare": [f.i64le() for _ in range(4)],
    }

NOCSPTRS = (16 - 4)

def bsdffs_superblock(f):
    return {
        "firstfield": f.i32le(),  # historic file system linked list, used for incore super blocks
        "unused_1": f.i32le(),  # used for incore super blocks
        "sblkno": f.i32le(),  # addr of super-block / frags
        "cblkno": f.i32le(),  # offset of cyl-block / frags
        "iblkno": f.i32le(),  # offset of inode-blocks / frags
        "dblkno": f.i32le(),  # offset of first data / frags
        "cgoffset": f.i32le(),  # cylinder group offset in cylinder
        "cgmask": f.i32le(),  # used to calc mod fs_ntrak
        "ffs1_time": f.i32le(),  # last time written
        "ffs1_size": f.i32le(),  # # of blocks in fs / frags
        "ffs1_dsize": f.i32le(),  # # of data blocks in fs
        "ncg": f.u32le(),  # # of cylinder groups
        "bsize": f.i32le(),  # size of basic blocks / bytes
        "fsize": f.i32le(),  # size of frag blocks / bytes
        "frag": f.i32le(),  # # of frags in a block in fs
        "minfree": f.i32le(),  # minimum percentage of free blocks
        "rotdelay": f.i32le(),  # # of ms for optimal next block
        "rps": f.i32le(),  # disk revolutions per second
        "bmask": f.i32le(),  # ``blkoff'' calc of blk offsets
        "fmask": f.i32le(),  # ``fragoff'' calc of frag offsets
        "bshift": f.i32le(),  # ``lblkno'' calc of logical blkno
        "fshift": f.i32le(),  # ``numfrags'' calc # of frags
        "maxcontig": f.i32le(),  # max # of contiguous blks
        "maxbpg": f.i32le(),  # max # of blks per cyl group
        "fragshift": f.i32le(),  # block to frag shift
        "fsbtodb": f.i32le(),  # fsbtodb and dbtofsb shift constant
        "sbsize": f.i32le(),  # actual size of super block
        "csmask": f.i32le(),  # csum block offset (now unused)
        "csshift": f.i32le(),  # csum block number (now unused)
        "nindir": f.i32le(),  # value of NINDIR
        "inopb": f.u32le(),  # inodes per file system block
        "nspf": f.i32le(),  # DEV_BSIZE sectors per frag
        "optim": f.i32le(),  # optimization preference, see below
        "npsect": f.i32le(),  # DEV_BSIZE sectors/track + spares
        "interleave": f.i32le(),  # DEV_BSIZE sector interleave
        "trackskew": f.i32le(),  # sector 0 skew, per track
        "id": [f.i32le() for _ in range(2)],  # unique filesystem id
        "ffs1_csaddr": f.i32le(),  # blk addr of cyl grp summary area
        "cssize": f.i32le(),  # cyl grp summary area size / bytes
        "cgsize": f.i32le(),  # cyl grp block size / bytes
        "ntrak": f.i32le(),  # tracks per cylinder
        "nsect": f.i32le(),  # DEV_BSIZE sectors per track
        "spc": f.i32le(),  # DEV_BSIZE sectors per cylinder
        "ncyl": f.i32le(),  # cylinders in file system
        "cpg": f.i32le(),  # cylinders per group
        "ipg": f.u32le(),  # inodes per group
        "fpg": f.i32le(),  # blocks per group * fs_frag
        "ffs1_cstotal": csum(f),  # cylinder summary information
        "fmod": f.i8(),  # super block modified flag
        "clean": f.i8(),  # file system is clean flag
        "ronly": f.i8(),  # mounted read-only flag
        "ffs1_flags": f.i8(),  # see FS_ below
        "fsmnt": f.ascii(468).strip("\x00"),  # name mounted on
        "volname": f.ascii(32).strip("\x00"),  # volume name
        "swuid": f.u64le(),  # system-wide uid
        "pad": f.i32le(),  # due to alignment of fs_swuid
        "cgrotor": f.i32le(),  # last cg searched
        "ocsp": [f.u64le() for _ in range(NOCSPTRS)],  # padding; was list of fs_cs buffers
        "contigdirs": f.u64le(),  # # of contiguously allocated dirs
        "csp": f.u64le(),  # cg summary info buffer for fs_cs
        "maxcluster": f.u64le(),  # max cluster in each cyl group
        "active": f.u64le(),  # reserved for snapshots
        "cpc": f.i32le(),  # cyl per cycle in postbl
        "maxbsize": f.i32le(),  # maximum blocking factor permitted
        "spareconf64": [f.i64le() for _ in range(17)],  # old rotation block list head
        "sblockloc": f.i64le(),  # offset of standard super block
        "cstotal": csum_total(f),  # cylinder summary information
        "time": f.i64le(),  # time last written
        "size": f.i64le(),  # number of blocks in fs
        "dsize": f.i64le(),  # number of data blocks in fs
        "csaddr": f.i64le(),  # blk addr of cyl grp summary area
        "pendingblocks": f.i64le(),  # blocks in process of being freed
        "pendinginodes": f.u32le(),  # inodes in process of being freed
        "snapinum": [f.u32le() for _ in range(20)],  # space reserved for snapshots
        "avgfilesize": f.u32le(),  # expected average file size
        "avgfpdir": f.u32le(),  # expected # of files per directory
        "sparecon": [f.i32le() for _ in range(26)],  # reserved for future constants
        "flags": f.u32le(),  # see FS_ flags below
        "fscktime": f.i32le(),  # last time fsck(8)ed
        "contigsumsize": f.i32le(),  # size of cluster summary array
        "maxsymlinklen": f.i32le(),  # max length of an internal symlink
        "inodefmt": f.i32le(),  # format of on-disk inodes
        "maxfilesize": f.u64le(),  # maximum representable file size
        "qbmask": f.i64le(),  # ~fs_bmask - for use with quad size
        "qfmask": f.i64le(),  # ~fs_fmask - for use with quad size
        "state": f.i32le(),  # validate fs_clean field
        "postblformat": f.i32le(),  # format of positional layout tables
        "nrpos": f.i32le(),  # number of rotational positions
        "postbloff": f.i32le(),  # (u_int16) rotation block list head
        "rotbloff": f.i32le(),  # (u_int8) blocks for each rotation
        "magic": assert(f.i32le(), 0x19540119),  # magic number
        "space": f.u8(),  # list of blocks for each rotation
    }

def ufs2_dinode(f):
    """
    struct ufs2_dinode {
	    u_int16_t	di_mode;	/*   0: IFMT, permissions; see below. */
	    int16_t		di_nlink;	/*   2: File link count. */
	    u_int32_t	di_uid;		/*   4: File owner. */
	    u_int32_t	di_gid;		/*   8: File group. */
	    u_int32_t	di_blksize;	/*  12: Inode blocksize. */
	    u_int64_t	di_size;	/*  16: File byte count. */
	    u_int64_t	di_blocks;	/*  24: Bytes actually held. */
	    int64_t		di_atime;	/*  32: Last access time. */
	    int64_t		di_mtime;	/*  40: Last modified time. */
	    int64_t		di_ctime;	/*  48: Last inode change time. */
	    int64_t		di_birthtime;	/*  56: Inode creation time. */
	    int32_t		di_mtimensec;	/*  64: Last modified time. */
	    int32_t		di_atimensec;	/*  68: Last access time. */
	    int32_t		di_ctimensec;	/*  72: Last inode change time. */
	    int32_t		di_birthnsec;	/*  76: Inode creation time. */
	    int32_t		di_gen;		/*  80: Generation number. */
	    u_int32_t	di_kernflags;	/*  84: Kernel flags. */
	    u_int32_t	di_flags;	/*  88: Status flags (chflags). */
	    int32_t		di_extsize;	/*  92: External attributes block. */
	    int64_t		di_extb[NXADDR];/*  96: External attributes block. */
	    int64_t		di_db[NDADDR];	/* 112: Direct disk blocks. */
	    int64_t		di_ib[NIADDR];	/* 208: Indirect disk blocks. */
	    int64_t		di_spare[3];	/* 232: Reserved; currently unused */
    };
    """
    return {
        "di_mode": f.u16le(),
        "di_nlink": f.i16le(),
        "di_uid": f.u32le(),
        "di_gid": f.u32le(),
        "di_blksize": f.u32le(),
        "di_size": f.u64le(),
        "di_blocks": f.u64le(),
        "di_atime": f.i64le(),
        "di_mtime": f.i64le(),
        "di_ctime": f.i64le(),
        "di_birthtime": f.i64le(),
        "di_mtimensec": f.i32le(),
        "di_atimensec": f.i32le(),
        "di_ctimensec": f.i32le(),
        "di_birthnsec": f.i32le(),
        "di_gen": f.i32le(),
        "di_kernflags": f.u32le(),
        "di_flags": f.u32le(),
        "di_extsize": f.i32le(),
        "di_extb": [f.i64le() for _ in range(2)],
        "di_db": [f.i64le() for _ in range(12)],
        "di_ib": [f.i64le() for _ in range(3)],
        "di_spare": [f.i64le() for _ in range(3)],
    }

ROOTINO = 2

def INOPB(fs):
    return fs["inopb"]

def fsbtodb(fs, b):
    return b << fs["fsbtodb"]

def cgbase(fs, c):
    return fs["fpg"] * c

def cgimin(fs, cg):
    return cgstart(fs, cg) + fs["iblkno"]

def cgstart(fs, cg):
    return (cgbase(fs, cg) + fs["cgoffset"] * (cg & ~(fs["cgmask"])))

def ino_to_cg(fs, x):
    return int(x / fs["ipg"])

def blkstofrags(fs, blks):
    return blks << fs["fragshift"]

def ino_to_fsba(fs, ino):
    """
	((daddr_t)(cgimin(fs, ino_to_cg(fs, x)) +			\
	    (blkstofrags((fs), (((x) % (fs)->fs_ipg) / INOPB(fs))))))
    """
    return cgimin(fs, ino_to_cg(fs, ino)) + blkstofrags(fs, ino % int(fs["ipg"] / INOPB(fs)))

def ufs_get_inode(f, fs, ino):
    off = fsbtodb(fs, ino_to_fsba(fs, ino)) * BLOCK_SIZE
    inode_reader = f.clone().skip(off)

    return ufs2_dinode(inode_reader)

def parse_bsdffs(f):
    superblock = bsdffs_superblock(f.clone().skip(65536))
    print(superblock)
    root_ino = ufs_get_inode(f, superblock, ROOTINO)
    print(root_ino, oct(root_ino["di_mode"]))
    return

def main(f):
    mbr_header = mbr(f)

    first_bootable = [p for p in mbr_header["partitions"] if p["status"] & 0x80][0]

    part_reader = f.slice(first_bootable["lba_start"] * BLOCK_SIZE, first_bootable["sectors"] * BLOCK_SIZE)

    part_reader.skip(512)  # skip boot code

    disklabel_header = disklabel(part_reader)

    sector_size = disklabel_header["secsize"]
    block_size = disklabel_header["secperunit"]

    for part in disklabel_header["partitions"]:
        if part["fstype"] == "unused":
            continue
        elif part["fstype"] == "bsdffs":
            print(part)
            size = part["size"] + (part["sizeh"] << 32)
            offset = part["offset"] + (part["offseth"] << 32)
            parse_bsdffs(f.slice(offset * sector_size, size * sector_size))
        elif part["fstype"] == "swap":
            print("swap")
        else:
            print("unhandled", part["fstype"])
