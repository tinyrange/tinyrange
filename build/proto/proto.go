package proto

import (
	"crypto/sha256"
	"encoding/hex"

	"google.golang.org/protobuf/proto"
)

type Hash string

type ArchiveSource = isFileSource_Source

func (d *Definition) Hash() Hash {
	// serialize the definition to bytes then hash with sha256
	opts := proto.MarshalOptions{}
	b, err := opts.Marshal(d)
	if err != nil {
		panic(err)
	}
	h := sha256.Sum256(b)
	return Hash(hex.EncodeToString(h[:]))
}
