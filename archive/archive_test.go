package archive

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"testing"
)

func BenchmarkHexEncode(b *testing.B) {
	hash := make([]byte, sha256.Size)
	dst := make([]byte, hex.EncodedLen(len(hash)))

	for b.Loop() {
		hex.Encode(dst, hash)
	}
}

func BenchmarkEntryEncode(b *testing.B) {
	hash := make([]byte, sha256.Size)

	ent := Entry{
		Name: "name",
	}

	var s staticPrintf

	for b.Loop() {
		for range 1000 {
			s.Reset()

			ent2 := ent

			ent2.Kind = EntryKindRegular
			ent2.Size = 1024
			ent2.Mode = fs.FileMode(0644)

			if err := ent2.encode(&s, hash, 0); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkCopyBuffer(b *testing.B) {
	srcData := make([]byte, 1024)

	copyBuffer := make([]byte, 1024*32)

	dst := new(bytes.Buffer)

	reader := bytes.NewReader(srcData)

	limited := io.LimitedReader{}

	for b.Loop() {
		dst.Reset()
		for range 1000 {
			reader.Reset(srcData)
			limited.N = 1024
			limited.R = reader
			_, err := io.CopyBuffer(dst, &limited, copyBuffer)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkHashBuffer(b *testing.B) {
	srcData := make([]byte, 1024)

	if _, err := rand.Read(srcData); err != nil {
		b.Fatal(err)
	}

	hash := sha256.New()

	for b.Loop() {
		for range 1000 {
			hash.Reset()
			hash.Write(srcData)
		}
	}
}

func CreateArchiveWithSize(index io.Writer, contents io.Writer, items int, fileData []byte) error {
	writer, err := NewWriter(index, contents)
	if err != nil {
		return err
	}

	reader := bytes.NewReader(fileData)

	for range items {
		reader.Reset(fileData)
		if err := writer.WriteEntry(
			&Entry{
				Name: "file",
				Kind: EntryKindRegular,
				Size: int64(len(fileData)),
				Mode: 0644,
			},
			reader,
		); err != nil {
			return err
		}
	}

	return nil
}

func BenchmarkArchiveCreate(b *testing.B) {
	var randData [1024]byte

	_, err := rand.Read(randData[:])
	if err != nil {
		b.Fatal(err)
	}

	index := new(bytes.Buffer)
	contents := new(bytes.Buffer)

	for b.Loop() {
		index.Reset()
		contents.Reset()

		err := CreateArchiveWithSize(index, contents, 1000, randData[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkArchiveRead(b *testing.B) {
	var randData [1024]byte

	_, err := rand.Read(randData[:])
	if err != nil {
		b.Fatal(err)
	}

	index := new(bytes.Buffer)
	contents := new(bytes.Buffer)

	if err := CreateArchiveWithSize(index, contents, 1000, randData[:]); err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		ark, err := NewReader(bytes.NewReader(index.Bytes()), nil, bytes.NewReader(contents.Bytes()))
		if err != nil {
			b.Fatal(err)
		}
		defer ark.Close()

		total := 0

		for {
			err := ark.NextEntry()
			if err == io.EOF {
				break
			} else if err != nil {
				b.Fatalf("failed to read entry: %v", err)
			}

			total += int(ark.Size())
		}

		if total != 1000*1024 {
			b.Fatalf("unexpected total size: %d", total)
		}
	}
}
