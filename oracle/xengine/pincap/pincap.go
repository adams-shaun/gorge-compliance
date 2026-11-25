// Package pincap reports the capabilities of the pinned gorge module that
// this repository can only see through the module's exported API. It exists
// so one place, not each caller, decides whether a field is available at the
// pin: a feature that depends on a newer gorge commit is gated here and turns
// itself on when the pin moves, instead of being written against a field the
// pinned module does not have.
package pincap

import (
	"reflect"

	"github.com/adams-shaun/gorge/engine/rules"
)

// Counter is one player counter a scripted start seeds: gorge's counter kind
// (ENERGY, POISON, EXPERIENCE) and amount.
type Counter struct {
	Kind string
	N    int32
}

// counterElem reports the pinned rules.StartSeat.PlayerCounters element type
// when the field exists AND has the shape this package can write: a slice of
// structs with Kind string and N int32. A field that exists with another shape
// is reported as absent, so SetStartPlayerCounters can never silently return
// false at a caller that only checked PlayerCounters.
func counterElem() (reflect.Type, bool) {
	f, ok := reflect.TypeOf(rules.StartSeat{}).FieldByName("PlayerCounters")
	if !ok || f.Type.Kind() != reflect.Slice {
		return nil, false
	}
	elem := f.Type.Elem()
	if elem.Kind() != reflect.Struct {
		return nil, false
	}
	kind, okKind := elem.FieldByName("Kind")
	n, okN := elem.FieldByName("N")
	if !okKind || kind.Type.Kind() != reflect.String || !okN || n.Type.Kind() != reflect.Int32 {
		return nil, false
	}
	return elem, true
}

// PlayerCounters reports whether the pinned rules.StartSeat carries the
// PlayerCounters field in the shape SetStartPlayerCounters writes (gorge PR
// for the xproof PayEnergy starts). The oracle runner's setup field rides the
// same commit: the gorge change adds rules.oracleSeat.PlayerCounters and
// rules.StartSeat.PlayerCounters together, so one probe covers both sides.
//
// A false result is not a workaround: the caller keeps the behaviour it had at
// this pin and the field appears the moment the pin moves.
func PlayerCounters() bool {
	_, ok := counterElem()
	return ok
}

// SetStartPlayerCounters copies cs into ss.PlayerCounters when the pinned
// rules.StartSeat has that field, and reports whether it did. The field's
// element type (Kind string; N int32) is checked structurally so a renamed or
// retyped field cannot panic or silently take a wrong value.
func SetStartPlayerCounters(ss *rules.StartSeat, cs []Counter) bool {
	elem, ok := counterElem()
	if !ok {
		return false
	}
	v := reflect.ValueOf(ss).Elem().FieldByName("PlayerCounters")
	if !v.IsValid() || !v.CanSet() {
		return false
	}
	out := reflect.MakeSlice(v.Type(), 0, len(cs))
	for _, c := range cs {
		e := reflect.New(elem).Elem()
		e.FieldByName("Kind").SetString(c.Kind)
		e.FieldByName("N").SetInt(int64(c.N))
		out = reflect.Append(out, e)
	}
	v.Set(out)
	return true
}
