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
	"runtime/pprof"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
)

const DEFAuLT_BUILDER = "alpine@3.20"

var (
	loginBuilder     string
	loginCpuCores    int
	loginMemorySize  int
	loginStorageSize int
	loginExec        string
	loginLoadConfig  string
	loginSaveConfig  string
	loginDebug       bool
	loginNoScripts   bool
	loginFiles       []string
	loginArchives    []string
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

		if loginLoadConfig != "" {
			f, err := os.Open(loginLoadConfig)
			if err != nil {
				return err
			}
			defer f.Close()

			def, err := builder.UnmarshalDefinition(f)
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			if _, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{
				AlwaysRebuild: true,
			}); err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			return nil
		} else {
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

			if loginNoScripts {
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

			if loginExec != "" {
				dir = append(dir, common.DirectiveRunCommand{Command: loginExec})
			} else {
				dir = append(dir, common.DirectiveRunCommand{Command: "interactive"})
			}

			def := builder.NewBuildVmDefinition(
				dir,
				nil, nil,
				"",
				loginCpuCores, loginMemorySize, loginStorageSize,
				"ssh", loginDebug,
			)

			if loginSaveConfig != "" {
				w, err := os.Create(loginSaveConfig)
				if err != nil {
					return err
				}
				defer w.Close()

				if err := builder.MarshalDefinition(w, def); err != nil {
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
	loginCmd.PersistentFlags().StringVar(&loginSaveConfig, "save-definition", "", "Serialize the definition to the specified filename.")
	loginCmd.PersistentFlags().StringVarP(&loginLoadConfig, "load-definition", "c", "", "Run a virtual machine from a serialized definition.")
	loginCmd.PersistentFlags().BoolVar(&loginNoScripts, "no-scripts", false, "Disable script execution.")
	loginCmd.PersistentFlags().StringArrayVarP(&loginFiles, "file", "f", []string{}, "Specify local files/URLs to be copied into the virtual machine. URLs will be downloaded to the build directory first.")
	loginCmd.PersistentFlags().StringArrayVarP(&loginArchives, "archive", "a", []string{}, "Specify archives to be copied into the virtual machine. A copy will be made in the build directory.")
	rootCmd.AddCommand(loginCmd)
}
