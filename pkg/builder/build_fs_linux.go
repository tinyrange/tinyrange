//go:build linux

package builder

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"golang.org/x/sys/unix"
)

func writeFile(ent filesystem.Entry, name string) error {
	in, err := ent.Open()
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer in.Close()

	out, err := os.Create(name)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (frags *FragmentsBuilderResult) ExtractAndRunScripts(target string) error {
	var commands []string
	var deferredRuns []func() error
	for _, frag := range frags.Fragments {
		if ark := frag.Archive; ark != nil {
			f := filesystem.NewLocalFile(ark.HostFilename, nil)

			archive, err := filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return fmt.Errorf("failed to read archive: %w", err)
			}

			entries, err := archive.Entries()
			if err != nil {
				return fmt.Errorf("failed to read archive: %w", err)
			}

			for _, ent := range entries {
				name := filepath.Join(target, ark.Target, ent.Name())

				if ok, _ := common.Exists(name); ok {
					continue
				}

				switch ent.Typeflag() {
				case filesystem.TypeDirectory:
					// slog.Info("directory", "name", name)
					name = strings.TrimSuffix(name, "/")
					if err := os.Mkdir(name, os.ModePerm); err != nil {
						return fmt.Errorf("failed to mkdir %s: %w", name, err)
					}
				case filesystem.TypeSymlink:
					// slog.Info("symlink", "name", name)
					if err := os.Symlink(ent.Linkname(), name); err != nil {
						return fmt.Errorf("failed to symlink in guest: %w", err)
					}
				case filesystem.TypeLink:
					// slog.Info("link", "name", name)
					if err := os.Link(filepath.Join(target, ark.Target, ent.Linkname()), name); err != nil {
						return fmt.Errorf("failed to link in guest: %w", err)
					}
				case filesystem.TypeRegular:
					// slog.Info("reg", "name", name)
					if err := writeFile(ent, name); err != nil {
						return err
					}
				default:
					return fmt.Errorf("unimplemented entry type: %s", ent.Typeflag())
				}

				deferredRuns = append(deferredRuns, func() error {
					name := filepath.Join(ark.Target, ent.Name())

					// ignore errors
					os.Chown(name, ent.Uid(), ent.Gid())
					os.Chmod(name, ent.Mode())

					return nil
				})
			}
		} else if cmd := frag.RunCommand; cmd != nil {
			commands = append(commands, cmd.Command)
		} else if builtin := frag.Builtin; builtin != nil {
			if builtin.Name == "init" {
				if err := os.WriteFile(
					filepath.Join(target, builtin.GuestFilename),
					initExec.INIT_EXECUTABLE,
					os.FileMode(0755),
				); err != nil {
					return fmt.Errorf("failed to write init executable")
				}
			} else {
				return fmt.Errorf("unknown builtin: %s", builtin.Name)
			}
		} else {
			return fmt.Errorf("unknown fragment kind: %+v", frag)
		}
	}

	for _, bind := range []string{"/proc", "/sys", "/dev"} {
		if err := common.Ensure(target+bind, os.FileMode(0755)); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", target+bind, err)
		}
		if err := unix.Mount(bind, target+bind, "auto", unix.MS_RDONLY|unix.MS_BIND|unix.MS_REC, ""); err != nil {
			return fmt.Errorf("failed to bind mount %s to %s: %w", bind, target+bind, err)
		}
	}

	// Chroot to the target.
	if err := unix.Chroot(target); err != nil {
		return fmt.Errorf("failed to chroot: %w", err)
	}

	// Chdir to the new root.
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir: %w", err)
	}

	// Run all the deferred runs.
	for _, deferred := range deferredRuns {
		if err := deferred(); err != nil {
			return fmt.Errorf("failed to call deferred: %w", err)
		}
	}

	// Run each command in the new root.
	for _, cmd := range commands {
		if err := common.RunCommand(cmd); err != nil {
			return fmt.Errorf("failed to run command %s: %w", cmd, err)
		}
	}

	return nil
}
