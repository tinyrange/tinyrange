package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"

	"github.com/tinyrange/tinyrange/experimental/fs3/structgen/libstruct"
	"github.com/tinyrange/tinyrange/pkg/htm"
	"github.com/tinyrange/tinyrange/pkg/htm/html"
	"github.com/tinyrange/tinyrange/pkg/log"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func randomColor() string {
	// generate a random CSS color that will be readable with black text

	for {
		r := rand.Intn(256)
		g := rand.Intn(256)
		b := rand.Intn(256)

		// Use the YIQ formula to determine the brightness of the color.
		// See https://en.wikipedia.org/wiki/YIQ
		yiq := (r*299 + g*587 + b*114) / 1000

		if yiq > 128 {
			return fmt.Sprintf("#%02x%02x%02x", r, g, b)
		}
	}
}

// Segment represents a contiguous block of bytes and whether it is annotated.
type Segment struct {
	Data  []byte
	Color string
	Label string
}

// AnnotatedBuffer holds a slice of segments.
type AnnotatedBuffer struct {
	Segments []Segment
}

// NewAnnotatedBuffer creates a new AnnotatedBuffer with a single segment of the given size.
func NewAnnotatedBuffer(bytes []byte) *AnnotatedBuffer {
	return &AnnotatedBuffer{
		Segments: []Segment{
			{
				Data: bytes,
			},
		},
	}
}

// Annotate marks a region (from offset to offset+length) with the given annotation flag.
// It splits segments as necessary and then merges adjacent segments with the same flag.
func (ab *AnnotatedBuffer) Annotate(offset, length int, color string, label string) error {
	if length <= 0 {
		return fmt.Errorf("annotation length must be positive")
	}

	// Check that the annotation range is within the bounds of our total data.
	totalLength := ab.TotalLength()
	if offset < 0 || offset+length > totalLength {
		return fmt.Errorf("annotation out of bounds: offset=%d, length=%d, totalLength=%d", offset, length, totalLength)
	}

	newSegments := make([]Segment, 0, len(ab.Segments)*2) // Pre-allocate with a reasonable capacity
	currentOffset := 0

	// Iterate over each segment to see if and how it is affected.
	for _, seg := range ab.Segments {
		segLen := len(seg.Data)
		segStart := currentOffset
		segEnd := currentOffset + segLen

		// No overlap: segment is completely before or after the annotation region.
		if segEnd <= offset || segStart >= offset+length {
			newSegments = append(newSegments, seg)
		} else {
			// Handle potential left part that comes before the annotation.
			if segStart < offset {
				leftSize := offset - segStart
				newSegments = append(newSegments, Segment{
					Data:  seg.Data[:leftSize],
					Color: seg.Color,
					Label: seg.Label,
				})
			}

			// Determine the overlap (middle part).
			overlapStart := max(segStart, offset)
			overlapEnd := min(segEnd, offset+length)
			overlapSize := overlapEnd - overlapStart
			dataStartIdx := overlapStart - segStart

			newSegments = append(newSegments, Segment{
				Data:  seg.Data[dataStartIdx : dataStartIdx+overlapSize],
				Color: color,
				Label: label,
			})

			// Handle the right part (after the annotation) if any.
			if segEnd > offset+length {
				rightStartIdx := dataStartIdx + overlapSize
				newSegments = append(newSegments, Segment{
					Data:  seg.Data[rightStartIdx:],
					Color: seg.Color,
					Label: seg.Label,
				})
			}
		}
		currentOffset += segLen
	}

	// Merge adjacent segments with the same color and label
	ab.Segments = mergeAdjacentSegments(newSegments)
	return nil
}

// mergeAdjacentSegments combines neighboring segments that have the same color and label
func mergeAdjacentSegments(segments []Segment) []Segment {
	if len(segments) <= 1 {
		return segments
	}

	result := make([]Segment, 0, len(segments))
	current := segments[0]

	for i := 1; i < len(segments); i++ {
		next := segments[i]
		// If the current and next segments share the same color and label, merge them
		if current.Color == next.Color && current.Label == next.Label {
			mergedData := make([]byte, len(current.Data)+len(next.Data))
			copy(mergedData, current.Data)
			copy(mergedData[len(current.Data):], next.Data)
			current.Data = mergedData
		} else {
			// If they differ, add the current segment to results and start a new current
			result = append(result, current)
			current = next
		}
	}

	// Don't forget to add the last segment
	result = append(result, current)
	return result
}

// Slice returns a new slice of segments within the given range.
// This creates new segments (with their own copies of data) from the original buffer.
func (ab *AnnotatedBuffer) Slice(offset, size int) ([]Segment, error) {
	if size <= 0 {
		return nil, fmt.Errorf("slice size must be positive")
	}

	totalLength := ab.TotalLength()
	if offset < 0 || offset+size > totalLength {
		return nil, fmt.Errorf("slice out of bounds: offset=%d, size=%d, totalLength=%d", offset, size, totalLength)
	}

	ret := make([]Segment, 0, len(ab.Segments))
	currentOffset := 0

	for _, seg := range ab.Segments {
		segLen := len(seg.Data)
		segStart := currentOffset
		segEnd := currentOffset + segLen

		// No overlap: segment is completely before or after the slice region.
		if segEnd <= offset || segStart >= offset+size {
			// Skip segments with no overlap
		} else {
			// Calculate the part of this segment that overlaps with the requested slice
			overlapStart := max(segStart, offset)
			overlapEnd := min(segEnd, offset+size)
			overlapSize := overlapEnd - overlapStart

			// Calculate the start index within this segment's Data
			dataStartIdx := overlapStart - segStart

			// Create a new segment with a copy of the data
			dataCopy := make([]byte, overlapSize)
			copy(dataCopy, seg.Data[dataStartIdx:dataStartIdx+overlapSize])

			ret = append(ret, Segment{
				Data:  dataCopy,
				Color: seg.Color,
				Label: seg.Label,
			})
		}
		currentOffset += segLen
	}

	return ret, nil
}

// TotalLength returns the total length of all segments in the buffer
func (ab *AnnotatedBuffer) TotalLength() int {
	totalLength := 0
	for _, seg := range ab.Segments {
		totalLength += len(seg.Data)
	}
	return totalLength
}

// Helper functions.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func hexDump(b []byte) string {
	ret := strings.Builder{}
	for _, c := range b {
		ret.WriteString(fmt.Sprintf("%02x ", c))
	}
	return ret.String()
}

func printableCharDump(b []byte) string {
	ret := strings.Builder{}
	for _, c := range b {
		if c >= 32 && c <= 126 {
			ret.WriteByte(c)
		} else {
			ret.WriteByte('.')
		}
	}
	return ret.String()
}

type BinaryAnnotator struct {
	parent   *BinaryAnnotator
	children []*BinaryAnnotator
	name     string
	typ      string
	length   int64
	offset   int64 // offset in parent
	bytes    []byte
}

func (b *BinaryAnnotator) String() string { return b.name }
func (b *BinaryAnnotator) Bytes() []byte  { return b.bytes }

func (b *BinaryAnnotator) Slice(name string, typ string, size int64, offset int64) *BinaryAnnotator {
	ret := &BinaryAnnotator{
		parent: b,
		name:   name,
		typ:    typ,
		length: size,
		offset: offset,
		bytes:  b.bytes[offset : offset+size],
	}
	b.children = append(b.children, ret)
	return ret
}

func (b *BinaryAnnotator) WriteHTML(w io.Writer) error {
	log.Default().Info("writing HTML")

	sliceable := NewAnnotatedBuffer(b.bytes)

	var sliceChild func(int64, *BinaryAnnotator)

	sliceChild = func(offset int64, b *BinaryAnnotator) {
		color := randomColor()
		label := fmt.Sprintf("%s (%s)", b.name, b.typ)
		sliceable.Annotate(int(offset+b.offset), int(b.length), color, label)

		for _, child := range b.children {
			sliceChild(offset+b.offset, child)
		}
	}

	for _, child := range b.children {
		sliceChild(0, child)
	}

	var segs htm.Group

	width := 32

	for i := 0; i < len(b.bytes); i += width {
		var group htm.Group

		group = append(group, html.Span(
			html.StyleAttr("color: #888; width: 6em; display: inline-block;"),
			html.Textf("%08x", i),
		))

		slice, err := sliceable.Slice(i, width)
		if err != nil {
			return fmt.Errorf("failed to slice: %w", err)
		}

		for _, seg := range slice {
			group = append(group, html.Span(
				html.StyleAttr(fmt.Sprintf("background-color: %s; white-space: pre;", seg.Color)),
				html.TitleAttr(seg.Label),
				html.Text(hexDump(seg.Data)),
			))
		}

		group = append(group, html.Span(
			html.StyleAttr("width: 2em; display: inline-block;"),
		))

		for _, seg := range slice {
			group = append(group, html.Span(
				html.StyleAttr(fmt.Sprintf("background-color: %s; white-space: pre;", seg.Color)),
				html.TitleAttr(seg.Label),
				html.Text(printableCharDump(seg.Data)),
			))
		}

		segs = append(segs, html.Div(
			html.StyleAttr("display: flex"),
			group,
		))
	}

	// for _, seg := range sliceable.Segments {
	// 	segs = append(segs, html.Span(
	// 		html.StyleAttr(fmt.Sprintf("background-color: %s", seg.Color)),
	// 		html.TitleAttr(seg.Label),
	// 		html.Text(fmt.Sprintf("%x", seg.Data)),
	// 	))
	// }

	root := html.Html(
		html.Head(
			html.Title("Binary Annotator"),
			html.Style("body { font-family: monospace; }"),
		),
		html.Body(
			html.Div(
				segs,
			),
		),
	)

	log.Default().Info("rendering HTML")

	return htm.Render(context.Background(), w, root)
}

var builtinConverters = map[libstruct.TypeInstanceBuiltin]func([]byte) (starlark.Value, error){
	libstruct.TypeInstanceBuiltinUint8: func(bytes []byte) (starlark.Value, error) {
		return starlark.MakeInt(int(bytes[0])), nil
	},

	libstruct.TypeInstanceBuiltinUint16LE: func(bytes []byte) (starlark.Value, error) {
		val := binary.LittleEndian.Uint16(bytes)
		return starlark.MakeInt(int(val)), nil
	},

	libstruct.TypeInstanceBuiltinUint32LE: func(bytes []byte) (starlark.Value, error) {
		val := binary.LittleEndian.Uint32(bytes)
		return starlark.MakeInt(int(val)), nil
	},

	libstruct.TypeInstanceBuiltinChar: func(bytes []byte) (starlark.Value, error) {
		return starlark.String(bytes), nil
	},
}

func starlarkValueFromType(name string, typ libstruct.TypeInstance, bytes *BinaryAnnotator, ref libstruct.ReferenceResolver) (starlark.Value, error) {
	switch typ := typ.(type) {
	case *libstruct.TypeInstanceArray:
		var ret []starlark.Value

		if typ.ElementType == libstruct.TypeInstanceBuiltinChar {
			return starlark.String(bytes.bytes), nil
		} else if typ.ElementType == libstruct.TypeInstanceBuiltinUint8 {
			return starlark.Bytes(bytes.bytes), nil
		}

		elementSize, err := libstruct.SizeOf(typ.ElementType, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to get size of element type: %w", err)
		}

		elementCount, err := libstruct.EvaluateExpression(typ.Size)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate size expression: %w", err)
		}

		var currentOffset int64 = 0

		for i := int64(0); i < elementCount; i++ {
			slice := bytes.Slice(strconv.Itoa(int(i)), typ.ElementType.String(), elementSize, currentOffset)

			val, err := starlarkValueFromType(strconv.Itoa(int(i)), typ.ElementType, slice, ref)
			if err != nil {
				return nil, fmt.Errorf("failed to convert to starlark value: %w", err)
			}

			ret = append(ret, val)

			currentOffset += elementSize
		}

		return starlark.NewList(ret), nil
	case libstruct.TypeInstanceBuiltin:
		converter, ok := builtinConverters[typ]
		if !ok {
			return nil, fmt.Errorf("unknown builtin type: %s", typ)
		}

		return converter(bytes.bytes)
	case libstruct.TypeInstanceReference:
		inner, err := ref(typ)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference: %w", err)
		}

		return starlarkValueFromType(string(typ), inner, bytes, ref)
	case *libstruct.EnumDeclaration:
		return starlarkValueFromType(name, typ.ValueType, bytes, ref)
	case *libstruct.StructDeclaration:
		s := &Struct{
			Name: name,
			Ast:  typ,
			ref:  ref,
		}

		return s.InstanceWith(bytes)
	case *libstruct.UnionDeclaration:
		return &Union{
			Name:  name,
			Ast:   typ,
			Bytes: bytes,
			ref:   ref,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected type: %T", typ)
	}
}

type Union struct {
	Name  string
	Ast   *libstruct.UnionDeclaration
	Bytes *BinaryAnnotator

	ref libstruct.ReferenceResolver
}

// Attr implements starlark.HasAttrs.
func (s *Union) Attr(name string) (starlark.Value, error) {
	for _, member := range s.Ast.Members {
		switch member := member.(type) {
		case *libstruct.StructOrUnionMember:
			if member.Name == name {
				size, err := libstruct.SizeOf(member.ValueType, s.ref)
				if err != nil {
					return nil, fmt.Errorf("failed to get size of member: %w", err)
				}

				slice := s.Bytes.Slice(member.Name, member.ValueType.String(), size, 0)

				return starlarkValueFromType(member.Name, member.ValueType, slice, s.ref)
			}
		}
	}

	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (s *Union) AttrNames() []string {
	var names []string

	for _, member := range s.Ast.Members {
		switch member := member.(type) {
		case *libstruct.StructOrUnionMember:
			names = append(names, member.Name)
		}
	}

	return names
}

func (s *Union) String() string      { return s.Name }
func (*Union) Type() string          { return "Union" }
func (*Union) Hash() (uint32, error) { return 0, fmt.Errorf("Union is not hashable") }
func (*Union) Truth() starlark.Bool  { return starlark.True }
func (*Union) Freeze()               {}

var (
	_ starlark.Value    = (*Union)(nil)
	_ starlark.HasAttrs = (*Union)(nil)
)

type Struct struct {
	Name   string
	Ast    *libstruct.StructDeclaration
	Fields map[string]starlark.Value

	ref libstruct.ReferenceResolver
}

// Attr implements starlark.HasAttrs.
func (s *Struct) Attr(name string) (starlark.Value, error) {
	val, ok := s.Fields[name]
	if !ok {
		return nil, nil
	}

	return val, nil
}

// AttrNames implements starlark.HasAttrs.
func (s *Struct) AttrNames() []string {
	var names []string

	for name := range s.Fields {
		names = append(names, name)
	}

	return names
}

func (s *Struct) Size() (int64, error) {
	return libstruct.SizeOf(s.Ast, s.ref)
}

func (s *Struct) InstanceWith(buf *BinaryAnnotator) (*Struct, error) {
	ret := &Struct{
		Name:   s.Name,
		Ast:    s.Ast,
		Fields: make(map[string]starlark.Value),
	}

	var currentOffset int64 = 0

	for _, member := range s.Ast.Members {
		switch member := member.(type) {
		case *libstruct.StructOrUnionMember:
			size, err := libstruct.SizeOf(member.ValueType, s.ref)
			if err != nil {
				return nil, fmt.Errorf("failed to get size of member: %w", err)
			}

			slice := buf.Slice(member.Name, member.ValueType.String(), size, currentOffset)

			val, err := starlarkValueFromType(member.Name, member.ValueType, slice, s.ref)
			if err != nil {
				return nil, fmt.Errorf("failed to convert to starlark value: %w", err)
			}

			ret.Fields[member.Name] = val

			currentOffset += size
		case *libstruct.StructSizeMember:
			// ignore
		default:
			return nil, fmt.Errorf("unexpected member type: %T", member)
		}
	}

	return ret, nil
}

func (s *Struct) String() string      { return s.Name }
func (*Struct) Type() string          { return "Struct" }
func (*Struct) Hash() (uint32, error) { return 0, fmt.Errorf("Struct is not hashable") }
func (*Struct) Truth() starlark.Bool  { return starlark.True }
func (*Struct) Freeze()               {}

var (
	_ starlark.Value    = (*Struct)(nil)
	_ starlark.HasAttrs = (*Struct)(nil)
)

type BinaryReader struct {
	top *BinaryAnnotator
}

// Attr implements starlark.HasAttrs.
func (b *BinaryReader) Attr(name string) (starlark.Value, error) {
	if name == "instance" {
		return starlark.NewBuiltin("instance", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var s *Struct
			var offset int64

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"struct", &s,
				"offset", &offset,
			); err != nil {
				return nil, err
			}

			size, err := s.Size()
			if err != nil {
				return nil, fmt.Errorf("failed to get size of struct: %w", err)
			}

			child := b.top.Slice(s.Name, "struct", size, offset)

			inst, err := s.InstanceWith(child)
			if err != nil {
				return nil, fmt.Errorf("failed to instance struct: %w", err)
			}

			return inst, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (b *BinaryReader) AttrNames() []string {
	return []string{"instance"}
}

func (*BinaryReader) String() string        { return "BinaryReader" }
func (*BinaryReader) Type() string          { return "BinaryReader" }
func (*BinaryReader) Hash() (uint32, error) { return 0, fmt.Errorf("BinaryReader is not hashable") }
func (*BinaryReader) Truth() starlark.Bool  { return starlark.True }
func (*BinaryReader) Freeze()               {}

var (
	_ starlark.Value    = (*BinaryReader)(nil)
	_ starlark.HasAttrs = (*BinaryReader)(nil)
)

type ParsedStruct struct {
	ast *libstruct.File
}

func (b *ParsedStruct) resolveReference(ref libstruct.TypeInstanceReference) (libstruct.TypeInstance, error) {
	name := string(ref)

	for _, decl := range b.ast.Declarations {
		switch decl := decl.(type) {
		case *libstruct.TypeDeclaration:
			if decl.Name == name {
				return decl.InnerType, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to resolve reference: %s", name)
}

func (b *ParsedStruct) valueFromAst(name string, ast libstruct.TypeInstance) (starlark.Value, error) {
	switch ast := ast.(type) {
	case *libstruct.StructDeclaration:
		return &Struct{
			Name:   name,
			Ast:    ast,
			Fields: nil,
			ref:    b.resolveReference,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected type: %T", ast)
	}
}

// Attr implements starlark.HasAttrs.
func (b *ParsedStruct) Attr(name string) (starlark.Value, error) {
	for _, decl := range b.ast.Declarations {
		switch decl := decl.(type) {
		case *libstruct.TypeDeclaration:
			if decl.Name == name {
				return b.valueFromAst(name, decl.InnerType)
			}
		default:
			return nil, fmt.Errorf("unexpected declaration type: %T", decl)
		}
	}

	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (b *ParsedStruct) AttrNames() []string {
	var names []string

	for _, decl := range b.ast.Declarations {
		switch decl := decl.(type) {
		case *libstruct.TypeDeclaration:
			names = append(names, decl.Name)
		}
	}

	return names
}

func (*ParsedStruct) String() string        { return "ParsedStruct" }
func (*ParsedStruct) Type() string          { return "ParsedStruct" }
func (*ParsedStruct) Hash() (uint32, error) { return 0, fmt.Errorf("ParsedStruct is not hashable") }
func (*ParsedStruct) Truth() starlark.Bool  { return starlark.True }
func (*ParsedStruct) Freeze()               {}

var (
	_ starlark.Value    = (*ParsedStruct)(nil)
	_ starlark.HasAttrs = (*ParsedStruct)(nil)
)

var (
	structFile = flag.String("struct", "", "Path to the struct file")
	scriptFile = flag.String("script", "", "Path to a Starlark script used to instance the struct")
	input      = flag.String("input", "", "Path to the input file fed as input to the script")
	output     = flag.String("output", "", "Path to the output file to write a HTML report")
)

func appMain() error {
	flag.Parse()

	if *structFile == "" {
		return fmt.Errorf("struct file is required")
	}

	if *scriptFile == "" {
		return fmt.Errorf("script file is required")
	}

	if *input == "" {
		return fmt.Errorf("input file is required")
	}

	if *output == "" {
		return fmt.Errorf("output file is required")
	}

	structFile, err := os.Open(*structFile)
	if err != nil {
		return fmt.Errorf("failed to open struct file: %w", err)
	}
	defer structFile.Close()

	structAst, err := libstruct.Parse(structFile)
	if err != nil {
		return fmt.Errorf("failed to parse struct file: %w", err)
	}

	parsedStruct := &ParsedStruct{
		ast: structAst,
	}

	bytes, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	reader := &BinaryReader{
		top: &BinaryAnnotator{
			name:   "top",
			offset: 0,
			length: int64(len(bytes)),
			bytes:  bytes,
		},
	}

	thread := &starlark.Thread{}

	if _, err := starlark.ExecFileOptions(syntax.LegacyFileOptions(), thread, *scriptFile, nil, starlark.StringDict{
		"reader": reader,
		"struct": parsedStruct,
	}); err != nil {
		return fmt.Errorf("failed to execute script: %w", err)
	}

	outFile, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer outFile.Close()

	if err := reader.top.WriteHTML(outFile); err != nil {
		return fmt.Errorf("failed to write HTML: %w", err)
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Default().Error("fatal", "error", err)
		os.Exit(1)
	}
}
