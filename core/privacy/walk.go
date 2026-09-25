package privacy

import (
	"reflect"
	"strings"
)

var textType = reflect.TypeFor[Text]()

// Walk calls fn for every Text in the value that v points to. The path is
// the dotted JSON name of the field, such as "command" or "input". Walk
// visits struct fields, pointers, slices, and arrays. So a new payload field
// of type Text gets labels and export treatment with no new code.
func Walk(v any, fn func(path string, t *Text)) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return
	}
	walk(rv.Elem(), "", fn)
}

func walk(v reflect.Value, path string, fn func(string, *Text)) {
	if v.Type() == textType {
		if v.CanAddr() {
			fn(path, v.Addr().Interface().(*Text))
		}
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			walk(v.Elem(), path, fn)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			walk(v.Field(i), joinPath(path, jsonName(f)), fn)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walk(v.Index(i), path, fn)
		}
	}
}

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "" || name == "-" {
		return f.Name
	}
	return name
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}
