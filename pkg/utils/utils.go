package utils

import "reflect"

// CheckForError takes any input and returns the error if it is one, otherwise nil.
func CheckForError(input interface{}) error {
	err, ok := input.(error)
	if ok {
		return err
	}
	return nil
}

// IsNil checks if a value is nil, including typed nils.
// In Go, an interface containing a typed nil (e.g., (*T)(nil)) is not == nil.
// This function handles both cases.
func IsNil(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
