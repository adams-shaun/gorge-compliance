package mtgish

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A synthetic mtgish line (a made-up card, no Oracle text): a 2/2 flier with
// ward {2} and "When this enters, it deals 3 damage to any target".
const sampleLine = `{"_OracleCard":"Card","Name":"Test Drake","Typeline":{"Supertypes":[],"Cardtypes":["Creature"],"Subtypes":["Drake"]},` +
	`"ManaCost":[{"_ManaSymbol":"ManaCostGeneric","args":2},{"_ManaSymbol":"ManaCostU"},{"_ManaSymbol":"ManaCostGW"}],` +
	`"Rules":[{"_Rule":"Flying"},{"_Rule":"Ward","args":{"_Cost":"PayMana","args":[{"_ManaSymbol":"ManaCostGeneric","args":2}]}},` +
	`{"_Rule":"TriggerA","args":[{"_Trigger":"WhenAPermanentEntersTheBattlefield","args":{"_Permanents":"SinglePermanent","args":{"_Permanent":"ThisPermanent"}}},` +
	`{"_Actions":"Targeted","args":[[{"_Target":"AnyTarget"}],{"_Actions":"ActionList","args":[` +
	`{"_Action":"PermanentDealsDamage","args":[{"_Permanent":"ThisPermanent"},{"_GameNumber":"Integer","args":3},{"_DamageRecipient":"Ref_AnyTarget"}]}]}]}]},` +
	`{"_Rule":"SomethingNew","args":1}],"CardPT":{"Power":2,"Toughness":2}}` + "\n"

func TestReadAndSummariseMtgish(t *testing.T) {
	cs, err := Read(strings.NewReader(sampleLine+sampleLine), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := cs["Test Drake"]
	if c == nil || len(cs) != 1 {
		t.Fatalf("Read: %v", SortedNames(cs))
	}
	ss := FromMtgish(c)
	if len(ss) != 1 {
		t.Fatalf("faces: %d", len(ss))
	}
	s := ss[0]
	if s.ManaCost != "{2}{U}{G/W}" || s.PT != "2/2" || !reflect.DeepEqual(s.Types, []string{"creature", "drake"}) {
		t.Errorf("T1: cost %q pt %q types %v", s.ManaCost, s.PT, s.Types)
	}
	if !reflect.DeepEqual(sorted(s.Keywords), []string{"flying", "ward:{2}"}) {
		t.Errorf("keywords %v", s.Keywords)
	}
	got := map[string]string{}
	for _, f := range s.Fields() {
		got[f.Name] = joinTokens(f.Tokens)
	}
	want := map[string]string{
		"abilities": "triggered:etb unmapped:Rule.SomethingNew",
		"targets":   "any",
		"effects":   "damage:3",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	// A filter that keeps nothing keeps nothing.
	none, err := Read(strings.NewReader(sampleLine), func(string) bool { return false })
	if err != nil || len(none) != 0 {
		t.Errorf("filtered Read: %v %v", none, err)
	}
}

func TestManaCostCanonical(t *testing.T) {
	for _, tc := range []struct {
		syms []string
		want string
	}{
		{[]string{"G", "2", "W"}, "{2}{W}{G}"},
		{[]string{"X", "R", "R"}, "{X}{R}{R}"},
		{[]string{"W/G", "G/W"}, "{G/W}{G/W}"},
		{[]string{"G/P"}, "{G/P}"},
		{[]string{"0"}, "{0}"},
		{nil, ""},
	} {
		var m manaCost
		for _, s := range tc.syms {
			m.add(s)
		}
		if got := m.String(); got != tc.want {
			t.Errorf("%v -> %q, want %q", tc.syms, got, tc.want)
		}
	}
	for in, want := range map[string]string{"GW": "G/W", "2W": "2/W", "WP": "W/P", "GWP": "G/W/P", "12": "12", "W": "W"} {
		if got := gorgeManaSymbol(in); got != want {
			t.Errorf("gorgeManaSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMultisetDiff(t *testing.T) {
	for _, tc := range []struct {
		a, b         []string
		onlyA, onlyB string
	}{
		{[]string{"draw:1", "damage:3"}, []string{"damage:3", "draw:1"}, "-", "-"},
		{[]string{"draw:*"}, []string{"draw:2"}, "-", "-"},
		{[]string{"draw:1"}, []string{"draw:2"}, "draw:1", "draw:2"},
		{[]string{"counters:*"}, []string{"counters:coin:1"}, "-", "-"},
		{[]string{"move:*>battlefield"}, []string{"move:exile>battlefield"}, "-", "-"},
		{[]string{"move:*>battlefield"}, []string{"move:exile>hand"}, "move:*>battlefield", "move:exile>hand"},
		{[]string{"static", "static"}, []string{"static"}, "static", "-"},
	} {
		a, b := multisetDiff(tc.a, tc.b)
		if joinTokens(a) != tc.onlyA || joinTokens(b) != tc.onlyB {
			t.Errorf("%v vs %v: got %q | %q, want %q | %q", tc.a, tc.b, joinTokens(a), joinTokens(b), tc.onlyA, tc.onlyB)
		}
	}
}

func TestAlternativesMerge(t *testing.T) {
	e := &effects{}
	e.merge([]*effects{
		{targets: []string{"creature"}, effects: []string{"damage:2", "damage:3", "gainlife:1"}},
		{targets: []string{"creature"}, effects: []string{"damage:4", "grant:flying"}},
	})
	if joinTokens(sorted(e.targets)) != "creature" {
		t.Errorf("targets %v", e.targets)
	}
	// damage differs between branches: as often as the branch with the
	// most, with no literal amount.
	if got := joinTokens(sorted(e.effects)); got != "damage:* damage:* gainlife:1 grant:flying" {
		t.Errorf("effects %q", got)
	}
}

func TestDiffRowsAndGaps(t *testing.T) {
	g := &Summary{Card: "C", Face: "C", ManaCost: "{1}", Abilities: []Ability{{Kind: "spell", Effects: []string{"draw:2"}}}}
	m := &Summary{Card: "C", Face: "C", ManaCost: "{1}", Abilities: []Ability{{Kind: "spell", Effects: []string{unmapped("Action.X")}}}}
	rows := Diff(g, m)
	if len(rows) != 1 || rows[0].Field != "effects" || !rows[0].Gap() {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Gorge != "draw:2" || rows[0].Mtgish != "unmapped:Action.X" {
		t.Errorf("row %+v", rows[0])
	}
}

func TestLedgerRoundTripAndDiff(t *testing.T) {
	rows := []Row{
		{Card: "B", Tier: T4, Field: "effects", Gorge: "draw:2", Mtgish: "draw:3", Class: "mtgish-bug", Note: "n"},
		{Card: "A", Tier: T3, Field: "targets", Gorge: "permanent", Mtgish: "permanentx0..1", Class: "forge-bug", Note: "n"},
	}
	path := filepath.Join(t.TempDir(), "X.tsv")
	if err := WriteLedger(path, "X", rows); err != nil {
		t.Fatal(err)
	}
	back, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].Card != "A" || back[1].Class != "mtgish-bug" {
		t.Fatalf("round trip %+v", back)
	}
	live := []Row{back[1], {Card: "C", Tier: T1, Field: "pt", Gorge: "1/1", Mtgish: "2/2"}}
	added, stale := LedgerDiff(live, back)
	if len(added) != 1 || added[0].Card != "C" || len(stale) != 1 || stale[0].Card != "A" {
		t.Errorf("added %+v stale %+v", added, stale)
	}
	merged := Merge(live, back)
	if merged[0].Class != "mtgish-bug" || merged[1].Class != "untriaged" {
		t.Errorf("merge %+v", merged)
	}
}

func TestTriggerClass(t *testing.T) {
	for v, want := range map[string]string{
		"WhenAPermanentEntersTheBattlefield":       "etb",
		"WhenAPermanentDies":                       "dies",
		"AtTheBeginningOfAPlayersUpkeep":           "upkeep",
		"WhenACreatureDealsCombatDamageToAPlayer":  "combat-damage-player",
		"WhenAPermanentIsPutIntoAPlayersGraveyard": "dies",
		"WhenAPlayerCastsASpell":                   "cast",
		"WhenSomethingNobodyMapped":                "unmapped:Trigger.WhenSomethingNobodyMapped",
	} {
		if got := triggerClass(v); got != want {
			t.Errorf("triggerClass(%s) = %q, want %q", v, got, want)
		}
	}
	for _, tc := range []struct {
		mode string
		p    map[string]string
		want string
	}{
		{"ChangesZone", map[string]string{"Destination": "Battlefield"}, "etb"},
		{"ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard"}, "dies"},
		{"Phase", map[string]string{"Phase": "End of Turn"}, "end-step"},
		{"DamageDone", map[string]string{"CombatDamage": "True", "ValidTarget": "Player"}, "combat-damage-player"},
	} {
		if got := gorgeTriggerClass(tc.mode, tc.p); got != tc.want {
			t.Errorf("gorgeTriggerClass(%s, %v) = %q, want %q", tc.mode, tc.p, got, tc.want)
		}
	}
}
