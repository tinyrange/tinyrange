package proto

import (
	"crypto/sha256"
	"encoding/hex"

	"google.golang.org/protobuf/proto"
)

type ArchiveSource = isFileSource_Source

func (r *BuildReceipt) AsBytes() []byte {
	opts := proto.MarshalOptions{}
	b, err := opts.Marshal(r)
	if err != nil {
		panic(err)
	}
	return b
}

func (d *Definition) AsBytes() []byte {
	opts := proto.MarshalOptions{}
	b, err := opts.Marshal(d)
	if err != nil {
		panic(err)
	}
	return b
}

func (d *Definition) Hash() *Hash {
	h := sha256.Sum256(d.AsBytes())
	return &Hash{
		Value: hex.EncodeToString(h[:]),
	}
}
