package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/database"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	"github.com/tinyrange/tinyrange/pkg/tinyrange"
	"gopkg.in/yaml.v3"
)

func parseOciImageName(name string) *builder.FetchOciImageDefinition {
	name, tag, ok := strings.Cut(name, ":")

	if !ok {
		tag = "latest"
	}

	return builder.NewFetchOCIImageDefinition(
		builder.DEFAULT_REGISTRY,
		name,
		tag,
		"amd64",
	)
}

func runWithCommandLineConfig(buildDir string, rebuild bool, image string, execCommand string, cpuCores int, memoryMb int, storageSize int) error {
	db := database.New(buildDir)

	fragments := []config.Fragment{
		{Builtin: &config.BuiltinFragment{Name: "init", GuestFilename: "/init"}},
		{Builtin: &config.BuiltinFragment{Name: "init.star", GuestFilename: "/init.star"}},
	}

	{
		def := parseOciImageName(image)

		ctx := db.NewBuildContext(def)

		res, err := db.Build(ctx, def, common.BuildOptions{
			AlwaysRebuild: rebuild,
		})
		if err != nil {
			return err
		}

		if err := builder.ParseJsonFromFile(res, &def); err != nil {
			return err
		}

		for _, hash := range def.LayerArchives {
			filename, err := ctx.FilenameFromDigest(hash)
			if err != nil {
				return err
			}

			fragments = append(fragments, config.Fragment{Archive: &config.ArchiveFragment{HostFilename: filename}})
		}
	}

	if execCommand != "" {
		initJson, err := json.Marshal(&struct {
			SSHCommand []string `json:"ssh_command"`
		}{
			SSHCommand: []string{"/bin/sh", "-c", execCommand},
		})
		if err != nil {
			return err
		}

		fragments = append(fragments, config.Fragment{FileContents: &config.FileContentsFragment{GuestFilename: "/init.json", Contents: initJson}})
	}

	kernelFilename := ""

	{
		def := builder.NewFetchHttpBuildDefinition(builder.OFFICIAL_KERNEL_URL, 0)

		ctx := db.NewBuildContext(def)

		res, err := db.Build(ctx, def, common.BuildOptions{
			AlwaysRebuild: rebuild,
		})
		if err != nil {
			return err
		}

		kernelFilename, err = ctx.FilenameFromDigest(res.Digest())
		if err != nil {
			return err
		}
	}

	hypervisorScript, err := common.GetAdjacentExecutable("tinyrange_qemu.star")
	if err != nil {
		return err
	}

	return tinyrange.RunWithConfig(buildDir, config.TinyRangeConfig{
		HypervisorScript: hypervisorScript,
		KernelFilename:   kernelFilename,
		CPUCores:         cpuCores,
		MemoryMB:         memoryMb,
		RootFsFragments:  fragments,
		StorageSize:      storageSize,
		Interaction:      "ssh",
	}, *debug, false, "", "")
}

var (
	cpuCores         = flag.Int("cpu-cores", 1, "set the number of cpu cores in the VM")
	memoryMb         = flag.Int("memory", 1024, "set the number of megabytes of RAM in the VM")
	storageSize      = flag.Int("storage-size", 512, "the size of the VM storage in megabytes")
	image            = flag.String("image", "library/alpine:latest", "the OCI image to boot inside the virtual machine")
	configFile       = flag.String("config", "", "passes a custom config. this overrides all other flags.")
	debug            = flag.Bool("debug", false, "redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup")
	buildDir         = flag.String("build-dir", common.GetDefaultBuildDir(), "the directory to build definitions to")
	rebuild          = flag.Bool("rebuild", false, "always rebuild the kernel and image definitions")
	execCommand      = flag.String("exec", "", "if set then run a command rather than creating a login shell")
	printVersion     = flag.Bool("version", false, "print the version information")
	exportFilesystem = flag.String("export-filesystem", "", "write the filesystem to the host filesystem")
	listenNbd        = flag.String("listen-nbd", "", "Listen with an NBD server on the given address and port")
)

func tinyRangeMain() error {
	flag.Parse()

	if *printVersion {
		fmt.Printf("TinyRange version: %s\nThe University of Queensland\n", buildinfo.VERSION)
		return nil
	}

	if err := common.Ensure(*buildDir, fs.ModePerm); err != nil {
		return fmt.Errorf("failed to create build dir: %w", err)
	}

	if *configFile != "" {
		f, err := os.Open(*configFile)
		if err != nil {
			return err
		}
		defer f.Close()

		var cfg config.TinyRangeConfig

		if strings.HasSuffix(f.Name(), ".json") {

			dec := json.NewDecoder(f)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}
		} else if strings.HasSuffix(f.Name(), ".yml") {
			dec := yaml.NewDecoder(f)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}
		}

		return tinyrange.RunWithConfig(*buildDir, cfg, *debug, false, *exportFilesystem, *listenNbd)
	} else {
		return runWithCommandLineConfig(*buildDir, *rebuild, *image, *execCommand, *cpuCores, *memoryMb, *storageSize)
	}
}

func main() {
	if err := tinyRangeMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
