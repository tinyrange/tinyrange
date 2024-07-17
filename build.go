///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
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
	buildOs   = flag.String("os", runtime.GOOS, "Specify the operating system to build for.")
	buildArch = flag.String("arch", runtime.GOARCH, "Specify the architecture to build for.")
	buildDir  = flag.String("buildDir", "build/", "Specify the build dir to write build outputs to.")
	debug     = flag.Bool("debug", false, "Print executed commands.")
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

func buildTinyRange2ForTarget(buildDir string, buildOs string, buildArch string) (string, error) {
	outputFilename := getTarget(buildDir, buildOs, "tinyrange2")

	args := []string{
		"build",
		"-o", outputFilename,
	}

	args = append(args, "github.com/tinyrange/tinyrange/cmd/tinyrange2")

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

	log.Printf("Build TinyRange2 for target: %s/%s", buildOs, buildArch)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return outputFilename, nil
}

func buildInstallerForTarget(buildDir string, targetName string, buildOs string, buildArch string) (string, error) {
	outputFilename := getTarget(buildDir, buildOs, fmt.Sprintf("tinyrange_installer_%s_%s", buildOs, buildArch))

	args := []string{
		"build",
		"-o", outputFilename,
	}

	if targetName != "" {
		args = append(args, fmt.Sprintf("github.com/tinyrange/tinyrange/build/%s", targetName))
	} else {
		args = append(args, "github.com/tinyrange/tinyrange/build")
	}

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

	log.Printf("Build Installer for target: %s/%s", buildOs, buildArch)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return outputFilename, nil
}

func getTargetDir(buildDir string, targetOs string, targetArch string) (string, string, error) {
	if targetOs == runtime.GOOS && targetArch == runtime.GOARCH {
		return buildDir, "", nil
	}

	targetName := fmt.Sprintf("cross-%s-%s", targetOs, targetArch)

	newDir := filepath.Join(buildDir, targetName)

	err := os.MkdirAll(newDir, os.ModePerm)
	if err != nil {
		return "", "", fmt.Errorf("failed to create directory: %v", err)
	}

	return newDir, targetName, nil
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

func generateRev() error {
	out := exec.Command("git", "describe", "--tags", "--dirty")

	buf := new(bytes.Buffer)

	out.Stdout = buf
	out.Stderr = os.Stderr
	out.Stdin = os.Stdin

	if *debug {
		log.Printf("executing %v", out.Args)
	}

	err := out.Run()
	if err != nil {
		log.Printf("git describe --tags failed. Writing fallback")

		err := os.WriteFile("pkg/buildinfo/commit.txt", []byte("nongit"), os.ModePerm)
		if err != nil {
			return err
		}

		return nil
	}

	f, err := os.Create("pkg/buildinfo/commit.txt")
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s", strings.Trim(buf.String(), "\n\r"))
	if err != nil {
		return err
	}

	return nil
}

func main() {
	flag.Parse()

	if err := os.Setenv("CGO_ENABLED", "0"); err != nil {
		log.Fatal(err)
	}

	if err := generateRev(); err != nil {
		log.Fatal(err)
	}

	if err := buildInitForTarget(*buildArch); err != nil {
		log.Fatal(err)
	}

	target, targetName, err := getTargetDir(*buildDir, *buildOs, *buildArch)
	if err != nil {
		log.Fatal(err)
	}
	if targetName != "" {
		if *buildOs == "windows" {
			if err := copyFile(
				filepath.Join(*buildDir, "installer_windows.go"),
				filepath.Join(target, "installer.go"),
			); err != nil {
				log.Fatal(err)
			}
		} else {
			if err := copyFile(
				filepath.Join(*buildDir, "installer.go"),
				filepath.Join(target, "installer.go"),
			); err != nil {
				log.Fatal(err)
			}
		}
	}

	if _, err := buildPkg2ForTarget(target, *buildOs, *buildArch); err != nil {
		log.Fatal(err)
	}

	if _, err := buildTinyRangeForTarget(target, *buildOs, *buildArch); err != nil {
		log.Fatal(err)
	}

	if _, err := buildTinyRange2ForTarget(target, *buildOs, *buildArch); err != nil {
		log.Fatal(err)
	}

	if _, err := buildInstallerForTarget(target, targetName, *buildOs, *buildArch); err != nil {
		log.Fatal(err)
	}
}
