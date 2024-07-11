package installer

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var HYPERVISOR_SCRIPTS = []string{
	"tinyrange_qemu.star",
}

func getDefaultTargetDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return filepath.Join(home, ".local", "bin")
	} else {
		return filepath.Join(home, "bin")
	}
}

func getExecutableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	} else {
		return name
	}
}

func copyFile(source string, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return nil
}

var (
	target     = flag.String("target", getDefaultTargetDirectory(), "specify the installation target directory to write binaries to")
	confirm    = flag.Bool("confirm", false, "silently install TinyRange automatically")
	hypervisor = flag.String("hypervisor", "", "select the hypervisor script to install")
)

func InstallerMain(tinyRangeBinary []byte, pkg2Binary []byte) error {
	flag.Parse()

	fmt.Printf("=== TinyRange %s Installer ===\n", buildinfo.VERSION)

	// Figure out what directory we will write to.
	if *target == "" {
		return fmt.Errorf("could not automatically determine target directory. please rerun with -target <dir>")
	}

	// Check for installer updates.

	hypervisorOptions := []struct {
		kind string
		name string
	}{}

	if *hypervisor == "" {
		// Figure out which hypervisor script we will install and get it ready.

		// Look for local scripts first next to the executable.
		exeDir, err := os.Executable()
		if err != nil {
			return err
		}

		exeDir = filepath.Dir(exeDir)

		for _, opt := range HYPERVISOR_SCRIPTS {
			filename := filepath.Join(exeDir, opt)
			if ok, _ := common.Exists(filename); ok {
				hypervisorOptions = append(hypervisorOptions, struct {
					kind string
					name string
				}{
					kind: "local",
					name: filename,
				})
			}
		}

		// TODO(joshua): Try and find online hypervisor scripts from Github.

		if len(hypervisorOptions) == 0 {
			return fmt.Errorf("could not find a hypervisor. please download a hypervisor script from https://github.com/tinyrange/tinyrange/")
		}
	}

	if !*confirm {
		buf := bufio.NewReader(os.Stdin)

		// Ask for user confirmation before we start.
		fmt.Printf("TinyRange will be installed to \"%s\".\n", *target)

		if *hypervisor == "" {
			fmt.Printf("Please select a hypervisor script from the following list:\n")

			for i, opt := range hypervisorOptions {
				fmt.Printf("  [%d] %s - %s\n", i, opt.name, opt.kind)
			}

			for {
				if len(hypervisorOptions) == 1 {
					fmt.Print("Which option should we install [0]: ")
				} else {
					fmt.Printf("Which option should we install [0-%d]: ", len(hypervisorOptions)-1)
				}

				response, err := buf.ReadString('\n')
				if err != nil {
					return err
				}

				response = strings.TrimSuffix(response, "\n")

				i, err := strconv.ParseInt(response, 10, 64)
				if err != nil {
					fmt.Printf("Invalid input: %s", err)
					continue
				}

				*hypervisor = hypervisorOptions[i].name

				break
			}
		} else {
			fmt.Printf("Using Hypervisor script \"%s\".\n", *hypervisor)
		}

		fmt.Printf("Installation is ready to start.\n")

		for {
			fmt.Print("Please type \"y\" or \"yes\" to confirm: ")
			response, err := buf.ReadString('\n')
			if err != nil {
				return err
			}

			response = strings.TrimSuffix(response, "\n")

			if response == "y" || response == "yes" {
				break
			}
		}
	} else {
		if *hypervisor == "" {
			return fmt.Errorf("no hypervisor specified. please rerun with -hypervisor <path>")
		}

		fmt.Printf("Installing TinyRange to \"%s\".\n", *target)
		fmt.Printf("Using Hypervisor script \"%s\".\n", *hypervisor)
	}

	// Install the hypervisor first.
	// This ensures that TinyRange will always be able to find a hypervisor and won't have errors.
	hypervisorPath := filepath.Join(*target, filepath.Base(*hypervisor))
	if err := copyFile(*hypervisor, hypervisorPath); err != nil {
		return err
	}

	// Extract the embedded binaries to the output directory.
	tinyRangePath := filepath.Join(*target, getExecutableName("tinyrange"))
	if err := os.WriteFile(tinyRangePath, tinyRangeBinary, os.FileMode(0755)); err != nil {
		return err
	}

	pkg2Path := filepath.Join(*target, getExecutableName("pkg2"))
	if err := os.WriteFile(pkg2Path, pkg2Binary, os.FileMode(0755)); err != nil {
		return err
	}

	fmt.Printf("TinyRange installed to %s.\n", *target)

	return nil
}
