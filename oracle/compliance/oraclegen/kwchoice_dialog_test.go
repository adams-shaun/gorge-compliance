package oraclegen

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// xmageKWChoiceDialogExpected is the XMage GainsChoiceOfAbilitiesEffect choice
// label set for each face in xmageKWChoiceDialogFaces, written from the XMage
// source at XMAGE_REF fdde07931f independently of the production map and of
// xmageKeywordRuleText, so the census below compares against the source rather
// than against itself. Each label is the keyword ability's getRule() (the
// effect wraps it in CardUtil.stripReminderText): lower case, and Rampage's
// numeric parameter written as a space.
var xmageKWChoiceDialogExpected = map[string][]string{
	"alchemist's gift":           {"deathtouch", "lifelink"},
	"argivian avenger":           {"flying", "vigilance", "deathtouch", "haste"},
	"assassin initiate":          {"flying", "deathtouch", "lifelink"},
	"atraxa's skitterfang":       {"flying", "vigilance", "deathtouch", "lifelink"},
	"ballroom brawlers":          {"first strike", "lifelink"},
	"butcher of the horde":       {"vigilance", "lifelink", "haste"},
	"ezrim, agency chief":        {"vigilance", "lifelink", "hexproof"},
	"gabriel angelfire":          {"flying", "first strike", "trample", "rampage 3"},
	"gideon blackblade":          {"vigilance", "lifelink", "indestructible"},
	"golem artisan":              {"flying", "trample", "haste"},
	"hungering puppetbeast":      {"trample", "hexproof", "haste"},
	"hunter's axe":               {"trample", "deathtouch"},
	"jodah's avenger":            {"double strike", "protection from red", "vigilance", "shadow"},
	"lunar avenger":              {"flying", "first strike", "haste"},
	"manifold mouse":             {"double strike", "trample"},
	"multiform wonder":           {"flying", "vigilance", "lifelink"},
	"orcish medicine":            {"lifelink", "indestructible"},
	"practiced offense":          {"double strike", "lifelink"},
	"rattleback apothecary":      {"menace", "lifelink"},
	"shifting ceratops":          {"reach", "trample", "haste"},
	"silverquill pledgemage":     {"flying", "lifelink"},
	"steel seraph":               {"flying", "vigilance", "lifelink"},
	"urza's avenger":             {"banding", "flying", "first strike", "trample"},
	"veteran warleader":          {"first strike", "vigilance", "trample"},
	"viper, cruel conspirator":   {"deathtouch", "lifelink"},
	"wingnut, bat on the belfry": {"flying", "menace", "haste"},
}

// Lunar Avenger's resolving KWChoice$ pick (gorge's mode "Flying") is XMage's
// "Choose an ability to gain" Choice dialog, read from the CHOICE queue by the
// ability's rule text "flying" -- never numeric mode 1 on the mode queue
// (xpt-c93fe5d1). The sibling picks prove the whole candidate list maps across.
func TestKWChoiceDialogScriptsKeywordRuleText(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Lunar Avenger")
	if !ok {
		t.Fatal("Lunar Avenger missing from the corpus")
	}
	modes := ModeNumbers(card.Faces[0])
	for _, label := range []string{"Flying", "First Strike", "Haste"} {
		if modes[label] != ModeKWChoiceQueue {
			t.Fatalf("ModeNumbers[%q] = %d, want ModeKWChoiceQueue (%#v)", label, modes[label], modes)
		}
	}
	for _, tc := range []struct{ label, answer string }{
		{"Flying", "flying"},
		{"First Strike", "first strike"},
		{"Haste", "haste"},
	} {
		d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "mode", GorgeKind: "modes", Resume: "modes",
			Options: 3, Min: 1, Max: 1, Picks: []string{tc.label}, PickIdx: []int{0}, PickKinds: []string{"mode"},
			PickRefs: []string{"p0:Lunar Avenger"}}
		got := xanswers([]rules.OracleDecision{d}, 2, modes, nil)
		want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: tc.answer}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q pick = %#v, want %#v", tc.label, got, want)
		}
	}
	// A face outside the proven sets keeps the numeric mode answer: its XMage
	// ask is a chooseUse or absent (Linvala, Eowyn).
	for _, name := range []string{"Linvala, Shield of Sea Gate", "Eowyn, Lady of Rohan"} {
		other, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		for _, f := range other.Faces {
			for label, m := range ModeNumbers(f) {
				if m == ModeKWChoiceQueue || m == ModeKWBespokeChoiceQueue {
					t.Fatalf("%s: unproven face label %q routed to a KWChoice dialog", f.Name, label)
				}
			}
		}
	}
}

// Angelic Skirmisher's XMage ask is a bespoke "Choose ability for your
// creatures" Choice over "First strike", "Vigilance", "Lifelink": the pick
// "First Strike" scripts {choice, "First strike"} and no numeric mode
// (xpt-ae625e8f).
func TestKWChoiceBespokeDialogScriptsXMageLabel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Angelic Skirmisher")
	if !ok {
		t.Fatal("Angelic Skirmisher missing from the corpus")
	}
	modes := ModeNumbers(card.Faces[0])
	labels := kwChoiceBespokeLabels(card.Faces[0])
	if len(labels) != len(xmageKWChoiceBespokeFaces["angelic skirmisher"]) {
		t.Fatalf("bespoke labels %v, table %v", labels, xmageKWChoiceBespokeFaces)
	}
	for _, label := range labels {
		want, ok := xmageKWChoiceBespokeFaces["angelic skirmisher"][label]
		if !ok {
			t.Fatalf("label %q not in the XMage table", label)
		}
		if modes[label] != ModeKWBespokeChoiceQueue {
			t.Fatalf("ModeNumbers[%q] = %d, want ModeKWBespokeChoiceQueue (%#v)", label, modes[label], modes)
		}
		d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "mode", GorgeKind: "modes", Resume: "modes",
			Options: 3, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{0}, PickKinds: []string{"mode"},
			PickRefs: []string{"p0:Angelic Skirmisher"}}
		got := xanswers([]rules.OracleDecision{d}, 2, modes, nil)
		wantA := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: want}}}
		if !reflect.DeepEqual(got, wantA) {
			t.Errorf("%q pick = %#v, want %#v", label, got, wantA)
		}
		if setup := setupModeAnswers(d, modes); len(setup) != 1 || setup[0].Kind != "setup_choice" || setup[0].Value != want {
			t.Errorf("%q setup answer = %#v, want setup_choice %q", label, setup, want)
		}
	}
}

// Census: every face in xmageKWChoiceDialogFaces is a corpus face whose Pump
// KWChoice$ labels all map to the KWChoice Choice-dialog queue, and every
// routed label's XMage rule text agrees with an independently written table.
// A new entry needs an XMage-source proof (see choice_dialog.go).
func TestKWChoiceDialogFacesCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if len(xmageKWChoiceDialogFaces) != 26 || len(xmageKWChoiceDialogExpected) != 26 {
		t.Fatalf("xmageKWChoiceDialogFaces has %d entries, expected-label table %d; adding one needs an XMage-source proof (see its comment)",
			len(xmageKWChoiceDialogFaces), len(xmageKWChoiceDialogExpected))
	}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			key := strings.ToLower(f.Name)
			if !xmageKWChoiceDialogFaces[key] {
				continue
			}
			labels := kwChoiceDialogLabels(f)
			if len(labels) == 0 {
				t.Errorf("%s: no Pump KWChoice$ labels", f.Name)
				continue
			}
			modes := ModeNumbers(f)
			got := make([]string, 0, len(labels))
			for _, label := range labels {
				if modes[label] != ModeKWChoiceQueue {
					t.Errorf("%s: KWChoice label %q = %d, want ModeKWChoiceQueue", f.Name, label, modes[label])
				}
				got = append(got, xmageKeywordRuleText(label))
			}
			want := append([]string(nil), xmageKWChoiceDialogExpected[key]...)
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: XMage labels %v, source says %v", f.Name, got, want)
			}
			seen[key] = true
		}
	}
	if len(seen) != len(xmageKWChoiceDialogFaces) {
		t.Errorf("listed faces found in the corpus: %v of %d", seen, len(xmageKWChoiceDialogFaces))
	}
}
