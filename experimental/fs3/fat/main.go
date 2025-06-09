package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/pkg/log"
)

type FATKind string

const (
	InvalidKind FATKind = "Invalid"
	FAT12       FATKind = "FAT12"
	FAT16       FATKind = "FAT16"
	FAT32       FATKind = "FAT32"
	ExFAT       FATKind = "ExFAT"
)

type FATFilesystem struct {
	reader io.ReaderAt

	bpb BiosParameterBlock
}

func (fs *FATFilesystem) sectorSize() int {
	return int(fs.bpb.GetBytesPerSector())
}

func (fs *FATFilesystem) totalSectors() int {
	if fs.bpb.GetTotalSectors16() != 0 {
		return int(fs.bpb.GetTotalSectors16())
	} else {
		return int(fs.bpb.GetTotalSectors32())
	}
}

func (fs *FATFilesystem) rootDirSectors() int {
	return int(((fs.bpb.GetRootDirectoryEntries() * 32) + (fs.bpb.GetBytesPerSector() - 1)) / fs.bpb.GetBytesPerSector())
}

func (fs *FATFilesystem) firstDataSector() int {
	return int(fs.bpb.GetReservedSectors()) + (int(fs.bpb.GetTableCount()) * fs.fatSize()) + fs.rootDirSectors()
}

func (fs *FATFilesystem) fatSize() int {
	if fs.bpb.GetTableSize16() != 0 {
		return int(fs.bpb.GetTableSize16())
	} else {
		return int(fs.bpb.GetExtendedBootRecord().GetFat32().GetTableSize32())
	}
}

func (fs *FATFilesystem) dataSectors() int {
	return fs.totalSectors() - (int(fs.bpb.GetReservedSectors()) + (int(fs.bpb.GetTableCount()) * fs.fatSize()) + fs.rootDirSectors())
}

func (fs *FATFilesystem) totalClusters() int {
	return fs.dataSectors() / int(fs.bpb.GetSectorsPerCluster())
}

func (fs *FATFilesystem) firstRootDirSector() int {
	return fs.firstDataSector() - fs.rootDirSectors()
}

func (fs *FATFilesystem) Kind() FATKind {
	if fs.sectorSize() == 0 {
		return ExFAT
	}

	totalClusters := fs.totalClusters()

	if totalClusters < 4085 {
		return FAT12
	} else if totalClusters < 65525 {
		return FAT16
	} else {
		return FAT32
	}
}

func NewFATFilesystem(reader io.ReaderAt) (*FATFilesystem, error) {
	// parse the boot sector

	var bpb BiosParameterBlock
	if _, err := io.ReadFull(io.NewSectionReader(reader, 0, bpb.Size()), bpb[:]); err != nil {
		return nil, fmt.Errorf("failed to read boot sector: %w", err)
	}

	// check the boot sector signature
	if bpb.GetSignature() != 0xAA55 {
		return nil, fmt.Errorf("invalid boot sector signature")
	}

	ret := &FATFilesystem{
		reader: reader,
		bpb:    bpb,
	}

	kind := ret.Kind()
	if kind == InvalidKind {
		return nil, fmt.Errorf("unsupported FAT kind")
	}

	slog.Default().Info("FAT filesystem",
		"kind", kind,
		"sectorSize", ret.sectorSize(),
		"totalSectors", ret.totalSectors(),
		"rootDirSectors", ret.rootDirSectors(),
		"firstDataSector", ret.firstDataSector(),
		"fatSize", ret.fatSize(),
		"dataSectors", ret.dataSectors(),
		"totalClusters", ret.totalClusters(),
		"firstRootDirSector", ret.firstRootDirSector(),
	)

	return ret, nil
}

var (
	input = flag.String("input", "", "input file")
)

func appMain() error {
	flag.Parse()

	if *input == "" {
		return fmt.Errorf("input file is required")
	}

	f, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer f.Close()

	fs, err := NewFATFilesystem(f)
	if err != nil {
		return fmt.Errorf("failed to open FAT filesystem: %w", err)
	}

	_ = fs

	return fmt.Errorf("not implemented")
}

func main() {
	if err := appMain(); err != nil {
		log.Default().Error("fatal", "error", err)
		os.Exit(1)
	}
}
