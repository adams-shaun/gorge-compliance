package oraclegen_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestXMageAbilityNicknameSelfReference pins the cost-prefix rewrite for a
// card whose Oracle text names it by a short form where XMage's rule text
// carries {this}. King Darien XLVIII is the comma-less case: the oracle prints
// "Sacrifice King Darien" while SacrificeSourceCost's rule text is "sacrifice
// {this}" (KingDarienXLVIII.java), so the emitted selector must be the {this}
// spelling or TestPlayer's startsWith lookup finds nothing. Jirina is the
// comma-named control, Catti-brie the NICKNAME-in-Cost$ spelling, and
// Goblin Sledder the negative: its "Sacrifice a Goblin" names a type, not the
// source, so the marker-gated rewrite must leave it alone.
func TestXMageAbilityNicknameSelfReference(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		// Comma-less, NICKNAME in the first ability's description only.
		{"King Darien XLVIII", map[int]string{0: "{3}{G}{W}", 1: "Sacrifice {this}"}},
		// Comma-named control: the title is the short form.
		{"Jirina, Dauntless General", map[int]string{0: "Sacrifice {this}"}},
		// Comma-less, NICKNAME only in the Cost$ token.
		{"Catti-brie of Mithral Hall", map[int]string{0: "{1}, Remove all +1/+1 counters from {this}"}},
		// Comma-less, NICKNAME in both costs and descriptions.
		{"Rasputin Dreamweaver", map[int]string{
			0: "Remove a dream counter from {this}: Add",
			1: "Remove a dream counter from {this}: Prevent",
		}},
		// Comma-less with no NICKNAME token: "Goblin" is a creature type the
		// cost may sacrifice, not the source's short name.
		{"Goblin Sledder", map[int]string{0: "Sacrifice a Goblin"}},
	} {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", tc.card)
			}
			got, why := oraclegen.XMageAbility(c.Faces[0])
			if why != "" {
				t.Fatalf("%s mapping ambiguous: %s", tc.card, why)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("%s mapped %d abilities %q, want %d", tc.card, len(got), got, len(tc.want))
			}
			for i, prefix := range tc.want {
				if got[i] != prefix {
					t.Errorf("%s ability %d prefix = %q, want %q", tc.card, i, got[i], prefix)
				}
			}
		})
	}
}

// TestXMageAbilityCommaLessShortNameExtensionFailsClosed pins the other half
// of the comma-less rewrite: when the short name appears in the rule text an
// extension has to cross, the mapping still fails closed instead of copying a
// name XMage prints as {this} only in the cost. The face uses the NICKNAME
// token, so the short name is derived exactly as it is for King Darien.
func TestXMageAbilityCommaLessShortNameExtensionFailsClosed(t *testing.T) {
	f := &cards.Face{
		Name:   "Test Source XLVIII",
		Oracle: "{T}: Test Source gains flying.\n{T}: Test Source gains vigilance.",
		Abilities: []*cards.SA{
			{Kind: "AB", Params: map[string]string{"SpellDescription": "NICKNAME gains flying."}},
			{Kind: "AB", Params: map[string]string{"SpellDescription": "NICKNAME gains vigilance."}},
		},
	}
	got, why := oraclegen.XMageAbility(f)
	if got != nil || why != "activate xmage text ambiguous" {
		t.Fatalf("comma-less short-name extension = %v, %q; want ambiguous", got, why)
	}
}
