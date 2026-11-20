package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestGrantedTypeCyclingPrefix pins the XMage rule text of a granted
// TypeCycling line: XMage's CyclingAbility carries the printed type word
// ("Swampcycling", "Slivercycling", "Artifact landcycling"), so the selector
// is that word plus the braced cost -- not the Forge keyword head.
func TestGrantedTypeCyclingPrefix(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"TypeCycling:Swamp:3", "Swampcycling {3}"},
		{"TypeCycling:Sliver:3", "Slivercycling {3}"},
		{"TypeCycling:Basic:1 G", "Basic landcycling {1}{G}"},
		{"TypeCycling:Land.Artifact:2", "Artifact landcycling {2}"},
		// A cost the exporter cannot spell still selects by the printed word.
		{"TypeCycling:Swamp:Pay 2 life", "Swampcycling"},
	}
	for _, tc := range cases {
		sa := cards.GrantedKeywordAbility(tc.line)
		if sa == nil {
			t.Fatalf("%q: GrantedKeywordAbility returned nil", tc.line)
		}
		got, ok := XMageGrantedAbilityPrefix(sa, "Homing Sliver")
		if !ok {
			t.Errorf("%q: refused, want %q", tc.line, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: prefix = %q, want %q", tc.line, got, tc.want)
		}
	}
	// The keyword head alone is not a type: fail closed rather than selecting
	// nothing for a real granted cycling window.
	if got, ok := xmageGrantedKeywordPrefix("TypeCycling::3"); ok {
		t.Errorf("TypeCycling with no type word: prefix = %q, want a refusal", got)
	}
}
