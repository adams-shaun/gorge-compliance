package mtgish

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// abilityLine renders one summary's abilities as "shape[targets](effects)".
func abilityLine(s *Summary) string {
	var out []string
	for _, a := range s.Abilities {
		out = append(out, a.Shape()+"["+strings.Join(a.Targets, "+")+"]("+strings.Join(a.Effects, ",")+")")
	}
	return strings.Join(sorted(out), " ")
}

func p2aParams(kv ...string) map[string]string {
	p := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		p[kv[i]] = kv[i+1]
	}
	return p
}

func TestP2aGorgeTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		face *cards.Face
		want string
	}{
		{"support N is up to N target creatures",
			&cards.Face{Name: "Test Card", ManaCost: "1 G", Abilities: []*cards.SA{
				{Kind: "SP", API: "PutCounter", Params: p2aParams("Support", "2")},
			}},
			"spell[creaturex0..2](counters:+1/+1:1)"},
		{"X target creatures, X paid: a non-literal count",
			&cards.Face{Name: "Test Card", ManaCost: "X G", SVars: map[string]string{"X": "TargetedObjects$Amount"},
				Abilities: []*cards.SA{
					{Kind: "SP", API: "Tap", Params: p2aParams("ValidTgts", "Creature", "TargetMin", "0", "TargetMax", "MaxTgt")},
				}},
			"spell[creature](tap)"},
		{"up to X target creatures stays up to",
			&cards.Face{Name: "Test Card", ManaCost: "X G", SVars: map[string]string{"X": "TargetedObjects$Amount"},
				Abilities: []*cards.SA{
					{Kind: "SP", API: "Tap", Params: p2aParams("ValidTgts", "Creature", "TargetMin", "0", "TargetMax", "MaxTgt",
						"SpellDescription", "Tap up to X target creatures.")},
				}},
			"spell[creaturex0..*](tap)"},
		{"any number of targets without an X cost stays any number",
			&cards.Face{Name: "Test Card", ManaCost: "1 G", SVars: map[string]string{"X": "TargetedObjects$Amount"},
				Abilities: []*cards.SA{
					{Kind: "SP", API: "Tap", Params: p2aParams("ValidTgts", "Creature", "TargetMin", "0", "TargetMax", "MaxTgt")},
				}},
			"spell[creaturex0..*](tap)"},
		{"a keyword-only spell's placeholder is no ability",
			&cards.Face{Name: "Test Card", ManaCost: "5 R", Keywords: []string{"Cascade"}, Abilities: []*cards.SA{
				{Kind: "SP", API: "Pump", Params: p2aParams("StackDescription", "None")},
			}},
			""},
	} {
		if got := abilityLine(gorgeFace("Test Card", tc.face, TypeIndex{})); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestP2aGorgeTriggersStaticsRepls(t *testing.T) {
	reveal := &cards.SA{API: "Reveal", Params: p2aParams("Defined", "You"),
		Sub: &cards.SA{API: "ImmediateTrigger", Params: p2aParams("Execute", "TrigDraw")}}
	f := &cards.Face{
		Name:  "Test Card",
		Types: []string{"Enchantment"},
		SVars: map[string]string{"TrigDraw": "DB$ Draw"},
		Triggers: []cards.Trigger{
			// Reveal the first card you draw each turn; whenever you reveal
			// a land this way, draw a card.
			{Mode: "Drawn", Effect: reveal, Params: p2aParams("Mode", "Drawn", "Number", "1", "Static", "True", "ForReveal", "True")},
			// Enchanted creature is a copy of the chosen card.
			{Mode: "Attached", Effect: &cards.SA{API: "Clone", Params: p2aParams("CloneTarget", "Enchanted")},
				Params: p2aParams("Mode", "Attached", "Static", "True")},
		},
		Statics: []cards.Static{
			{Mode: "FlipCoinDoubler", Params: p2aParams("ValidPlayer", "You")},
			{Mode: "CantBlock", Params: p2aParams("ValidCard", "Card.Self+withShadow", "Secondary", "True")},
			{Mode: "Continuous", Params: p2aParams("Affected", "Card.TopLibrary+YouCtrl,Creature.faceDown+YouDontCtrl",
				"AffectedZone", "Library,Battlefield", "MayLookAt", "You")},
		},
		Repls: []cards.Repl{
			{Event: "Untap", Params: p2aParams("ValidStepTurnToController", "You", "ReplaceWith", "RepX")},
			{Event: "Counter", Params: p2aParams("ValidSA", "Spell", "ReplaceWith", "RepY")},
			{Event: "DamageDone", Params: p2aParams("PreventionEffect", "True", "ReplaceWith", "RepZ")},
			{Event: "Draw", With: &cards.SA{API: "Draw", Sub: &cards.SA{API: "Reveal", Sub: &cards.SA{API: "Cleanup"}}},
				Params: p2aParams("ReplaceWith", "RevealedDraw")},
		},
	}
	got := abilityLine(gorgeFace("Test Card", f, TypeIndex{}))
	for _, want := range []string{
		"static[](player:revealfirstcarddrawnduringeachplayersturn)",
		"triggered:draw[](draw:1)",
		"static[](copy)",
		"replacement[](replace:wouldflipacoin)",
		"static[](player:maylookatfacedownpermanents)",
		"static[](player:maylookattopcardoflibraryanytime)",
		"replacement[](replace:woulduntap)",
		"replacement[](replace:wouldcounteraspell)",
		"replacement[](replace:preventdamage)",
		"static[](player:revealseachcarddrawn)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "rule:cantblock") {
		t.Errorf("the shadow helper static is no printed ability: %s", got)
	}
}

func TestP2aMtgish(t *testing.T) {
	// "Can't be the target of blue spells or abilities from blue sources":
	// two mtgish halves, one static.
	s := &Summary{Abilities: []Ability{
		{Kind: "static", Effects: []string{"rule:cantbethetargetofspells"}},
		{Kind: "activated", Effects: []string{"draw:1"}},
		{Kind: "static", Effects: []string{"rule:cantbethetargetofabilities"}},
	}}
	p2aMergeCantTarget(s)
	if got, want := abilityLine(s), "activated[](draw:1) static[](rule:cantbethetargetofspellsorabilities)"; got != want {
		t.Errorf("cant-target merge: got %q, want %q", got, want)
	}
	if got := playerEffectToken("MayRevealFirstCardDrawnDuringEachPlayersTurn"); got != "player:mayrevealfirstcarddrawnduringeachplayersturn" {
		t.Errorf("may reveal: %q", got)
	}
	for _, v := range []string{"WhenAnyNumberOfPermanentsArePutIntoAPlayersGraveyards", "WhenASpellOrAbilityCausesAPermanentToBePutIntoAPlayersGraveyard"} {
		if got := triggerClass(v); got != "dies" {
			t.Errorf("%s: %q, want dies", v, got)
		}
	}
	// A level whose box only changes P/T is no static; a level granting a
	// keyword is one static granting it.
	lv := mtgishOne(t, `{"_Rule":"LevelUp","args":[{"_Cost":"PayMana","args":[{"_ManaSymbol":"ManaCostGeneric","args":2}]},[`+
		`{"_Level":"LevelRange","args":[1,3,{"Power":4,"Toughness":4},[]]},`+
		`{"_Level":"LevelPlus","args":[4,{"Power":6,"Toughness":6},[{"_Rule":"Trample"}]]}]]}`)
	if got, want := abilityLine(lv), "static[](grant:trample)"; got != want {
		t.Errorf("level up: got %q, want %q", got, want)
	}
	// A conspiracy's start-of-game rule is a pregame ability.
	if got := shapes(mtgishOne(t, `{"_Rule":"YouAreTheStartingPlayer"}`)); got != "pregame" {
		t.Errorf("starting player: %q", got)
	}
}
