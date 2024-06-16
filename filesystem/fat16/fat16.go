package fat16

import (
	"encoding/binary"
	"fmt"
	"log/slog"

	vm "github.com/tinyrange/vm"
)

type fatType byte

const (
	fatType12 fatType = iota
	fatType16
	fatType32
	fatTypeExFat
)

func (typ fatType) String() string {
	switch typ {
	case fatType12:
		return "FAT12"
	case fatType16:
		return "FAT16"
	case fatType32:
		return "FAT32"
	case fatTypeExFat:
		return "ExFAT"
	default:
		panic("unimplemented")
	}
}

type directory struct {
	entries []DirectoryRecord
}

type Fat16Filesystem struct {
	bpb      BiosParameterBlock
	fatTable vm.RawRegion
	root     *directory
}

func (fs *Fat16Filesystem) sectorSize() uint16 {
	return fs.bpb.BytesPerSector()
}

func (fs *Fat16Filesystem) totalSectors() uint32 {
	if fs.bpb.TotalSectors16() == 0 {
		return fs.bpb.TotalSectors32()
	} else {
		return uint32(fs.bpb.TotalSectors16())
	}
}

func (fs *Fat16Filesystem) fatSizeSectors() uint16 {
	if fs.bpb.TableSize16() == 0 {
		panic("unimplemented")
	} else {
		return fs.bpb.TableSize16()
	}
}

func (fs *Fat16Filesystem) rootDirSectors() uint16 {
	return ((fs.bpb.RootDirectoryEntries() * 32) + (fs.bpb.BytesPerSector() - 1)) / fs.bpb.BytesPerSector()
}

func (fs *Fat16Filesystem) dataSectors() uint32 {
	return fs.totalSectors() - (uint32(fs.bpb.ReservedSectors()) + (uint32(fs.bpb.FatCount()) * uint32(fs.fatSizeSectors())) + uint32(fs.rootDirSectors()))
}

func (fs *Fat16Filesystem) totalClusters() uint32 {
	return fs.dataSectors() / uint32(fs.bpb.SectorsPerCluster())
}

func (fs *Fat16Filesystem) firstFatSector() uint32 {
	return uint32(fs.bpb.ReservedSectors())
}

func (fs *Fat16Filesystem) fsType() fatType {
	if fs.bpb.BytesPerSector() == 0 {
		return fatTypeExFat
	} else if fs.totalClusters() < 4085 {
		return fatType12
	} else if fs.totalClusters() < 65525 {
		return fatType16
	} else {
		return fatType32
	}
}

func (fs *Fat16Filesystem) firstDataSector() uint32 {
	return uint32(fs.bpb.ReservedSectors()) + (uint32(fs.bpb.FatCount()) * uint32(fs.fatSizeSectors())) + uint32(fs.rootDirSectors())
}

func (fs *Fat16Filesystem) firstRootDirectorySector() uint32 {
	return fs.firstDataSector() - uint32(fs.rootDirSectors())
}

func (fs *Fat16Filesystem) firstSectorOfCluster(cluster uint32) uint32 {
	return ((cluster - 2) * uint32(fs.bpb.SectorsPerCluster())) + fs.firstDataSector()
}

func (fs *Fat16Filesystem) getClusterInFat(activeCluster uint32) uint32 {
	if fs.fsType() == fatType12 {
		fatOffset := activeCluster + (activeCluster / 2)

		val := binary.LittleEndian.Uint16(fs.fatTable[fatOffset : fatOffset+2])

		if activeCluster&1 != 0 {
			return uint32(val >> 4)
		} else {
			return uint32(val & 0xfff)
		}
	} else if fs.fsType() == fatType16 {
		fatOffset := activeCluster * 2

		return uint32(binary.LittleEndian.Uint16(fs.fatTable[fatOffset : fatOffset+2]))
	} else {
		panic("fsType unimplemented")
	}
}

func (fs *Fat16Filesystem) clusterFromSector(sector uint32) uint32 {
	return sector / uint32(fs.bpb.SectorsPerCluster())
}

func MapFat16Filesystem(_vm *vm.VirtualMemory, off int64) (*Fat16Filesystem, error) {
	fs := &Fat16Filesystem{}

	// Map the BIOSParameterBlock
	if err := _vm.Reinterpret(&fs.bpb, off+0); err != nil {
		return nil, err
	}

	slog.Info("",
		"type", fs.fsType(),
		"bpb", fs.bpb,
		"firstRootDirectorySector", fs.firstRootDirectorySector(),
	)

	// Map the FAT
	fs.fatTable = make(vm.RawRegion, fs.fatSizeSectors()*fs.sectorSize())
	if err := _vm.Reinterpret(fs.fatTable, off+int64(fs.firstFatSector()*uint32(fs.sectorSize()))); err != nil {
		return nil, err
	}

	rootDirCluster := fs.clusterFromSector(fs.firstRootDirectorySector())

	// Go through and map all the directories and files.
	currentDirBytes := make([]byte, fs.sectorSize()*uint16(fs.bpb.SectorsPerCluster()))

	fs.root = &directory{}

	rootDirOffset := off + int64(fs.firstRootDirectorySector()*uint32(fs.sectorSize()))

outer:
	for {
		clusterOffset := 0

		// Read the root directory cluster.
		if _, err := _vm.ReadAt(currentDirBytes, rootDirOffset); err != nil {
			return nil, err
		}

		for {
			if clusterOffset >= len(currentDirBytes) {
				break
			}

			// Map unused records anyway.
			if currentDirBytes[clusterOffset] == 0x00 {
				break outer
			}

			ent := DirectoryRecord{}

			if err := _vm.Reinterpret(&ent, rootDirOffset); err != nil {
				return nil, err
			}

			fs.root.entries = append(fs.root.entries, ent)

			rootDirOffset += ent.Size()
			clusterOffset += int(ent.Size())
		}

		rootDirCluster = fs.getClusterInFat(rootDirCluster)

		if rootDirCluster >= 0xFF8 {
			break
		}

		rootDirOffset = int64(rootDirCluster) * int64(fs.bpb.SectorsPerCluster()) * int64(fs.sectorSize())
	}

	for _, ent := range fs.root.entries {
		slog.Info("", "ent", ent, "filename", string(ent[0:8]), "ext", string(ent[8:11]))
	}

	return nil, fmt.Errorf("not implemented")
}
