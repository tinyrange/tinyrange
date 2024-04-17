package jinja2

import (
	"embed"
	"io"
	"path"
	"testing"
)

//go:embed testdata/*.j2
var testData embed.FS

func Test(t *testing.T) {
	ents, err := testData.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range ents {
		t.Run(file.Name(), func(t *testing.T) {
			eval := &Jinja2Evaluator{}

			filename := path.Join("testdata", file.Name())

			f, err := testData.Open(filename)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()

			contents, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}

			out, err := eval.ToStarlark(string(contents))
			if err != nil {
				t.Fatal(err)
			}

			_ = out
		})
	}
}
