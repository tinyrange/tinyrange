package cvmfs

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
	"github.com/tinyrange/tinyrange/pkg/sqlite"
)

const (
	CVMFS_FLAG_DIR                       = 1
	CVMFS_FLAG_NESTED_CATALOG_TRANSITION = 2
	CVMFS_FLAG_NESTED_CATALOG_ROOT       = 32
	CVMFS_FLAG_REGULAR_FILE              = 4
	CVMFS_FLAG_SYMLINK                   = 8
	CVMFS_FLAG_CHUNKED_FILE              = 64
	CVMFS_FLAG_EXTERNAL_FILE             = 128
)

type CVMFSNestedCatalog struct {
	Path string
	Sha1 string
	Size int64
}

type CVMFSChunk struct {
	Md5Path1 int64
	Md5Path2 int64
	Offset   int64
	Size     int64
	Hash     []byte
}

func (chunk *CVMFSChunk) PathHash() string {
	return fmt.Sprintf("%x:%x", chunk.Md5Path1, chunk.Md5Path2)
}

type CVMFSEntry struct {
	Catalog *CVMFSCatalog

	Md5Path1  int64
	Md5Path2  int64
	Parent1   int64
	Parent2   int64
	Hardlinks int64
	Hash      []byte
	Size      int64
	Mode      int64
	Mtime     int64
	Flags     int64
	Name      string
	Symlink   string
	Uid       int64
	Gid       int64
	Xattr     []byte

	FullPath string
	Chunks   []CVMFSChunk
}

func (ent *CVMFSEntry) PathHash() string {
	return fmt.Sprintf("%x:%x", ent.Md5Path1, ent.Md5Path2)
}

func (ent *CVMFSEntry) ParentHash() string {
	return fmt.Sprintf("%x:%x", ent.Parent1, ent.Parent2)
}

func (ent *CVMFSEntry) IsChunked() bool {
	return ent.Flags&CVMFS_FLAG_CHUNKED_FILE != 0
}

func (ent *CVMFSEntry) Kind() filesystem.FileType {
	if ent.Flags&CVMFS_FLAG_DIR != 0 {
		return filesystem.TypeDirectory
	}

	if ent.Flags&CVMFS_FLAG_SYMLINK != 0 {
		return filesystem.TypeSymlink
	}

	return filesystem.TypeRegular
}

type CVMFSCatalog struct {
	db *sqlite.SQLiteDatabase
}

func (catalog *CVMFSCatalog) Entries() ([]CVMFSEntry, error) {
	chunks, err := catalog.Chunks()
	if err != nil {
		return nil, err
	}

	tbl, err := catalog.db.Table("catalog")
	if err != nil {
		return nil, err
	}

	var ret []CVMFSEntry

	if err := tbl.Read(func(row []any) error {
		ent := CVMFSEntry{
			Catalog: catalog,

			Md5Path1:  row[0].(int64),
			Md5Path2:  row[1].(int64),
			Parent1:   row[2].(int64),
			Parent2:   row[3].(int64),
			Hardlinks: row[4].(int64),
			Size:      row[6].(int64),
			Mode:      row[7].(int64),
			Mtime:     row[8].(int64),
			Flags:     row[9].(int64),
			Name:      row[10].(string),
			Symlink:   row[11].(string),
			Uid:       row[12].(int64),
			Gid:       row[13].(int64),
		}

		if row[5] != nil {
			ent.Hash = row[5].([]byte)
		}

		if row[14] != nil {
			ent.Xattr = row[14].([]byte)
		}

		if ent.IsChunked() {
			pathHash := ent.PathHash()

			if chunks, ok := chunks[pathHash]; ok {
				slices.SortFunc(chunks, func(a CVMFSChunk, b CVMFSChunk) int {
					return cmp.Compare(a.Offset, b.Offset)
				})
				ent.Chunks = chunks
			} else {
				return fmt.Errorf("chunks not found for %s", pathHash)
			}
		}

		ret = append(ret, ent)

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (catalog *CVMFSCatalog) NestedCatalogs() ([]CVMFSNestedCatalog, error) {
	tbl, err := catalog.db.Table("nested_catalogs")
	if _, ok := err.(sqlite.ErrTableNotFound); ok {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var ret []CVMFSNestedCatalog

	if err := tbl.Read(func(row []any) error {
		ret = append(ret, CVMFSNestedCatalog{
			Path: row[0].(string),
			Sha1: row[1].(string),
			Size: row[2].(int64),
		})

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (catalog *CVMFSCatalog) Chunks() (map[string][]CVMFSChunk, error) {
	chunks, err := catalog.db.Table("chunks")
	if _, ok := err.(sqlite.ErrTableNotFound); ok {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	ret := make(map[string][]CVMFSChunk)

	if err := chunks.Read(func(row []any) error {
		chunk := CVMFSChunk{
			Md5Path1: row[0].(int64),
			Md5Path2: row[1].(int64),
			Offset:   row[2].(int64),
			Size:     row[3].(int64),
			Hash:     row[4].([]byte),
		}

		pathHash := chunk.PathHash()

		if _, ok := ret[pathHash]; !ok {
			ret[pathHash] = make([]CVMFSChunk, 0)
		}

		ret[pathHash] = append(ret[pathHash], chunk)

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func openCVMFSCatalog(file io.ReaderAt) (*CVMFSCatalog, error) {
	db, err := sqlite.OpenDatabase(file)
	if err != nil {
		return nil, err
	}

	return &CVMFSCatalog{
		db: db,
	}, nil
}

type CVMFSManifest struct {
	rootCatalogHash             string
	rootCatalogSize             uint64
	rootCatalogAlternativeName  bool
	rootPathMD5                 string
	signingCertificateHash      string
	garbageCollectable          bool
	namedTagHistoryDatabaseHash string
	timestamp                   uint64
	rootCatalogTimeToLive       uint64
	revisionNumber              uint64
	name                        string
	jsonMetadataHash            string
	reflogChecksumHash          string
}

func parseCVMFSManifest(file io.Reader) (*CVMFSManifest, error) {
	var err error

	manifest := &CVMFSManifest{}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		key := line[0]
		value := line[1:]

		if line == "--" {
			break
		}

		switch key {
		case 'C':
			manifest.rootCatalogHash = value
		case 'B':
			manifest.rootCatalogSize, err = strconv.ParseUint(value, 0, 64)
			if err != nil {
				return nil, err
			}
		case 'R':
			manifest.rootPathMD5 = value
		case 'D':
			manifest.rootCatalogTimeToLive, err = strconv.ParseUint(value, 0, 64)
			if err != nil {
				return nil, err
			}
		case 'S':
			manifest.revisionNumber, err = strconv.ParseUint(value, 0, 64)
			if err != nil {
				return nil, err
			}
		case 'G':
			manifest.garbageCollectable = value == "yes"
		case 'A':
			manifest.rootCatalogAlternativeName = value == "yes"
		case 'N':
			manifest.name = value
		case 'X':
			manifest.signingCertificateHash = value
		case 'H':
			manifest.namedTagHistoryDatabaseHash = value
		case 'T':
			manifest.timestamp, err = strconv.ParseUint(value, 0, 64)
			if err != nil {
				return nil, err
			}
		case 'M':
			manifest.jsonMetadataHash = value
		case 'Y':
			manifest.reflogChecksumHash = value
		default:
			return nil, fmt.Errorf("unknown key: %s", string(key))
		}
	}

	return manifest, nil
}

type CVMFSRepository struct {
	db       common.MinimalBuildContext
	mirror   string
	repo     string
	manifest *CVMFSManifest
}

func (repo *CVMFSRepository) fetchFile(hash string, suffix string, compressed bool) (io.ReaderAt, error) {
	url := fmt.Sprintf("%s/%s/data/%s/%s%s", repo.mirror, repo.repo, hash[:2], hash[2:], suffix)

	def := repo.db.Factory().NewFetchHttpBuildDefinition(url, 0, nil)

	if compressed {
		def = repo.db.Factory().NewDecompressFileBuildDefinition(def, ".zlib")
	}

	art, err := repo.db.BuildChild(def)
	if err != nil {
		return nil, err
	}

	defFile, err := art.Default()
	if err != nil {
		return nil, err
	}

	return defFile.Open()
}

func (repo *CVMFSRepository) fetchManifest() (io.Reader, error) {
	url := fmt.Sprintf("%s/%s/.cvmfspublished", repo.mirror, repo.repo)

	def := repo.db.Factory().NewFetchHttpBuildDefinition(url, time.Hour*4, nil)

	art, err := repo.db.BuildChild(def)
	if err != nil {
		return nil, err
	}

	defFile, err := art.Default()
	if err != nil {
		return nil, err
	}

	return defFile.Open()
}

func (repo *CVMFSRepository) getManifest() (*CVMFSManifest, error) {
	if repo.manifest == nil {
		manifestFile, err := repo.fetchManifest()
		if err != nil {
			return nil, err
		}

		manifest, err := parseCVMFSManifest(manifestFile)
		if err != nil {
			return nil, err
		}

		repo.manifest = manifest
	}

	return repo.manifest, nil
}

func (repo *CVMFSRepository) RootCatalog() (*CVMFSCatalog, error) {
	manifest, err := repo.getManifest()
	if err != nil {
		return nil, err
	}

	catalogFile, err := repo.fetchFile(manifest.rootCatalogHash, "C", true)
	if err != nil {
		return nil, err
	}

	return openCVMFSCatalog(catalogFile)
}

func (repo *CVMFSRepository) GetCatalog(nested CVMFSNestedCatalog) (*CVMFSCatalog, error) {
	catalogFile, err := repo.fetchFile(nested.Sha1, "C", true)
	if err != nil {
		return nil, err
	}

	return openCVMFSCatalog(catalogFile)
}

func (repo *CVMFSRepository) GetAllFilesWithPrefix(prefix string) ([]CVMFSEntry, error) {
	var ret []CVMFSEntry

	hasCommonFragment := func(s1 string, s2 string) bool {
		if strings.HasPrefix(s1, s2) {
			return true
		}

		if len(s2) > len(s1) && strings.HasPrefix(s2, s1+"/") {
			return true
		}

		return false
	}

	var collectFilesFromCatalog func(catalog *CVMFSCatalog) error

	paths := make(map[string]string)

	paths["0:0"] = "/"

	collectFilesFromCatalog = func(catalog *CVMFSCatalog) error {
		ents, err := catalog.Entries()
		if err != nil {
			return err
		}

		for _, ent := range ents {
			parentPath, ok := paths[ent.ParentHash()]
			if !ok {
				return fmt.Errorf("parent not found: %s", ent.ParentHash())
			}

			childPath := path.Unix.Join(parentPath, ent.Name)

			if _, ok := paths[ent.PathHash()]; strings.HasPrefix(childPath, prefix) && !ok {
				ent.FullPath = strings.TrimPrefix(childPath, prefix)
				if ent.FullPath == "" {
					ent.FullPath = "/"
				}
				ret = append(ret, ent)
			}

			paths[ent.PathHash()] = childPath
		}

		nested, err := catalog.NestedCatalogs()
		if err != nil {
			return err
		}

		for _, nestedCatalog := range nested {
			if !hasCommonFragment(nestedCatalog.Path, prefix) {
				continue
			}

			child, err := repo.GetCatalog(nestedCatalog)
			if err != nil {
				return err
			}

			if err := collectFilesFromCatalog(child); err != nil {
				return err
			}
		}

		return nil
	}

	rootCatalog, err := repo.RootCatalog()
	if err != nil {
		return nil, err
	}

	if err := collectFilesFromCatalog(rootCatalog); err != nil {
		return nil, err
	}

	return ret, nil
}

func NewRepository(db common.MinimalBuildContext, mirror string, repo string) *CVMFSRepository {
	return &CVMFSRepository{
		db:     db,
		mirror: mirror,
		repo:   repo,
	}
}
