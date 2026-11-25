package httpapi

import (
	"encoding/json"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// The document declares arrays and maps non-nullable (huma.DefaultArrayNullable
// is off), so responses must never carry a JSON null where a list or object
// belongs: emptySlices answers every nil slice in a response body as [] and
// every nil map as {}.

func init() {
	huma.DefaultArrayNullable = false
}

var rawMessageType = reflect.TypeFor[json.RawMessage]()

// emptySlices is a Huma transformer: v with every nil slice and map made
// empty. It copies only when v holds one somewhere.
func emptySlices(_ huma.Context, _ string, v any) (any, error) {
	value := reflect.ValueOf(v)
	if !value.IsValid() || !hasNilSlice(value, 0) {
		return v, nil
	}
	clone := reflect.New(value.Type()).Elem()
	clone.Set(value)
	fillSlices(clone, 0)
	return clone.Interface(), nil
}

// maxSliceDepth bounds the walk; response bodies are far shallower.
const maxSliceDepth = 32

func hasNilSlice(v reflect.Value, depth int) bool {
	if depth > maxSliceDepth {
		return false
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil() && hasNilSlice(v.Elem(), depth+1)
	case reflect.Slice:
		if v.Type() == rawMessageType {
			return false
		}
		if v.IsNil() {
			return true
		}
		for i := range v.Len() {
			if hasNilSlice(v.Index(i), depth+1) {
				return true
			}
		}
	case reflect.Array:
		for i := range v.Len() {
			if hasNilSlice(v.Index(i), depth+1) {
				return true
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() && hasNilSlice(v.Field(i), depth+1) {
				return true
			}
		}
	case reflect.Map:
		if v.IsNil() {
			return true
		}
		for iter := v.MapRange(); iter.Next(); {
			if hasNilSlice(iter.Value(), depth+1) {
				return true
			}
		}
	}
	return false
}

// fillSlices makes every nil slice reachable from the settable v empty,
// copying the pointers, slices and maps on the way so the original value is
// never changed.
func fillSlices(v reflect.Value, depth int) {
	if depth > maxSliceDepth || !hasNilSlice(v, depth) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		elem := reflect.New(v.Type().Elem())
		elem.Elem().Set(v.Elem())
		fillSlices(elem.Elem(), depth+1)
		v.Set(elem)
	case reflect.Interface:
		elem := reflect.New(v.Elem().Type()).Elem()
		elem.Set(v.Elem())
		fillSlices(elem, depth+1)
		v.Set(elem)
	case reflect.Slice:
		if v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			return
		}
		copied := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(copied, v)
		for i := range copied.Len() {
			fillSlices(copied.Index(i), depth+1)
		}
		v.Set(copied)
	case reflect.Array:
		for i := range v.Len() {
			fillSlices(v.Index(i), depth+1)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillSlices(v.Field(i), depth+1)
			}
		}
	case reflect.Map:
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
			return
		}
		copied := reflect.MakeMapWithSize(v.Type(), v.Len())
		for iter := v.MapRange(); iter.Next(); {
			elem := reflect.New(v.Type().Elem()).Elem()
			elem.Set(iter.Value())
			fillSlices(elem, depth+1)
			copied.SetMapIndex(iter.Key(), elem)
		}
		v.Set(copied)
	}
}
