// Package canonical implements deterministic JSON serialization used by
// AARM components that must hash or compare semantically identical content.
// Object keys are recursively sorted. Arrays preserve order. Scalars use the
// encoding/json defaults. Hot inputs (map[string]interface{},
// []map[string]interface{}, []interface{}, []string) are walked directly
// without round-tripping through encoding/json first. Any other type falls
// through to json.Marshal.
package canonical

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
)

// MarshalJSON serializes v with recursively sorted object keys. Nil inputs
// (untyped nil, or a nil map/slice/pointer of any concrete type) serialize
// to the JSON literal "null" so callers that hash optional payloads get a
// stable representation regardless of whether the caller passes a typed nil
// or no value at all.
func MarshalJSON(v interface{}) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	if rv := reflect.ValueOf(v); rv.IsValid() {
		switch rv.Kind() {
		case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface:
			if rv.IsNil() {
				return []byte("null"), nil
			}
		}
	}
	switch x := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(keyBytes)
			buf.WriteByte(':')
			child, err := MarshalJSON(x[k])
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []map[string]interface{}:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			child, err := MarshalJSON(item)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case []interface{}:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			child, err := MarshalJSON(item)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case []string:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			child, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(x)
	}
}
