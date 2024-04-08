package db

import (
	"bytes"
	"encoding/json"
	"encoding/xml"

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
				Name string `xml:"name,attr"`
				Pre  string `xml:"pre,attr"`
				Ver  string `xml:"ver,attr"`
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

type rpmRepoPrimary struct {
	Packages []rpmPackage `xml:"package"`
}

func rpmReadXml(thread *starlark.Thread, f File) (starlark.Value, error) {
	dec := xml.NewDecoder(f)

	var primary rpmRepoPrimary

	if err := dec.Decode(&primary); err != nil {
		return starlark.None, err
	}

	buf := bytes.Buffer{}

	enc := json.NewEncoder(&buf)

	if err := enc.Encode(primary); err != nil {
		return starlark.None, err
	}

	return starlark.Call(
		thread,
		starlarkjson.Module.Members["decode"],
		starlark.Tuple{starlark.String(buf.Bytes())},
		[]starlark.Tuple{},
	)
}
