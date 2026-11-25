package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
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
		got, ok := XMageGrantedAbilityPrefix(sa, "Homing Sliver", nil)
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

// grantSA builds a granted-ability SA from its Forge params.
func grantSA(params map[string]string) *cards.SA { return &cards.SA{Params: params} }

// TestGrantedCostPrefixExtendsOnCollision: Mu Yanling's emblem grants an
// Island "{T}: Draw a card.". The cost alone ("{T}") is also the start of the
// Island's own "{T}: Add {U}.", so XMage's startsWith selection would tap it
// for mana; the prefix is extended into the description until it is unique. A
// recipient with nothing to collide with keeps the bare cost ("{R}").
func TestGrantedCostPrefixExtendsOnCollision(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	face := func(name string) *cards.Face {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("no corpus card %q", name)
		}
		return c.Faces[0]
	}
	island := face("Island")
	draw := grantSA(map[string]string{"Cost": "T", "SpellDescription": "Draw a card."})
	got, ok := XMageGrantedAbilityPrefix(draw, "Island", island)
	if !ok {
		t.Fatal("Island grant refused")
	}
	if strings.HasPrefix("{T}: Add {U}.", got) || !strings.HasPrefix("{T}: Draw a card.", got) {
		t.Errorf("Island grant prefix = %q, want a prefix of the draw text that is not a prefix of {T}: Add {U}.", got)
	}
	if want := "{T}: Draw"; got != want {
		t.Errorf("Island grant prefix = %q, want %q", got, want)
	}
	// No other text starts with {T} on a vanilla recipient: the cost stays.
	if got, ok := XMageGrantedAbilityPrefix(draw, "Grizzly Bears", face("Grizzly Bears")); !ok || got != "{T}" {
		t.Errorf("vanilla recipient = %q, %v; want {T}", got, ok)
	}
	pump := grantSA(map[string]string{"Cost": "R", "SpellDescription": "CARDNAME gets +1/+0 until end of turn."})
	if got, ok := XMageGrantedAbilityPrefix(pump, "Dalakos, Crafter of Wonders", face("Dalakos, Crafter of Wonders")); !ok || got != "{R}" {
		t.Errorf("lone mana-cost grant = %q, %v; want {R}", got, ok)
	}
	// An extension that cannot distinguish the text, a description that runs
	// through a self reference, and an unknown recipient all fail closed.
	same := grantSA(map[string]string{"Cost": "T", "SpellDescription": "Add {U}."})
	if got, ok := XMageGrantedAbilityPrefix(same, "Island", island); ok {
		t.Errorf("identical text = %q, want a refusal", got)
	}
	self := grantSA(map[string]string{"Cost": "T", "SpellDescription": "CARDNAME deals 1 damage to any target."})
	if got, ok := XMageGrantedAbilityPrefix(self, "Island", island); ok {
		t.Errorf("self-referential description = %q, want a refusal", got)
	}
	if got, ok := XMageGrantedAbilityPrefix(draw, "Island", nil); ok {
		t.Errorf("nil recipient = %q, want a refusal", got)
	}
}
