package build

import (
	"io"
	"testing"

	"github.com/tinyrange/tinyrange/build/cache/memory"
	"github.com/tinyrange/tinyrange/build/internal"
	"github.com/tinyrange/tinyrange/build/internal/common"
)

func TestSimple(t *testing.T) {
	db, err := internal.New(memory.NewCache())
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	art, err := db.Build(db.Factory().NewWriteFile([]byte("hello world")))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	f, err := art.Open(common.FileType_Plain)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(data) != "hello world" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}
