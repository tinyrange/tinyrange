package storage

import "io"

// Hash is the hex-encoded SHA-1 used by Git objects.
// For simplicity we treat it as a string here.
type Hash string

// ObjectType represents the Git object type.
type ObjectType int

const (
	ObjInvalid ObjectType = iota
	ObjCommit
	ObjTree
	ObjBlob
	ObjTag
)

// Object represents a raw Git object (header implied by type+size).
type Object struct {
	Type ObjectType
	Size int64
	Data []byte
}

// Header returns the canonical loose-object header: "<type> <size>\x00"
func (o *Object) Header() string {
	var kind string
	switch o.Type {
	case ObjCommit:
		kind = "commit"
	case ObjTree:
		kind = "tree"
	case ObjBlob:
		kind = "blob"
	case ObjTag:
		kind = "tag"
	default:
		kind = "unknown"
	}
	return kind + " " + itoa(o.Size) + "\x00"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [32]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}

// ObjectStore handles object persistence by ID.
type ObjectStore interface {
	Has(h Hash) (bool, error)
	Get(h Hash) (*Object, error)
	Put(obj *Object) (Hash, error)
	Iter(func(h Hash, t ObjectType, size int64) error) error
}

// RefStore manages named refs (e.g., refs/heads/main) -> object IDs.
type RefStore interface {
	List() (map[string]Hash, error)
	Get(name string) (Hash, bool, error)
	Set(name string, h Hash) error
	// CompareAndSwap updates name to new if current is old; empty old disables CAS.
	CompareAndSwap(name string, old Hash, new Hash) (bool, error)
	DefaultBranch() (string, error)
	SetDefaultBranch(name string) error
}

// Repo abstracts a bare repository (objects + refs).
type Repo interface {
	Objects() ObjectStore
	Refs() RefStore
}

// Registry loads repos by name/key for servers.
type Registry interface {
	Get(name string) (Repo, bool)
	CreateBare(name string) error
	// ImportPack receives a pack stream and updates refs per updateFn.
	ImportPack(name string, r io.Reader, updateFn func(RefStore) error) error
}
