package ext4

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

const (
	DEFAULT_BLOCK_SIZE                     = 4096
	EXT4_SUPERBLOCK_MAGIC           uint16 = uint16(61267)
	EXT4_EXTENT_MAGIC               uint16 = uint16(62218)
	EXT4_VALID_FS                   uint16 = uint16(1)
	EXT4_ERROR_FS                   uint16 = uint16(2)
	EXT4_ERRORS_CONTINUE            uint16 = uint16(1)
	EXT4_ERRORS_RO                  uint16 = uint16(2)
	EXT4_ERRORS_PANIC               uint16 = uint16(3)
	EXT4_OS_LINUX                   uint32 = uint32(0)
	EXT4_OS_HURD                    uint32 = uint32(1)
	EXT4_OS_MASIX                   uint32 = uint32(2)
	EXT4_OS_FREEBSD                 uint32 = uint32(3)
	EXT4_OS_LITES                   uint32 = uint32(4)
	EXT4_GOOD_OLD_REV               uint32 = uint32(0)
	EXT4_DYNAMIC_REV                uint32 = uint32(1)
	EXT4_FEATURE_INCOMPAT_64BIT     uint32 = uint32(128)
	EXT4_FEATURE_COMPAT_HAS_JOURNAL uint32 = uint32(4)
)

type Superblock [1024]byte

func (s *Superblock) TagGenerated() {}

func (s *Superblock) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *Superblock) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *Superblock) Size() int64 {
	return 1024
}
func (s *Superblock) InodesCount() uint32 {
	return binary.LittleEndian.Uint32((*s)[0:])
}
func (s *Superblock) SetInodesCount(value uint32) {
	binary.LittleEndian.PutUint32((*s)[0:], value)
}
func (s *Superblock) BlocksCountLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[4:])
}
func (s *Superblock) SetBlocksCountLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[4:], value)
}
func (s *Superblock) RBlocksCountLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[8:])
}
func (s *Superblock) SetRBlocksCountLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[8:], value)
}
func (s *Superblock) FreeBlocksCountLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[12:])
}
func (s *Superblock) SetFreeBlocksCountLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[12:], value)
}
func (s *Superblock) FreeInodesCount() uint32 {
	return binary.LittleEndian.Uint32((*s)[16:])
}
func (s *Superblock) SetFreeInodesCount(value uint32) {
	binary.LittleEndian.PutUint32((*s)[16:], value)
}
func (s *Superblock) FirstDataBlock() uint32 {
	return binary.LittleEndian.Uint32((*s)[20:])
}
func (s *Superblock) SetFirstDataBlock(value uint32) {
	binary.LittleEndian.PutUint32((*s)[20:], value)
}
func (s *Superblock) LogBlockSize() uint32 {
	return binary.LittleEndian.Uint32((*s)[24:])
}
func (s *Superblock) SetLogBlockSize(value uint32) {
	binary.LittleEndian.PutUint32((*s)[24:], value)
}
func (s *Superblock) LogClusterSize() uint32 {
	return binary.LittleEndian.Uint32((*s)[28:])
}
func (s *Superblock) SetLogClusterSize(value uint32) {
	binary.LittleEndian.PutUint32((*s)[28:], value)
}
func (s *Superblock) BlocksPerGroup() uint32 {
	return binary.LittleEndian.Uint32((*s)[32:])
}
func (s *Superblock) SetBlocksPerGroup(value uint32) {
	binary.LittleEndian.PutUint32((*s)[32:], value)
}
func (s *Superblock) ClustersPerGroup() uint32 {
	return binary.LittleEndian.Uint32((*s)[36:])
}
func (s *Superblock) SetClustersPerGroup(value uint32) {
	binary.LittleEndian.PutUint32((*s)[36:], value)
}
func (s *Superblock) InodesPerGroup() uint32 {
	return binary.LittleEndian.Uint32((*s)[40:])
}
func (s *Superblock) SetInodesPerGroup(value uint32) {
	binary.LittleEndian.PutUint32((*s)[40:], value)
}
func (s *Superblock) Mtime() uint32 {
	return binary.LittleEndian.Uint32((*s)[44:])
}
func (s *Superblock) SetMtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[44:], value)
}
func (s *Superblock) Wtime() uint32 {
	return binary.LittleEndian.Uint32((*s)[48:])
}
func (s *Superblock) SetWtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[48:], value)
}
func (s *Superblock) MntCount() uint16 {
	return binary.LittleEndian.Uint16((*s)[52:])
}
func (s *Superblock) SetMntCount(value uint16) {
	binary.LittleEndian.PutUint16((*s)[52:], value)
}
func (s *Superblock) MaxMntCount() uint16 {
	return binary.LittleEndian.Uint16((*s)[54:])
}
func (s *Superblock) SetMaxMntCount(value uint16) {
	binary.LittleEndian.PutUint16((*s)[54:], value)
}
func (s *Superblock) Magic() uint16 {
	return binary.LittleEndian.Uint16((*s)[56:])
}
func (s *Superblock) SetMagic(value uint16) {
	binary.LittleEndian.PutUint16((*s)[56:], value)
}
func (s *Superblock) State() uint16 {
	return binary.LittleEndian.Uint16((*s)[58:])
}
func (s *Superblock) SetState(value uint16) {
	binary.LittleEndian.PutUint16((*s)[58:], value)
}
func (s *Superblock) Errors() uint16 {
	return binary.LittleEndian.Uint16((*s)[60:])
}
func (s *Superblock) SetErrors(value uint16) {
	binary.LittleEndian.PutUint16((*s)[60:], value)
}
func (s *Superblock) MinorRevLevel() uint16 {
	return binary.LittleEndian.Uint16((*s)[62:])
}
func (s *Superblock) SetMinorRevLevel(value uint16) {
	binary.LittleEndian.PutUint16((*s)[62:], value)
}
func (s *Superblock) Lastcheck() uint32 {
	return binary.LittleEndian.Uint32((*s)[64:])
}
func (s *Superblock) SetLastcheck(value uint32) {
	binary.LittleEndian.PutUint32((*s)[64:], value)
}
func (s *Superblock) Checkinterval() uint32 {
	return binary.LittleEndian.Uint32((*s)[68:])
}
func (s *Superblock) SetCheckinterval(value uint32) {
	binary.LittleEndian.PutUint32((*s)[68:], value)
}
func (s *Superblock) CreatorOs() uint32 {
	return binary.LittleEndian.Uint32((*s)[72:])
}
func (s *Superblock) SetCreatorOs(value uint32) {
	binary.LittleEndian.PutUint32((*s)[72:], value)
}
func (s *Superblock) RevLevel() uint32 {
	return binary.LittleEndian.Uint32((*s)[76:])
}
func (s *Superblock) SetRevLevel(value uint32) {
	binary.LittleEndian.PutUint32((*s)[76:], value)
}
func (s *Superblock) DefResuid() uint16 {
	return binary.LittleEndian.Uint16((*s)[80:])
}
func (s *Superblock) SetDefResuid(value uint16) {
	binary.LittleEndian.PutUint16((*s)[80:], value)
}
func (s *Superblock) DefResgid() uint16 {
	return binary.LittleEndian.Uint16((*s)[82:])
}
func (s *Superblock) SetDefResgid(value uint16) {
	binary.LittleEndian.PutUint16((*s)[82:], value)
}
func (s *Superblock) FirstIno() uint32 {
	return binary.LittleEndian.Uint32((*s)[84:])
}
func (s *Superblock) SetFirstIno(value uint32) {
	binary.LittleEndian.PutUint32((*s)[84:], value)
}
func (s *Superblock) InodeSize() uint16 {
	return binary.LittleEndian.Uint16((*s)[88:])
}
func (s *Superblock) SetInodeSize(value uint16) {
	binary.LittleEndian.PutUint16((*s)[88:], value)
}
func (s *Superblock) BlockGroupNr() uint16 {
	return binary.LittleEndian.Uint16((*s)[90:])
}
func (s *Superblock) SetBlockGroupNr(value uint16) {
	binary.LittleEndian.PutUint16((*s)[90:], value)
}
func (s *Superblock) FeatureCompat() uint32 {
	return binary.LittleEndian.Uint32((*s)[92:])
}
func (s *Superblock) SetFeatureCompat(value uint32) {
	binary.LittleEndian.PutUint32((*s)[92:], value)
}
func (s *Superblock) FeatureIncompat() uint32 {
	return binary.LittleEndian.Uint32((*s)[96:])
}
func (s *Superblock) SetFeatureIncompat(value uint32) {
	binary.LittleEndian.PutUint32((*s)[96:], value)
}
func (s *Superblock) FeatureRoCompat() uint32 {
	return binary.LittleEndian.Uint32((*s)[100:])
}
func (s *Superblock) SetFeatureRoCompat(value uint32) {
	binary.LittleEndian.PutUint32((*s)[100:], value)
}
func (s *Superblock) Uuid() [16]uint8 {
	var result [16]uint8
	copy(result[:], (*s)[104:120])
	return result
}
func (s *Superblock) VolumeName() string {
	data := (*s)[120:136]
	str := string(data)
	return strings.TrimRight(str, "\x00 ")
}
func (s *Superblock) SetVolumeName(value string) {
	for i := 0; i < 16; i++ {
		(*s)[120+i] = ' '
	}
	copy((*s)[120:], []byte(value))
}
func (s *Superblock) LastMounted() string {
	data := (*s)[136:200]
	str := string(data)
	return strings.TrimRight(str, "\x00 ")
}
func (s *Superblock) SetLastMounted(value string) {
	for i := 0; i < 64; i++ {
		(*s)[136+i] = ' '
	}
	copy((*s)[136:], []byte(value))
}
func (s *Superblock) AlgorithmUsageBitmap() uint32 {
	return binary.LittleEndian.Uint32((*s)[200:])
}
func (s *Superblock) SetAlgorithmUsageBitmap(value uint32) {
	binary.LittleEndian.PutUint32((*s)[200:], value)
}
func (s *Superblock) PreallocBlocks() uint8 {
	return uint8((*s)[204])
}
func (s *Superblock) SetPreallocBlocks(value uint8) {
	(*s)[204] = byte(value)
}
func (s *Superblock) PreallocDirBlocks() uint8 {
	return uint8((*s)[205])
}
func (s *Superblock) SetPreallocDirBlocks(value uint8) {
	(*s)[205] = byte(value)
}
func (s *Superblock) ReservedGdtBlocks() uint16 {
	return binary.LittleEndian.Uint16((*s)[206:])
}
func (s *Superblock) SetReservedGdtBlocks(value uint16) {
	binary.LittleEndian.PutUint16((*s)[206:], value)
}
func (s *Superblock) JournalUuid() [16]uint8 {
	var result [16]uint8
	copy(result[:], (*s)[208:224])
	return result
}
func (s *Superblock) JournalInum() uint32 {
	return binary.LittleEndian.Uint32((*s)[224:])
}
func (s *Superblock) SetJournalInum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[224:], value)
}
func (s *Superblock) JournalDev() uint32 {
	return binary.LittleEndian.Uint32((*s)[228:])
}
func (s *Superblock) SetJournalDev(value uint32) {
	binary.LittleEndian.PutUint32((*s)[228:], value)
}
func (s *Superblock) LastOrphan() uint32 {
	return binary.LittleEndian.Uint32((*s)[232:])
}
func (s *Superblock) SetLastOrphan(value uint32) {
	binary.LittleEndian.PutUint32((*s)[232:], value)
}
func (s *Superblock) HashSeed() [16]uint8 {
	var result [16]uint8
	copy(result[:], (*s)[236:252])
	return result
}
func (s *Superblock) DefHashVersion() uint8 {
	return uint8((*s)[252])
}
func (s *Superblock) SetDefHashVersion(value uint8) {
	(*s)[252] = byte(value)
}
func (s *Superblock) JnlBackupType() uint8 {
	return uint8((*s)[253])
}
func (s *Superblock) SetJnlBackupType(value uint8) {
	(*s)[253] = byte(value)
}
func (s *Superblock) DescSize() uint16 {
	return binary.LittleEndian.Uint16((*s)[254:])
}
func (s *Superblock) SetDescSize(value uint16) {
	binary.LittleEndian.PutUint16((*s)[254:], value)
}
func (s *Superblock) SDefaultMountOpts() uint32 {
	return binary.LittleEndian.Uint32((*s)[256:])
}
func (s *Superblock) SetSDefaultMountOpts(value uint32) {
	binary.LittleEndian.PutUint32((*s)[256:], value)
}
func (s *Superblock) FirstMetaBg() uint32 {
	return binary.LittleEndian.Uint32((*s)[260:])
}
func (s *Superblock) SetFirstMetaBg(value uint32) {
	binary.LittleEndian.PutUint32((*s)[260:], value)
}
func (s *Superblock) MkfsTime() uint32 {
	return binary.LittleEndian.Uint32((*s)[264:])
}
func (s *Superblock) SetMkfsTime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[264:], value)
}
func (s *Superblock) JnlBlocks() [68]uint8 {
	var result [68]uint8
	copy(result[:], (*s)[268:336])
	return result
}
func (s *Superblock) BlocksCountHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[336:])
}
func (s *Superblock) SetBlocksCountHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[336:], value)
}
func (s *Superblock) RBlocksCountHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[340:])
}
func (s *Superblock) SetRBlocksCountHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[340:], value)
}
func (s *Superblock) FreeBlocksCountHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[344:])
}
func (s *Superblock) SetFreeBlocksCountHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[344:], value)
}
func (s *Superblock) MinExtraIsize() uint16 {
	return binary.LittleEndian.Uint16((*s)[348:])
}
func (s *Superblock) SetMinExtraIsize(value uint16) {
	binary.LittleEndian.PutUint16((*s)[348:], value)
}
func (s *Superblock) WantExtraIsize() uint16 {
	return binary.LittleEndian.Uint16((*s)[350:])
}
func (s *Superblock) SetWantExtraIsize(value uint16) {
	binary.LittleEndian.PutUint16((*s)[350:], value)
}
func (s *Superblock) Flags() uint32 {
	return binary.LittleEndian.Uint32((*s)[352:])
}
func (s *Superblock) SetFlags(value uint32) {
	binary.LittleEndian.PutUint32((*s)[352:], value)
}
func (s *Superblock) RaidStride() uint16 {
	return binary.LittleEndian.Uint16((*s)[356:])
}
func (s *Superblock) SetRaidStride(value uint16) {
	binary.LittleEndian.PutUint16((*s)[356:], value)
}
func (s *Superblock) MmpUpdateInterval() uint16 {
	return binary.LittleEndian.Uint16((*s)[358:])
}
func (s *Superblock) SetMmpUpdateInterval(value uint16) {
	binary.LittleEndian.PutUint16((*s)[358:], value)
}
func (s *Superblock) MmpBlock() uint64 {
	return binary.LittleEndian.Uint64((*s)[360:])
}
func (s *Superblock) SetMmpBlock(value uint64) {
	binary.LittleEndian.PutUint64((*s)[360:], value)
}
func (s *Superblock) RaidStripeWidth() uint32 {
	return binary.LittleEndian.Uint32((*s)[368:])
}
func (s *Superblock) SetRaidStripeWidth(value uint32) {
	binary.LittleEndian.PutUint32((*s)[368:], value)
}
func (s *Superblock) LogGroupsPerFlex() uint8 {
	return uint8((*s)[372])
}
func (s *Superblock) SetLogGroupsPerFlex(value uint8) {
	(*s)[372] = byte(value)
}
func (s *Superblock) ChecksumType() uint8 {
	return uint8((*s)[373])
}
func (s *Superblock) SetChecksumType(value uint8) {
	(*s)[373] = byte(value)
}
func (s *Superblock) EncryptionLevel() uint8 {
	return uint8((*s)[374])
}
func (s *Superblock) SetEncryptionLevel(value uint8) {
	(*s)[374] = byte(value)
}
func (s *Superblock) ReservedPad() uint8 {
	return uint8((*s)[375])
}
func (s *Superblock) SetReservedPad(value uint8) {
	(*s)[375] = byte(value)
}
func (s *Superblock) KbytesWritten() uint64 {
	return binary.LittleEndian.Uint64((*s)[376:])
}
func (s *Superblock) SetKbytesWritten(value uint64) {
	binary.LittleEndian.PutUint64((*s)[376:], value)
}
func (s *Superblock) SnapshotInum() uint32 {
	return binary.LittleEndian.Uint32((*s)[384:])
}
func (s *Superblock) SetSnapshotInum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[384:], value)
}
func (s *Superblock) SnapshotId() uint32 {
	return binary.LittleEndian.Uint32((*s)[388:])
}
func (s *Superblock) SetSnapshotId(value uint32) {
	binary.LittleEndian.PutUint32((*s)[388:], value)
}
func (s *Superblock) SnapshotRBlocksCount() uint64 {
	return binary.LittleEndian.Uint64((*s)[392:])
}
func (s *Superblock) SetSnapshotRBlocksCount(value uint64) {
	binary.LittleEndian.PutUint64((*s)[392:], value)
}
func (s *Superblock) SnapshotList() uint32 {
	return binary.LittleEndian.Uint32((*s)[400:])
}
func (s *Superblock) SetSnapshotList(value uint32) {
	binary.LittleEndian.PutUint32((*s)[400:], value)
}
func (s *Superblock) ErrorCount() uint32 {
	return binary.LittleEndian.Uint32((*s)[404:])
}
func (s *Superblock) SetErrorCount(value uint32) {
	binary.LittleEndian.PutUint32((*s)[404:], value)
}
func (s *Superblock) FirstErrorTime() uint32 {
	return binary.LittleEndian.Uint32((*s)[408:])
}
func (s *Superblock) SetFirstErrorTime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[408:], value)
}
func (s *Superblock) FirstErrorIno() uint32 {
	return binary.LittleEndian.Uint32((*s)[412:])
}
func (s *Superblock) SetFirstErrorIno(value uint32) {
	binary.LittleEndian.PutUint32((*s)[412:], value)
}
func (s *Superblock) FirstErrorBlock() uint64 {
	return binary.LittleEndian.Uint64((*s)[416:])
}
func (s *Superblock) SetFirstErrorBlock(value uint64) {
	binary.LittleEndian.PutUint64((*s)[416:], value)
}
func (s *Superblock) FirstErrorFunc() string {
	data := (*s)[424:456]
	str := string(data)
	return strings.TrimRight(str, "\x00 ")
}
func (s *Superblock) SetFirstErrorFunc(value string) {
	for i := 0; i < 32; i++ {
		(*s)[424+i] = ' '
	}
	copy((*s)[424:], []byte(value))
}
func (s *Superblock) FirstErrorLine() uint32 {
	return binary.LittleEndian.Uint32((*s)[456:])
}
func (s *Superblock) SetFirstErrorLine(value uint32) {
	binary.LittleEndian.PutUint32((*s)[456:], value)
}
func (s *Superblock) LastErrorTime() uint32 {
	return binary.LittleEndian.Uint32((*s)[460:])
}
func (s *Superblock) SetLastErrorTime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[460:], value)
}
func (s *Superblock) LastErrorIno() uint32 {
	return binary.LittleEndian.Uint32((*s)[464:])
}
func (s *Superblock) SetLastErrorIno(value uint32) {
	binary.LittleEndian.PutUint32((*s)[464:], value)
}
func (s *Superblock) LastErrorLine() uint32 {
	return binary.LittleEndian.Uint32((*s)[468:])
}
func (s *Superblock) SetLastErrorLine(value uint32) {
	binary.LittleEndian.PutUint32((*s)[468:], value)
}
func (s *Superblock) LastErrorBlock() uint64 {
	return binary.LittleEndian.Uint64((*s)[472:])
}
func (s *Superblock) SetLastErrorBlock(value uint64) {
	binary.LittleEndian.PutUint64((*s)[472:], value)
}
func (s *Superblock) LastErrorFunc() string {
	data := (*s)[480:512]
	str := string(data)
	return strings.TrimRight(str, "\x00 ")
}
func (s *Superblock) SetLastErrorFunc(value string) {
	for i := 0; i < 32; i++ {
		(*s)[480+i] = ' '
	}
	copy((*s)[480:], []byte(value))
}
func (s *Superblock) MountOpts() [64]uint8 {
	var result [64]uint8
	copy(result[:], (*s)[512:576])
	return result
}
func (s *Superblock) UsrQuotaInum() uint32 {
	return binary.LittleEndian.Uint32((*s)[576:])
}
func (s *Superblock) SetUsrQuotaInum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[576:], value)
}
func (s *Superblock) GrpQuotaInum() uint32 {
	return binary.LittleEndian.Uint32((*s)[580:])
}
func (s *Superblock) SetGrpQuotaInum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[580:], value)
}
func (s *Superblock) OverheadClusters() uint32 {
	return binary.LittleEndian.Uint32((*s)[584:])
}
func (s *Superblock) SetOverheadClusters(value uint32) {
	binary.LittleEndian.PutUint32((*s)[584:], value)
}
func (s *Superblock) BackupBgs() [8]uint8 {
	var result [8]uint8
	copy(result[:], (*s)[588:596])
	return result
}
func (s *Superblock) EncryptAlgos() [4]uint8 {
	var result [4]uint8
	copy(result[:], (*s)[596:600])
	return result
}
func (s *Superblock) EncryptPwSalt() [16]uint8 {
	var result [16]uint8
	copy(result[:], (*s)[600:616])
	return result
}
func (s *Superblock) LpfIno() uint32 {
	return binary.LittleEndian.Uint32((*s)[616:])
}
func (s *Superblock) SetLpfIno(value uint32) {
	binary.LittleEndian.PutUint32((*s)[616:], value)
}
func (s *Superblock) PrjQuotaInum() uint32 {
	return binary.LittleEndian.Uint32((*s)[620:])
}
func (s *Superblock) SetPrjQuotaInum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[620:], value)
}
func (s *Superblock) ChecksumSeed() uint32 {
	return binary.LittleEndian.Uint32((*s)[624:])
}
func (s *Superblock) SetChecksumSeed(value uint32) {
	binary.LittleEndian.PutUint32((*s)[624:], value)
}
func (s *Superblock) WtimeHi() uint8 {
	return uint8((*s)[628])
}
func (s *Superblock) SetWtimeHi(value uint8) {
	(*s)[628] = byte(value)
}
func (s *Superblock) MtimeHi() uint8 {
	return uint8((*s)[629])
}
func (s *Superblock) SetMtimeHi(value uint8) {
	(*s)[629] = byte(value)
}
func (s *Superblock) MkfsTimeHi() uint8 {
	return uint8((*s)[630])
}
func (s *Superblock) SetMkfsTimeHi(value uint8) {
	(*s)[630] = byte(value)
}
func (s *Superblock) LastcheckHi() uint8 {
	return uint8((*s)[631])
}
func (s *Superblock) SetLastcheckHi(value uint8) {
	(*s)[631] = byte(value)
}
func (s *Superblock) FirstErrorTimeHi() uint8 {
	return uint8((*s)[632])
}
func (s *Superblock) SetFirstErrorTimeHi(value uint8) {
	(*s)[632] = byte(value)
}
func (s *Superblock) LastErrorTimeHi() uint8 {
	return uint8((*s)[633])
}
func (s *Superblock) SetLastErrorTimeHi(value uint8) {
	(*s)[633] = byte(value)
}
func (s *Superblock) FirstErrorErrcode() uint8 {
	return uint8((*s)[634])
}
func (s *Superblock) SetFirstErrorErrcode(value uint8) {
	(*s)[634] = byte(value)
}
func (s *Superblock) LastErrorErrcode() uint8 {
	return uint8((*s)[635])
}
func (s *Superblock) SetLastErrorErrcode(value uint8) {
	(*s)[635] = byte(value)
}
func (s *Superblock) Encoding() uint16 {
	return binary.LittleEndian.Uint16((*s)[636:])
}
func (s *Superblock) SetEncoding(value uint16) {
	binary.LittleEndian.PutUint16((*s)[636:], value)
}
func (s *Superblock) EncodingFlags() uint16 {
	return binary.LittleEndian.Uint16((*s)[638:])
}
func (s *Superblock) SetEncodingFlags(value uint16) {
	binary.LittleEndian.PutUint16((*s)[638:], value)
}
func (s *Superblock) OrphanFileInum() uint32 {
	return binary.LittleEndian.Uint32((*s)[640:])
}
func (s *Superblock) SetOrphanFileInum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[640:], value)
}
func (s *Superblock) Reserved() [376]uint8 {
	var result [376]uint8
	copy(result[:], (*s)[644:1020])
	return result
}
func (s *Superblock) Checksum() uint32 {
	return binary.LittleEndian.Uint32((*s)[1020:])
}
func (s *Superblock) SetChecksum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[1020:], value)
}
func (s *Superblock) BlocksCount() uint32 {
	return uint32(uint64(s.BlocksCountLo()) | uint64(s.BlocksCountHi())<<32)
}
func (s *Superblock) RBlocksCount() uint32 {
	return uint32(uint64(s.RBlocksCountLo()) | uint64(s.RBlocksCountHi())<<32)
}
func (s *Superblock) FreeBlocksCount() uint32 {
	return uint32(uint64(s.FreeBlocksCountLo()) | uint64(s.FreeBlocksCountHi())<<32)
}
func (s *Superblock) Validate() error {
	if s.Magic() != EXT4_SUPERBLOCK_MAGIC {
		return fmt.Errorf("validation failed: check condition not met")
	}
	return nil
}
func NewSuperblock() *Superblock {
	var s Superblock
	return &s
}

type BlockGroupDescriptor [64]byte

func (s *BlockGroupDescriptor) TagGenerated() {}

func (s *BlockGroupDescriptor) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *BlockGroupDescriptor) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *BlockGroupDescriptor) Size() int64 {
	return 64
}
func (s *BlockGroupDescriptor) BlockBitmapLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[0:])
}
func (s *BlockGroupDescriptor) SetBlockBitmapLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[0:], value)
}
func (s *BlockGroupDescriptor) InodeBitmapLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[4:])
}
func (s *BlockGroupDescriptor) SetInodeBitmapLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[4:], value)
}
func (s *BlockGroupDescriptor) InodeTableLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[8:])
}
func (s *BlockGroupDescriptor) SetInodeTableLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[8:], value)
}
func (s *BlockGroupDescriptor) FreeBlocksCountLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[12:])
}
func (s *BlockGroupDescriptor) SetFreeBlocksCountLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[12:], value)
}
func (s *BlockGroupDescriptor) FreeInodesCountLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[14:])
}
func (s *BlockGroupDescriptor) SetFreeInodesCountLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[14:], value)
}
func (s *BlockGroupDescriptor) UsedDirsCountLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[16:])
}
func (s *BlockGroupDescriptor) SetUsedDirsCountLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[16:], value)
}
func (s *BlockGroupDescriptor) Flags() uint16 {
	return binary.LittleEndian.Uint16((*s)[18:])
}
func (s *BlockGroupDescriptor) SetFlags(value uint16) {
	binary.LittleEndian.PutUint16((*s)[18:], value)
}
func (s *BlockGroupDescriptor) ExcludeBitmapLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[20:])
}
func (s *BlockGroupDescriptor) SetExcludeBitmapLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[20:], value)
}
func (s *BlockGroupDescriptor) BlockBitmapCsumLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[24:])
}
func (s *BlockGroupDescriptor) SetBlockBitmapCsumLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[24:], value)
}
func (s *BlockGroupDescriptor) InodeBitmapCsumLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[26:])
}
func (s *BlockGroupDescriptor) SetInodeBitmapCsumLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[26:], value)
}
func (s *BlockGroupDescriptor) ItableUnusedLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[28:])
}
func (s *BlockGroupDescriptor) SetItableUnusedLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[28:], value)
}
func (s *BlockGroupDescriptor) Checksum() uint16 {
	return binary.LittleEndian.Uint16((*s)[30:])
}
func (s *BlockGroupDescriptor) SetChecksum(value uint16) {
	binary.LittleEndian.PutUint16((*s)[30:], value)
}
func (s *BlockGroupDescriptor) BlockBitmapHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[32:])
}
func (s *BlockGroupDescriptor) SetBlockBitmapHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[32:], value)
}
func (s *BlockGroupDescriptor) InodeBitmapHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[36:])
}
func (s *BlockGroupDescriptor) SetInodeBitmapHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[36:], value)
}
func (s *BlockGroupDescriptor) InodeTableHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[40:])
}
func (s *BlockGroupDescriptor) SetInodeTableHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[40:], value)
}
func (s *BlockGroupDescriptor) FreeBlocksCountHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[44:])
}
func (s *BlockGroupDescriptor) SetFreeBlocksCountHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[44:], value)
}
func (s *BlockGroupDescriptor) FreeInodesCountHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[46:])
}
func (s *BlockGroupDescriptor) SetFreeInodesCountHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[46:], value)
}
func (s *BlockGroupDescriptor) UsedDirsCountHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[48:])
}
func (s *BlockGroupDescriptor) SetUsedDirsCountHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[48:], value)
}
func (s *BlockGroupDescriptor) ItableUnusedHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[50:])
}
func (s *BlockGroupDescriptor) SetItableUnusedHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[50:], value)
}
func (s *BlockGroupDescriptor) ExcludeBitmapHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[52:])
}
func (s *BlockGroupDescriptor) SetExcludeBitmapHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[52:], value)
}
func (s *BlockGroupDescriptor) BlockBitmapCsumHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[56:])
}
func (s *BlockGroupDescriptor) SetBlockBitmapCsumHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[56:], value)
}
func (s *BlockGroupDescriptor) InodeBitmapCsumHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[58:])
}
func (s *BlockGroupDescriptor) SetInodeBitmapCsumHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[58:], value)
}
func (s *BlockGroupDescriptor) Reserved() uint32 {
	return binary.LittleEndian.Uint32((*s)[60:])
}
func (s *BlockGroupDescriptor) SetReserved(value uint32) {
	binary.LittleEndian.PutUint32((*s)[60:], value)
}
func (s *BlockGroupDescriptor) BlockBitmap() uint32 {
	return uint32(uint64(s.BlockBitmapLo()) | uint64(s.BlockBitmapHi())<<32)
}
func (s *BlockGroupDescriptor) InodeBitmap() uint32 {
	return uint32(uint64(s.InodeBitmapLo()) | uint64(s.InodeBitmapHi())<<32)
}
func (s *BlockGroupDescriptor) InodeTable() uint32 {
	return uint32(uint64(s.InodeTableLo()) | uint64(s.InodeTableHi())<<32)
}
func (s *BlockGroupDescriptor) BgFreeBlocksCount() uint32 {
	return uint32(uint64(s.FreeBlocksCountLo()) | uint64(s.FreeBlocksCountHi())<<16)
}
func (s *BlockGroupDescriptor) BgFreeInodesCount() uint32 {
	return uint32(uint64(s.FreeInodesCountLo()) | uint64(s.FreeInodesCountHi())<<16)
}
func (s *BlockGroupDescriptor) BgUsedDirsCount() uint32 {
	return uint32(uint64(s.UsedDirsCountLo()) | uint64(s.UsedDirsCountHi())<<16)
}
func NewBlockGroupDescriptor() *BlockGroupDescriptor {
	var s BlockGroupDescriptor
	return &s
}

type Inode [256]byte

func (s *Inode) TagGenerated() {}

func (s *Inode) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *Inode) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *Inode) Size() int64 {
	return 256
}
func (s *Inode) Mode() uint16 {
	return binary.LittleEndian.Uint16((*s)[0:])
}
func (s *Inode) SetMode(value uint16) {
	binary.LittleEndian.PutUint16((*s)[0:], value)
}
func (s *Inode) Uid() uint16 {
	return binary.LittleEndian.Uint16((*s)[2:])
}
func (s *Inode) SetUid(value uint16) {
	binary.LittleEndian.PutUint16((*s)[2:], value)
}
func (s *Inode) SizeLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[4:])
}
func (s *Inode) SetSizeLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[4:], value)
}
func (s *Inode) Atime() uint32 {
	return binary.LittleEndian.Uint32((*s)[8:])
}
func (s *Inode) SetAtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[8:], value)
}
func (s *Inode) Ctime() uint32 {
	return binary.LittleEndian.Uint32((*s)[12:])
}
func (s *Inode) SetCtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[12:], value)
}
func (s *Inode) Mtime() uint32 {
	return binary.LittleEndian.Uint32((*s)[16:])
}
func (s *Inode) SetMtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[16:], value)
}
func (s *Inode) Dtime() uint32 {
	return binary.LittleEndian.Uint32((*s)[20:])
}
func (s *Inode) SetDtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[20:], value)
}
func (s *Inode) Gid() uint16 {
	return binary.LittleEndian.Uint16((*s)[24:])
}
func (s *Inode) SetGid(value uint16) {
	binary.LittleEndian.PutUint16((*s)[24:], value)
}
func (s *Inode) LinksCount() uint16 {
	return binary.LittleEndian.Uint16((*s)[26:])
}
func (s *Inode) SetLinksCount(value uint16) {
	binary.LittleEndian.PutUint16((*s)[26:], value)
}
func (s *Inode) BlocksLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[28:])
}
func (s *Inode) SetBlocksLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[28:], value)
}
func (s *Inode) Flags() uint32 {
	return binary.LittleEndian.Uint32((*s)[32:])
}
func (s *Inode) SetFlags(value uint32) {
	binary.LittleEndian.PutUint32((*s)[32:], value)
}
func (s *Inode) IVersion() uint32 {
	return binary.LittleEndian.Uint32((*s)[36:])
}
func (s *Inode) SetIVersion(value uint32) {
	binary.LittleEndian.PutUint32((*s)[36:], value)
}
func (s *Inode) BlockMagic() uint16 {
	return binary.LittleEndian.Uint16((*s)[40:])
}
func (s *Inode) SetBlockMagic(value uint16) {
	binary.LittleEndian.PutUint16((*s)[40:], value)
}
func (s *Inode) BlockEntries() uint16 {
	return binary.LittleEndian.Uint16((*s)[42:])
}
func (s *Inode) SetBlockEntries(value uint16) {
	binary.LittleEndian.PutUint16((*s)[42:], value)
}
func (s *Inode) BlockMax() uint16 {
	return binary.LittleEndian.Uint16((*s)[44:])
}
func (s *Inode) SetBlockMax(value uint16) {
	binary.LittleEndian.PutUint16((*s)[44:], value)
}
func (s *Inode) BlockDepth() uint16 {
	return binary.LittleEndian.Uint16((*s)[46:])
}
func (s *Inode) SetBlockDepth(value uint16) {
	binary.LittleEndian.PutUint16((*s)[46:], value)
}
func (s *Inode) BlockGeneration() uint32 {
	return binary.LittleEndian.Uint32((*s)[48:])
}
func (s *Inode) SetBlockGeneration(value uint32) {
	binary.LittleEndian.PutUint32((*s)[48:], value)
}
func (s *Inode) Block0Block() uint32 {
	return binary.LittleEndian.Uint32((*s)[52:])
}
func (s *Inode) SetBlock0Block(value uint32) {
	binary.LittleEndian.PutUint32((*s)[52:], value)
}
func (s *Inode) Block0Len() uint16 {
	return binary.LittleEndian.Uint16((*s)[56:])
}
func (s *Inode) SetBlock0Len(value uint16) {
	binary.LittleEndian.PutUint16((*s)[56:], value)
}
func (s *Inode) Block0StartHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[58:])
}
func (s *Inode) SetBlock0StartHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[58:], value)
}
func (s *Inode) Block0StartLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[60:])
}
func (s *Inode) SetBlock0StartLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[60:], value)
}
func (s *Inode) Block1Block() uint32 {
	return binary.LittleEndian.Uint32((*s)[64:])
}
func (s *Inode) SetBlock1Block(value uint32) {
	binary.LittleEndian.PutUint32((*s)[64:], value)
}
func (s *Inode) Block1Len() uint16 {
	return binary.LittleEndian.Uint16((*s)[68:])
}
func (s *Inode) SetBlock1Len(value uint16) {
	binary.LittleEndian.PutUint16((*s)[68:], value)
}
func (s *Inode) Block1StartHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[70:])
}
func (s *Inode) SetBlock1StartHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[70:], value)
}
func (s *Inode) Block1StartLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[72:])
}
func (s *Inode) SetBlock1StartLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[72:], value)
}
func (s *Inode) Block2Block() uint32 {
	return binary.LittleEndian.Uint32((*s)[76:])
}
func (s *Inode) SetBlock2Block(value uint32) {
	binary.LittleEndian.PutUint32((*s)[76:], value)
}
func (s *Inode) Block2Len() uint16 {
	return binary.LittleEndian.Uint16((*s)[80:])
}
func (s *Inode) SetBlock2Len(value uint16) {
	binary.LittleEndian.PutUint16((*s)[80:], value)
}
func (s *Inode) Block2StartHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[82:])
}
func (s *Inode) SetBlock2StartHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[82:], value)
}
func (s *Inode) Block2StartLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[84:])
}
func (s *Inode) SetBlock2StartLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[84:], value)
}
func (s *Inode) Block3Block() uint32 {
	return binary.LittleEndian.Uint32((*s)[88:])
}
func (s *Inode) SetBlock3Block(value uint32) {
	binary.LittleEndian.PutUint32((*s)[88:], value)
}
func (s *Inode) Block3Len() uint16 {
	return binary.LittleEndian.Uint16((*s)[92:])
}
func (s *Inode) SetBlock3Len(value uint16) {
	binary.LittleEndian.PutUint16((*s)[92:], value)
}
func (s *Inode) Block3StartHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[94:])
}
func (s *Inode) SetBlock3StartHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[94:], value)
}
func (s *Inode) Block3StartLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[96:])
}
func (s *Inode) SetBlock3StartLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[96:], value)
}
func (s *Inode) Generation() uint32 {
	return binary.LittleEndian.Uint32((*s)[100:])
}
func (s *Inode) SetGeneration(value uint32) {
	binary.LittleEndian.PutUint32((*s)[100:], value)
}
func (s *Inode) FileAclLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[104:])
}
func (s *Inode) SetFileAclLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[104:], value)
}
func (s *Inode) SizeHigh() uint32 {
	return binary.LittleEndian.Uint32((*s)[108:])
}
func (s *Inode) SetSizeHigh(value uint32) {
	binary.LittleEndian.PutUint32((*s)[108:], value)
}
func (s *Inode) ObsoFaddr() uint32 {
	return binary.LittleEndian.Uint32((*s)[112:])
}
func (s *Inode) SetObsoFaddr(value uint32) {
	binary.LittleEndian.PutUint32((*s)[112:], value)
}
func (s *Inode) BlocksHigh() uint16 {
	return binary.LittleEndian.Uint16((*s)[116:])
}
func (s *Inode) SetBlocksHigh(value uint16) {
	binary.LittleEndian.PutUint16((*s)[116:], value)
}
func (s *Inode) FileAclHigh() uint16 {
	return binary.LittleEndian.Uint16((*s)[118:])
}
func (s *Inode) SetFileAclHigh(value uint16) {
	binary.LittleEndian.PutUint16((*s)[118:], value)
}
func (s *Inode) UidHigh() uint16 {
	return binary.LittleEndian.Uint16((*s)[120:])
}
func (s *Inode) SetUidHigh(value uint16) {
	binary.LittleEndian.PutUint16((*s)[120:], value)
}
func (s *Inode) GidHigh() uint16 {
	return binary.LittleEndian.Uint16((*s)[122:])
}
func (s *Inode) SetGidHigh(value uint16) {
	binary.LittleEndian.PutUint16((*s)[122:], value)
}
func (s *Inode) ChecksumLo() uint16 {
	return binary.LittleEndian.Uint16((*s)[124:])
}
func (s *Inode) SetChecksumLo(value uint16) {
	binary.LittleEndian.PutUint16((*s)[124:], value)
}
func (s *Inode) Reserved() uint16 {
	return binary.LittleEndian.Uint16((*s)[126:])
}
func (s *Inode) SetReserved(value uint16) {
	binary.LittleEndian.PutUint16((*s)[126:], value)
}
func (s *Inode) ExtraIsize() uint16 {
	return binary.LittleEndian.Uint16((*s)[128:])
}
func (s *Inode) SetExtraIsize(value uint16) {
	binary.LittleEndian.PutUint16((*s)[128:], value)
}
func (s *Inode) ChecksumHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[130:])
}
func (s *Inode) SetChecksumHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[130:], value)
}
func (s *Inode) CtimeExtra() uint32 {
	return binary.LittleEndian.Uint32((*s)[132:])
}
func (s *Inode) SetCtimeExtra(value uint32) {
	binary.LittleEndian.PutUint32((*s)[132:], value)
}
func (s *Inode) MtimeExtra() uint32 {
	return binary.LittleEndian.Uint32((*s)[136:])
}
func (s *Inode) SetMtimeExtra(value uint32) {
	binary.LittleEndian.PutUint32((*s)[136:], value)
}
func (s *Inode) AtimeExtra() uint32 {
	return binary.LittleEndian.Uint32((*s)[140:])
}
func (s *Inode) SetAtimeExtra(value uint32) {
	binary.LittleEndian.PutUint32((*s)[140:], value)
}
func (s *Inode) Crtime() uint32 {
	return binary.LittleEndian.Uint32((*s)[144:])
}
func (s *Inode) SetCrtime(value uint32) {
	binary.LittleEndian.PutUint32((*s)[144:], value)
}
func (s *Inode) CrtimeExtra() uint32 {
	return binary.LittleEndian.Uint32((*s)[148:])
}
func (s *Inode) SetCrtimeExtra(value uint32) {
	binary.LittleEndian.PutUint32((*s)[148:], value)
}
func (s *Inode) VersionHi() uint32 {
	return binary.LittleEndian.Uint32((*s)[152:])
}
func (s *Inode) SetVersionHi(value uint32) {
	binary.LittleEndian.PutUint32((*s)[152:], value)
}
func (s *Inode) Projid() uint32 {
	return binary.LittleEndian.Uint32((*s)[156:])
}
func (s *Inode) SetProjid(value uint32) {
	binary.LittleEndian.PutUint32((*s)[156:], value)
}
func (s *Inode) Extra() [96]byte {
	var result [96]byte
	copy(result[:], (*s)[160:256])
	return result
}
func (s *Inode) SetExtra(value [96]byte) {
	copy((*s)[160:256], value[:])
}
func (s *Inode) Block0Start() uint32 {
	return uint32(uint64(s.Block0StartLo()) | uint64(s.Block0StartHi())<<32)
}
func (s *Inode) Block1Start() uint32 {
	return uint32(uint64(s.Block1StartLo()) | uint64(s.Block1StartHi())<<32)
}
func (s *Inode) Block2Start() uint32 {
	return uint32(uint64(s.Block2StartLo()) | uint64(s.Block2StartHi())<<32)
}
func (s *Inode) Block3Start() uint32 {
	return uint32(uint64(s.Block3StartLo()) | uint64(s.Block3StartHi())<<32)
}
func (s *Inode) NSize() uint32 {
	return uint32(uint64(s.SizeLo()) | uint64(s.SizeHigh())<<32)
}
func (s *Inode) Blocks() uint32 {
	return uint32(uint64(s.BlocksLo()) | uint64(s.BlocksHigh())<<16)
}
func NewInode() *Inode {
	var s Inode
	return &s
}

type ExtentTreeHeader [12]byte

func (s *ExtentTreeHeader) TagGenerated() {}

func (s *ExtentTreeHeader) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *ExtentTreeHeader) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *ExtentTreeHeader) Size() int64 {
	return 12
}
func (s *ExtentTreeHeader) Magic() uint16 {
	return binary.LittleEndian.Uint16((*s)[0:])
}
func (s *ExtentTreeHeader) SetMagic(value uint16) {
	binary.LittleEndian.PutUint16((*s)[0:], value)
}
func (s *ExtentTreeHeader) Entries() uint16 {
	return binary.LittleEndian.Uint16((*s)[2:])
}
func (s *ExtentTreeHeader) SetEntries(value uint16) {
	binary.LittleEndian.PutUint16((*s)[2:], value)
}
func (s *ExtentTreeHeader) Max() uint16 {
	return binary.LittleEndian.Uint16((*s)[4:])
}
func (s *ExtentTreeHeader) SetMax(value uint16) {
	binary.LittleEndian.PutUint16((*s)[4:], value)
}
func (s *ExtentTreeHeader) Depth() uint16 {
	return binary.LittleEndian.Uint16((*s)[6:])
}
func (s *ExtentTreeHeader) SetDepth(value uint16) {
	binary.LittleEndian.PutUint16((*s)[6:], value)
}
func (s *ExtentTreeHeader) Generation() uint32 {
	return binary.LittleEndian.Uint32((*s)[8:])
}
func (s *ExtentTreeHeader) SetGeneration(value uint32) {
	binary.LittleEndian.PutUint32((*s)[8:], value)
}
func (s *ExtentTreeHeader) Validate() error {
	if s.Magic() != EXT4_EXTENT_MAGIC {
		return fmt.Errorf("validation failed: check condition not met")
	}
	return nil
}
func NewExtentTreeHeader() *ExtentTreeHeader {
	var s ExtentTreeHeader
	return &s
}

type ExtentTreeIdx [12]byte

func (s *ExtentTreeIdx) TagGenerated() {}

func (s *ExtentTreeIdx) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *ExtentTreeIdx) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *ExtentTreeIdx) Size() int64 {
	return 12
}
func (s *ExtentTreeIdx) Block() uint32 {
	return binary.LittleEndian.Uint32((*s)[0:])
}
func (s *ExtentTreeIdx) SetBlock(value uint32) {
	binary.LittleEndian.PutUint32((*s)[0:], value)
}
func (s *ExtentTreeIdx) LeafLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[4:])
}
func (s *ExtentTreeIdx) SetLeafLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[4:], value)
}
func (s *ExtentTreeIdx) LeafHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[8:])
}
func (s *ExtentTreeIdx) SetLeafHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[8:], value)
}
func (s *ExtentTreeIdx) Unused() uint16 {
	return binary.LittleEndian.Uint16((*s)[10:])
}
func (s *ExtentTreeIdx) SetUnused(value uint16) {
	binary.LittleEndian.PutUint16((*s)[10:], value)
}
func NewExtentTreeIdx() *ExtentTreeIdx {
	var s ExtentTreeIdx
	return &s
}

type ExtentTreeNode [12]byte

func (s *ExtentTreeNode) TagGenerated() {}

func (s *ExtentTreeNode) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *ExtentTreeNode) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *ExtentTreeNode) Size() int64 {
	return 12
}
func (s *ExtentTreeNode) Block() uint32 {
	return binary.LittleEndian.Uint32((*s)[0:])
}
func (s *ExtentTreeNode) SetBlock(value uint32) {
	binary.LittleEndian.PutUint32((*s)[0:], value)
}
func (s *ExtentTreeNode) Len() uint16 {
	return binary.LittleEndian.Uint16((*s)[4:])
}
func (s *ExtentTreeNode) SetLen(value uint16) {
	binary.LittleEndian.PutUint16((*s)[4:], value)
}
func (s *ExtentTreeNode) StartHi() uint16 {
	return binary.LittleEndian.Uint16((*s)[6:])
}
func (s *ExtentTreeNode) SetStartHi(value uint16) {
	binary.LittleEndian.PutUint16((*s)[6:], value)
}
func (s *ExtentTreeNode) StartLo() uint32 {
	return binary.LittleEndian.Uint32((*s)[8:])
}
func (s *ExtentTreeNode) SetStartLo(value uint32) {
	binary.LittleEndian.PutUint32((*s)[8:], value)
}
func (s *ExtentTreeNode) Start() uint32 {
	return uint32(uint64(s.StartLo()) | uint64(s.StartHi())<<32)
}
func NewExtentTreeNode() *ExtentTreeNode {
	var s ExtentTreeNode
	return &s
}

type ExtentTreeTail [4]byte

func (s *ExtentTreeTail) TagGenerated() {}

func (s *ExtentTreeTail) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *ExtentTreeTail) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *ExtentTreeTail) Size() int64 {
	return 4
}
func (s *ExtentTreeTail) Checksum() uint32 {
	return binary.LittleEndian.Uint32((*s)[0:])
}
func (s *ExtentTreeTail) SetChecksum(value uint32) {
	binary.LittleEndian.PutUint32((*s)[0:], value)
}
func NewExtentTreeTail() *ExtentTreeTail {
	var s ExtentTreeTail
	return &s
}

type DirEntry2 [8]byte

func (s *DirEntry2) TagGenerated() {}

func (s *DirEntry2) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= s.Size() {
		return 0, io.EOF
	}
	n = copy(p, (*s)[off:])
	return n, nil
}
func (s *DirEntry2) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= s.Size() {
		return 0, io.ErrUnexpectedEOF
	}
	n = copy((*s)[off:], p)
	return n, nil
}
func (s *DirEntry2) Size() int64 {
	return 8
}
func (s *DirEntry2) Inode() uint32 {
	return binary.LittleEndian.Uint32((*s)[0:])
}
func (s *DirEntry2) SetInode(value uint32) {
	binary.LittleEndian.PutUint32((*s)[0:], value)
}
func (s *DirEntry2) RecLen() uint16 {
	return binary.LittleEndian.Uint16((*s)[4:])
}
func (s *DirEntry2) SetRecLen(value uint16) {
	binary.LittleEndian.PutUint16((*s)[4:], value)
}
func (s *DirEntry2) NameLen() uint8 {
	return uint8((*s)[6])
}
func (s *DirEntry2) SetNameLen(value uint8) {
	(*s)[6] = byte(value)
}
func (s *DirEntry2) FileType() uint8 {
	return uint8((*s)[7])
}
func (s *DirEntry2) SetFileType(value uint8) {
	(*s)[7] = byte(value)
}
func NewDirEntry2() *DirEntry2 {
	var s DirEntry2
	return &s
}

type Ext4Layout struct {
	vm *vm.VirtualMemory
}

func NewExt4Layout(virtualMemory *vm.VirtualMemory) *Ext4Layout {
	return &Ext4Layout{vm: virtualMemory}
}
func (l *Ext4Layout) Superblock() *Superblock {
	var r Superblock
	if err := l.vm.Reinterpret(&r, 1024); err != nil {
		return nil
	}
	return &r
}
