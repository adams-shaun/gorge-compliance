package pincap

import (
	"reflect"
	"testing"
)

// TestCounterShape: only a struct with exported Kind string and N int32 is
// writable. An unexported field must be reported absent, or the reflective
// setter would panic at SetString on a value it cannot set.
func TestCounterShape(t *testing.T) {
	type good struct {
		Kind string
		N    int32
	}
	type hiddenKind struct {
		kind string
		N    int32
	}
	type hiddenN struct {
		Kind string
		n    int32
	}
	type wrongN struct {
		Kind string
		N    int
	}
	for _, tc := range []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"good", reflect.TypeOf(good{}), true},
		{"unexported kind", reflect.TypeOf(hiddenKind{}), false},
		{"unexported n", reflect.TypeOf(hiddenN{}), false},
		{"wrong n type", reflect.TypeOf(wrongN{}), false},
		{"not a struct", reflect.TypeOf(0), false},
	} {
		if got := counterShape(tc.typ); got != tc.want {
			t.Errorf("%s: counterShape = %v, want %v", tc.name, got, tc.want)
		}
	}
}
