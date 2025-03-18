package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/tinyrange/tinyrange/pkg/log"
)

type Hash string

func (h Hash) String() string {
	return string(h)
}

func GetSha256Hash(content []byte) Hash {
	sum := sha256.Sum256(content)

	return Hash(hex.EncodeToString(sum[:]))
}

type SerializableValue interface {
	SerializableType() string
}

type SerializableList []SerializableValue

// SerializableType implements SerializableValue.
func (s SerializableList) SerializableType() string { return "SerializableList" }

type SerializableString string

// SerializableType implements SerializableValue.
func (s SerializableString) SerializableType() string { return "SerializableString" }

type SerializableBool bool

// SerializableType implements SerializableValue.
func (s SerializableBool) SerializableType() string { panic("SerializableString") }

var (
	_ SerializableValue = SerializableList{}
	_ SerializableValue = SerializableString("")
	_ SerializableValue = SerializableBool(false)
)

type ValueCaster interface {
	AsSerializableValue() (SerializableValue, error)
}

type Definition interface {
	SerializableValue

	Create(params SerializableValue) Definition
	Params() SerializableValue
}

var registeredTypes = make(map[string]SerializableValue)

func RegisterType(typ SerializableValue) {
	name := typ.SerializableType()

	if typ, exists := registeredTypes[name]; exists {
		panic(fmt.Sprintf("type %s conflicts with another type %T", name, typ))
	}

	registeredTypes[name] = typ
}

func init() {
	RegisterType(SerializableList{})
}

type serializedValue struct {
	TypeName string
	Values   map[string]json.RawMessage
}

type definitionPointer struct {
	TypeName string
	Hash     Hash
}

type serializedDefinition struct {
	TypeName string
	Params   map[string]json.RawMessage
}

type CacheMissFunction func(hash Hash) (io.ReadCloser, error)

type DefinitionDatabase struct {
	mtx          sync.RWMutex
	cache        map[Hash]Definition
	cacheInverse map[Definition]Hash
	miss         CacheMissFunction
}

func (db *DefinitionDatabase) getDefinitionByHash(hash Hash) Definition {
	db.mtx.RLock()
	defer db.mtx.RUnlock()

	def, ok := db.cache[hash]
	if !ok {
		return nil
	}
	return def
}

func (db *DefinitionDatabase) writeToCache(hash Hash, def Definition) {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	db.cache[hash] = def
	db.cacheInverse[def] = hash
}

func (db *DefinitionDatabase) GetDefinitionByHash(hash Hash) (Definition, error) {
	if def := db.getDefinitionByHash(hash); def != nil {
		return def, nil
	}

	f, err := db.miss(hash)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	def, err := db.UnmarshalDefinition(f)
	if err != nil {
		return nil, err
	}

	db.writeToCache(hash, def)

	return def, nil
}

func (db *DefinitionDatabase) getHash(d Definition) (Hash, bool) {
	db.mtx.RLock()
	defer db.mtx.RUnlock()

	hash, ok := db.cacheInverse[d]
	return hash, ok
}

func (db *DefinitionDatabase) HashDefinition(d Definition) (Hash, error) {
	if hash, ok := db.getHash(d); ok {
		return hash, nil
	}

	val, err := db.MarshalDefinition(d)
	if err != nil {
		return "", err
	}

	hash := GetSha256Hash(val)

	db.writeToCache(hash, d)

	return hash, nil
}

func (db *DefinitionDatabase) marshalSerializableValue(params SerializableValue) (map[string]json.RawMessage, error) {
	ret := make(map[string]json.RawMessage)

	val := reflect.ValueOf(params)

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("attempt to marshal non struct: %T", params)
	}

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
		} else if typ.Kind() == reflect.Map {
			ret := make(map[string]any)

			if typ.Key() != reflect.TypeFor[string]() {
				return nil, fmt.Errorf("encoding maps only supports string keys")
			}

			for _, k := range field.MapKeys() {
				key, err := encodeValue(k)
				if err != nil {
					return nil, err
				}

				val, err := encodeValue(field.MapIndex(k))
				if err != nil {
					return nil, err
				}

				ret[key.(string)] = val
			}

			return ret, nil
		} else {
			val := field.Interface()

			if caster, ok := val.(ValueCaster); ok {
				newVal, err := caster.AsSerializableValue()
				if err != nil {
					return nil, err
				}

				val = newVal
			}

			switch val := val.(type) {
			case Definition:
				hash, err := db.HashDefinition(val)
				if err != nil {
					return nil, err
				}

				return definitionPointer{
					TypeName: val.SerializableType(),
					Hash:     hash,
				}, nil
			case SerializableString:
				return val, nil
			case SerializableBool:
				return val, nil
			case SerializableList:
				var ret []any

				for _, item := range val {
					childVal := reflect.ValueOf(item)

					child, err := encodeValue(childVal)
					if err != nil {
						return nil, err
					}

					ret = append(ret, child)
				}

				return ret, nil
			case SerializableValue:
				values, err := db.marshalSerializableValue(val)
				if err != nil {
					return nil, err
				}

				return serializedValue{
					TypeName: val.SerializableType(),
					Values:   values,
				}, nil
			case string:
				return val, nil
			case int:
				return val, nil
			case bool:
				return val, nil
			case int8:
				return val, nil
			case int16:
				return val, nil
			case int32:
				return val, nil
			case int64:
				return val, nil
			case uint8:
				return val, nil
			case uint16:
				return val, nil
			case uint32:
				return val, nil
			case uint64:
				return val, nil
			default:
				return nil, fmt.Errorf("encodeValue not implemented: %T %+v", val, val)
			}
		}
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// only encode fields that are exported.
		if fieldType.PkgPath != "" {
			continue
		}

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

func (db *DefinitionDatabase) MarshalDefinition(d Definition) ([]byte, error) {
	serialized := &serializedDefinition{
		TypeName: d.SerializableType(),
	}

	params := d.Params()

	var err error
	serialized.Params, err = db.marshalSerializableValue(params)
	if err != nil {
		return nil, err
	}

	return json.Marshal(serialized)
}

func (db *DefinitionDatabase) unmarshalObject(params any, input map[string]json.RawMessage) (any, error) {
	typ := reflect.TypeOf(params)
	ret := reflect.New(typ)
	val := ret.Elem()

	var decodeValue func(field reflect.Value, val json.RawMessage) error

	decodeValue = func(field reflect.Value, val json.RawMessage) error {
		fieldType := field.Type()
		if fieldType.Implements(reflect.TypeFor[Definition]()) {
			var ptr definitionPointer

			if err := json.Unmarshal(val, &ptr); err != nil {
				return err
			}

			def, err := db.unmarshalPointer(ptr)
			if err != nil {
				return err
			}

			if def != nil {
				defVal := reflect.ValueOf(def)

				if !defVal.CanConvert(fieldType) {
					log.Info("", "kind", defVal.Kind())
					if defVal.Kind() != reflect.Pointer {
						return fmt.Errorf("can not convert %s to %s", defVal.Type(), fieldType)
					}

					defVal = defVal.Elem()

					if !defVal.CanConvert(fieldType) {
						return fmt.Errorf("can not convert %s to %s", defVal.Type(), fieldType)
					}
				}

				if !field.CanSet() {
					return fmt.Errorf("can not set field %s", field)
				}

				field.Set(defVal.Convert(fieldType))
			}

			return nil
		} else if fieldType.Implements(reflect.TypeFor[SerializableValue]()) {
			var ret any

			if err := json.Unmarshal(val, &ret); err != nil {
				return err
			}

			switch ret := ret.(type) {
			case string:
				field.Set(reflect.ValueOf(SerializableString(ret)))

				return nil
			case map[string]any:
				typeName, ok := ret["TypeName"]
				if !ok {
					return fmt.Errorf("got nested struct without type information: %+v", ret)
				}

				str, ok := typeName.(string)
				if !ok {
					return fmt.Errorf("got nested struct without type information: %+v", ret)
				}

				def, err := db.unmarshalSerializableValue(str, val)
				if err != nil {
					return err
				}

				defVal := reflect.ValueOf(def)

				if !defVal.CanConvert(fieldType) {
					if defVal.Kind() != reflect.Pointer {
						return fmt.Errorf("can not convert %s to %s", defVal.Type(), fieldType)
					}

					defVal = defVal.Elem()

					if !defVal.CanConvert(fieldType) {
						return fmt.Errorf("can not convert %s to %s", defVal.Type(), fieldType)
					}
				}

				if !field.CanSet() {
					return fmt.Errorf("can not set field %s", field)
				}

				field.Set(defVal.Convert(fieldType))

				return nil
			case []any:
				var values []json.RawMessage

				if err := json.Unmarshal(val, &values); err != nil {
					return err
				}

				retLst := reflect.MakeSlice(reflect.TypeFor[SerializableList](), len(values), len(values))

				for i, val := range values {
					if err := decodeValue(retLst.Index(i), val); err != nil {
						return err
					}
				}

				field.Set(retLst)

				return nil
			case nil:
				return nil
			default:
				return fmt.Errorf("decodeValue(SerializableValue) not implemented: %T %+v", ret, ret)
			}
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
			case reflect.Map:
				var values map[string]json.RawMessage

				if err := json.Unmarshal(val, &values); err != nil {
					return err
				}

				ret := reflect.MakeMap(fieldType)

				for k, val := range values {
					key := reflect.New(fieldType.Key()).Elem()
					if err := decodeValue(key, []byte(k)); err != nil {
						return err
					}

					value := reflect.New(fieldType.Elem()).Elem()
					if err := decodeValue(value, val); err != nil {
						return err
					}

					ret.SetMapIndex(key, value)
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
			case reflect.Int:
				var ret int

				if err := json.Unmarshal(val, &ret); err != nil {
					return err
				}

				field.SetInt(int64(ret))

				return nil
			case reflect.Bool:
				var ret bool

				if err := json.Unmarshal(val, &ret); err != nil {
					return err
				}

				field.SetBool(ret)

				return nil
			case reflect.Uint8:
				var ret uint8

				if err := json.Unmarshal(val, &ret); err != nil {
					return err
				}

				field.SetUint(uint64(ret))

				return nil
			case reflect.Int64:
				var ret int64

				if err := json.Unmarshal(val, &ret); err != nil {
					return err
				}

				field.SetInt(ret)

				return nil
			default:
				return fmt.Errorf("decodeValue not implemented: %s %s", fieldType, val)
			}
		}
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		val, ok := input[fieldType.Name]
		if !ok {
			// Ignore fields that don't exist in the input.
			continue
		}

		if !field.CanSet() {
			return nil, fmt.Errorf("cannot set field %s", fieldType.Name)
		}

		if err := decodeValue(field, val); err != nil {
			return nil, err
		}
	}

	return val.Interface(), nil
}

func (db *DefinitionDatabase) unmarshalParameters(params SerializableValue, input map[string]json.RawMessage) (SerializableValue, error) {
	ret, err := db.unmarshalObject(params, input)
	if err != nil {
		return nil, err
	}

	return ret.(SerializableValue), nil
}

func (db *DefinitionDatabase) UnmarshalDefinition(input io.Reader) (Definition, error) {
	var def serializedDefinition

	dec := json.NewDecoder(input)

	if err := dec.Decode(&def); err != nil {
		return nil, err
	}

	val, ok := registeredTypes[def.TypeName]
	if !ok {
		return nil, fmt.Errorf("factory for type %s not found", def.TypeName)
	}

	fac, ok := val.(Definition)
	if !ok {
		return nil, fmt.Errorf("factory for type %s is not a Definition", def.TypeName)
	}

	params, err := db.unmarshalParameters(fac.Params(), def.Params)
	if err != nil {
		return nil, err
	}

	return fac.Create(params), nil
}

func (db *DefinitionDatabase) unmarshalPointer(ptr definitionPointer) (Definition, error) {
	if ptr.TypeName == "" {
		// assume a null ptr.

		return nil, nil
	}

	if ptr.Hash == "" {
		return nil, fmt.Errorf("attempt to unmarshalPointer with empty hash")
	}

	if val := db.getDefinitionByHash(ptr.Hash); val != nil {
		return val, nil
	}

	f, err := db.miss(ptr.Hash)
	if err != nil {
		return nil, fmt.Errorf("could not find definitionCache entry for %s: %s", ptr.Hash, err)
	}
	defer f.Close()

	def, err := db.UnmarshalDefinition(f)
	if err != nil {
		return nil, err
	}

	db.writeToCache(ptr.Hash, def)

	return def, nil
}

func (db *DefinitionDatabase) unmarshalSerializableValue(typeName string, val json.RawMessage) (SerializableValue, error) {
	fac, ok := registeredTypes[typeName]
	if !ok {
		return nil, fmt.Errorf("factory for type %s not found", typeName)
	}

	if _, ok := fac.(Definition); ok {
		var ptr definitionPointer

		if err := json.Unmarshal(val, &ptr); err != nil {
			return nil, err
		}

		return db.unmarshalPointer(ptr)
	} else {
		var obj serializedValue

		if err := json.Unmarshal(val, &obj); err != nil {
			return nil, err
		}

		ret, err := db.unmarshalObject(fac, obj.Values)
		if err != nil {
			return nil, err
		}

		return ret.(SerializableValue), nil
	}
}

func NewDefinitionDatabase(miss CacheMissFunction) *DefinitionDatabase {
	return &DefinitionDatabase{
		cache:        make(map[Hash]Definition),
		cacheInverse: make(map[Definition]Hash),
		miss:         miss,
	}
}
