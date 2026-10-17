package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestStaticG7ClassServed is the level-B wave-4 class G7 ("static effect
// unobservable") ratchet over the rows lb4-g7 served: each row's requirement
// is generated, and its scenario carries the observation that serves it.
// A row that goes back to a skip, or loses its observation, fails here by
// name.
func TestStaticG7ClassServed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key string
		// want is a substring of the item's JSON the observation must put
		// there: the expectation, the fixture or the compare opt-in.
		want string
	}{
		// A stack-scoped Delve grant priced by the offer gate (rules'
		// hasCastDelve): the probe one short of mana is offered.
		{"Teval, Arbiter of Virtue", "static#0.0", `"offered":{"seat":0,"kind":"cast","card":"p0:Hill Giant"},"want":true`},
		// Granted cast-trigger keywords (static_granted_spell_trigger.go).
		{"Prismari, the Inspiration", "static#0.0", `"trigger_on_stack":"p0:Divination","want":true`},
		{"Quandrix, the Proof", "static#0.0", `"trigger_on_stack":"p0:Divination","want":true`},
		// Split second (static_granted_split_second.go).
		{"Samut, Tyrant of Naktamun", "static#0.0", `"offered":{"seat":0,"kind":"cast","card":"p0:Shock"},"want":false`},
		// Colour hexproof-from under its own compare opt-in.
		{"Tam, Mindful First-Year", "static#0.0", `"compare":["keywords_hexproof_from"]`},
		// Token recipient and token target (static_granted_token.go).
		{"Insidious Roots", "static#0.0", `"card":"p0:token:Goblin"`},
		{"Ghired, Mirror of the Wilds", "static#0.0", `"card":"p0:Dragon Fodder"`},
		// Gated self grant target fixtures (static_granted_gated_targets.go).
		{"Slick Imitator", "static#0.0", `"card":"p0:Accumulated Knowledge"`},
		{"Adagia, Windswept Bastion", "static#0.0", `"Ornithopter"`},
		// Behold riders: the Condition$ OptionalCost gate and the surveil
		// graveyarded by the paid branch.
		{"Osseous Exhale", "static#0.0", `"cast_mode":"optionalcost"`},
		{"Piercing Exhale", "static#0.0", `{"kind":"arrange","pick":[]}`},
		// Lesson graveyard gates (static_legality_gated.go).
		{"The Lion-Turtle", "static#0.0", `"can_attack":{"attacker":"p0:The Lion-Turtle"},"want":false`},
		{"The Lion-Turtle", "static#0.1", `"can_block":{"blocker":"p0:The Lion-Turtle","attacker":"p1:Savannah Lions"},"want":false`},
		{"Platypus-Bear", "static#0.0", `"graveyard":["Firebending Lesson"]`},
		// Ward-scoped DisableTriggers (static_disable_ward.go).
		{"Nowhere to Run", "static#0.1", `"trigger_on_stack":"p1:Tomakul Honor Guard","want":false`},
		// Panharmonicon's changeling fallback probe.
		{"Splinter, Radical Rat", "static#0.0", `"card":"p0:Irregular Cohort"`},
		// CombatDamageToughness on an equipped attacker.
		{"Bark of Doran", "static#0.1", `"op":"attach","seat":0,"card":"p0:Bark of Doran","attached_to":"p0:Giant Spider"`},
		// A Room door's hand-size static, cast rather than placed.
		{"Roaring Furnace // Steaming Sauna", "static#1.0", `"card":"p0:Steaming Sauna"`},
		// Unlocked Room doors (static_unlocked_doors.go).
		{"Rampaging Soulrager", "static#0.0", `"card":"p0:Prop Room"`},
		// Protection from opponents on the controller, seen as prevented
		// combat damage (static_player_protection.go).
		{"Absolute Virtue", "static#0.0", `"p1":{"battlefield":["Grizzly Bears","Absolute Virtue"]}`},
		// A granted web-slinging alternative cost (static_granted_webslinging.go).
		{"Peter Parker", "static#1.0", `"card":"p0:Krenko, Mob Boss"},"want":true`},
		// A Vehicle's CDA P/T printed by crewing it.
		{"Lumbering Worldwagon", "static#0.0", `"xmage_ability":["","","Crew 4",""]`},
	} {
		t.Run(tc.card, func(t *testing.T) {
			req := counterReq(t, reg, tc.card, tc.key)
			item, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("skip = %q, want served", skip.Reason)
			}
			b, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tc.want) {
				t.Fatalf("item lacks %s:\n%s", tc.want, b)
			}
		})
	}
}
