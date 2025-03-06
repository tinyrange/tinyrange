package cvmfs

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/path"
)

type CVMFSEntry struct {
	Md5Path1  uint64
	Md5Path2  uint64
	Parent1   uint64
	Parent2   uint64
	Hardlinks uint64
	Hash      []byte
	Size      uint64
	Mode      uint64
	Mtime     uint64
	Flags     uint64
	Name      string
	Symlink   string
	Uid       uint64
	Gid       uint64
	Xattr     []byte
}

type CVMFSCatalog struct {
	db *SQLiteDatabase
}

func (catalog *CVMFSCatalog) Entries() ([]CVMFSEntry, error) {
	tbl, err := catalog.db.Table("catalog")
	if err != nil {
		return nil, err
	}

	var ret []CVMFSEntry

	if err := tbl.Read(func(row []any) error {
		ent := CVMFSEntry{
			Md5Path1:  row[0].(uint64),
			Md5Path2:  row[1].(uint64),
			Parent1:   row[2].(uint64),
			Parent2:   row[3].(uint64),
			Hardlinks: row[4].(uint64),
			Size:      row[6].(uint64),
			Mode:      row[7].(uint64),
			Mtime:     row[8].(uint64),
			Flags:     row[9].(uint64),
			Name:      row[10].(string),
			Symlink:   row[11].(string),
			Uid:       row[12].(uint64),
			Gid:       row[13].(uint64),
		}

		if row[5] != nil {
			ent.Hash = row[5].([]byte)
		}

		if row[14] != nil {
			ent.Xattr = row[14].([]byte)
		}

		ret = append(ret, ent)

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

type CVMFSNestedCatalog struct {
	Path string
	Sha1 string
	Size uint64
}

func (catalog *CVMFSCatalog) NestedCatalogs() ([]CVMFSNestedCatalog, error) {
	tbl, err := catalog.db.Table("nested_catalogs")
	if _, ok := err.(ErrTableNotFound); ok {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var ret []CVMFSNestedCatalog

	if err := tbl.Read(func(row []any) error {
		ret = append(ret, CVMFSNestedCatalog{
			Path: row[0].(string),
			Sha1: row[1].(string),
			Size: row[2].(uint64),
		})

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func openCVMFSCatalog(file io.ReaderAt) (*CVMFSCatalog, error) {
	db, err := OpenDatabase(file)
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
	db       common.PackageDatabase
	mirror   string
	repo     string
	manifest *CVMFSManifest
}

func (repo *CVMFSRepository) fetchFile(hash string, suffix string, compressed bool) (io.ReaderAt, error) {
	url := fmt.Sprintf("%s/%s/data/%s/%s%s", repo.mirror, repo.repo, hash[:2], hash[2:], suffix)

	def := builder.Factory.NewFetchHttpBuildDefinition(url, 0, nil)

	if compressed {
		def = builder.Factory.NewDecompressFileBuildDefinition(def, ".zlib")
	}

	art, err := repo.db.Builder().Build(def, common.BuildOptions{})
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

	def := builder.Factory.NewFetchHttpBuildDefinition(url, time.Hour*4, nil)

	art, err := repo.db.Builder().Build(def, common.BuildOptions{})
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

	hashPath := func(ent CVMFSEntry) string {
		return fmt.Sprintf("%x:%x", ent.Md5Path1, ent.Md5Path2)
	}

	hashParent := func(ent CVMFSEntry) string {
		return fmt.Sprintf("%x:%x", ent.Parent1, ent.Parent2)
	}

	collectFilesFromCatalog = func(catalog *CVMFSCatalog) error {
		ents, err := catalog.Entries()
		if err != nil {
			return err
		}

		for _, ent := range ents {
			parentPath, ok := paths[hashParent(ent)]
			if !ok {
				return fmt.Errorf("parent not found: %s", hashParent(ent))
			}

			childPath := path.Unix.Join(parentPath, ent.Name)

			if _, ok := paths[hashPath(ent)]; hasCommonFragment(childPath, prefix) && !ok {
				slog.Info("entry", "path", childPath)
				ret = append(ret, ent)
			}

			paths[hashPath(ent)] = childPath
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

func NewRepository(db common.PackageDatabase, mirror string, repo string) *CVMFSRepository {
	return &CVMFSRepository{
		db:     db,
		mirror: mirror,
		repo:   repo,
	}
}
