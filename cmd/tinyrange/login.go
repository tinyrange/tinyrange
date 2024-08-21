package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
)

const DEFAuLT_BUILDER = "alpine@3.20"

var (
	loginBuilder      string
	loginCpuCores     int
	loginMemorySize   int
	loginStorageSize  int
	loginExec         string
	loginDebug        bool
	loginNoScripts    bool
	loginFiles        []string
	loginArchives     []string
	loginOutput       string
	loginWriteRoot    string
	loginRunRoot      string
	loginRunContainer bool
)

func detectArchiveExtractor(base common.BuildDefinition, filename string) (common.BuildDefinition, error) {
	if builder.ReadArchiveSupportsExtracting(filename) {
		return builder.NewReadArchiveBuildDefinition(base, filename), nil
	} else {
		return nil, fmt.Errorf("no extractor for %s", filename)
	}
}

func sha256HashFromReader(r io.Reader) (string, error) {
	h := sha256.New()

	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256HashFromFile(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	return sha256HashFromReader(f)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Start a virtual machine with a builder and a list of packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootCpuProfile != "" {
			f, err := os.Create(rootCpuProfile)
			if err != nil {
				return err
			}
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		if loginRunContainer {
			if os.Getuid() == 0 {
				tmpDir, err := os.MkdirTemp(os.TempDir(), "tinyrange_rootfs_*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)

				if err := common.MountTempFilesystem(tmpDir); err != nil {
					return err
				}

				loginRunRoot = tmpDir
			} else {
				return common.EscalateToRoot()
			}
		}

		if loginRunRoot != "" {
			uid := os.Getuid()
			if uid != 0 {
				return fmt.Errorf("run-root needs to run as UID 0")
			}

			ents, err := os.ReadDir(loginRunRoot)
			if err != nil {
				return err
			}

			if len(ents) != 0 {
				return fmt.Errorf("run-root target is not empty")
			}
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		if loginBuilder == "list" {
			for name, builder := range db.ContainerBuilders {
				fmt.Printf(" - %s - %s\n", name, builder.DisplayName)
			}
			return nil
		}

		var pkgs []common.PackageQuery

		for _, arg := range args {
			q, err := common.ParsePackageQuery(arg)
			if err != nil {
				return err
			}

			pkgs = append(pkgs, q)
		}

		var dir []common.Directive

		if loginBuilder == "" {
			return fmt.Errorf("please specify a builder")
		}

		var tags common.TagList

		tags = append(tags, "level3", "defaults")

		if loginNoScripts || loginWriteRoot != "" {
			tags = append(tags, "noScripts")
		}

		planDirective, err := builder.NewPlanDefinition(loginBuilder, pkgs, tags)
		if err != nil {
			return err
		}

		dir = append(dir, planDirective)

		for _, filename := range loginFiles {
			if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
				parsed, err := url.Parse(filename)
				if err != nil {
					return err
				}

				base := path.Base(parsed.Path)

				dir = append(dir, common.DirectiveAddFile{
					Definition: builder.NewFetchHttpBuildDefinition(filename, 0),
					Filename:   path.Join("/root", base),
				})
			} else {
				absPath, err := filepath.Abs(filename)
				if err != nil {
					return err
				}

				dir = append(dir, common.DirectiveLocalFile{
					HostFilename: absPath,
					Filename:     path.Join("/root", filepath.Base(absPath)),
				})
			}
		}

		for _, filename := range loginArchives {
			var def common.BuildDefinition
			if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
				def = builder.NewFetchHttpBuildDefinition(filename, 0)

				parsed, err := url.Parse(filename)
				if err != nil {
					return err
				}

				filename = parsed.Path
			} else {
				hash, err := sha256HashFromFile(filename)
				if err != nil {
					return err
				}

				def = builder.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
					return os.Open(filename)
				})
			}

			ark, err := detectArchiveExtractor(def, filename)
			if err != nil {
				return err
			}

			dir = append(dir, common.DirectiveArchive{Definition: ark, Target: "/root"})
		}

		if loginWriteRoot != "" {
			dir = append(dir, common.DirectiveBuiltin{Name: "init", GuestFilename: "init"})

			def := builder.NewBuildFsDefinition(dir, "tar")

			ctx := db.NewBuildContext(def)

			f, err := db.Build(ctx, def, common.BuildOptions{})
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			fh, err := f.Open()
			if err != nil {
				return err
			}
			defer fh.Close()

			out, err := os.Create(path.Base(loginWriteRoot))
			if err != nil {
				return err
			}
			defer out.Close()

			if _, err := io.Copy(out, fh); err != nil {
				return err
			}

			return nil
		}

		if loginExec != "" {
			dir = append(dir, common.DirectiveRunCommand{Command: loginExec})
		} else {
			dir = append(dir, common.DirectiveRunCommand{Command: "interactive"})
		}

		if loginRunRoot != "" {
			dir = append(dir, common.DirectiveBuiltin{Name: "init", GuestFilename: "init"})

			def := builder.NewBuildFsDefinition(dir, "fragments")

			ctx := db.NewBuildContext(def)

			f, err := db.Build(ctx, def, common.BuildOptions{})
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			frags, err := builder.ParseFragmentsBuilderResult(f)
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			if err := frags.ExtractAndRunScripts(loginRunRoot); err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			return nil
		}

		def := builder.NewBuildVmDefinition(
			dir,
			nil, nil,
			loginOutput,
			loginCpuCores, loginMemorySize, loginStorageSize,
			"ssh", loginDebug,
		)

		if loginOutput != "" {
			ctx := db.NewBuildContext(def)

			f, err := db.Build(ctx, def, common.BuildOptions{})
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			fh, err := f.Open()
			if err != nil {
				return err
			}
			defer fh.Close()

			out, err := os.Create(path.Base(loginOutput))
			if err != nil {
				return err
			}
			defer out.Close()

			if _, err := io.Copy(out, fh); err != nil {
				return err
			}

			return nil
		} else {
			ctx := db.NewBuildContext(def)
			if _, err := db.Build(ctx, def, common.BuildOptions{
				AlwaysRebuild: true,
			}); err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			if common.IsVerbose() {
				ctx.DisplayTree()
			}

			return nil
		}
	},
}

func init() {
	loginCmd.PersistentFlags().StringVarP(&loginBuilder, "builder", "b", DEFAuLT_BUILDER, "The container builder used to construct the virtual machine.")
	loginCmd.PersistentFlags().IntVar(&loginCpuCores, "cpu", 1, "The number of CPU cores to allocate to the virtual machine.")
	loginCmd.PersistentFlags().IntVar(&loginMemorySize, "ram", 1024, "The amount of ram in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().IntVar(&loginStorageSize, "storage", 1024, "The amount of storage to allocate in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().BoolVar(&loginDebug, "debug", false, "Redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup.")
	loginCmd.PersistentFlags().StringVarP(&loginExec, "exec", "E", "", "Run a different command rather than dropping into a shell.")
	loginCmd.PersistentFlags().BoolVar(&loginNoScripts, "no-scripts", false, "Disable script execution.")
	loginCmd.PersistentFlags().StringArrayVarP(&loginFiles, "file", "f", []string{}, "Specify local files/URLs to be copied into the virtual machine. URLs will be downloaded to the build directory first.")
	loginCmd.PersistentFlags().StringArrayVarP(&loginArchives, "archive", "a", []string{}, "Specify archives to be copied into the virtual machine. A copy will be made in the build directory.")
	loginCmd.PersistentFlags().StringVarP(&loginOutput, "output", "o", "", "Write the specified file from the guest to the host.")
	loginCmd.PersistentFlags().StringVar(&loginWriteRoot, "write-root", "", "Write the root filesystem as a .tar.gz archive.")
	if runtime.GOOS == "linux" {
		if os.Getuid() == 0 {
			// Needs to be running as root.
			loginCmd.PersistentFlags().StringVar(&loginRunRoot, "run-root", "", "Extract the generated root filesystem to a given path and run init scripts. This should only be run in a fresh container filesystem.")
		}
		loginCmd.PersistentFlags().BoolVar(&loginRunContainer, "run-container", false, "use a user namespace to escalate to root privileges so run-root can be used. Also creates a tmpfs for run-root.")
	}
	rootCmd.AddCommand(loginCmd)
}
