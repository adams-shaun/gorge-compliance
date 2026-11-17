package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// The pay side of a triggered optional NON-mana cost with no cost_posed
// decline in its step is XMage's DoIfCostPaid chooseUse: it is posed whenever
// the cost can be paid, so the "yes" must lead the pay's own follow-up pick
// (the discard or sacrifice). Unscripted, XMage's strict run stops at
// "Missing CHOICE def" on the Yes/No dialog (Melded Moxite "Discard a card and
// draw two cards?", Vampire Gourmand "Sacrifice another creature and ...?",
// Beetle-Headed Merchants, farm shape EntersBattlefieldTriggeredAbility /
// AttacksTriggeredAbility).
func TestXAnswersTriggerCostPayNonMana(t *testing.T) {
	pay := func(label, ref string) rules.OracleDecision {
		return rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{0},
			PickRefs: []string{ref}, PickKinds: []string{"trigger_cost_pay"},
			ObjectPicks: []string{ref}, Via: "answer"}
	}
	manaPay := func(label, ref string) rules.OracleDecision {
		d := pay(label, ref)
		d.TriggerCostMana = true
		return d
	}
	discard := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 7, Min: 1, Max: 1, Picks: []string{"Diplomatic Relations"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Diplomatic Relations"}, PickKinds: []string{"discard"},
		ObjectPicks: []string{"p0:Diplomatic Relations"}, Via: "answer"}
	sacrifice := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Mona Lisa, Science Geek"}, PickIdx: []int{2},
		PickRefs: []string{"p0:Mona Lisa, Science Geek"}, PickKinds: []string{"sacrifice"},
		ObjectPicks: []string{"p0:Mona Lisa, Science Geek"}, Via: "answer"}

	for _, c := range []struct {
		name string
		ds   []rules.OracleDecision
		want [][]XAnswer
	}{
		{"discard", []rules.OracleDecision{pay("Discard 1 card", "p0:Melded Moxite"), discard},
			[][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "Diplomatic Relations"}}}},
		{"sacrifice", []rules.OracleDecision{pay("Sacrifice another creature or artifact", "p0:Beetle-Headed Merchants"), sacrifice},
			[][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "Mona Lisa, Science Geek"}}}},
		{"sacrifice alone", []rules.OracleDecision{pay("Sacrifice another creature", "p0:Vampire Gourmand")},
			[][]XAnswer{nil, {{0, "choice", "yes"}}}},
		// A life or energy phrase also begins "Pay ", but the engine's
		// TriggerCostMana flag is false: the cost is not pure mana and
		// XMage's DoIfCostPaid still asks its chooseUse (Corpses of the
		// Lost's "You may pay 1 life. If you do, ..."). This is the case
		// the generic payment() label skip used to swallow.
		{"pay life", []rules.OracleDecision{pay("Pay 1 life", "p0:Corpses of the Lost")},
			[][]XAnswer{nil, {{0, "choice", "yes"}}}},
		{"pay energy", []rules.OracleDecision{pay("Pay 2 energy", "p0:Some Energy Card")},
			[][]XAnswer{nil, {{0, "choice", "yes"}}}},
		// A pure mana pay carries TriggerCostMana and is answered by the real
		// mana route (triggerCostAnswer), never by this non-mana classifier.
		{"mana pay", []rules.OracleDecision{manaPay("Pay W", "p0:Brigid")},
			[][]XAnswer{nil, {{0, "choice", "yes"}}}},
		// The engine leaves TriggerCostMana false for an X cost, so the phrase
		// itself must still classify as mana: a scripted "yes" here is left
		// unconsumed and XMage strict still reports "Pay {X}?" (Wildborn
		// Preserver). A raw Forge-style label and a blight keep their
		// pre-existing routing.
		{"mana X label", []rules.OracleDecision{pay("Pay X", "p0:Wildborn Preserver")}, nil},
		{"mana hybrid phyrexian label", []rules.OracleDecision{pay("Pay W/U/P", "p0:Brigid")}, nil},
		{"raw mana label", []rules.OracleDecision{pay("Pay {W}", "p0:Brigid")}, nil},
		{"blight", []rules.OracleDecision{pay("Blight 1", "p0:Scuzzback Scrounger")}, nil},
	} {
		got := xanswers(c.ds, 2, nil, nil)
		if c.want == nil {
			if got != nil {
				t.Errorf("%s: xanswers = %#v, want nothing scripted", c.name, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: xanswers = %#v, want %#v", c.name, got, c.want)
		}
	}
}

// An as-enters colour pick beside a second replacement effect (Edgewall Inn's
// "enters tapped"): XMage first asks the controller to ORDER the replacement
// effects (chooseReplacementEffect), a decision gorge never poses. The
// generator must not invent an answer for that ask -- the driver answers it
// itself (ScenarioReplay.ScriptedChoicePlayer.chooseReplacementEffect) -- so
// the colour stays the ONLY answer and leads step zero's queue, ahead of any
// gameplay answer. A second queued answer would shift every later ask.
func TestXAnswersSetupColourIsTheOnlyReplacementAnswer(t *testing.T) {
	colour := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 5, Min: 1, Max: 1, Picks: []string{"White"}, PickIdx: []int{0},
		PickRefs: []string{"White"}, PickKinds: []string{"color"}, Resume: "etb", Via: "answer"}
	got := xanswers([]rules.OracleDecision{colour}, 3, nil, nil)
	want := [][]XAnswer{{{0, "setup_choice", "White"}}, nil, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// The replacement-order fix lives in the Java driver, so pin the override
// itself: the colour guard above passes on develop and with the override
// removed, and the driver-asks helper test only exercises the
// replacementScripted predicate. Without this override an unscripted
// multi-effect order ask is a strict "Missing CHOICE def" (Edgewall Inn's
// "enters tapped" beside "as it enters, choose a color"), and with a queued
// setup colour at the head it consumes the wrong answer. The override must
// hand an unscripted multi-effect ask to the base AI WITHOUT touching the
// queue, and leave a scripted head to TestPlayer.
func TestXAnswersReplacementOrderDelegatesUnscriptedAsk(t *testing.T) {
	source, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	const sig = "public int chooseReplacementEffect(Map<String, String> effectsMap,"
	start := strings.Index(java, sig)
	if start < 0 {
		t.Fatal("ScenarioReplay.ScriptedChoicePlayer lost its chooseReplacementEffect override: an unscripted multi-effect order ask (Edgewall Inn) is a strict miss again")
	}
	end := strings.Index(java[start:], "\n        }")
	if end < 0 {
		t.Fatal("chooseReplacementEffect is unterminated")
	}
	method := java[start : start+end]
	for _, want := range []string{
		"effectsMap.size() > 1",
		"!replacementScripted(getChoices(), effectsMap.values())",
		"return getRealPlayer().chooseReplacementEffect(effectsMap, objectsMap, game);",
		"return super.chooseReplacementEffect(effectsMap, objectsMap, game);",
	} {
		if !strings.Contains(method, want) {
			t.Errorf("chooseReplacementEffect must delegate an unscripted multi-effect ask without consuming the head: missing %q\n%s", want, method)
		}
	}
	if !strings.Contains(java, "static boolean replacementScripted(") {
		t.Fatal("ScenarioReplay.ScriptedChoicePlayer lost its replacementScripted helper")
	}
}
