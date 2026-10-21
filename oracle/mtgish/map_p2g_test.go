package mtgish

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Synthetic parameters and made-up mtgish JSON only; no card text.

func TestP2gMayPlay(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    map[string]string
		want string
	}{
		{"an alternative cost with no zone permission", map[string]string{"MayPlayAltManaCost": "0", "MayPlayDontGrantZonePermissions": "True"}, "alternativecost"},
		{"flash alone", map[string]string{"MayPlayWithFlash": "True"}, "flash"},
		{"flash beside an alternative cost stays a may-play", map[string]string{"MayPlayWithFlash": "True", "MayPlayAltManaCost": "0", "MayPlayDontGrantZonePermissions": "True"}, "mayplay"},
		{"flash beside a free cast stays a may-play", map[string]string{"MayPlayWithFlash": "True", "MayPlayWithoutManaCost": "True"}, "mayplay"},
		{"a plain permission", map[string]string{}, "mayplay"},
	} {
		if got := p2gMayPlay(tc.p); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestP2gPlayerEffect(t *testing.T) {
	for _, tc := range []struct {
		name, js string
		want     []string
	}{
		{"once each turn, cast for an alternate cost",
			`{"_PlayerEffect":"OnceEachTurnMayAction","args":{"_Action":"CastASpellFromHandForAlternateCost","args":[]}}`,
			[]string{"alternativecost"}},
		{"once each turn, another action",
			`{"_PlayerEffect":"OnceEachTurnMayAction","args":{"_Action":"DrawACard"}}`, nil},
	} {
		var x any
		if err := json.Unmarshal([]byte(tc.js), &x); err != nil {
			t.Fatal(err)
		}
		got, _ := p2gPlayerEffect(x)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
