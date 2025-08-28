///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const PACKAGE_NAME = "github.com/tinyrange/tinyrange"

var QEMU_VERSION = "v9.2.1"
var QEMU_REPO = "https://github.com/tinyrange/qemu_autobuild"

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

func buildInitForTarget(buildOs string, buildArch string) error {
	if buildOs == "linux" {
		// init is embedded inside the main TinyRange executable on Linux.
		return nil
	}

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
	if buildOs == "windows" && !strings.HasSuffix(targetFilename, ".exe") {
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

func getTargetDir(buildDir string, targetOs string, targetArch string) (newBuildDir string, err error) {
	if targetOs == runtime.GOOS && targetArch == runtime.GOARCH {
		return buildDir, nil
	}

	targetName := fmt.Sprintf("cross-%s-%s", targetOs, targetArch)

	newDir := filepath.Join(buildDir, targetName)

	err = os.MkdirAll(newDir, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create directory: %v", err)
	}

	return newDir, nil
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

func executeOptions(options []string) ([]string, error) {
	var ret []string

	for _, opt := range options {
		// Remove any trailing \r characters and trim whitespace.
		opt = strings.TrimSuffix(strings.TrimSpace(opt), "\r")

		if len(opt) == 0 {
			continue
		}

		slog.Info("Executing option", "option", opt)

		if strings.HasPrefix(opt, "%mkdir ") {
			dir := strings.TrimPrefix(opt, "%mkdir ")
			if err := os.MkdirAll(dir, os.ModePerm); err != nil {
				return nil, err
			}
		} else if strings.HasPrefix(opt, "%rmdir ") {
			dir := strings.TrimPrefix(opt, "%rmdir ")
			if err := os.RemoveAll(dir); err != nil {
				return nil, err
			}
		} else if strings.HasPrefix(opt, "%writeFile ") {
			parts := strings.SplitN(strings.TrimPrefix(opt, "%writeFile "), " ", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid writeFile option: %s", opt)
			}

			if err := os.WriteFile(parts[0], []byte(parts[1]), os.ModePerm); err != nil {
				return nil, err
			}
		} else {
			ret = append(ret, opt)
		}
	}

	return ret, nil
}

func runTest(filename string, verbose bool, experimental string) error {
	args := []string{
		"build/tinyrange",
		"login",
		"-c",
		filename,
	}

	if verbose {
		args = append(args, "--verbose")
	}

	if experimental != "" {
		for _, exp := range strings.Split(experimental, ",") {
			args = append(args, "--experimental", exp)
		}
	}

	slog.Info("Running test", "filename", filename)

	optsFile := filepath.Join(filepath.Dir(filename), "test.opts")
	if exists, err := os.Stat(optsFile); err == nil && exists.Mode().IsRegular() {
		optsContents, err := os.ReadFile(optsFile)
		if err != nil {
			return fmt.Errorf("failed to read options file: %w", err)
		}

		opts, err := executeOptions(strings.Split(string(optsContents), "\n"))
		if err != nil {
			return fmt.Errorf("failed to execute options: %w", err)
		}

		args = append(args, opts...)
	}

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("Running Test: %+v", cmd.Args)

	return cmd.Run()
}

func runTests(filename string, verbose bool, experimental string) error {
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

			if err := runTests(child, verbose, experimental); err != nil {
				return err
			}
		}
	} else {
		if filepath.Ext(filename) == ".yml" {
			if err := runTest(filename, verbose, experimental); err != nil {
				return err
			}
		}
	}

	return nil
}

func buildRelease(buildOs string, buildArch string, cgo bool) error {
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

	targetDir, err := getTargetDir(*buildDir, buildOs, buildArch)
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
	// If there's a QEMU prebuilt for this OS, copy it to the archive.
	if qemuInfo, ok := qemuExecutables[fmt.Sprintf("%s/%s", buildOs, buildArch)]; ok {
		if err := archive.CopyFile(getTarget(targetDir, buildOs, qemuInfo.executableName), "tinyqemu/"+qemuInfo.executableName); err != nil {
			return err
		}
	}

	if buildOs == "darwin" && cgo {
		if err := archive.CopyFile(getTarget(targetDir, buildOs, "tinyrange_vz"), "tinyrange_vz"+exeSuffix); err != nil {
			return err
		}
	}

	slog.Info("built release", "os", buildOs, "arch", buildArch, "archive", archiveName)

	return nil
}

type qemuInfo struct {
	downloadName   string
	executableName string
}

var qemuExecutables = map[string]qemuInfo{
	"linux/amd64":   {"qemu-linux-amd64", "qemu-system-x86_64"},
	"linux/arm64":   {"qemu-linux-arm64", "qemu-system-aarch64"},
	"darwin/arm64":  {"qemu-darwin-arm64", "qemu-system-aarch64"},
	"windows/amd64": {"qemu-windows-amd64.exe", "qemu-system-x86_64.exe"},
}

type simpleDownloadProgress struct {
	total         int64
	contentLength int64
	lastPrint     time.Time
}

func (s *simpleDownloadProgress) Write(p []byte) (int, error) {
	s.total += int64(len(p))
	if time.Since(s.lastPrint) > 100*time.Millisecond || s.total == s.contentLength {
		fmt.Printf("\rDownloading QEMU: % 10d/% 10d (% 3.2f)", s.total, s.contentLength, float64(s.total)/float64(s.contentLength)*100)
		s.lastPrint = time.Now()
	}
	return len(p), nil
}

func ensureQemu(version string, repo string, targetDir string, buildOs string, buildArch string) error {
	qemuInfo, ok := qemuExecutables[fmt.Sprintf("%s/%s", buildOs, buildArch)]
	if !ok {
		return fmt.Errorf("no QEMU prebuilt for %s/%s", buildOs, buildArch)
	}

	qemuPath := filepath.Join(targetDir, qemuInfo.executableName)
	if _, err := os.Stat(qemuPath); err == nil {
		return nil
	}

	// download qemu
	downloadUrl := fmt.Sprintf("%s/releases/download/%s/%s", repo, version, qemuInfo.downloadName)

	resp, err := http.Get(downloadUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download QEMU: %s", resp.Status)
	}

	out, err := os.Create(qemuPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// make QEMU executable
	if err := out.Chmod(0755); err != nil {
		return err
	}

	log.Printf("Downloading QEMU %s for %s/%s", version, buildOs, buildArch)

	if _, err := io.Copy(io.MultiWriter(&simpleDownloadProgress{contentLength: resp.ContentLength}, out), resp.Body); err != nil {
		return err
	}

	fmt.Println()

	log.Printf("Finished Downloading QEMU %s for %s/%s", version, buildOs, buildArch)

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
			"netbsd/amd64",
			"illumos/amd64",
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

func runExperimentalScripts(basePath string, exp string) error {
	expPath := filepath.Join("experimental", exp)

	buildFile, err := os.ReadFile(filepath.Join(basePath, expPath, "build"))
	if err != nil {
		return err
	}

	buildFileLines := strings.Split(string(buildFile), "\n")

	for _, line := range buildFileLines {
		if strings.HasPrefix(line, "#") {
			continue
		}

		tokens := strings.Split(line, " ")
		if len(tokens) == 0 {
			continue
		}

		if tokens[0] == "run" {
			args := []string{"go", "run", PACKAGE_NAME + "/" + tokens[1]}

			args = append(args, tokens[2:]...)

			log.Printf("Running Experimental Script: %v", args)

			cmd := exec.Command(args[0], args[1:]...)

			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Dir = filepath.Join(basePath, expPath)

			if err := cmd.Run(); err != nil {
				return err
			}
		} else {
			log.Printf("Unknown command: %s", tokens[0])
		}
	}

	return nil
}

func getBasePath() (string, error) {
	basePath, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// check if go.mod exists and if not, go up one directory until found
	for {
		if _, err := os.Stat(filepath.Join(basePath, "go.mod")); err == nil {
			break
		}

		if basePath == filepath.Dir(basePath) {
			return "", fmt.Errorf("could not find go.mod")
		}

		basePath = filepath.Dir(basePath)
	}

	return basePath, nil
}

func runCommand(cmdName string, args ...string) error {
	cmd := exec.Command(cmdName, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	env := os.Environ()
	if !*cgo {
		env = append(env, "CGO_ENABLED=0")
	}
	cmd.Env = env

	return cmd.Run()
}

var (
	buildOs          = flag.String("os", runtime.GOOS, "Specify the operating system to build for.")
	buildArch        = flag.String("arch", runtime.GOARCH, "Specify the architecture to build for.")
	buildDir         = flag.String("buildDir", "build/", "Specify the build dir to write build outputs to.")
	cross            = flag.String("cross", "", "Specify another init executable architecture to build (options x86_64 and aarch64).")
	debug            = flag.Bool("debug", false, "Print executed commands.")
	run              = flag.Bool("run", false, "Run TinyRange with the remaining arguments.")
	test             = flag.String("test", "", "Run all .yml files in a subdirectory using TinyRange.")
	testVerbose      = flag.Bool("test-verbose", false, "Run tests in verbose mode.")
	testExperimental = flag.String("test-experimental", "", "Add experimental feature flags to the test.")
	release          = flag.Bool("release", false, "Build a release version of TinyRange.")
	cgo              = flag.Bool("cgo", false, "Build VMMs that require CGO.")
	exp              = flag.String("exp", "", "Run a experimental feature.")
	runExpScripts    = flag.Bool("exp-scripts", false, "Run experimental scripts defined by a build file in the experimental path.")
	install          = flag.Bool("install", false, "Install TinyRange and any compatible VMMs into the GOPATH")
)

func main() {
	flag.Parse()

	basePath, err := getBasePath()
	if err != nil {
		log.Fatal(err)
	}

	if *exp != "" {
		if _, err := os.Stat(filepath.Join(basePath, "experimental", *exp, "build")); err == nil && *runExpScripts {
			if err := runExperimentalScripts(basePath, *exp); err != nil {
				log.Fatal(err)
			}
		}

		args := []string{"go", "run", PACKAGE_NAME + "/experimental/" + *exp}
		args = append(args, flag.Args()...)
		cmd := exec.Command(args[0], args[1:]...)

		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}

		return
	}

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

	if err := buildInitForTarget(*buildOs, *buildArch); err != nil {
		log.Fatal(err)
	}

	if *cross != "" {
		if err := buildInitForCross(*cross); err != nil {
			log.Fatal(err)
		}

		// automatically copy the kernel to the build directory
		kernelFilename := filepath.Join("pkg", "linux", "kernel", *cross, "vmlinux")

		if err := copyFile(kernelFilename, filepath.Join(
			*buildDir,
			fmt.Sprintf("tinyrange_kernel_%s", *cross),
		)); err != nil {
			log.Fatal(err)
		}
	}

	target, err := getTargetDir(*buildDir, *buildOs, *buildArch)
	if err != nil {
		log.Fatal(err)
	}

	for _, vmm := range buildVmmList {
		if _, err := buildVMMForTarget(target, *buildOs, *buildArch, vmm.Name, vmm.Cgo); err != nil {
			log.Fatal(err)
		}

		if vmm.Name == "qemu" {
			if err := ensureQemu(QEMU_VERSION, QEMU_REPO, target, *buildOs, *buildArch); err != nil {
				log.Printf("WARN: %v", err)
			}
		}
	}

	filename, err := buildTinyRangeForTarget(target, *buildOs, *buildArch)
	if err != nil {
		log.Fatal(err)
	}

	if *release {
		if err := buildRelease(*buildOs, *buildArch, *cgo); err != nil {
			log.Fatal(err)
		}
	} else if *test != "" {
		if err := runTests(*test, *testVerbose, *testExperimental); err != nil {
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
	} else if *install {
		log.Printf("installing tinyrange")
		if err := runCommand("go", "install", PACKAGE_NAME); err != nil {
			log.Fatal(err)
		}

		for _, vmm := range buildVmmList {
			log.Printf("installing VMM %s", vmm.Name)
			if err := runCommand("go", "install", PACKAGE_NAME+"/cmd/tinyrange_"+vmm.Name); err != nil {
				log.Fatal(err)
			}
		}
	}
}
