package vmm

import (
	"strings"
	"testing"
)

func TestBuildMountScriptFsck(t *testing.T) {
	volumes := []volumeInfo{
		{VolumeName: "vol1", GuestPath: "/data", Persist: true},
		{VolumeName: "vol2", GuestPath: "/cache", Persist: false},
	}
	script := buildMountScript(volumes, false)

	if !strings.Contains(script, "\"run\" in dir() and path_exists(\"/sbin/fsck.ext4\")") {
		t.Fatalf("fsck run check missing: %s", script)
	}
	if strings.Count(script, "fsck.ext4") != 1 {
		t.Fatalf("expected fsck check only for persistent volume: %s", script)
	}
	mountIdx := strings.Index(script, "mount(\"ext4\", \"/dev/vdb\", \"/data\"")
	fsckIdx := strings.Index(script, "fsck.ext4")
	if fsckIdx > mountIdx {
		t.Fatalf("fsck should run before mount: %s", script)
	}
	if idx := strings.Index(script, "/dev/vdc"); idx != -1 {
		if strings.Contains(script[idx:], "fsck.ext4") {
			t.Fatalf("non-persistent volume should not run fsck: %s", script)
		}
	}
}
