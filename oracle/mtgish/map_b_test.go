package mtgish

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// A made-up card script (no card text): a vote whose outcomes are SVars, a
// bookkeeping Static$ phase trigger, two replacement shapes and counter-type
// spellings.
const unitBScript = `Name:Test Bureaucrat
ManaCost:2 W
Types:Creature Human
PT:2/2
T:Mode$ Phase | Phase$ EndCombat | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigVote
SVar:TrigVote:DB$ Vote | Defined$ Player | Choices$ DBDraw,DBGain | VoteTiedAbility$ DBGain
SVar:DBDraw:DB$ Draw | NumCards$ 2
SVar:DBGain:DB$ GainLife | LifeAmount$ 3
T:Mode$ Phase | Phase$ Cleanup | Execute$ DBReset | Static$ True
SVar:DBReset:DB$ Cleanup | ClearRemembered$ True
R:Event$ BeginPhase | ActiveZones$ Battlefield | ValidPlayer$ You | Phase$ Draw | Skip$ True
R:Event$ ProduceMana | ActiveZones$ Battlefield | ValidCard$ Plains | ReplaceWith$ DBReset
A:AB$ PutCounter | Cost$ T | ValidTgts$ Creature | CounterType$ M0M1 | CounterNum$ 1
A:AB$ PutCounter | Cost$ 1 | Defined$ Self | CounterTypes$ First Strike,P1P1
`

func TestUnitBGorgeCard(t *testing.T) {
	c, diags := cards.ParseBytes("test_bureaucrat.txt", []byte(unitBScript))
	if c == nil {
		t.Fatalf("parse: %v", diags)
	}
	ss := FromGorge("Test Bureaucrat", c, TypeIndex{})
	if len(ss) != 1 {
		t.Fatalf("faces: %d", len(ss))
	}
	got := map[string]string{}
	for _, f := range ss[0].Fields() {
		got[f.Name] = joinTokens(f.Tokens)
	}
	want := map[string]string{
		// The Static$ cleanup trigger is bookkeeping, not an ability.
		"abilities": "activated activated replacement static triggered:endcombat",
		"effects": "counters:+0/-1:1 counters:+1/+1:1 counters:firststrike:1 draw:2 gainlife:3 " +
			"player:skipsdrawstep replace:wouldproducemana vote",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestUnitBAPIEffects(t *testing.T) {
	for _, tc := range []struct {
		api    string
		params map[string]string
		want   []string
	}{
		{"Explore", nil, []string{"explore"}},
		{"Fog", nil, []string{"prevent"}},
		{"GainControlVariant", nil, []string{"gaincontrol"}},
		{"ChooseDirection", nil, nil},
		{"AlterAttribute", map[string]string{"Attributes": "Suspected"}, []string{"alterattribute"}},
		{"AlterAttribute", map[string]string{"Attributes": "Solved"}, nil},
		{"RearrangeTopOfLibrary", map[string]string{"NumCards": "3"}, []string{"dig:3", "move:library>library"}},
		{"DigMultiple", map[string]string{"DigNum": "4", "DestinationZone2": "Graveyard"},
			[]string{"dig:4", "move:library>hand", "move:library>graveyard"}},
		{"MustBlock", map[string]string{"BlockAllDefined": "True", "DefinedAttacker": "Valid Card.attacking"}, []string{"rule:mustblockeachattacker"}},
		{"MustBlock", map[string]string{"BlockAllDefined": "True", "DefinedAttacker": "TriggeredAttacker"}, []string{"rule:mustblockattacker"}},
		{"MustBlock", nil, []string{"rule:mustblockattacker"}},
		{"Radiation", map[string]string{"Num": "2"}, []string{"counters:rad:2"}},
		{"Balance", map[string]string{"Zone": "Hand"}, []string{"discard:*"}},
		{"Balance", map[string]string{"Valid": "Land"}, []string{"sacrifice"}},
		{"AddOrRemoveCounter", map[string]string{"CounterType": "TIME"}, []string{"counters:time:1", "countersremove"}},
		{"Meld", nil, []string{"move:battlefield>exile", "move:battlefield>exile", "meld"}},
	} {
		got := gorgeAPIEffects(&cards.SA{API: tc.api, Params: tc.params}, nil)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %v = %v, want %v", tc.api, tc.params, got, tc.want)
		}
	}
	vote := &cards.SA{API: "Vote", Params: map[string]string{"Choices": "A,B", "VoteTiedAbility": "B"}}
	if got := svarRefs(vote); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("vote refs %v", got)
	}
}

func TestUnitBCounterTypes(t *testing.T) {
	for in, want := range map[string]string{
		"P1P1": "+1/+1", "M1M1": "-1/-1", "M0M1": "+0/-1", "M1M0": "-1/+0", "P1P0": "+1/+0",
		"PUPA": "pupa", "First Strike": "firststrike", "DOUBLE_STRIKE": "doublestrike",
		"REPR": "reprieve", "Any": "*", "EachFromSource": "*", "EachType_Permanent.YouCtrl": "*", "": "*",
	} {
		if got := gorgeCounterType(in); got != want {
			t.Errorf("gorgeCounterType(%q) = %q, want %q", in, got, want)
		}
	}
	if got := gorgeCounterTokens("EACH P1P1,Flying", "1"); !reflect.DeepEqual(got, []string{"counters:+1/+1:1", "counters:flying:1"}) {
		t.Errorf("each list %v", got)
	}
}

func TestUnitBTriggersAndHidden(t *testing.T) {
	for _, tc := range []struct {
		p    map[string]string
		want string
	}{
		{map[string]string{"Phase": "EndCombat"}, "endcombat"},
		{map[string]string{"Phase": "End Of Turn"}, "end-step"},
		{map[string]string{"FirstUpkeep": "True"}, "upkeep"},
	} {
		if got := gorgeTriggerClass("Phase", tc.p); got != tc.want {
			t.Errorf("Phase %v = %q, want %q", tc.p, got, tc.want)
		}
	}
	for k, want := range map[string]string{
		"HIDDEN All creatures able to block CARDNAME do so.": "rule:allcreaturesmustblockit",
		"HIDDEN CARDNAME can only attack alone.":             "rule:canonlyattackalone",
		"HIDDEN Unblockable":                                 "rule:cantbeblocked",
	} {
		if got := hiddenKeywordToken(k); got != want {
			t.Errorf("hidden %q = %q, want %q", k, got, want)
		}
	}
	if lookMove("CloakNumberGenericCards") != "cloak" || lookMove("ManifestAGenericCard") != "manifest" {
		t.Error("lookMove cloak/manifest")
	}
}
