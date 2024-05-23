package db

import "testing"

func TestOpenPath(t *testing.T) {
	f := newFilesystem()

	if _, err := f.mkdir("hello"); err != nil {
		t.Fatal(err)
	}

	dir2, err := f.mkdir("hello/world")
	if err != nil {
		t.Fatal(err)
	}

	parent, name, ok, err := f.openPath("hello/world", false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("f.openPath returned false")
	}

	child, ok, err := parent.getChild(name, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("f.openPath returned false")
	}

	if child != dir2 {
		t.Fatal("child != dir2")
	}
}
