package mtgish

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit A's families, on synthetic lines for made-up cards (no Oracle text,
// no copied record): both sides must emit the same tokens.

// mtgishSummary decodes one synthetic mtgish line and summarises its face.
func mtgishSummary(t *testing.T, line string) *Summary {
	t.Helper()
	cs, err := Read(strings.NewReader(line+"\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("cards: %v", SortedNames(cs))
	}
	for _, c := range cs {
		return FromMtgish(c)[0]
	}
	return nil
}

// gorgeSummary compiles one synthetic Forge-style script and summarises its
// first face.
func gorgeSummary(t *testing.T, script string) *Summary {
	t.Helper()
	c, diags := cards.ParseBytes("t.txt", []byte(script))
	if c == nil {
		t.Fatalf("parse: %v", diags)
	}
	c.Link()
	return FromGorge(c.Faces[0].Name, c, nil)[0]
}

func summaryField(s *Summary, name string) string {
	for _, f := range s.Fields() {
		if f.Name == name {
			return joinTokens(f.Tokens)
		}
	}
	return "?"
}

const (
	mR   = `{"_ManaSymbol":"ManaCostR"}`
	m1   = `{"_ManaSymbol":"ManaCostGeneric","args":1}`
	m2   = `{"_ManaSymbol":"ManaCostGeneric","args":2}`
	mBG  = `{"_ManaSymbol":"ManaCostBG"}`
	m4   = `{"_ManaSymbol":"ManaCostGeneric","args":4}`
	rFly = `{"_Rule":"Flying"}`
)

func TestClassLevelCosts(t *testing.T) {
	m := mtgishSummary(t, `{"_OracleCard":"Card","Name":"Test Class","Typeline":{"Supertypes":[],"Cardtypes":["Enchantment"],"Subtypes":["Class"]},`+
		`"ManaCost":[`+mR+`],"Rules":[{"_Rule":"ClassAbilities","args":[`+
		`{"_ClassAbility":"ClassAbility","args":[[`+m1+`,`+mR+`],[`+rFly+`]]},`+
		`{"_ClassAbility":"ClassAbility","args":[[`+m4+`,`+mBG+`,`+mBG+`],[]]}]}]}`)
	if got, want := summaryField(m, "keywords"), "class:2:{1}{R} class:3:{4}{B/G}{B/G} flying"; got != want {
		t.Errorf("mtgish keywords %q, want %q", got, want)
	}
	g := gorgeSummary(t, "Name:Test Class\nManaCost:R\nTypes:Enchantment Class\n"+
		"K:Class:2:1 R:AddStaticAbility$ SFly\n"+
		"SVar:SFly:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Flying\n"+
		"K:Class:3:4 BG BG:AddStaticAbility$ SFly\n")
	if got, want := summaryField(g, "keywords"), "class:2:{1}{R} class:3:{4}{B/G}{B/G}"; got != want {
		t.Errorf("gorge keywords %q, want %q", got, want)
	}
}

func TestSpreeModeCosts(t *testing.T) {
	mode := func(cost, action string) string {
		return `{"_SpreeAction":"SpreeAction","args":[{"_Cost":"PayMana","args":[` + cost + `]},` +
			`{"_Actions":"ActionList","args":[{"_Action":"` + action + `"}]}]}`
	}
	m := mtgishSummary(t, `{"_OracleCard":"Card","Name":"Test Spree","Typeline":{"Supertypes":[],"Cardtypes":["Instant"],"Subtypes":[]},`+
		`"ManaCost":[`+mR+`],"Rules":[{"_Rule":"SpellActions_Spree","args":[`+mode(m1, "DrawACard")+`,`+mode(m2+`,`+mR, "DrawACard")+`]}]}`)
	if got, want := summaryField(m, "keywords"), "spree:{1} spree:{2}{R}"; got != want {
		t.Errorf("mtgish keywords %q, want %q", got, want)
	}
	g := gorgeSummary(t, "Name:Test Spree\nManaCost:R\nTypes:Instant\nK:Spree\n"+
		"A:SP$ Charm | Choices$ DBA,DBB | MinCharmNum$ 1 | CharmNum$ 2\n"+
		"SVar:DBA:DB$ Draw | ModeCost$ 1\nSVar:DBB:DB$ Draw | ModeCost$ 2 R\n")
	if got, want := summaryField(g, "keywords"), "spree:{1} spree:{2}{R}"; got != want {
		t.Errorf("gorge keywords %q, want %q", got, want)
	}
}

func TestStationThresholds(t *testing.T) {
	land := `{"_Rule":"PlayerEffect","args":[{"_Player":"You"},[{"_PlayerEffect":"MayPlayAnAdditionalLand"}]]}`
	m := mtgishSummary(t, `{"_OracleCard":"Card","Name":"Test Ship","Typeline":{"Supertypes":[],"Cardtypes":["Artifact"],"Subtypes":["Spacecraft"]},`+
		`"ManaCost":[`+mR+`],"Rules":[{"_Rule":"Station"},`+
		`{"_Rule":"StationCharged","args":[{"_GameRange":"ValueOrBigger","args":3},[`+land+`]]},`+
		`{"_Rule":"StationChargedAnimate","args":[{"_GameRange":"ValueOrBigger","args":8},[`+rFly+`],{"_PT":"PT","args":[4,4]}]}]}`)
	g := gorgeSummary(t, "Name:Test Ship\nManaCost:R\nTypes:Artifact Spacecraft\nPT:4/4\nK:Station:8\n"+
		"S:Mode$ Continuous | AffectedDefined$ Self | Affected$ Card.counters_GE3_CHARGE | AddStaticAbility$ SLand\n"+
		"SVar:SLand:Mode$ Continuous | Affected$ You | AdjustLandPlays$ 1\n"+
		"S:Mode$ Continuous | AffectedDefined$ Self | Affected$ Card.counters_GE8_CHARGE | AddType$ Creature | AddKeyword$ Flying\n")
	for _, tc := range []struct{ field, want string }{
		{"keywords", "station station:3+ station:8+"},
		{"pt", "4/4"},
		{"abilities", "static static"},
		{"effects", "grant:flying player:mayplayanadditionalland type"},
	} {
		if got := summaryField(m, tc.field); got != tc.want {
			t.Errorf("mtgish %s %q, want %q", tc.field, got, tc.want)
		}
		if got := summaryField(g, tc.field); got != tc.want {
			t.Errorf("gorge %s %q, want %q", tc.field, got, tc.want)
		}
	}
}

func TestHandToLibraryCount(t *testing.T) {
	m := mtgishSummary(t, `{"_OracleCard":"Card","Name":"Test Storm","Typeline":{"Supertypes":[],"Cardtypes":["Instant"],"Subtypes":[]},`+
		`"ManaCost":[`+mR+`],"Rules":[{"_Rule":"SpellActions","args":{"_Actions":"ActionList","args":[`+
		`{"_Action":"PutNumCardsFromHandOnTopOfLibraryAnyOrder","args":{"_GameNumber":"Integer","args":2}}]}}]}`)
	if got, want := summaryField(m, "effects"), "move:hand>library:2"; got != want {
		t.Errorf("mtgish effects %q, want %q", got, want)
	}
	g := gorgeSummary(t, "Name:Test Storm\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 2 | Mandatory$ True\n")
	if got, want := summaryField(g, "effects"), "move:hand>library:2"; got != want {
		t.Errorf("gorge effects %q, want %q", got, want)
	}
	for _, tc := range []struct {
		api  string
		p    map[string]string
		want string
	}{
		{"ChangeZone", map[string]string{"Origin": "Hand", "Destination": "Library"}, "move:hand>library:1"},
		{"ChangeZone", map[string]string{"Origin": "Hand", "Destination": "Library", "Defined": "Remembered"}, "move:hand>library:*"},
		{"ChangeZone", map[string]string{"Origin": "Hand", "Destination": "Library", "ChangeNum": "X"}, "move:hand>library:*"},
		{"ChangeZoneAll", map[string]string{"Origin": "Hand", "Destination": "Library"}, "move:hand>library"},
		{"ChangeZone", map[string]string{"Origin": "Graveyard", "Destination": "Library", "Defined": "Self"}, "move:graveyard>library"},
	} {
		if got := changeZoneToken(tc.api, tc.p); got != tc.want {
			t.Errorf("changeZoneToken(%s, %v) = %q, want %q", tc.api, tc.p, got, tc.want)
		}
	}
}
