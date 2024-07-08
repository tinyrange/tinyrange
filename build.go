///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
)

type ZipArchive struct {
	writer *zip.Writer
	prefix string
}

func (z *ZipArchive) Close() error {
	return z.writer.Close()
}

func (z *ZipArchive) WriteFile(filename string, content []byte) error {
	f, err := z.writer.Create(z.prefix + filename)
	if err != nil {
		return err
	}

	_, err = f.Write(content)
	if err != nil {
		return err
	}

	return nil
}

func (z *ZipArchive) CopyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	stats, err := in.Stat()
	if err != nil {
		return err
	}

	hdr := &zip.FileHeader{
		Name:   z.prefix + dst,
		Method: zip.Deflate,
	}
	hdr.SetMode(stats.Mode())
	hdr.Modified = stats.ModTime()

	f, err := z.writer.CreateHeader(hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, in)
	if err != nil {
		return err
	}

	return nil
}

func (z *ZipArchive) CopyDirectory(src string, dst string) error {
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, ent := range ents {
		if ent.IsDir() {
			err := z.CopyDirectory(filepath.Join(src, ent.Name()), path.Join(dst, ent.Name()))
			if err != nil {
				return err
			}
		} else {
			err := z.CopyFile(filepath.Join(src, ent.Name()), path.Join(dst, ent.Name()))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func NewArchive(w io.Writer, prefix string) *ZipArchive {
	return &ZipArchive{writer: zip.NewWriter(w), prefix: prefix}
}

var (
	buildOs    = flag.String("os", runtime.GOOS, "Specify the operating system to build for.")
	buildArch  = flag.String("arch", runtime.GOARCH, "Specify the architecture to build for.")
	buildDir   = flag.String("buildDir", "build/", "Specify the build dir to write build outputs to.")
	releaseDir = flag.String("releaseDir", "release/", "Specify the directory to put releases in.")
	debug      = flag.Bool("debug", false, "Print executed commands.")
	release    = flag.Bool("release", false, "Generate a release package for the built operating system (supports Windows).")
)

func buildInitForTarget(buildArch string) error {
	if buildArch == "wasm" {
		buildArch = "amd64"
	}

	args := []string{
		"build",
		"-o", filepath.Join("pkg", "init", "init"),
	}

	args = append(args, "github.com/tinyrange/tinyrange/cmd/init")

	cmd := exec.Command("go", args...)

	cmd.Env = cmd.Environ()

	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, "GOOS=linux")
	cmd.Env = append(cmd.Env, "GOARCH="+buildArch)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if *debug {
		log.Printf("executing %v", cmd.Args)
	}

	log.Printf("Build init for target: linux/%s", buildArch)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func getTarget(buildDir string, buildOs string, name string) string {
	targetFilename := filepath.Join(buildDir, name)
	if buildOs == "windows" {
		targetFilename += ".exe"
	}
	return targetFilename
}

func buildTinyRangeForTarget(buildDir string, buildOs string, buildArch string) (string, error) {
	outputFilename := getTarget(buildDir, buildOs, "tinyrange")

	args := []string{
		"build",
		"-o", outputFilename,
	}

	args = append(args, "github.com/tinyrange/tinyrange/cmd/tinyrange")

	cmd := exec.Command("go", args...)

	cmd.Env = cmd.Environ()

	cmd.Env = append(cmd.Env, "GOOS="+buildOs)
	cmd.Env = append(cmd.Env, "GOARCH="+buildArch)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if *debug {
		log.Printf("executing %v", cmd.Args)
	}

	log.Printf("Build TinyRange for target: %s/%s", buildOs, buildArch)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return outputFilename, nil
}

func buildPkg2ForTarget(buildDir string, buildOs string, buildArch string) (string, error) {
	outputFilename := getTarget(buildDir, buildOs, "pkg2")

	args := []string{
		"build",
		"-o", outputFilename,
	}

	args = append(args, "github.com/tinyrange/tinyrange/cmd/pkg2")

	cmd := exec.Command("go", args...)

	cmd.Env = cmd.Environ()

	cmd.Env = append(cmd.Env, "GOOS="+buildOs)
	cmd.Env = append(cmd.Env, "GOARCH="+buildArch)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if *debug {
		log.Printf("executing %v", cmd.Args)
	}

	log.Printf("Build Pkg2 for target: %s/%s", buildOs, buildArch)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return outputFilename, nil
}

func buildReleaseForTarget(releaseDir string, buildDir string, buildOs string, buildArch string) (string, error) {
	err := os.MkdirAll(releaseDir, os.ModePerm)
	if err != nil {
		return "", err
	}

	releaseFilename := filepath.Join(releaseDir, fmt.Sprintf("tinyrange_%s_%s.zip", buildOs, buildArch))

	releaseArchive, err := os.Create(releaseFilename)
	if err != nil {
		return "", err
	}
	defer releaseArchive.Close()

	archive := NewArchive(releaseArchive, "tinyrange/")
	defer archive.Close()

	if err := buildInitForTarget(buildArch); err != nil {
		log.Fatal(err)
	}

	filename, err := buildTinyRangeForTarget(filepath.Join(buildDir, buildOs), buildOs, buildArch)
	if err != nil {
		return "", err
	}

	if buildOs == "windows" {
		if err := archive.CopyFile(filename, "tinyrange.exe"); err != nil {
			return "", err
		}
	} else {
		if err := archive.CopyFile(filename, "tinyrange"); err != nil {
			return "", err
		}
	}

	filename, err = buildPkg2ForTarget(filepath.Join(buildDir, buildOs), buildOs, buildArch)
	if err != nil {
		return "", err
	}

	if buildOs == "windows" {
		if err := archive.CopyFile(filename, "pkg2.exe"); err != nil {
			return "", err
		}
	} else {
		if err := archive.CopyFile(filename, "pkg2"); err != nil {
			return "", err
		}
	}

	// Create tinyrange.portable.
	if err := archive.WriteFile("tinyrange.portable", []byte("")); err != nil {
		return "", err
	}

	// Copy the tinyrange_qemu.star file.
	if err := archive.CopyFile(filepath.Join(buildDir, "tinyrange_qemu.star"), "tinyrange_qemu.star"); err != nil {
		return "", err
	}

	// if buildOs == "windows" {
	// 	if err := archive.CopyDirectory("release/tinyQemu/", "qemu/"); err != nil {
	// 		return "", err
	// 	}
	// }

	return releaseFilename, nil
}

func main() {
	flag.Parse()

	if err := os.Setenv("CGO_ENABLED", "0"); err != nil {
		log.Fatal(err)
	}

	if *release {
		log.Printf("Building Release for %s", *buildOs)
		releaseFilename, err := buildReleaseForTarget(*releaseDir, *buildDir, *buildOs, *buildArch)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Built release archive to: %s", releaseFilename)
	} else {
		log.Printf("Build init")
		if err := buildInitForTarget(*buildArch); err != nil {
			log.Fatal(err)
		}

		log.Printf("Build Pkg2")
		if _, err := buildPkg2ForTarget(*buildDir, *buildOs, *buildArch); err != nil {
			log.Fatal(err)
		}

		log.Printf("Build TinyRange")
		if _, err := buildTinyRangeForTarget(*buildDir, *buildOs, *buildArch); err != nil {
			log.Fatal(err)
		}
	}
}
