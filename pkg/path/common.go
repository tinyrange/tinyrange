package path

type Path interface {
	// Base returns the last element of path.
	Base(elem string) string
	// Dir returns all but the last element of path.
	Dir(elem string) string
	// Join joins any number of path elements into a single path.
	Join(elem ...string) string
	// Clean returns the shortest path name equivalent to path by purely lexical processing.
	Clean(path string) string
	// Ext returns the file name extension used by path.
	Ext(path string) string
	// Abs returns an absolute representation of path.
	Abs(path string) (string, error)
	// IsAbs reports whether the path is absolute.
	IsAbs(path string) bool
	// Split splits path immediately following the final slash.
	Split(path string) (dir, file string)
	// Rel returns a relative path from the base to the target.
	Rel(base, target string) (string, error)
}
