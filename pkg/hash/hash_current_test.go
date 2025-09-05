package hash_test

import (
	"fmt"
	"io"
	"testing"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

// newDB creates a DefinitionDatabase with a miss function that always errors.
func newDB(t *testing.T) *hash.DefinitionDatabase {
	t.Helper()
	return hash.NewDefinitionDatabase(func(h hash.Hash) (io.ReadCloser, error) {
		return nil, fmt.Errorf("cache miss for %s", h)
	})
}

// nothing

func TestHash_StableForSameDefinition(t *testing.T) {
	db := newDB(t)

	def1 := builder.Factory.NewFetchOCIImageDefinition("registry-1", "library/alpine", "latest", "amd64")
	def2 := builder.Factory.NewFetchOCIImageDefinition("registry-1", "library/alpine", "latest", "amd64")

	h1, err := db.HashDefinition(def1)
	if err != nil {
		t.Fatalf("hash def1: %v", err)
	}

	h2, err := db.HashDefinition(def2)
	if err != nil {
		t.Fatalf("hash def2: %v", err)
	}

	if h1 != h2 {
		t.Fatalf("expected same hash, got %s != %s", h1, h2)
	}
}

func TestHash_MapOrderInsensitive(t *testing.T) {
	db := newDB(t)

	// Same headers, different insertion orders.
	hA := map[string]string{"Z": "1", "A": "2", "M": "3"}
	hB := map[string]string{}
	hB["M"] = "3"
	hB["A"] = "2"
	hB["Z"] = "1"

	d1 := builder.Factory.NewFetchHttpBuildDefinition("https://example.com/file", 0, hA)
	d2 := builder.Factory.NewFetchHttpBuildDefinition("https://example.com/file", 0, hB)

	x1, err := db.HashDefinition(d1)
	if err != nil {
		t.Fatalf("hash d1: %v", err)
	}

	x2, err := db.HashDefinition(d2)
	if err != nil {
		t.Fatalf("hash d2: %v", err)
	}

	if x1 != x2 {
		t.Fatalf("expected same hash for same map contents, got %s != %s", x1, x2)
	}
}

func TestHash_SliceOrderSensitive(t *testing.T) {
	db := newDB(t)

	dirs1 := []common.Directive{
		common.DirectiveRunCommand{Command: "echo one"},
		common.DirectiveRunCommand{Command: "echo two"},
	}
	dirs2 := []common.Directive{
		common.DirectiveRunCommand{Command: "echo two"},
		common.DirectiveRunCommand{Command: "echo one"},
	}

	def1 := builder.Factory.NewBuildFsDefinition(dirs1, "initramfs")
	def2 := builder.Factory.NewBuildFsDefinition(dirs2, "initramfs")

	h1, err := db.HashDefinition(def1)
	if err != nil {
		t.Fatalf("hash def1: %v", err)
	}

	h2, err := db.HashDefinition(def2)
	if err != nil {
		t.Fatalf("hash def2: %v", err)
	}

	if h1 == h2 {
		t.Fatalf("expected different hashes for different slice order, got %s == %s", h1, h2)
	}
}

func TestHash_NestedDefinitionAffectsParent(t *testing.T) {
	db := newDB(t)

	k1 := builder.Factory.NewConstantHashDefinition("KERNEL_A", nil)
	k2 := builder.Factory.NewConstantHashDefinition("KERNEL_B", nil)

	vm1 := builder.Factory.NewBuildVmDefinition(nil, k1, nil, "", 0, 0, false, config.ArchInvalid, config.ArchInvalid, 0, "", "", false)
	vm2 := builder.Factory.NewBuildVmDefinition(nil, k2, nil, "", 0, 0, false, config.ArchInvalid, config.ArchInvalid, 0, "", "", false)

	h1, err := db.HashDefinition(vm1)
	if err != nil {
		t.Fatalf("hash vm1: %v", err)
	}

	h2, err := db.HashDefinition(vm2)
	if err != nil {
		t.Fatalf("hash vm2: %v", err)
	}

	if h1 == h2 {
		t.Fatalf("expected different hashes when nested definition changes, got %s == %s", h1, h2)
	}
}

func TestHash_FieldChangesAffectHash(t *testing.T) {
	db := newDB(t)

	// Toggle a boolean and assert hash changes.
	vm1 := builder.Factory.NewBuildVmDefinition(nil, nil, nil, "", 2, 1024, false, config.HostArchitecture, config.HostArchitecture, 8, "ssh", "key1", false)
	vm2 := builder.Factory.NewBuildVmDefinition(nil, nil, nil, "", 2, 1024, true, config.HostArchitecture, config.HostArchitecture, 8, "ssh", "key1", false)

	h1, err := db.HashDefinition(vm1)
	if err != nil {
		t.Fatalf("hash vm1: %v", err)
	}
	h2, err := db.HashDefinition(vm2)
	if err != nil {
		t.Fatalf("hash vm2: %v", err)
	}

	if h1 == h2 {
		t.Fatalf("expected different hashes when AutoScale changes, got %s == %s", h1, h2)
	}
}
