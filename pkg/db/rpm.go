package db

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"time"

	"github.com/cavaliergopher/rpm"
	"github.com/tinyrange/pkg2/pkg/common"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

type rpmPackage struct {
	Type    string `xml:"type,attr"`
	Name    string `xml:"name"`
	Arch    string `xml:"arch"`
	Version struct {
		Epoch string `xml:"epoch,attr"`
		Ver   string `xml:"ver,attr"`
		Rel   string `xml:"rel,attr"`
	} `xml:"version"`
	Checksum struct {
		Text  string `xml:",chardata"`
		Type  string `xml:"type,attr"`
		Pkgid string `xml:"pkgid,attr"`
	} `xml:"checksum"`
	Summary     string `xml:"summary"`
	Description string `xml:"description"`
	Packager    string `xml:"packager"`
	URL         string `xml:"url"`
	Time        struct {
		File  int64 `xml:"file,attr"`
		Build int64 `xml:"build,attr"`
	} `xml:"time"`
	Size struct {
		Package   int64 `xml:"package,attr"`
		Installed int64 `xml:"installed,attr"`
		Archive   int64 `xml:"archive,attr"`
	} `xml:"size"`
	Location struct {
		Text string `xml:",chardata"`
		Href string `xml:"href,attr"`
	} `xml:"location"`
	Format struct {
		License     string `xml:"license"`
		Vendor      string `xml:"vendor"`
		Group       string `xml:"group"`
		Buildhost   string `xml:"buildhost"`
		Sourcerpm   string `xml:"sourcerpm"`
		HeaderRange struct {
			Start string `xml:"start,attr"`
			End   string `xml:"end,attr"`
		} `xml:"header-range"`
		Provides struct {
			Entry []struct {
				Name  string `xml:"name,attr"`
				Flags string `xml:"flags,attr"`
				Epoch int64  `xml:"epoch,attr"`
				Ver   string `xml:"ver,attr"`
				Rel   string `xml:"rel,attr"`
			} `xml:"entry"`
		} `xml:"provides"`
		Requires struct {
			Entry []struct {
				Name  string `xml:"name,attr"`
				Flags string `xml:"flags,attr"`
				Pre   string `xml:"pre,attr"`
				Ver   string `xml:"ver,attr"`
			} `xml:"entry"`
		} `xml:"requires"`
		Conflicts struct {
			Entry []struct {
				Name  string `xml:"name,attr"`
				Flags string `xml:"flags,attr"`
				Epoch int64  `xml:"epoch,attr"`
				Ver   string `xml:"ver,attr"`
			} `xml:"entry"`
		} `xml:"conflicts"`
		Obsoletes struct {
			Entry []struct {
				Name  string `xml:"name,attr"`
				Flags string `xml:"flags,attr"`
				Epoch int64  `xml:"epoch,attr"`
				Ver   string `xml:"ver,attr"`
			} `xml:"entry"`
		} `xml:"obsoletes"`
		File []struct {
			Text string `xml:",chardata"`
			Type string `xml:"type,attr"`
		} `xml:"file"`
	} `xml:"format"`
}

type rpmRepoPrimaryIterator struct {
	primary *rpmRepoPrimary
	index   int
}

// Done implements starlark.Iterator.
func (it *rpmRepoPrimaryIterator) Done() {
	it.index = len(it.primary.Packages)
}

// Next implements starlark.Iterator.
func (it *rpmRepoPrimaryIterator) Next(p *starlark.Value) bool {
	if it.index >= len(it.primary.Packages) {
		return false
	}

	ent := it.primary.Packages[it.index]

	bytes, err := json.Marshal(&ent)
	if err != nil {
		it.Done()
		return false
	}

	val, err := starlark.Call(
		&starlark.Thread{},
		starlarkjson.Module.Members["decode"],
		starlark.Tuple{starlark.String(bytes)},
		[]starlark.Tuple{},
	)
	if err != nil {
		it.Done()
		return false
	}

	*p = val

	it.index += 1

	return true
}

type rpmRepoPrimary struct {
	Packages []rpmPackage `xml:"package"`
}

// Iterate implements starlark.Iterable.
func (r *rpmRepoPrimary) Iterate() starlark.Iterator {
	return &rpmRepoPrimaryIterator{primary: r}
}

func (*rpmRepoPrimary) String() string        { return "rpmRepoPrimary" }
func (*rpmRepoPrimary) Type() string          { return "rpmRepoPrimary" }
func (*rpmRepoPrimary) Hash() (uint32, error) { return 0, fmt.Errorf("rpmRepoPrimary is not hashable") }
func (*rpmRepoPrimary) Truth() starlark.Bool  { return starlark.True }
func (*rpmRepoPrimary) Freeze()               {}

var (
	_ starlark.Value    = &rpmRepoPrimary{}
	_ starlark.Iterable = &rpmRepoPrimary{}
)

func rpmReadXml(thread *starlark.Thread, f io.Reader) (starlark.Value, error) {
	dec := xml.NewDecoder(f)

	var primary rpmRepoPrimary

	if err := dec.Decode(&primary); err != nil {
		return starlark.None, err
	}

	return &primary, nil
}

type starRpm struct {
	pkg           *rpm.Package
	payloadReader io.ReadCloser
	openedPayload bool
}

// Attr implements starlark.HasAttrs.
func (s *starRpm) Attr(name string) (starlark.Value, error) {
	if name == "payload" {
		return NewFile(nil, "", func() (io.ReadCloser, error) {
			if s.openedPayload {
				return nil, fmt.Errorf("payload has already been opened")
			}

			s.openedPayload = true

			return s.payloadReader, nil
		}, nil), nil
	} else if name == "payload_compression" {
		return starlark.String(s.pkg.PayloadCompression()), nil
	} else if name == "metadata" {
		var metadata = struct {
			Name                     string
			Version                  string
			Release                  string
			Epoch                    int
			Summary                  string
			Description              string
			BuildTime                time.Time
			BuildHost                string
			InstallTime              time.Time
			Size                     uint64
			ArchiveSize              uint64
			Distribution             string
			Vendor                   string
			License                  string
			Packager                 string
			Groups                   []string
			ChangeLog                []string
			Source                   []string
			Patch                    []string
			URL                      string
			OperatingSystem          string
			Architecture             string
			PreInstallScript         string
			PreInstallScriptProgram  []string
			PostInstallScript        string
			PostInstallScriptProgram []string
			PreUninstallScript       string
			PostUninstallScript      string
			OldFilenames             []string
			SourceRPM                string
			RPMVersion               string
			Platform                 string
		}{
			Name:                     s.pkg.Name(),
			Version:                  s.pkg.Version(),
			Release:                  s.pkg.Release(),
			Epoch:                    s.pkg.Epoch(),
			Summary:                  s.pkg.Summary(),
			Description:              s.pkg.Description(),
			BuildTime:                s.pkg.BuildTime(),
			BuildHost:                s.pkg.BuildHost(),
			InstallTime:              s.pkg.InstallTime(),
			Size:                     s.pkg.Size(),
			ArchiveSize:              s.pkg.ArchiveSize(),
			Distribution:             s.pkg.Distribution(),
			Vendor:                   s.pkg.Vendor(),
			License:                  s.pkg.License(),
			Packager:                 s.pkg.Packager(),
			Groups:                   s.pkg.Groups(),
			ChangeLog:                s.pkg.ChangeLog(),
			Source:                   s.pkg.Source(),
			Patch:                    s.pkg.Patch(),
			URL:                      s.pkg.URL(),
			OperatingSystem:          s.pkg.OperatingSystem(),
			Architecture:             s.pkg.Architecture(),
			PreInstallScript:         s.pkg.PreInstallScript(),
			PreInstallScriptProgram:  s.pkg.Header.GetTag(1085).StringSlice(),
			PostInstallScript:        s.pkg.PostInstallScript(),
			PostInstallScriptProgram: s.pkg.Header.GetTag(1086).StringSlice(),
			PreUninstallScript:       s.pkg.PreUninstallScript(),
			PostUninstallScript:      s.pkg.PostUninstallScript(),
			OldFilenames:             s.pkg.OldFilenames(),
			SourceRPM:                s.pkg.SourceRPM(),
			RPMVersion:               s.pkg.RPMVersion(),
			Platform:                 s.pkg.Platform(),
		}

		bytes, err := json.Marshal(&metadata)
		if err != nil {
			return nil, err
		}

		return starlark.String(bytes), nil
	}
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (s *starRpm) AttrNames() []string {
	return []string{"payload", "payload_compression", "metadata"}
}

func (*starRpm) String() string        { return "starRpm" }
func (*starRpm) Type() string          { return "starRpm" }
func (*starRpm) Hash() (uint32, error) { return 0, fmt.Errorf("starRpm is not hashable") }
func (*starRpm) Truth() starlark.Bool  { return starlark.True }
func (*starRpm) Freeze()               {}

var (
	_ starlark.Value    = &starRpm{}
	_ starlark.HasAttrs = &starRpm{}
)

func parseRpm(fif common.FileIf) (starlark.Value, error) {
	f, err := fif.Open()
	if err != nil {
		return starlark.None, err
	}

	pkg, err := rpm.Read(f)
	if err != nil {
		return starlark.None, err
	}

	return &starRpm{pkg: pkg, payloadReader: f}, nil
}
