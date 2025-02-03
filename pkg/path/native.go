package path

import "path/filepath"

type nativePathImplementation struct{}

// Join implements Path.
func (n *nativePathImplementation) Join(elem ...string) string {
	return filepath.Join(elem...)
}

// Base implements Path.
func (n *nativePathImplementation) Base(elem string) string {
	return filepath.Base(elem)
}

// Dir implements Path.
func (n *nativePathImplementation) Dir(elem string) string {
	return filepath.Dir(elem)
}

// Clean implements Path.
func (n *nativePathImplementation) Clean(path string) string {
	return filepath.Clean(path)
}

// Ext implements Path.
func (n *nativePathImplementation) Ext(path string) string {
	return filepath.Ext(path)
}

// Abs implements Path.
func (n *nativePathImplementation) Abs(path string) (string, error) {
	return filepath.Abs(path)
}

// IsAbs implements Path.
func (n *nativePathImplementation) IsAbs(path string) bool {
	return filepath.IsAbs(path)
}

// Split implements Path.
func (n *nativePathImplementation) Split(path string) (dir, file string) {
	return filepath.Split(path)
}

var (
	_ Path = &nativePathImplementation{}
)

var Native = &nativePathImplementation{}
