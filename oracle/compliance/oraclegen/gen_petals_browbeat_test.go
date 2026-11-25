package oraclegen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// The xproof find xs_XP_petals-of-insight_cast_s2: cast the spell, then
// answer its DB$ GenericChoice with ReturnPetals (the first Choices$ label).
// XMage's PetalsOfInsightEffect asks that election as one chooseUse ("Put the
// cards on the bottom of your library in any order?"), true = bottom and
// return. The scripted answer must be yes on the CHOICE queue, never the
// numeric mode "1" that leaves the chooseUse to TestPlayer's AI.
const petalsOfInsightScenario = `{
 "name": "xproof petals-of-insight cast seed 2", "turn": 1, "cr": [],
 "why": "xproof find xs_XP_petals-of-insight_cast_s2, library_top compacted",
 "setup": {
  "p0": {"battlefield": ["Island", "Island", "Island", "Island", "Island"], "hand": ["Petals of Insight"], "library_top": ["Island", "Island", "Island", "Island", "Island"], "life": 20},
  "p1": {"library_top": ["Plains", "Plains", "Plains", "Plains", "Plains"], "life": 20}
 },
 "steps": [
  {"op": "activate", "seat": 0, "card": "p0:Island", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Island#2", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Island#3", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Island#4", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Island#5", "ability_index": 0},
  {"op": "cast", "seat": 0, "card": "p0:Petals of Insight"},
  {"op": "pass", "seat": 0},
  {"op": "pass", "seat": 1},
  {"op": "pass", "seat": 0, "answers": [
    {"kind": "choose", "pick": ["yes"]},
    {"kind": "modes", "pick": ["You may put those cards on the bottom of your library in any order. If you do, return CARDNAME to its owner's hand."]}
  ]},
  {"op": "pass", "seat": 1},
  {"op": "pass", "seat": 0}
 ]
}`

// TestPetalsOfInsightChoiceAnswersOnTheYesNoQueue proves the two Choices$
// labels take the chooseUse's two answers: ReturnPetals (XMage's true branch)
// is yes and DrawCards is no. The real find scenario supplies the ReturnPetals
// pick; the DrawCards pick is the other offered label.
func TestPetalsOfInsightChoiceAnswersOnTheYesNoQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Petals of Insight")
	if !ok {
		t.Fatal("Petals of Insight missing from the corpus")
	}
	face := card.Faces[0]
	pairs, ok := chooseUseLabels(face)
	if !ok || len(pairs) != 1 {
		t.Fatalf("chooseUseLabels(Petals of Insight) = %v, %v; want one pair", pairs, ok)
	}
	returnLabel, drawLabel := pairs[0][0], pairs[0][1]
	if !strings.Contains(returnLabel, "bottom of your library") || !strings.Contains(drawLabel, "draw three cards") {
		t.Fatalf("labels = %q / %q, want ReturnPetals / DrawCards", returnLabel, drawLabel)
	}
	modes := ModeNumbers(face)
	if modes[returnLabel] != ModeYesQueue || modes[drawLabel] != ModeNoQueue {
		t.Fatalf("modes[%q]/[%q] = %d/%d, want yes/no (%#v)",
			returnLabel, drawLabel, modes[returnLabel], modes[drawLabel], modes)
	}

	// The real find scenario: gorge picks ReturnPetals, so XMage's chooseUse
	// must be answered yes on seat 0's choice queue.
	res, err := rules.RunOracleScenarioJSON(reg, []byte(petalsOfInsightScenario))
	if err != nil {
		t.Fatalf("scenario did not run: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("gorge failed the scenario: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	var sc Scenario
	if err := json.Unmarshal([]byte(petalsOfInsightScenario), &sc); err != nil {
		t.Fatal(err)
	}
	answered := false
	for _, d := range res.Decisions {
		if d.Kind != "mode" || len(d.Picks) != 1 || d.Picks[0] != returnLabel {
			continue
		}
		answered = true
		got := XAnswersForScenario(reg, res, sc, modes, nil)
		if d.Step >= len(got) {
			t.Fatalf("decision step %d outside the answer stream", d.Step)
		}
		want := []XAnswer{
			{Seat: 0, Kind: "choice", Value: "yes"},
			// Petals' ReturnPetals branch is a Dig all-to-bottom, and
			// XMage's putCardsOnBottomOfLibrary(..., anyOrder=true) asks
			// the order of the first N-1 looked-at cards (the last is
			// placed unasked). All three here are Islands.
			{Seat: 0, Kind: "choice", Value: "Island"},
			{Seat: 0, Kind: "choice", Value: "Island"},
		}
		if !reflect.DeepEqual(got[d.Step], want) {
			t.Fatalf("xmage_answers[%d] = %+v, want %+v", d.Step, got[d.Step], want)
		}
	}
	if !answered {
		t.Fatalf("scenario posed no ReturnPetals mode pick: %+v", res.Decisions)
	}

	// The other offered label is the false branch.
	mk := func(label string, idx int) rules.OracleDecision {
		return rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes", Resume: "modes",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{idx}, PickKinds: []string{"mode"}}
	}
	got := xanswers([]rules.OracleDecision{mk(returnLabel, 0), mk(drawLabel, 1)}, 1, modes, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "choice", Value: "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReturnPetals/DrawCards answers = %+v, want %+v", got, want)
	}
}

// The xproof find xs_XP_browbeat_cast_s2: p0 casts Browbeat at p1 and takes
// the 5 damage itself. XMage's BrowbeatDrawEffect offers its chooseUse to
// every player in range and does not stop after the first acceptance, so the
// stream must answer p0 yes and p1 no; one answer (or the find's numeric
// "mode 1") leaves p1's ask to TestPlayer's AI, which took the damage
// (p1.life 15) where gorge stopped at the first payer.
const browbeatScenario = `{
 "name": "xproof browbeat cast seed 2", "turn": 1, "cr": [],
 "why": "xproof find xs_XP_browbeat_cast_s2, library_top compacted",
 "setup": {
  "p0": {"battlefield": ["Mountain", "Mountain", "Mountain"], "hand": ["Browbeat"], "library_top": ["Mountain", "Mountain", "Mountain", "Mountain", "Mountain"], "life": 20},
  "p1": {"library_top": ["Plains", "Plains", "Plains", "Plains", "Plains"], "life": 20}
 },
 "steps": [
  {"op": "activate", "seat": 0, "card": "p0:Mountain", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Mountain#2", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Mountain#3", "ability_index": 0},
  {"op": "cast", "seat": 0, "card": "p0:Browbeat", "targets": ["p1"]},
  {"op": "pass", "seat": 0},
  {"op": "pass", "seat": 1},
  {"op": "pass", "seat": 0, "answers": [{"kind": "modes", "pick": ["Take 5 damage"]}]}
 ]
}`

// browbeatDeclineThenPay is the same find with p0 declining and the targeted
// p1 taking the damage: gorge then asks p1, and XMage's two asks must map to
// no (p0) and yes (p1).
const browbeatDeclineThenPay = `{
 "name": "xproof browbeat cast seed 2", "turn": 1, "cr": [],
 "why": "xproof find xs_XP_browbeat_cast_s2, p0 declines and p1 pays",
 "setup": {
  "p0": {"battlefield": ["Mountain", "Mountain", "Mountain"], "hand": ["Browbeat"], "library_top": ["Mountain", "Mountain", "Mountain", "Mountain", "Mountain"], "life": 20},
  "p1": {"library_top": ["Plains", "Plains", "Plains", "Plains", "Plains"], "life": 20}
 },
 "steps": [
  {"op": "activate", "seat": 0, "card": "p0:Mountain", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Mountain#2", "ability_index": 0},
  {"op": "activate", "seat": 0, "card": "p0:Mountain#3", "ability_index": 0},
  {"op": "cast", "seat": 0, "card": "p0:Browbeat", "targets": ["p1"]},
  {"op": "pass", "seat": 0},
  {"op": "pass", "seat": 1},
  {"op": "pass", "seat": 0, "answers": [
    {"kind": "modes", "pick": ["Don't"]},
    {"kind": "modes", "pick": ["Take 5 damage"]}
  ]}
 ]
}`

// TestBrowbeatUnlessPayerAnswersPerSeat proves the election is answered per
// payer seat and that the targeted player draws nothing once a payer took the
// damage (CR 118.12's unless-pay orientation).
func TestBrowbeatUnlessPayerAnswersPerSeat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		raw  string
		want []XAnswer
		life [2]int32 // p0, p1 after the spell resolves
	}{
		{
			name: "first payer accepts",
			raw:  browbeatScenario,
			want: []XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 1, Kind: "choice", Value: "no"}},
			life: [2]int32{15, 20},
		},
		{
			name: "first declines, targeted payer accepts",
			raw:  browbeatDeclineThenPay,
			want: []XAnswer{{Seat: 0, Kind: "choice", Value: "no"}, {Seat: 1, Kind: "choice", Value: "yes"}},
			life: [2]int32{20, 15},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := rules.RunOracleScenarioJSON(reg, []byte(tc.raw))
			if err != nil {
				t.Fatalf("scenario did not run: %v", err)
			}
			if len(res.Fails) != 0 {
				t.Fatalf("gorge failed the scenario: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
			}
			var sc Scenario
			if err := json.Unmarshal([]byte(tc.raw), &sc); err != nil {
				t.Fatal(err)
			}

			// Precondition and step: gorge posed the all-player damage
			// punisher's unless election.
			step := -1
			for _, d := range res.Decisions {
				if d.Resume != "unless_pay" {
					continue
				}
				if !strings.HasPrefix(d.Picks[0], "Take 5 damage") && !strings.HasPrefix(d.Picks[0], "Don't") {
					continue
				}
				step = d.Step
			}
			if step < 0 {
				t.Fatalf("scenario posed no Browbeat unless election: %+v", res.Decisions)
			}
			got := XAnswersForScenario(reg, res, sc, nil, nil)
			if step >= len(got) || !reflect.DeepEqual(got[step], tc.want) {
				t.Fatalf("xmage_answers[%d] = %+v, want %+v", step, got[step], tc.want)
			}

			last := res.Snapshots[len(res.Snapshots)-1]
			for seat, life := range tc.life {
				got := browbeatSeat(t, last, seat)
				if got.Life != life {
					t.Errorf("p%d life after the spell = %d, want %d", seat, got.Life, life)
				}
			}
			// The 5 damage replaces the draw entirely, so the target seat
			// drew none of its three cards.
			if hand := browbeatSeat(t, last, 1).Hand; len(hand) != 0 {
				t.Errorf("p1 hand after a paid election = %v, want no draw", hand)
			}
		})
	}
}

func browbeatSeat(t *testing.T, s rules.OracleSnapshot, seat int) rules.OracleSnapPlayer {
	t.Helper()
	for _, p := range s.Players {
		if p.Seat == seat {
			return p
		}
	}
	t.Fatalf("snapshot has no seat %d: %+v", seat, s.Players)
	return rules.OracleSnapPlayer{}
}

// TestUnlessDamageAllPayerGate pins the IR gate that decides when an
// unless-pay decision expands to one boolean per payer seat. It is the
// damage-punisher role (UnlessPayer$ Player with a DamageYou<N> cost), not a
// card name, so its siblings are covered by the same check, and a Player-payer
// rider whose XMage loop stops at the first payer (Rhystic Scrying) or a
// single-payer unless (Mana Leak) stays a one-answer decision.
func TestUnlessDamageAllPayerGate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Barbarian Bully", "Book Burning", "Breaking Point", "Browbeat"} {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from the corpus", name)
		}
		if !unlessDamageAllPlayers(card.Faces[0]) {
			t.Errorf("%s: the all-player DamageYou rider is not recognised", name)
		}
	}
	for _, name := range []string{"Rhystic Scrying", "Mana Leak"} {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from the corpus", name)
		}
		if unlessDamageAllPlayers(card.Faces[0]) {
			t.Errorf("%s: wrongly expanded to every payer", name)
		}
	}
}

// TestAllPlayerUnlessAnswersEverySeat proves the expansion answers every payer
// seat even when gorge asked only the accepting one, and that an all-decline
// group is not duplicated.
func TestAllPlayerUnlessAnswersEverySeat(t *testing.T) {
	decl := func(step, seat int, pick string) rules.OracleDecision {
		return rules.OracleDecision{Step: step, Seat: seat, Kind: "mode", GorgeKind: "modes", Resume: "unless_pay",
			Options: 2, Min: 1, Max: 1, Picks: []string{pick}, PickIdx: []int{0},
			OptionRefs: []string{"p0:Browbeat", "p0:Browbeat"}, PickKinds: []string{"mode"}}
	}
	// p0 accepted, so gorge stopped: XMage still asks p1 and p2.
	paid := &answerRouting{ds: []rules.OracleDecision{decl(0, 0, "Take 5 damage")},
		allPlayerUnless: map[int]bool{0: true}, seats: 3}
	want := []XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 1, Kind: "choice", Value: "no"}, {Seat: 2, Kind: "choice", Value: "no"}}
	if got := paid.allPlayerUnlessAnswers(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("paid answers = %+v, want %+v", got, want)
	}
	// Two recorded declines: one stream, no duplicate for the second decision.
	declined := &answerRouting{ds: []rules.OracleDecision{decl(0, 0, "Don't"), decl(0, 1, "Don't")},
		allPlayerUnless: map[int]bool{0: true, 1: true}, seats: 2}
	want = []XAnswer{{Seat: 0, Kind: "choice", Value: "no"}, {Seat: 1, Kind: "choice", Value: "no"}}
	if got := declined.allPlayerUnlessAnswers(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("decline answers = %+v, want %+v", got, want)
	}
	if got := declined.allPlayerUnlessAnswers(1); got != nil {
		t.Fatalf("second decline re-emitted %+v", got)
	}
}
