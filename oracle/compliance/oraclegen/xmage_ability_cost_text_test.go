package oraclegen_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestXMageAbilityCostSpelling pins the rewrites of the level-B activate
// prefix to XMage's rule text (xmage_ability_cost_text.go) on real corpus
// faces. Each want is the text AbilityImpl.getRule() renders for that ability;
// the Oracle-spelled prefix XMage rejected ("Can't find ability to activate
// command") is in the comment.
func TestXMageAbilityCostSpelling(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		// "{4}, {T}, Sacrifice this artifact": SacrificeSourceCost is "sacrifice {this}".
		{"Disruptor Pistol", map[int]string{0: "{4}, {T}, Sacrifice {this}"}},
		// "{5}, {T}, Sacrifice this land".
		{"Room of Refuge", map[int]string{0: "{T}", 1: "{5}, {T}, Sacrifice {this}"}},
		// "Sacrifice this creature".
		{"Budding Insurgent", map[int]string{0: "Sacrifice {this}"}},
		// "{3}, Exile Emrakul... from your hand": ExileSourceFromHandCost.
		{"Emrakul, the Exigent Doom", map[int]string{0: "{3}, Exile {this} from your hand"}},
		// Renew is an ability word: "<i>Renew</i> &mdash; ", and the
		// graveyard exile stays "this card".
		{"Sage of the Fang", map[int]string{0: "<i>Renew</i> &mdash; {3}{G}, Exile this card from your graveyard"}},
		// Channel: ability word plus DiscardSourceCost "discard this card".
		{"Boseiju, Who Endures", map[int]string{0: "{T}", 1: "<i>Channel</i> &mdash; {1}{G}, Discard this card"}},
		// Exhaust / Power-up / Boast: the ability class prepends a bare "Word &mdash; ".
		{"Brawn, Amadeus Cho", map[int]string{0: "Power-up &mdash; {4}{G/U}"}},
		{"Varragoth, Bloodsky Sire", map[int]string{0: "Boast &mdash; {1}{B}"}},
		// Forge prints this Exhaust header with an ASCII hyphen.
		{"Liliana the Repentant", map[int]string{0: "Exhaust &mdash; {5}{B}"}},
		// WaterbendCost is a mana-cost symbol: "waterbend {N}", lower-case.
		{"Aang's Iceberg", map[int]string{0: "waterbend {3}"}},
		{"North Pole Patrol", map[int]string{0: "{T}", 1: "waterbend {3}, {T}"}},
		{"Katara, Water Tribe's Hope", map[int]string{0: "waterbend {X}"}},
		{"Invasion Submersible", map[int]string{0: "Exhaust &mdash; waterbend {3}"}},
		// "Remove five +1/+1 counters from Ramos": the Oracle names the
		// card by its short name; RemoveCountersSourceCost prints
		// "remove five +1/+1 counters from {this}".
		{"Ramos, Dragon Engine", map[int]string{0: "Remove five +1/+1 counters from {this}"}},
		// The four MKM Cases: the solved ability's SacrificeSourceCost is
		// overridden with setText("sacrifice this Case") and the flavor
		// word "Solved" is italicized, so the Oracle spelling stays.
		{"Case of the Burning Masks", map[int]string{0: "<i>Solved</i> &mdash; Sacrifice this Case"}},
		{"Case of the Filched Falcon", map[int]string{0: "<i>Solved</i> &mdash; {2}{U}, Sacrifice this Case"}},
		{"Case of the Stashed Skeleton", map[int]string{0: "<i>Solved</i> &mdash; {1}{B}, Sacrifice this Case"}},
		{"Case of the Uneaten Feast", map[int]string{0: "<i>Solved</i> &mdash; Sacrifice this Case"}},
		// "{1}{W},{T}, Exile this artifact": the Oracle prints no space after
		// the first comma; Costs.getText joins with ", " (Phoenix Down).
		{"Phoenix Down", map[int]string{0: "{1}{W}, {T}, Exile {this}"}},
		// "named {this}" keeps the printed name: the cost text is a target
		// filter's message built from a literal name (GrandeurAbility's
		// "another card named " + cardName; Say Its Name's filter).
		{"Page, Loose Leaf", map[int]string{0: "{T}", 1: "<i>Grandeur</i> &mdash; Discard another card named Page, Loose Leaf"}},
		{"Say Its Name", map[int]string{1: "Exile this card and two other cards named Say Its Name from your graveyard"}},
	} {
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
	}
}
