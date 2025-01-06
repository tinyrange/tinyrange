///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
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

func (z *ZipArchive) CopyFromReader(filename string, r io.Reader) error {
	f, err := z.writer.Create(z.prefix + filename)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, r)
	if err != nil {
		return err
	}

	return nil
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

func crossArchToGoArch(crossArch string) string {
	switch crossArch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		panic("unknown cross architecture: " + crossArch)
	}
}

func buildInitForCross(crossArch string) error {
	args := []string{
		"build",
		"-o", filepath.Join("build", fmt.Sprintf("tinyrange_init_%s", crossArch)),
	}

	args = append(args, "github.com/tinyrange/tinyrange/cmd/init")

	cmd := exec.Command("go", args...)

	cmd.Env = cmd.Environ()

	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, "GOOS=linux")
	cmd.Env = append(cmd.Env, "GOARCH="+crossArchToGoArch(crossArch))

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if *debug {
		log.Printf("executing %v", cmd.Args)
	}

	log.Printf("Build cross init for target: linux/%s", crossArch)
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
		"-tags", "official",
	}

	args = append(args, "github.com/tinyrange/tinyrange")

	cmd := exec.Command("go", args...)

	cmd.Env = cmd.Environ()

	cmd.Env = append(cmd.Env, "GOOS="+buildOs)
	cmd.Env = append(cmd.Env, "GOARCH="+buildArch)
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")

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

func buildVMMForTarget(buildDir string, buildOs string, buildArch string, name string, cgo bool) (string, error) {
	outputFilename := getTarget(buildDir, buildOs, "tinyrange_"+name)

	args := []string{
		"build",
		"-o", outputFilename,
		"-tags", "official",
	}

	args = append(args, "github.com/tinyrange/tinyrange/cmd/tinyrange_"+name)

	cmd := exec.Command("go", args...)

	cmd.Env = cmd.Environ()

	cmd.Env = append(cmd.Env, "GOOS="+buildOs)
	cmd.Env = append(cmd.Env, "GOARCH="+buildArch)
	if !cgo {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if *debug {
		log.Printf("executing %v", cmd.Args)
	}

	log.Printf("Build VMM %s for target: %s/%s", name, buildOs, buildArch)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	if name == "vz" {
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("vz is only supported on darwin")
		}

		csCmd := exec.Command("codesign", "--entitlements", "tools/tinyrange.entitlements", "-s", "-", outputFilename)

		csCmd.Stdout = os.Stdout
		csCmd.Stderr = os.Stderr
		csCmd.Stdin = os.Stdin

		log.Printf("Signing tinyrange_vz with Virtualization Entitlements")
		err := csCmd.Run()
		if err != nil {
			log.Printf("Signing failed. The executable may already be signed.")
		}
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
		if _, err := os.Stat("pkg/buildinfo/commit.txt"); err == nil {
			return nil
		}

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

func runTest(filename string) error {
	cmd := exec.Command("build/tinyrange", "login", "-c", filename)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("Running Test: %+v", cmd.Args)

	return cmd.Run()
}

func runTests(filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}

	if info.IsDir() {
		ents, err := os.ReadDir(filename)
		if err != nil {
			return err
		}

		for _, ent := range ents {
			child := filepath.Join(filename, ent.Name())

			if err := runTests(child); err != nil {
				return err
			}
		}
	} else {
		if filepath.Ext(filename) == ".yml" {
			if err := runTest(filename); err != nil {
				return err
			}
		}
	}

	return nil
}

func buildRelease(buildOs string, buildArch string) error {
	if err := os.MkdirAll("release", os.ModePerm); err != nil {
		return err
	}

	archiveName := fmt.Sprintf("release/tinyrange-%s-%s.zip", buildOs, buildArch)

	f, err := os.Create(archiveName)
	if err != nil {
		return err
	}
	defer f.Close()

	archive := NewArchive(f, "tinyrange/")
	defer archive.Close()

	// copy tinyrange
	exeSuffix := ""
	if buildOs == "windows" {
		exeSuffix = ".exe"
	}

	targetDir, _, err := getTargetDir(*buildDir, buildOs, buildArch)
	if err != nil {
		return err
	}

	if err := archive.CopyFile(getTarget(targetDir, buildOs, "tinyrange"), "tinyrange"+exeSuffix); err != nil {
		return err
	}

	// create tinyrange.portable
	if err := archive.WriteFile("tinyrange.portable", []byte("")); err != nil {
		return err
	}

	// copy tinyrange_qemu to tinyqemu/tinyrange_qemu
	if err := archive.CopyFile(getTarget(targetDir, buildOs, "tinyrange_qemu"), "tinyqemu/tinyrange_qemu"+exeSuffix); err != nil {
		return err
	}

	// If this is windows and local/tinyqemu.zip exists, extract it to the archive.
	if buildOs == "windows" {
		if _, err := os.Stat("local/tinyqemu.zip"); err == nil {
			zf, err := os.Open("local/tinyqemu.zip")
			if err != nil {
				return err
			}
			defer zf.Close()

			fi, err := zf.Stat()
			if err != nil {
				return err
			}

			zr, err := zip.NewReader(zf, fi.Size())
			if err != nil {
				return err
			}

			for _, file := range zr.File {
				rc, err := file.Open()
				if err != nil {
					return err
				}
				defer rc.Close()

				if err := archive.CopyFromReader(file.Name, rc); err != nil {
					return err
				}
			}
		}
	}

	slog.Info("built release", "os", buildOs, "arch", buildArch, "archive", archiveName)

	return nil
}

type VMMInfo struct {
	Name           string
	SupportedPairs []string
	Cgo            bool
}

var vmmList = []VMMInfo{
	{
		Name: "qemu",
		SupportedPairs: []string{
			"windows/amd64",
			// "windows/arm64", Not supported yet without a special build of QEMU.
			"linux/amd64",
			"linux/arm64",
			"darwin/arm64",
			"darwin/amd64",
			"freebsd/amd64",
			"openbsd/amd64",
		},
	},
	{
		Name: "vz",
		SupportedPairs: []string{
			"darwin/arm64",
		},
		Cgo: true,
	},
}

var (
	buildOs   = flag.String("os", runtime.GOOS, "Specify the operating system to build for.")
	buildArch = flag.String("arch", runtime.GOARCH, "Specify the architecture to build for.")
	buildDir  = flag.String("buildDir", "build/", "Specify the build dir to write build outputs to.")
	cross     = flag.String("cross", "", "Specify another init executable architecture to build.")
	debug     = flag.Bool("debug", false, "Print executed commands.")
	run       = flag.Bool("run", false, "Run TinyRange with the remaining arguments.")
	test      = flag.String("test", "", "Run all .yml files in a subdirectory using TinyRange.")
	release   = flag.Bool("release", false, "Build a release version of TinyRange.")
	cgo       = flag.Bool("cgo", false, "Build VMMs that require CGO.")
)

func main() {
	flag.Parse()

	var buildVmmList []VMMInfo

	for _, vmm := range vmmList {
	inner:
		for _, pair := range vmm.SupportedPairs {
			if pair == fmt.Sprintf("%s/%s", *buildOs, *buildArch) && (!vmm.Cgo || *cgo) {
				buildVmmList = append(buildVmmList, vmm)
				break inner
			}
		}
	}

	if len(buildVmmList) == 0 {
		log.Fatalf("No VMMs supported for %s/%s", *buildOs, *buildArch)
	}

	if err := generateRev(); err != nil {
		log.Fatal(err)
	}

	if err := buildInitForTarget(*buildArch); err != nil {
		log.Fatal(err)
	}

	if *cross != "" {
		if err := buildInitForCross(*cross); err != nil {
			log.Fatal(err)
		}
	}

	target, _, err := getTargetDir(*buildDir, *buildOs, *buildArch)
	if err != nil {
		log.Fatal(err)
	}

	for _, vmm := range buildVmmList {
		if _, err := buildVMMForTarget(target, *buildOs, *buildArch, vmm.Name, vmm.Cgo); err != nil {
			log.Fatal(err)
		}
	}

	filename, err := buildTinyRangeForTarget(target, *buildOs, *buildArch)
	if err != nil {
		log.Fatal(err)
	}

	if *release {
		if err := buildRelease(*buildOs, *buildArch); err != nil {
			log.Fatal(err)
		}
	} else if *test != "" {
		if err := runTests(*test); err != nil {
			log.Fatal(err)
		}
	} else if *run {
		cmd := exec.Command(filename, flag.Args()...)

		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
	}
}
