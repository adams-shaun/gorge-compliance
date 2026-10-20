package mtgish

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit E's T1 classes, on synthetic made-up cards (no Oracle text, no
// copied records): a script for the gorge side, a hand-written line for the
// mtgish side.

func gorgeCard(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("test.txt", []byte(src))
	if c == nil || len(diags) > 0 {
		t.Fatalf("parse: %v", diags)
	}
	return c
}

func mtgishCard(t *testing.T, line string) *Card {
	t.Helper()
	cs, err := Read(strings.NewReader(line+"\n"), nil)
	if err != nil || len(cs) != 1 {
		t.Fatalf("read: %v %v", cs, err)
	}
	for _, c := range cs {
		return c
	}
	return nil
}

func fieldOf(s *Summary, name string) string {
	for _, f := range s.Fields() {
		if f.Name == name {
			return joinTokens(f.Tokens)
		}
	}
	return "?"
}

const vanilla = `"Typeline":{"Supertypes":[],"Cardtypes":["Creature"],"Subtypes":["Bear"]},"CardPT":{"Power":2,"Toughness":2}`

func TestFacePairingAliasFoldAndSeparateCards(t *testing.T) {
	// A Universes Within face: Forge names the crossover printing, the
	// Oracle name is its flavour alias.
	g := gorgeCard(t, "Name:Crossover Bear\nVariant:UniversesWithin:FlavorName:Oracle Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	m := mtgishCard(t, `{"_OracleCard":"Card","Name":"Oracle Bear",`+vanilla+`,"ManaCost":[{"_ManaSymbol":"ManaCostGeneric","args":1},{"_ManaSymbol":"ManaCostG"}],"Rules":[]}`)
	if _, rows := Compare("Oracle Bear", g, m, TypeIndex{}); len(rows) != 0 {
		t.Errorf("alias: %+v", rows)
	}
	// Oracle's modifier-letter colon against Forge's ASCII colon.
	g = gorgeCard(t, "Name:Ka:ta\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	m = mtgishCard(t, `{"_OracleCard":"Card","Name":"Ka`+"꞉"+`ta",`+vanilla+`,"ManaCost":[{"_ManaSymbol":"ManaCostGeneric","args":1},{"_ManaSymbol":"ManaCostG"}],"Rules":[]}`)
	if _, rows := Compare(m.Name, g, m, TypeIndex{}); len(rows) != 0 {
		t.Errorf("fold: %+v", rows)
	}
	// A meld piece: Forge's script carries the melded result as a second
	// face; mtgish prints it as a card of its own.
	g = gorgeCard(t, "Name:Half Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nAlternateMode:Meld\nOracle:x\nALTERNATE\nName:Whole Bear\nTypes:Creature Bear\nPT:9/9\nOracle:x\n")
	m = mtgishCard(t, `{"_OracleCard":"MeldPiece","Name":"Half Bear",`+vanilla+`,"ManaCost":[{"_ManaSymbol":"ManaCostGeneric","args":1},{"_ManaSymbol":"ManaCostG"}],"Rules":[]}`)
	p, rows := compareCard("Half Bear", g, m, TypeIndex{}, func(n string) bool { return n == "Whole Bear" })
	if len(rows) != 0 || len(p.Gorge) != 1 {
		t.Errorf("meld: %d gorge faces, rows %+v", len(p.Gorge), rows)
	}
	// ... unless mtgish has no such card: then the face is missing.
	if _, rows := compareCard("Half Bear", g, m, TypeIndex{}, func(string) bool { return false }); len(rows) != 1 || rows[0].Field != "face" {
		t.Errorf("meld, result absent: %+v", rows)
	}
}

func TestMtgishCharacteristicClasses(t *testing.T) {
	line := `{"_OracleCard":"Card","Name":"Test Ship","Typeline":{"Supertypes":[],"Cardtypes":["Artifact"],"Subtypes":["Spacecraft"]},"Rules":[` +
		`{"_Rule":"CDA_Color","args":{"_SettableColor":"Devoid"}},` +
		`{"_Rule":"Backup","args":[1,[{"_Rule":"Flying"}]]},{"_Rule":"Flying"},` +
		`{"_Rule":"Station"},{"_Rule":"StationChargedAnimate","args":[{"_GameRange":"ValueOrBigger","args":8},[{"_Rule":"Trample"},{"_Rule":"Activated","args":[{"_Cost":"TapPermanent"},{"_Actions":"ActionList","args":[]}]}],{"_PT":"PT","args":[4,5]}]},` +
		`{"_Rule":"SpellActions","args":{"_Actions":"ActionList","args":[{"_Action":"Cipher"}]}}]}`
	s := FromMtgish(mtgishCard(t, line))[0]
	if got := fieldOf(s, "keywords"); got != "backup:1 cipher devoid flying station" {
		t.Errorf("keywords %q", got)
	}
	if s.PT != "4/5" {
		t.Errorf("pt %q", s.PT)
	}
	if got := fieldOf(s, "effects"); got != "grant:ability grant:trample type" {
		t.Errorf("effects %q", got)
	}
	// "This is colorless" is a colour CDA, not devoid.
	s = FromMtgish(mtgishCard(t, `{"_OracleCard":"Card","Name":"Test Ghost","Typeline":{"Supertypes":[],"Cardtypes":["Instant"],"Subtypes":[]},"Rules":[{"_Rule":"CDA_Color","args":{"_SettableColor":"Colorless"}}]}`))[0]
	if fieldOf(s, "keywords") != "-" || fieldOf(s, "effects") != "cda" {
		t.Errorf("colorless: %q %q", fieldOf(s, "keywords"), fieldOf(s, "effects"))
	}
}

func TestStarPT(t *testing.T) {
	// A choice-linked self P/T static is the CDA the star prints; both
	// branches of a condition fold into one CDA ability.
	set := `{"_Rule":"PermanentLayerEffect","args":[{"_Permanent":"ThisPermanent"},[{"_StaticLayerEffect":"SetPower","args":{"_GameNumber":"TheChosenNumber"}}]]}`
	line := `{"_OracleCard":"Card","Name":"Test Shifter","Typeline":{"Supertypes":[],"Cardtypes":["Creature"],"Subtypes":[]},"Rules":[` +
		`{"_Rule":"IfElse","args":[{"_Condition":"IsPlayersTurn"},[` + set + `],[` + set + `]]}],"CardPT":{"Power":1,"Toughness":3}}`
	s := FromMtgish(mtgishCard(t, line))[0]
	if s.PT != "*/3" || fieldOf(s, "abilities") != "static" || fieldOf(s, "effects") != "cda" {
		t.Errorf("mtgish: pt %q abilities %q effects %q", s.PT, fieldOf(s, "abilities"), fieldOf(s, "effects"))
	}
	for _, tc := range []struct{ src, want string }{
		{"PT:1+*/3\nS:Mode$ Continuous | CharacteristicDefining$ True | SetPower$ X", "*/3"},
		{"PT:*/*", "0/0"}, // an "as this enters" choice: CardPT's 0/0
		{"PT:*/7-*\nS:Mode$ Continuous | SetPower$ X", "0/7"}, // no CDA flag: the printed base
		{"PT:2/2", "2/2"},
	} {
		f := gorgeCard(t, "Name:Test\nTypes:Creature\n"+tc.src+"\nOracle:x\n").Faces[0]
		if got := gorgePT(f); got != tc.want {
			t.Errorf("gorgePT(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestGorgeCharacteristicClasses(t *testing.T) {
	s := FromGorge("Test Brute", gorgeCard(t, "Name:Test Brute\nManaCost:2 R\nTypes:Creature Time Lord Brute\nPT:3/3\n"+
		"K:CARDNAME can't attack or block alone.\nK:MayFlashSac\nK:Affinity:Creature.Artifact:artifact creature\n"+
		"K:Hexproof:Card.MonoColor:monocolored\nOracle:x\n"), TypeIndex{})[0]
	if !reflect.DeepEqual(s.Types, []string{"creature", "timelord", "brute"}) {
		t.Errorf("types %v", s.Types)
	}
	if got := fieldOf(s, "keywords"); got != "affinity:artifact|creature hexprooffrom:monocolor" {
		t.Errorf("keywords %q", got)
	}
	if got := fieldOf(s, "effects"); got != "flash rule:cantattackalone rule:cantblockalone" {
		t.Errorf("effects %q", got)
	}

	// Fuse on both halves; a cleave SP is an alternative of the spell.
	ss := FromGorge("Test Split", gorgeCard(t, "Name:Test Split\nManaCost:R\nTypes:Instant\nK:Fuse\n"+
		"A:SP$ Draw | NumCards$ 1\nA:SP$ Draw | Cost$ 2 R | NumCards$ 1 | PrecostDesc$ Cleave | NonBasicSpell$ True\n"+
		"AlternateMode:Split\nOracle:x\nALTERNATE\nName:Test Other\nManaCost:G\nTypes:Instant\nA:SP$ GainLife | LifeAmount$ 2\nOracle:x\n"), TypeIndex{})
	if fieldOf(ss[0], "keywords") != "cleave fuse" || fieldOf(ss[1], "keywords") != "fuse" {
		t.Errorf("keywords %q / %q", fieldOf(ss[0], "keywords"), fieldOf(ss[1], "keywords"))
	}
	if fieldOf(ss[0], "abilities") != "spell" || fieldOf(ss[0], "effects") != "draw:1" {
		t.Errorf("cleave: %q %q", fieldOf(ss[0], "abilities"), fieldOf(ss[0], "effects"))
	}

	// A transformed planeswalker's back face prints no loyalty.
	ss = FromGorge("Test Walker", gorgeCard(t, "Name:Test Walker\nManaCost:2 G\nTypes:Legendary Planeswalker Test\nLoyalty:0\n"+
		"AlternateMode:DoubleFaced\nOracle:x\nALTERNATE\nName:Test Walker Back\nTypes:Legendary Planeswalker Test\nLoyalty:0\nOracle:x\n"), TypeIndex{})
	if ss[0].Loyalty != "0" || ss[1].Loyalty != "" {
		t.Errorf("loyalty %q / %q", ss[0].Loyalty, ss[1].Loyalty)
	}
}

func TestIntersectionsAndQualities(t *testing.T) {
	idx := TypeIndex{}
	for vt, want := range map[string]string{
		"Creature.Artifact": "creature", "Artifact.Creature": "creature", "Artifact": "artifact",
		"Creature.nonArtifact": "creature", "Land.Artifact+YouCtrl": "land",
	} {
		if got := joinTokens(idx.validClasses(vt, "")); got != want {
			t.Errorf("validClasses(%q) = %q, want %q", vt, got, want)
		}
	}
	and := map[string]any{"_Permanents": "And", "args": []any{
		map[string]any{"_Permanents": "IsCardtype", "args": "Artifact"},
		map[string]any{"_Permanents": "IsCardtype", "args": "Creature"},
	}}
	if got := joinTokens(permanentsClasses(and)); got != "creature" {
		t.Errorf("And classes %q", got)
	}
	or := map[string]any{"_Permanents": "Or", "args": and["args"]}
	if got := joinTokens(sorted(permanentsClasses(or))); got != "artifact creature" {
		t.Errorf("Or classes %q", got)
	}
	for in, want := range map[string]string{"eachcolor": "anycolor", "noncolorless": "anycolor", "multicolored": "multicolor", "red": "red"} {
		if got := colorQuality(in); got != want {
			t.Errorf("colorQuality(%q) = %q", in, got)
		}
	}
	if got := gorgeAffinity("Land.Snow:snow land"); got != "land|snow" {
		t.Errorf("gorgeAffinity %q", got)
	}
	if got := protectable(map[string]any{"_Protectable": "FromActivatedAndTriggeredAbilities"}); got != "activated|triggered" {
		t.Errorf("protectable %q", got)
	}
	g := FromGorge("Test Aura", gorgeCard(t, "Name:Test Aura\nManaCost:1 B\nTypes:Enchantment Aura\nK:Enchant:Creature.inZoneGraveyard:creature card in a graveyard\nOracle:x\n"), idx)[0]
	if got := fieldOf(g, "keywords"); got != "enchant:card-graveyard" {
		t.Errorf("enchant %q", got)
	}
}
