package redact

import (
	"reflect"
	"strings"
)

// Redaction from what the component declared, rather than from guessing.
//
// A component says which of its fields hold credentials — `format:"password"`
// on the struct tag, which is also what makes the editor render the field
// masked. That declaration is the fact; everything else in this package is an
// inference standing in for it. Matching field names catches `apiKey` and
// misses `x`; matching value shapes catches `sk-ant-…` and misses a password,
// a cookie, or a token from a provider whose format nobody hardcoded.
//
// Use this wherever the typed value is still in hand. Fall back to the
// heuristics only past that point, where a payload has become bytes and the
// schema is gone.

// secretTagValues mark a field as holding a credential. `format:"password"` is
// the one components already use, because it is what masks the field in the
// editor — a field a user is shown as dots is a field the runtime should not
// write down either.
func isSecretField(f reflect.StructField) bool {
	if strings.EqualFold(f.Tag.Get("format"), "password") {
		return true
	}
	if strings.EqualFold(f.Tag.Get("secret"), "true") {
		return true
	}
	return strings.EqualFold(f.Tag.Get("writeOnly"), "true")
}

// Declared returns a copy of v with every declared-secret field masked.
//
// Works on a struct, or any map/slice containing one. A value with no declared
// secrets comes back with changed=false, so a caller can skip the copy
// entirely — which is most payloads.
func Declared(v any) (any, bool) {
	if v == nil {
		return nil, false
	}
	out, changed := walk(reflect.ValueOf(v))
	if !changed {
		return v, false
	}
	return out.Interface(), true
}

func walk(rv reflect.Value) (reflect.Value, bool) {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return rv, false
		}
		inner, changed := walk(rv.Elem())
		if !changed {
			return rv, false
		}
		// The masked copy is returned as its own value: rebuilding the pointer
		// would mutate what the caller still holds, and this runs on payloads
		// that are about to be handled.
		return inner, true

	case reflect.Struct:
		return walkStruct(rv)

	case reflect.Map:
		return walkMap(rv)

	case reflect.Slice, reflect.Array:
		return walkSlice(rv)
	}
	return rv, false
}

func walkStruct(rv reflect.Value) (reflect.Value, bool) {
	t := rv.Type()
	copied := reflect.New(t).Elem()
	copied.Set(rv)
	changed := false

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported: nothing here can reach it anyway
		}
		fv := copied.Field(i)

		if isSecretField(f) {
			if fv.Kind() == reflect.String && fv.String() != "" {
				fv.SetString(Value)
				changed = true
			}
			continue
		}
		if inner, c := walk(fv); c {
			if inner.Type().AssignableTo(fv.Type()) {
				fv.Set(inner)
			} else if fv.Kind() == reflect.Interface {
				fv.Set(inner)
			}
			changed = true
		}
	}
	return copied, changed
}

func walkMap(rv reflect.Value) (reflect.Value, bool) {
	if rv.IsNil() {
		return rv, false
	}
	copied := reflect.MakeMapWithSize(rv.Type(), rv.Len())
	changed := false
	iter := rv.MapRange()
	for iter.Next() {
		val := iter.Value()
		if inner, c := walk(val); c {
			copied.SetMapIndex(iter.Key(), inner)
			changed = true
			continue
		}
		copied.SetMapIndex(iter.Key(), val)
	}
	if !changed {
		return rv, false
	}
	return copied, true
}

func walkSlice(rv reflect.Value) (reflect.Value, bool) {
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return rv, false
	}
	copied := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), rv.Len(), rv.Len())
	changed := false
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if inner, c := walk(item); c {
			copied.Index(i).Set(inner)
			changed = true
			continue
		}
		copied.Index(i).Set(item)
	}
	if !changed {
		return rv, false
	}
	return copied, true
}
