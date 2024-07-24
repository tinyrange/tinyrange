package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"reflect"
)

func getSha256Hash(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

type Parameters interface {
	TagParameters()
}

type Definition interface {
	Type() string
	Create(params Parameters) Definition
	Params() Parameters
}

var registeredTypes = make(map[string]Definition)

func registerType(typ Definition) {
	registeredTypes[typ.Type()] = typ
}

type DefinitionPointer struct {
	TypeName string
	Hash     string
}

type TestParameters struct {
	Recurse *TestDef
	Array1  []string
	Array2  []Definition
}

// TagParameters implements parameters.
func (t TestParameters) TagParameters() { panic("unimplemented") }

var (
	_ Parameters = TestParameters{}
)

type TestDef struct {
	params TestParameters
}

// Create implements definition.
func (t *TestDef) Create(params Parameters) Definition {
	return &TestDef{params: *params.(*TestParameters)}
}

// Type implements definition.
func (t *TestDef) Type() string { return "testDef" }

// Params implements definition.
func (t *TestDef) Params() Parameters { return t.params }

var (
	_ Definition = &TestDef{}
)

type serializedDefinition struct {
	TypeName string
	Params   map[string]json.RawMessage
}

type definitionDatabase struct {
	cache map[string]Definition
}

func (db *definitionDatabase) hashDefinition(d Definition) (string, error) {
	val, err := db.marshalDefinition(d)
	if err != nil {
		return "", err
	}

	hash := getSha256Hash(val)

	db.cache[hash] = d

	return hash, nil
}

func (db *definitionDatabase) marshalParameters(params Parameters) (map[string]json.RawMessage, error) {
	ret := make(map[string]json.RawMessage)

	val := reflect.ValueOf(params)
	typ := reflect.TypeOf(params)

	var encodeValue func(val reflect.Value) (any, error)

	encodeValue = func(field reflect.Value) (any, error) {
		typ := field.Type()

		if (typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Interface) && field.IsNil() {
			return nil, nil
		}

		if typ.Kind() == reflect.Slice {
			var ret []any

			for i := 0; i < field.Len(); i++ {
				val, err := encodeValue(field.Index(i))
				if err != nil {
					return nil, err
				}

				ret = append(ret, val)
			}

			return ret, nil
		} else {
			val := field.Interface()

			switch val := val.(type) {
			case Definition:
				hash, err := db.hashDefinition(val)
				if err != nil {
					return nil, err
				}

				return DefinitionPointer{
					TypeName: val.Type(),
					Hash:     hash,
				}, nil
			case string:
				return val, nil
			default:
				return nil, fmt.Errorf("encodeValue not implemented: %T %+v", val, val)
			}
		}
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		encoded, err := encodeValue(field)
		if err != nil {
			return nil, err
		}

		marshalled, err := json.Marshal(encoded)
		if err != nil {
			return nil, err
		}

		ret[fieldType.Name] = marshalled
	}

	return ret, nil
}

func (db *definitionDatabase) marshalDefinition(d Definition) ([]byte, error) {
	serialized := &serializedDefinition{
		TypeName: d.Type(),
	}

	params := d.Params()

	var err error
	serialized.Params, err = db.marshalParameters(params)
	if err != nil {
		return nil, err
	}

	return json.Marshal(serialized)
}

func (db *definitionDatabase) unmarshalParameters(params Parameters, input map[string]json.RawMessage) (Parameters, error) {
	typ := reflect.TypeOf(params)
	ret := reflect.New(typ)
	val := ret.Elem()

	var decodeValue func(field reflect.Value, val json.RawMessage) error

	decodeValue = func(field reflect.Value, val json.RawMessage) error {
		fieldType := field.Type()
		if fieldType.Implements(reflect.TypeFor[Definition]()) {
			var ptr DefinitionPointer

			if err := json.Unmarshal(val, &ptr); err != nil {
				return err
			}

			def, err := db.unmarshalPointer(ptr)
			if err != nil {
				return err
			}

			defVal := reflect.ValueOf(def)

			if !defVal.CanConvert(fieldType) {
				return fmt.Errorf("can not convert %s to %s", defVal.Type(), fieldType)
			}

			if !field.CanSet() {
				return fmt.Errorf("can not set field %s", field)
			}

			field.Set(defVal.Convert(fieldType))

			return nil
		} else {
			switch fieldType.Kind() {
			case reflect.Slice:
				var values []json.RawMessage

				if err := json.Unmarshal(val, &values); err != nil {
					return err
				}

				ret := reflect.MakeSlice(fieldType, len(values), len(values))

				for i, val := range values {
					if err := decodeValue(ret.Index(i), val); err != nil {
						return err
					}
				}

				field.Set(ret)

				return nil
			case reflect.String:
				var ret string

				if err := json.Unmarshal(val, &ret); err != nil {
					return err
				}

				field.SetString(ret)

				return nil
			default:
				return fmt.Errorf("decodeValue not implemented: %s", fieldType)
			}
		}
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		val, ok := input[fieldType.Name]
		if !ok {
			return nil, fmt.Errorf("could not find field with name %s", fieldType.Name)
		}

		if !field.CanSet() {
			return nil, fmt.Errorf("cannot set field %s", fieldType.Name)
		}

		if err := decodeValue(field, val); err != nil {
			return nil, err
		}
	}

	return ret.Interface().(Parameters), nil
}

func (db *definitionDatabase) unmarshalDefinition(input io.Reader) (Definition, error) {
	var def serializedDefinition

	dec := json.NewDecoder(input)

	if err := dec.Decode(&def); err != nil {
		return nil, err
	}

	fac, ok := registeredTypes[def.TypeName]
	if !ok {
		return nil, fmt.Errorf("factory for type %s not found", def.TypeName)
	}

	params, err := db.unmarshalParameters(fac.Params(), def.Params)
	if err != nil {
		return nil, err
	}

	return fac.Create(params), nil
}

func (db *definitionDatabase) unmarshalPointer(ptr DefinitionPointer) (Definition, error) {
	val, ok := db.cache[ptr.Hash]
	if !ok {
		return nil, fmt.Errorf("could not find definitionCache entry for %s", ptr.Hash)
	}

	return val, nil
}

func main() {
	registerType(&TestDef{})

	db := &definitionDatabase{cache: make(map[string]Definition)}

	t1 := &TestDef{params: TestParameters{Array1: []string{"Hello, World"}}}

	t2 := &TestDef{params: TestParameters{Recurse: t1, Array1: []string{"hello2"}, Array2: []Definition{t1}}}

	val, err := db.marshalDefinition(t2)
	if err != nil {
		log.Fatal(err)
	}

	slog.Info("", "val", string(val))

	t3, err := db.unmarshalDefinition(bytes.NewBuffer(val))
	if err != nil {
		log.Fatal(err)
	}

	slog.Info("", "t3", t3, "t3.recurse", t3.Params().(TestParameters).Recurse)
}
