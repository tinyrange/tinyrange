package archive2

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

func BenchmarkHexEncode(b *testing.B) {
	hash := make([]byte, sha256.Size)
	dst := make([]byte, hex.EncodedLen(len(hash)))

	for i := 0; i < b.N; i++ {
		hex.Encode(dst, hash)
	}
}

func BenchmarkEntryEncode(b *testing.B) {
	hash := make([]byte, sha256.Size)

	ent := new(EntryFactory)
	ent = ent.Name("file")

	var s staticPrintf

	for i := 0; i < b.N; i++ {
		for i := 0; i < 1000; i++ {
			s.Reset()

			ent = ent.
				Kind(EntryKindRegular).
				Size(1024).
				Mode(0644)

			if err := ent.encode(&s, hash, 0); err != nil {
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

	for i := 0; i < b.N; i++ {
		dst.Reset()
		for i := 0; i < 1000; i++ {
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

	for i := 0; i < b.N; i++ {
		for i := 0; i < 1000; i++ {
			hash.Reset()
			hash.Write(srcData)
		}
	}
}

func CreateArchiveWithSize(index io.Writer, contents io.Writer, items int, fileData []byte) error {
	writer, err := NewArchiveWriter(index, contents)
	if err != nil {
		return err
	}

	ent := new(EntryFactory)

	ent = ent.Name("file")

	reader := bytes.NewReader(fileData)

	for i := 0; i < items; i++ {
		reader.Reset(fileData)
		if err := writer.WriteEntry(
			ent.
				Kind(EntryKindRegular).
				Size(int64(len(fileData))).
				Mode(0644),
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

	for i := 0; i < b.N; i++ {
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

	for i := 0; i < b.N; i++ {
		ark, err := NewArchiveReader(bytes.NewReader(index.Bytes()), nil, bytes.NewReader(contents.Bytes()))
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
