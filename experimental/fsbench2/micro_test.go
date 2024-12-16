package main

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func BenchmarkFastAlloc(b *testing.B) {
	a := make([]int, b.N)
	for i := 0; i < b.N; i++ {
		a[i] = i
	}

	_ = a
}

func BenchmarkSlowAlloc(b *testing.B) {
	var a []int
	for i := 0; i < b.N; i++ {
		a = append(a, i)
	}

	_ = a
}

type testStruct struct {
	A int    `json:"a"`
	B string `json:"b"`
}

func BenchmarkJSONDecode(b *testing.B) {
	testString := `{"a": 1, "b": "test"}`

	for i := 0; i < b.N; i++ {
		var t testStruct

		if err := json.Unmarshal([]byte(testString), &t); err != nil {
			b.Fatal(err)
		}

		if t.A != 1 || t.B != "test" {
			b.Fatalf("unexpected result: %+v", t)
		}
	}
}

var testBytes = []byte{1, 0, 0, 0, 4, 0, 116, 101, 115, 116}

func BenchmarkBinaryDecode(b *testing.B) {
	var t testStruct

	for i := 0; i < b.N; i++ {
		a := int(binary.LittleEndian.Uint32(testBytes[:4]))

		strLen := int(binary.LittleEndian.Uint16(testBytes[4:6]))

		bStr := string(testBytes[6 : 6+strLen])

		if a != 1 || bStr != "test" {
			b.Fatalf("unexpected result: %+v", t)
		}
	}
}

func BenchmarkStructDecode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		t := MicroTest(testBytes[:6])

		bStr := string(testBytes[6 : 6+t.BSize()])

		if t.A() != 1 || bStr != "test" {
			b.Fatalf("unexpected result: %+v", t)
		}
	}
}
