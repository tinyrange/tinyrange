package ext4

import (
    "os"
    "os/exec"
    "path/filepath"
    "testing"

    "github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

// TestFsckIfInstalled creates a small filesystem image and runs fsck.ext4 (or
// e2fsck) if available to validate basic filesystem structure. This test is
// skipped when neither tool is present.
func TestFsckIfInstalled(t *testing.T) {
    fsck, err := exec.LookPath("fsck.ext4")
    if err != nil {
        fsck, err = exec.LookPath("e2fsck")
        if err != nil {
            t.Skip("fsck.ext4/e2fsck not installed; skipping")
        }
    }

    // Use a small VM to keep the image lightweight.
    _vm := vm.NewVirtualMemory(64*1024*1024, 4096) // 64 MiB

    fs, err := CreateExt4Filesystem(_vm, 0, _vm.Size())
    if err != nil {
        t.Fatalf("failed to create fs: %v", err)
    }

    // Add a small file.
    if err := fs.CreateFile("/hello", vm.RawRegion("world")); err != nil {
        t.Fatalf("failed to create file: %v", err)
    }

    // Write image to a temporary file inside the workspace.
    dir := t.TempDir()
    img := filepath.Join(dir, "fs.img")
    f, err := os.Create(img)
    if err != nil {
        t.Fatalf("create image: %v", err)
    }
    defer f.Close()

    if _, err := _vm.WriteSparseTo(f); err != nil {
        t.Fatalf("write image: %v", err)
    }

    // Run fsck read-only (-n) and force check (-f).
    cmd := exec.Command(fsck, "-fn", img)
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("fsck failed: %v\n%s", err, string(out))
    }
}

// TestFsckLargeImage validates a 2 GiB filesystem with a 1 GiB file via fsck
// if the tool is available. Skips otherwise.
func TestFsckLargeImage(t *testing.T) {
    fsck, err := exec.LookPath("fsck.ext4")
    if err != nil {
        fsck, err = exec.LookPath("e2fsck")
        if err != nil {
            t.Skip("fsck.ext4/e2fsck not installed; skipping")
        }
    }

    _vm := vm.NewVirtualMemory(2*1024*1024*1024, 4096) // 2 GiB
    fs, err := CreateExt4Filesystem(_vm, 0, _vm.Size())
    if err != nil {
        t.Fatalf("failed to create fs: %v", err)
    }

    // Create a 1 GiB zero-filled file (sparse content via ZeroRegion)
    region := vm.ZeroRegion(1 * 1024 * 1024 * 1024)
    if err := fs.CreateFile("/largefile", region); err != nil {
        t.Fatalf("failed to create large file: %v", err)
    }

    dir := t.TempDir()
    img := filepath.Join(dir, "fs-large.img")
    f, err := os.Create(img)
    if err != nil {
        t.Fatalf("create image: %v", err)
    }
    defer f.Close()

    if _, err := _vm.WriteSparseTo(f); err != nil {
        t.Fatalf("write image: %v", err)
    }

    cmd := exec.Command(fsck, "-fn", img)
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("fsck failed: %v\n%s", err, string(out))
    }
}
