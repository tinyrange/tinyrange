package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/go-github/github"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var (
	updateConfirm         bool
	updateDownload        string
	updateLocal           string
	updateForce           bool
	updateRevert          bool
	updateEmergencyRevert bool
)

func supportsUpdate() bool {
	return common.IsPortable()
}

func hasAutomaticRelease() bool {
	if runtime.GOARCH == "amd64" {
		return runtime.GOOS == "linux" || runtime.GOOS == "windows"
	} else if runtime.GOARCH == "arm64" {
		return runtime.GOOS == "linux" || runtime.GOOS == "darwin"
	} else {
		return false
	}
}

var expectedFilename = fmt.Sprintf("tinyrange-%s-%s.zip", runtime.GOOS, runtime.GOARCH)

func installLocalUpdate(filename string, deleteUpdate bool) error {
	if filepath.Base(filename) != expectedFilename {
		return fmt.Errorf("invalid filename %s expecting %s", filepath.Base(filename), expectedFilename)
	}

	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", filename, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("could not stat %s: %w", filename, err)
	}

	zipReader, err := zip.NewReader(f, info.Size())
	if err != nil {
		return fmt.Errorf("could not read zip file: %w", err)
	}

	// Extract the update to a new directory.
	updateDir := filepath.Join(rootBuildDir, "update")

	if err := common.Ensure(updateDir, 0755); err != nil {
		return fmt.Errorf("could not ensure update directory %s: %w", updateDir, err)
	}

	var newFiles []string

	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return fmt.Errorf("could not open %s: %w", file.Name, err)
		}
		defer fileReader.Close()

		newName := strings.TrimPrefix(file.Name, "tinyrange/")

		targetPath := filepath.Join(updateDir, newName)

		if newName != "tinyrange.portable" {
			newFiles = append(newFiles, newName)
		}

		if err := common.Ensure(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("could not ensure directory %s: %w", filepath.Dir(targetPath), err)
		}

		targetFile, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("could not create %s: %w", targetPath, err)
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return fmt.Errorf("could not copy %s to %s: %w", file.Name, targetPath, err)
		}

		if err := targetFile.Close(); err != nil {
			return fmt.Errorf("could not close %s: %w", targetPath, err)
		}

		if err := os.Chmod(targetPath, file.Mode()); err != nil {
			return fmt.Errorf("could not chmod %s: %w", targetPath, err)
		}
	}

	// Check that the new extracted version works.
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %w", err)
	}

	newExe := filepath.Join(updateDir, filepath.Base(executablePath))

	if ok, _ := common.Exists(newExe); !ok {
		return fmt.Errorf("could not find new executable %s", newExe)
	}

	slog.Info("staged new version", "path", updateDir)

	slog.Info("testing new version")

	args := []string{newExe, "login", "--buildDir", rootBuildDir, "-E", "echo \"New Version Tested\""}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Info("running new version", "args", args)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("new version failed to run: %w", err)
	}

	// Overwrite the current version with the new version backing up to the revert directory.

	revertDir := filepath.Join(rootBuildDir, "revert")

	if err := common.Ensure(revertDir, 0755); err != nil {
		return fmt.Errorf("could not ensure revert directory %s: %w", revertDir, err)
	}

	currentInstallDir := filepath.Dir(executablePath)

	var revertFiles []string

	for _, file := range newFiles {
		if ok, _ := common.Exists(filepath.Join(currentInstallDir, file)); ok {
			revertTarget := filepath.Join(revertDir, file)

			if err := common.Ensure(filepath.Dir(revertTarget), 0755); err != nil {
				return fmt.Errorf("could not ensure directory %s: %w", filepath.Dir(revertTarget), err)
			}

			if err := os.Rename(filepath.Join(currentInstallDir, file), revertTarget); err != nil {
				return fmt.Errorf("could not move %s to %s: %w", filepath.Join(currentInstallDir, file), revertTarget, err)
			}

			revertFiles = append(revertFiles, file)
		}

		target := filepath.Join(currentInstallDir, file)

		if err := common.Ensure(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("could not ensure directory %s: %w", filepath.Dir(target), err)
		}

		if err := os.Rename(filepath.Join(updateDir, file), target); err != nil {
			return fmt.Errorf("could not move %s to %s: %w", filepath.Join(updateDir, file), target, err)
		}
	}

	revertFilesJson, err := json.Marshal(&revertFiles)
	if err != nil {
		return fmt.Errorf("could not marshal revert files: %w", err)
	}

	if err := os.WriteFile(filepath.Join(revertDir, "revert.json"), revertFilesJson, 0644); err != nil {
		return fmt.Errorf("could not write revert file: %w", err)
	}

	slog.Info("finished installing new version")

	if deleteUpdate {
		slog.Info("deleting update file", "filename", filename)
		if err := os.Remove(filename); err != nil {
			return err
		}
	}

	return nil
}

func revertUpdate(targetInstallDir string) error {
	revertDir := filepath.Join(rootBuildDir, "revert")

	if ok, _ := common.Exists(filepath.Join(revertDir, "revert.json")); !ok {
		return fmt.Errorf("no previous version to revert to")
	}

	var revertFiles []string

	revertFile, err := os.Open(filepath.Join(revertDir, "revert.json"))
	if err != nil {
		return fmt.Errorf("could not open revert file: %w", err)
	}
	defer revertFile.Close()

	if err := json.NewDecoder(revertFile).Decode(&revertFiles); err != nil {
		return fmt.Errorf("could not decode revert file: %w", err)
	}

	for _, file := range revertFiles {
		revertFile := filepath.Join(revertDir, file)

		if ok, _ := common.Exists(revertFile); !ok {
			return fmt.Errorf("missing file %s", file)
		}

		info, err := os.Stat(revertFile)
		if err != nil {
			return fmt.Errorf("could not stat %s: %w", revertFile, err)
		}

		if err := common.CopyFile(revertFile, filepath.Join(targetInstallDir, file)); err != nil {
			return fmt.Errorf("could not copy %s to %s: %w", revertFile, filepath.Join(targetInstallDir, file), err)
		}

		if err := os.Chmod(filepath.Join(targetInstallDir, file), info.Mode()); err != nil {
			return fmt.Errorf("could not chmod %s: %w", filepath.Join(targetInstallDir, file), err)
		}
	}

	slog.Info("reverted to previous version")

	return nil
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Download the latest version of TinyRange",
	RunE: func(cmd *cobra.Command, args []string) error {
		var currentVersion buildinfo.VersionInfo

		if updateEmergencyRevert {
			updateRevert = true

			currentDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("could not get current directory: %w", err)
			}

			if ok, _ := common.Exists(filepath.Join(currentDir, "tinyrange.portable")); !ok {
				return fmt.Errorf("not a TinyRange installation")
			}

			rootBuildDir = filepath.Join(currentDir, "build")
		} else {
			if !supportsUpdate() && updateDownload == "" {
				return fmt.Errorf("cannot update installed versions of TinyRange")
			}

			if !hasAutomaticRelease() && updateDownload == "" {
				return fmt.Errorf("automatic updates are not supported on this platform (%s/%s)", runtime.GOOS, runtime.GOARCH)
			}

			if updateLocal != "" {
				return installLocalUpdate(updateLocal, false)
			}
		}

		if updateRevert {
			var targetInstallDir string
			if updateEmergencyRevert {
				var err error
				targetInstallDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("could not get current directory: %w", err)
				}
			} else {
				executablePath, err := os.Executable()
				if err != nil {
					return fmt.Errorf("could not get executable path: %w", err)
				}

				targetInstallDir = filepath.Dir(executablePath)
			}

			return revertUpdate(targetInstallDir)
		}

		slog.Info("checking for updates")

		outputFilename := filepath.Join(rootBuildDir, expectedFilename)

		if ok, _ := common.Exists(outputFilename); ok && !updateForce && updateDownload == "" {
			slog.Info("found already downloaded update", "filename", outputFilename)

			if updateConfirm {
				slog.Info("installing already downloaded update")

				return installLocalUpdate(outputFilename, true)
			} else {
				slog.Info("use --confirm to install the update")
			}

			return nil
		}

		if updateDownload == "" {
			if !updateForce {
				currentVersion = buildinfo.Version()
				if currentVersion.IsDev() {
					return fmt.Errorf("cannot update dev version")
				}
				slog.Info("current", "version", currentVersion)
			} else {
				slog.Warn("forcing update to new version")
			}
		} else {
			slog.Info("downloading to", "directory", updateDownload)
		}

		// get the list of releases from github.
		client := github.NewClient(nil)

		releases, _, err := client.Repositories.ListReleases(context.Background(), "tinyrange", "tinyrange", &github.ListOptions{})
		if err != nil {
			return err
		}

		// find the latest release.
		var latestRelease *github.RepositoryRelease

		var newVersion = currentVersion

		for _, release := range releases {
			if release.GetDraft() {
				continue
			}

			if release.GetPrerelease() {
				continue
			}

			if latestRelease == nil {
				latestRelease = release
			}

			tag := release.GetTagName()

			version, err := buildinfo.ParseVersion(tag)
			if err != nil {
				slog.Warn("invalid release tag", "tag", tag, "error", err)
				continue
			}

			if version.GreaterThan(newVersion) {
				latestRelease = release
				newVersion = version
			}
		}

		if newVersion == currentVersion {
			slog.Info("already up to date")
			return nil
		}

		if updateDownload == "" {
			slog.Info("found new version", "version", newVersion)

			// If the user has not confirmed the update, then we will not download it.
			if !updateConfirm {
				slog.Info("use --confirm to update")
				return nil
			}
		}

		for _, asset := range latestRelease.Assets {
			if asset.GetName() == expectedFilename {
				slog.Info("downloading", "url", asset.GetURL())

				resp, redirectUrl, err := client.Repositories.DownloadReleaseAsset(context.Background(), "tinyrange", "tinyrange", asset.GetID())
				if err != nil {
					return err
				}

				if redirectUrl != "" {
					slog.Info("redirected", "url", redirectUrl)

					redirected, err := http.Get(redirectUrl)
					if err != nil {
						return err
					}

					if redirected.StatusCode != http.StatusOK {
						return fmt.Errorf("unexpected status code %d", redirected.StatusCode)
					}

					resp = redirected.Body
				}

				if updateDownload != "" {
					outputFilename = filepath.Join(updateDownload, expectedFilename)
				}

				out, err := os.Create(outputFilename + ".tmp")
				if err != nil {
					return err
				}

				pb := progressbar.DefaultBytes(int64(asset.GetSize()), "downloading")
				defer pb.Clear()

				if _, err := io.Copy(io.MultiWriter(out, pb), resp); err != nil {
					return err
				}

				if err := out.Close(); err != nil {
					return err
				}

				if err := os.Rename(outputFilename+".tmp", outputFilename); err != nil {
					return err
				}

				if updateDownload != "" {
					slog.Info("downloaded", "filename", outputFilename)

					return nil
				} else {
					return installLocalUpdate(outputFilename, true)
				}
			}
		}

		return nil
	},
}

func init() {
	updateCmd.PersistentFlags().BoolVar(&updateConfirm, "confirm", false, "Confirm the update")
	updateCmd.PersistentFlags().StringVar(&updateDownload, "download", "", "Download the new version but do not install it. It will be downloaded to this directory")
	updateCmd.PersistentFlags().StringVar(&updateLocal, "local", "", "Install the update from a local file")
	updateCmd.PersistentFlags().BoolVar(&updateForce, "force", false, "Force the update")
	updateCmd.PersistentFlags().BoolVar(&updateRevert, "revert", false, "Revert to the previously installed version")
	updateCmd.PersistentFlags().BoolVar(&updateEmergencyRevert, "emergency-revert", false, "Revert to the previously installed version in the current directory")
	rootCmd.AddCommand(updateCmd)
}
