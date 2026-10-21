package mtgish

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// shapes renders a summary's T2 field.
func shapes(s *Summary) string {
	for _, f := range s.Fields() {
		if f.Name == "abilities" {
			return joinTokens(f.Tokens)
		}
	}
	return ""
}

// mtgishOne reads one synthetic mtgish line (a made-up card, no Oracle
// text) and summarises its only face.
func mtgishOne(t *testing.T, rules string) *Summary {
	t.Helper()
	line := `{"_OracleCard":"Card","Name":"Test Card","Typeline":{"Supertypes":[],"Cardtypes":["Creature"],"Subtypes":[]},` +
		`"ManaCost":[],"Rules":[` + rules + `],"CardPT":{"Power":1,"Toughness":1}}` + "\n"
	cs, err := Read(strings.NewReader(line), nil)
	if err != nil {
		t.Fatal(err)
	}
	return FromMtgish(cs["Test Card"])[0]
}

const (
	selfEnters  = `{"_Trigger":"WhenAPermanentEntersTheBattlefield","args":{"_Permanents":"SinglePermanent","args":{"_Permanent":"ThisPermanent"}}}`
	otherEnters = `{"_Trigger":"WhenAPermanentEntersTheBattlefield","args":{"_Permanents":"And","args":[{"_Permanents":"Other","args":{"_Permanent":"ThisPermanent"}},{"_Permanents":"IsCardtype","args":"Creature"}]}}`
	drawOne     = `{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}`
)

func TestMtgishShapeNormalisations(t *testing.T) {
	for _, tc := range []struct {
		name, rules, want string
	}{
		{"devoid is a keyword, not a CDA static",
			`{"_Rule":"CDA_Color","args":{"_CDA":"Devoid"}}`, "-"},
		{"this or another creature enters: one ability",
			`{"_Rule":"TriggerA","args":[` + selfEnters + `,` + drawOne + `]},{"_Rule":"TriggerA","args":[` + otherEnters + `,` + drawOne + `]}`,
			"triggered:etb"},
		{"combat damage to a player or planeswalker: one ability",
			`{"_Rule":"TriggerA","args":[{"_Trigger":"Or","args":[{"_Trigger":"WhenACreatureDealsCombatDamageToAPlayer","args":[]},` +
				`{"_Trigger":"WhenACreatureDealsCombatDamageToAPermanent","args":[]}]},` + drawOne + `]}`,
			"triggered:combat-damage-player"},
		{"enters tapped and a choice: one enter replacement",
			`{"_Rule":"AsPermanentEnters","args":[{"_Permanent":"ThisPermanent"},[{"_ReplacementActionWouldEnter":"EntersTapped"}]]},` +
				`{"_Rule":"AsPermanentEnters","args":[{"_Permanent":"ThisPermanent"},[{"_ReplacementActionWouldEnter":"ChooseAColor"}]]}`,
			"replacement"},
		{"a casting restriction of the spell is not an ability",
			`{"_Rule":"CastEffect","args":{"_CastEffect":"CantBeCastUnless","args":{"_Condition":"IsDuringCombat"}}}`, "-"},
		{"one static per player restriction",
			`{"_Rule":"PlayerEffect","args":[{"_Players":"You"},[{"_PlayerEffect":"CantGainLife"},{"_PlayerEffect":"CantDrawCards"}]]}`,
			"static static"},
		{"a level is one static with its P/T",
			`{"_Rule":"LevelUp","args":[{"_Cost":"PayMana","args":[]},[{"_Level":"Level","args":[{"_GameRange":"ValueOrBigger","args":1},` +
				`{"Power":2,"Toughness":2},[{"_Rule":"Flying"}]]}]]}`,
			"static"},
		{"two spell rules are one spell ability",
			`{"_Rule":"SpellActions","args":` + drawOne + `},{"_Rule":"SpellActions","args":` + drawOne + `}`, "spell"},
	} {
		if got := shapes(mtgishOne(t, tc.rules)); got != tc.want {
			t.Errorf("%s: abilities %q, want %q", tc.name, got, tc.want)
		}
	}
	s := mtgishOne(t, `{"_Rule":"CDA_Color","args":{"_CDA":"Devoid"}}`)
	if !reflect.DeepEqual(s.Keywords, []string{"devoid"}) {
		t.Errorf("devoid keywords %v", s.Keywords)
	}
	s = mtgishOne(t, `{"_Rule":"LevelUp","args":[{"_Cost":"PayMana","args":[]},[{"_Level":"Level","args":[{"_GameRange":"ValueOrBigger","args":1},`+
		`{"Power":2,"Toughness":2},[{"_Rule":"Flying"}]]}]]}`)
	if got := strings.Join(sorted(s.Abilities[0].Effects), " "); got != "grant:flying setpt" {
		t.Errorf("level static effects %q", got)
	}
}

func TestMtgishTriggerClassG(t *testing.T) {
	for v, want := range map[string]string{
		"WhenAPlayerDrawsACardExceptTheFirstCardDuringTheirDrawStep": "draw",
		"AtTheBeginningOfAPlayersDrawStep":                           "draw-step",
		"WhenAPlayerIsDealtCombatDamage":                             "combat-damage-player",
	} {
		if got := triggerClass(v); got != want {
			t.Errorf("triggerClass(%s) = %q, want %q", v, got, want)
		}
	}
}

func TestGorgeTriggerClassG(t *testing.T) {
	for _, tc := range []struct {
		mode string
		p    map[string]string
		want string
	}{
		{"AttackerBlockedByCreature", map[string]string{"ValidBlocker": "Card.Self", "ValidCard": "Creature.Green"}, "blocks"},
		{"AttackerBlockedByCreature", map[string]string{"ValidBlocker": "Creature.EnchantedBy", "ValidCard": "Creature"}, "blocks"},
		{"AttackerBlockedByCreature", map[string]string{"ValidBlocker": "Creature.Wall", "ValidCard": "Card.Self"}, "blocked"},
		{"Blocks", map[string]string{"ValidBlocked": "Card.Self", "ValidCard": "Creature"}, "blocked"},
		{"Blocks", map[string]string{"ValidCard": "Card.Self"}, "blocks"},
		{"DamageDone", map[string]string{"CombatDamage": "True", "ValidTarget": "Planeswalker,Player"}, "combat-damage-player"},
		{"DamageDone", map[string]string{"CombatDamage": "True", "ValidTarget": "You"}, "combat-damage-player"},
	} {
		if got := gorgeTriggerClass(tc.mode, tc.p); got != tc.want {
			t.Errorf("gorgeTriggerClass(%s, %v) = %q, want %q", tc.mode, tc.p, got, tc.want)
		}
	}
}

func TestGorgeShapeNormalisations(t *testing.T) {
	trig := func(mode string, kv ...string) cards.Trigger {
		p := map[string]string{"Mode": mode}
		for i := 0; i+1 < len(kv); i += 2 {
			p[kv[i]] = kv[i+1]
		}
		return cards.Trigger{Mode: mode, Params: p}
	}
	f := &cards.Face{
		Name:     "Test Card",
		Types:    []string{"Creature"},
		Keywords: []string{"MustBlock", "CantAttackOrBlockAlone"},
		Triggers: []cards.Trigger{
			// Bookkeeping: a Static$ True cleanup on leaving.
			trig("ChangesZone", "Origin", "Battlefield", "Destination", "Any", "Static", "True", "ValidCard", "Card.Self"),
			// "Whenever you cast or copy": two lines, one ability.
			trig("SpellCast", "Execute", "TrigX", "ValidCard", "Instant", "TriggerDescription", "Whenever you cast or copy an instant, X."),
			trig("SpellCopy", "Execute", "TrigX", "ValidCard", "Instant", "Secondary", "True"),
		},
		Statics: []cards.Static{
			// The spell's own additional cost.
			{Mode: "RaiseCost", Params: map[string]string{"ValidCard": "Card.Self", "Type": "Spell", "Cost": "Discard<1/Card>"}},
			// Pump and "can't block" in one line: two printed statics.
			{Mode: "Continuous", Params: map[string]string{"Affected": "Creature.EnchantedBy", "AddPower": "2",
				"AddHiddenKeyword": "CARDNAME can't block."}},
		},
		Repls: []cards.Repl{
			{Event: "BeginPhase", Params: map[string]string{"Phase": "Draw", "Skip": "True"}},
		},
	}
	s := gorgeFace("Test Card", f, TypeIndex{})
	if got, want := shapes(s), "static static static static static static triggered:cast"; got != want {
		t.Errorf("abilities %q, want %q", got, want)
	}
	if len(s.Keywords) != 0 {
		t.Errorf("keywords %v: keyword lines that are printed statics", s.Keywords)
	}
	var eff []string
	for _, a := range s.Abilities {
		eff = append(eff, a.Effects...)
	}
	want := "player:skipsdrawstep pump:+2/+0 rule:cantattackalone rule:cantblock rule:cantblockalone rule:mustbeblocked"
	if got := strings.Join(sorted(eff), " "); got != want {
		t.Errorf("effects %q, want %q", got, want)
	}
}

func TestSvarParamsAndMergeSpells(t *testing.T) {
	p := svarParams("Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigX")
	if p["Mode"] != "Attacks" || p["Execute"] != "TrigX" || p["ValidCard"] != "Card.Self" {
		t.Errorf("svarParams %v", p)
	}
	if svarParams("no params here") != nil {
		t.Error("svarParams of a non-parameter line")
	}
	s := &Summary{Abilities: []Ability{
		{Kind: "spell", Effects: []string{"search:hand"}},
		{Kind: "static"},
		{Kind: "spell", Effects: []string{"search:hand"}},
	}}
	mergeSpells(s, true)
	if shapes(s) != "spell static" || strings.Join(s.Abilities[0].Effects, " ") != "search:hand" {
		t.Errorf("alternatives merge %+v", s.Abilities)
	}
	s = &Summary{Abilities: []Ability{{Kind: "spell", Effects: []string{"draw:1"}}, {Kind: "spell", Effects: []string{"gainlife:2"}}}}
	mergeSpells(s, false)
	if shapes(s) != "spell" || len(s.Abilities[0].Effects) != 2 {
		t.Errorf("sequential merge %+v", s.Abilities)
	}
}
