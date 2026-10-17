package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestXAnswersQueueRoutingShapes is the census ratchet for the XMage answer
// routing. Every shape below is one the std3 re-audit diverged on: the engine
// option kind decides which of XMage's FIFO queues an answer reaches, so each
// table row pins the ANSWER shape, not the card. A new option kind that lands
// in the wrong queue changes a row here.
//
// The engine option kind is the whole authority: an answer carrying a
// diagnostic of "target" reaches addTarget, "skip" reaches neither queue (the
// computer player resolves it), and everything else reaches makeChoose.
func TestXAnswersQueueRoutingShapes(t *testing.T) {
	tests := []struct {
		name string
		// pre is a decision XMage's driver sees before d in the same step
		// (a look_ack's pacing ask), when the routing reads the neighbours.
		pre  *rules.OracleDecision
		d    rules.OracleDecision
		want []XAnswer
	}{
		{
			// Generic all-kept arrange routing shape. This synthetic decision
			// pins xanswers only; generated card coverage is pinned separately.
			name: "arrange keeps all cards: target skip",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", Options: 2, Min: 0, Max: 2, Picks: []string{"Forest", "Island"}, PickIdx: []int{0, 1}, PickKinds: []string{"graveyard", "graveyard"}},
			want: []XAnswer{{0, "target", "[target_skip]"}},
		},
		{
			// Generic partial arrange queue shape: selection, terminator, then
			// the emitted order labels. The driver must preserve that order.
			name: "arrange partial selection: selection skip then independent order",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", Options: 3, Min: 0, Max: 2, Picks: []string{"Forest"}, PickIdx: []int{0}, PickKinds: []string{"graveyard"}},
			want: []XAnswer{{0, "choice", "Forest"}, {0, "choice", "[choice_skip]"}, {0, "choice", "Forest"}},
		},
		{
			// A forced bottom order (Min==Max==Options) never offers a skip.
			name: "arrange forced order: picks only",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", Options: 2, Min: 2, Max: 2, Picks: []string{"Forest", "Island"}, PickIdx: []int{1, 0}, PickKinds: []string{"bottom", "dig_bottom"}},
			want: []XAnswer{{0, "choice", "Forest"}, {0, "choice", "Island"}},
		},
		{
			// chooseTriggeredAbility matches the ability's rule text (getRule)
			// or its source object's NAME. These two picks share one source
			// (Celebrate the Mountain-king), so a name could not tell them
			// apart and the rule text is the only distinguishing answer: a
			// same-source order keys every pick but the last in XMage's
			// rule-text form (trigger_order_text.go). A distinct-source order
			// names each source; see gen_trigger_order_test.go.
			name: "trigger order with one shared source: rule text for chooseTriggeredAbility",
			d:    rules.OracleDecision{Step: 0, Seat: 1, Kind: "order", GorgeKind: "trigger_order", Options: 2, Picks: []string{"Celebrate the Mountain-king: When this enters, do a thing.", "Celebrate the Mountain-king: When this enters, do another."}, PickRefs: []string{"p1:Celebrate the Mountain-king", "p1:Celebrate the Mountain-king"}, PickIdx: []int{0, 1}, PickKinds: []string{"trigger", "trigger"}},
			want: []XAnswer{{1, "choice", "When this enters, do a thing."}},
		},
		{
			// A card-type pick is makeChoose, not a target, however much its
			// label looks like an object name.
			name: "choose_n non-target pick: choice, not target",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 8, Max: 1, Picks: []string{"Artifact"}, PickRefs: []string{"Artifact"}, PickIdx: []int{0}, PickKinds: []string{"type"}},
			want: []XAnswer{{0, "choice", "Artifact"}},
		},
		{
			// An unlabelled exiled-card pick shows the object's name on the
			// choice queue rather than an empty label.
			name: "choose_n empty label: object name on the choice queue",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{""}, PickRefs: []string{"p0:Wastes#27"}, PickIdx: []int{0}, PickKinds: []string{"card"}},
			want: []XAnswer{{0, "choice", "Wastes"}},
		},
		{
			// A library search is a TargetCardInLibrary: the target queue.
			// Measured on the 2026-10-05 std pass: 47 search carriers (Shared
			// Roots, Nature's Rhythm, ...) agree only with the target answer;
			// leaving the search to XMage's AI diverged.
			name: "choose_n library search: target queue",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 39, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#9"}, PickIdx: []int{0}, PickKinds: []string{"search"}},
			want: []XAnswer{{0, "target", "Wastes"}},
		},
		{
			// A short library search stops XMage picking more with the
			// target queue's skip token.
			name: "choose_n short library search: target then target skip",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 39, Max: 2, Picks: []string{"Forest"}, PickRefs: []string{"p0:Forest"}, PickIdx: []int{0}, PickKinds: []string{"search"}},
			want: []XAnswer{{0, "target", "Forest"}, {0, "target", "[target_skip]"}},
		},
		{
			// "Choose a number" is TestPlayer.getAmount, which reads X=<n>.
			name: "choose a number: X=n on the choice queue",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 11, Max: 1, Picks: []string{"0"}, PickRefs: []string{"0"}, PickIdx: []int{0}, PickKinds: []string{"number"}},
			want: []XAnswer{{0, "choice", "X=0"}},
		},
		{
			// "Look at an opponent's hand" is a TargetOpponent: the target
			// queue, even when the engine offered one legal option.
			name: "choose_n forced player target: target queue",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Max: 1, Picks: []string{"p1"}, PickRefs: []string{"p1"}, PickIdx: []int{0}, PickKinds: []string{"player"}},
			want: []XAnswer{{0, "target", "p1"}},
		},
		{
			name: "controller chooses which opponent: choice queue, not target",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"Player 2"}, PickRefs: []string{"p1"}, PickIdx: []int{0}, PickKinds: []string{"opponent_choice"}},
			want: []XAnswer{{0, "choice", "Player 2"}},
		},
		{
			// A bare yes/no is an engine option whose kind is "yes".
			name: "yes/no boolean: choice yes",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"Yes"}, PickRefs: []string{"Yes"}, PickIdx: []int{0}, PickKinds: []string{"yes"}},
			want: []XAnswer{{0, "choice", "yes"}},
		},
		{
			// The engine's own "yes" option with a verb ("Yes — shuffle") is
			// still XMage's chooseUse (measured: Changeling Wayfinder).
			name: "yes with verb: chooseUse yes",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"Yes — shuffle"}, PickRefs: []string{"Yes — shuffle"}, PickIdx: []int{0}, PickKinds: []string{"yes"}},
			want: []XAnswer{{0, "choice", "yes"}},
		},
		{
			// A pile pick is not a boolean however yes-like "First pile" reads.
			name: "two-pile pick: choice, not a boolean yes",
			d:    rules.OracleDecision{Step: 0, Seat: 1, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"First pile"}, PickRefs: []string{"First pile"}, PickIdx: []int{0}, PickKinds: []string{"pile-a"}},
			want: []XAnswer{{1, "choice", "First pile"}},
		},
		{
			// An optional additional cost is XMage's chooseUse (measured:
			// Stir Up Trouble, Silence the Echo, Bogslither's Embrace agree
			// only with yes; the label as a choice diverged).
			name: "optional additional cost: chooseUse yes",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"Sacrifice artifact or creature"}, PickRefs: []string{"Sacrifice artifact or creature"}, PickIdx: []int{0}, PickKinds: []string{"altaddcost"}},
			want: []XAnswer{{0, "choice", "yes"}},
		},
		{
			// Declining a gift is chooseUse no (measured: Kitnap, Parting Gust).
			name: "gift declined: chooseUse no",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"Don't promise a gift"}, PickRefs: []string{"Don't promise a gift"}, PickIdx: []int{0}, PickKinds: []string{"gift_decline"}},
			want: []XAnswer{{0, "choice", "no"}},
		},
		{
			// A declined pick may be posed as a yes/no or an "up to" target;
			// both are scripted (measured: Zimone's Experiment).
			name: "declined pick: no then target skip",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 3, Min: 0, Max: 2, PickKinds: nil},
			want: []XAnswer{{0, "choice", "no"}, {0, "target", "[target_skip]"}},
		},
		{
			// A declined one-card pick after a hidden-zone look keeps the
			// measured pair at the routing level: the same decision shape
			// agrees with it on a cast item (Break Out's cast-resolve row),
			// so the XMage pose follows the card, not the decision. The
			// trigger template forces the card-side-proven look chooser's
			// pick instead (Whiskervale Forerunner: XMage's 1..1 makeChoose
			// rejects every skip token).
			name: "declined look pick max 1: the measured pair at routing",
			pre:  &rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "look_ack", Options: 1, Min: 1, Max: 1, Picks: []string{"Continue"}, PickIdx: []int{0}, PickKinds: []string{"yes"}, First: "Continue", Via: "answer"},
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "choice", Options: 1, Min: 0, Max: 1, OptionRefs: []string{"p0:Grizzly Bears"}},
			want: []XAnswer{{0, "choice", "no"}, {0, "target", "[target_skip]"}},
		},
		{
			// The max-2 look ask keeps the measured pair (Zimone's
			// Experiment agrees only with it).
			name: "declined look pick max 2: the measured pair",
			pre:  &rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "look_ack", Options: 1, Min: 1, Max: 1, Picks: []string{"Continue"}, PickIdx: []int{0}, PickKinds: []string{"yes"}, First: "Continue", Via: "answer"},
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "choice", Options: 2, Min: 0, Max: 2, OptionRefs: []string{"p0:Grizzly Bears", "p0:Forest"}},
			want: []XAnswer{{0, "choice", "no"}, {0, "target", "[target_skip]"}},
		},
		{
			// A declined "you may put it into your hand" hidden pick: XMage's
			// choose(Cards, TargetCard) is makeChoose, the choice queue
			// (measured on Sparring Dummy: "Missing CHOICE def"; the pair's
			// "no" is "Found wrong choice command").
			name: "declined hidden pick: choice skip only",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hidden_pick", Options: 1, Min: 0, Max: 1, First: "Wastes", OptionRefs: []string{"p0:Wastes#27"}},
			want: []XAnswer{{0, "choice", "[choice_skip]"}},
		},
		{
			// A declined optional Play after a hidden-zone look: XMage poses
			// the "cast from among them" ask as player.choose over a
			// TargetCard(0, 1), which TestPlayer serves from the CHOICE queue
			// (makeChoose), so the up-to skip token is a [choice_skip].
			// Measured on Cosmic Cube: a chooseUse "no" there is "Found wrong
			// choice command", and a [target_skip] is left unused ("Missing
			// CHOICE def") while the AI casts. A plain Play carrier
			// (Discover, Cascade — no look) stays with the "no".
			name: "declined optional play after look: choice skip only",
			pre:  &rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "look_ack", Options: 1, Min: 1, Max: 1, Picks: []string{"Continue"}, PickIdx: []int{0}, PickKinds: []string{"yes"}, First: "Continue", Via: "answer"},
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "mode", Resume: "play", Options: 2, Min: 0, Max: 1, First: "Play Grizzly Bears", OptionRefs: []string{"p0:Grizzly Bears#2", "p0:Shock"}},
			want: []XAnswer{{0, "choice", "[choice_skip]"}},
		},
		{
			// One castable card: XMage skips its "which card" target ask and
			// goes straight to chooseUse, so the decline is the "no" (a
			// target skip would be unused and XMage would cast).
			name: "declined optional play after look, one castable card: no",
			pre:  &rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "look_ack", Options: 1, Min: 1, Max: 1, Picks: []string{"Continue"}, PickIdx: []int{0}, PickKinds: []string{"yes"}, First: "Continue", Via: "answer"},
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "mode", Resume: "play", Options: 1, Min: 0, Max: 1, First: "Play Grizzly Bears", OptionRefs: []string{"p0:Grizzly Bears#2"}},
			want: []XAnswer{{0, "choice", "no"}},
		},
		{
			// A declined two-option look pick is not scripted from the pair
			// branch: SearchPicks forces the first card and the serving
			// template retargets the answer onto XMage's mandatory
			// TargetCardInExile (Fireglass Mentor). Without the forced pick
			// the routing's own skip shape is what the decision carries.
			name: "declined two-option look pick: choice skip",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "choice", Options: 2, Min: 0, Max: 1, OptionRefs: []string{"p0:Wastes#27", "p0:Wastes#39"}},
			want: []XAnswer{{0, "choice", "[choice_skip]"}},
		},
		{
			// A mana ability's colour pick is a real choice dialog; XMage's
			// Choice key is the colour NAME.
			name: "mana colour: Add W maps to White",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 5, Max: 1, Picks: []string{"Add W"}, PickRefs: []string{"p0:Boommobile"}, PickIdx: []int{0}, PickKinds: []string{"mana"}},
			want: []XAnswer{{0, "choice", "White"}},
		},
		{
			// A cast-fallback mana TAP is paid from the pool, not asked.
			name: "mana tap cost: skipped as payment()",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1, Picks: []string{"Pay 3 life"}, PickRefs: []string{"Pay 3 life"}, PickIdx: []int{0}, PickKinds: []string{"mana"}},
			want: nil,
		},
		{
			// An {X} announce is XMage's X= choice.
			name: "X announce: X=value on the choice queue",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 3, Max: 1, Picks: []string{"X = 2"}, PickRefs: []string{"X = 2"}, PickIdx: []int{2}, PickKinds: []string{"x"}},
			want: []XAnswer{{0, "choice", "X=2"}},
		},
		{
			// A short/"up to N" makeChoose stops with the queue's own token.
			name: "short up-to choose: choice skip to stop the dialog",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 5, Max: 5, Min: 0, Picks: []string{"Jace Beleren"}, PickRefs: []string{"p0:Jace Beleren"}, PickIdx: []int{0}, PickKinds: []string{"twopiles"}},
			want: []XAnswer{{0, "choice", "Jace Beleren"}, {0, "choice", "[choice_skip]"}},
		},
		{
			// The pile pick (Curator of Destinies, Fact or Fiction) is
			// XMage's opponent.chooseUse("Face-down", "Face-up"), a boolean
			// choice. Taking pile A -- the separator's face-down pile -- is
			// the boolean true, scripted "yes".
			name: "two-piles pick pile A: chooseUse yes",
			d:    rules.OracleDecision{Step: 0, Seat: 1, Kind: "choose_n", GorgeKind: "choose", Resume: "twopiles_pick", Options: 2, Max: 1, Picks: []string{"First pile"}, PickRefs: []string{"First pile"}, PickIdx: []int{0}, PickKinds: []string{"pile-a"}},
			want: []XAnswer{{1, "choice", "yes"}},
		},
		{
			// Pile B (the face-up pile) is the boolean false, "no".
			name: "two-piles pick pile B: chooseUse no",
			d:    rules.OracleDecision{Step: 0, Seat: 1, Kind: "choose_n", GorgeKind: "choose", Resume: "twopiles_pick", Options: 2, Max: 1, Picks: []string{"Second pile"}, PickRefs: []string{"Second pile"}, PickIdx: []int{1}, PickKinds: []string{"pile-b"}},
			want: []XAnswer{{1, "choice", "no"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := []rules.OracleDecision{tt.d}
			if tt.pre != nil {
				ds = append([]rules.OracleDecision{*tt.pre}, ds...)
			}
			// Every non-skip fixture must genuinely offer the picks it pins;
			// a vacuous fixture would assert nothing about the routing. A
			// declined ask pins the decline script itself: the decline must
			// be legal (Min 0) and the ask actually offered (Options > 0).
			if tt.want != nil {
				for _, d := range ds {
					if len(d.Picks) != 0 {
						continue
					}
					if d.Min != 0 || d.Options == 0 {
						t.Fatalf("declined fixture %s must be an optional ask with options offered", tt.name)
					}
				}
			}
			got := XAnswers(ds, 1, nil)
			var want [][]XAnswer
			if tt.want != nil {
				want = [][]XAnswer{tt.want}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("XAnswers = %#v, want %#v", got, want)
			}
		})
	}
}

// TestYesNoRequiresAnEngineBoolean proves yesNo accepts the engine's own
// boolean kinds (yes/no) and the measured chooseUse families (optional
// additional cost, gift, primary), and rejects every other labelled pick --
// an endure or activation choice is a real makeChoose (measured: Dusyut
// Earthcarver, Phantom Interference agree only with the label).
func TestYesNoRequiresAnEngineBoolean(t *testing.T) {
	valid := rules.OracleDecision{Options: 2, Picks: []string{"Yes"}, PickRefs: []string{"Yes"}, PickIdx: []int{0}, PickKinds: []string{"yes"}}
	if got, ok := yesNo(valid); !ok || got != "yes" {
		t.Fatalf("yesNo(exact yes) = %q, %v; want yes", got, ok)
	}
	for name, d := range map[string]rules.OracleDecision{
		"card type":     {Options: 2, Picks: []string{"Artifact"}, PickRefs: []string{"Artifact"}, PickIdx: []int{0}, PickKinds: []string{"type"}},
		"first pile":    {Options: 2, Picks: []string{"First pile"}, PickRefs: []string{"First pile"}, PickIdx: []int{0}, PickKinds: []string{"pile-a"}},
		"empty label":   {Options: 2, Picks: []string{""}, PickRefs: []string{"p0:Wastes#27"}, PickIdx: []int{0}, PickKinds: []string{"card"}},
		"legacy label":  {Options: 2, Picks: []string{"Yes — discard"}, PickRefs: []string{"Yes — discard"}, PickIdx: []int{0}},
		"activate":      {Options: 2, Picks: []string{"Activate Llanowar Elves for mana"}, PickRefs: []string{"p0:Llanowar Elves"}, PickIdx: []int{0}, PickKinds: []string{"activate"}},
		"endure":        {Options: 2, Picks: []string{"Put +1/+1 counters on it"}, PickRefs: []string{"p0:Dusyut Earthcarver"}, PickIdx: []int{0}, PickKinds: []string{"endure_counters"}},
		"three options": {Options: 3, Picks: []string{"Yes"}, PickRefs: []string{"Yes"}, PickIdx: []int{0}, PickKinds: []string{"yes"}},
		"two picks":     {Options: 2, Picks: []string{"Yes", "No"}, PickRefs: []string{"Yes", "No"}, PickIdx: []int{0, 1}, PickKinds: []string{"yes", "no"}},
	} {
		t.Run(name, func(t *testing.T) {
			// The precondition: each fixture really does offer a two-way ask,
			// so the rejection is about its kind, not an empty fixture.
			if d.Options < 2 || len(d.Picks) == 0 {
				t.Fatal("fixture must offer a genuine pick")
			}
			if got, ok := yesNo(d); ok {
				t.Fatalf("yesNo(%+v) = %q, true; want rejected", d, got)
			}
		})
	}

	// The gift ask lists the decline first (option 0), so the promise is
	// option 1 and must still read as XMage's yes (Jolly Gerbils trigger#0.0
	// promised in gorge and never in XMage while the index decided it).
	promise := rules.OracleDecision{Options: 2, Picks: []string{"Promise b a gift"}, PickRefs: []string{"Promise b a gift"}, PickIdx: []int{1}, PickKinds: []string{"gift_promise"}}
	if got, ok := yesNo(promise); !ok || got != "yes" {
		t.Fatalf("yesNo(gift promise at option 1) = %q, %v; want yes", got, ok)
	}
	decline := rules.OracleDecision{Options: 2, Picks: []string{"Don't promise a gift"}, PickRefs: []string{"Don't promise a gift"}, PickIdx: []int{0}, PickKinds: []string{"gift_decline"}}
	if got, ok := yesNo(decline); !ok || got != "no" {
		t.Fatalf("yesNo(gift decline at option 0) = %q, %v; want no", got, ok)
	}

	// A snapshot written before PickKinds existed carries no kind; an exact
	// boolean label with ref==label is still a true yes/no.
	legacy := rules.OracleDecision{Options: 2, Picks: []string{"Yes"}, PickRefs: []string{"Yes"}, PickIdx: []int{0}}
	if got, ok := yesNo(legacy); !ok || got != "yes" {
		t.Fatalf("yesNo(legacy exact boolean) = %q, %v; want yes", got, ok)
	}
}

// TestMayYesQueuesEveryFallbackDecisionInStepOrder pins requirement 5: a step
// that poses an earlier yes/no and then a declined pick must queue BOTH, in
// that order -- appending only the declined pick lets the yes/no ask eat it.
func TestMayYesQueuesEveryFallbackDecisionInStepOrder(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "cast"}}}
	ds := []rules.OracleDecision{
		{Step: 0, GorgeKind: "choose", First: "Yes", Options: 2, Via: "fallback"},
		{Step: 0, GorgeKind: "choose", First: "Forest", Options: 3, Via: "fallback", Picks: []string{"Forest"}, PickIdx: []int{0}},
		{Step: 1, GorgeKind: "choose", First: "Island", Options: 2, Via: "fallback"},
	}
	got, changed := MayYes(sc, ds)
	if !changed || len(got.Steps[0].Answers) != 2 {
		t.Fatalf("MayYes = changed %v, steps %+v; want both same-step fallback asks", changed, got.Steps)
	}
	want := []Answer{{Kind: "choose", Pick: []string{"Yes"}}, {Kind: "choose", Pick: []string{"Forest"}}}
	if !reflect.DeepEqual(got.Steps[0].Answers, want) {
		t.Fatalf("answers = %+v, want %+v", got.Steps[0].Answers, want)
	}

	// A fallback that deliberately answered past option 0 (the runner's
	// "done" on an optional triggered cost's unpayable mana window) is not
	// re-pinned to option 0: that would tap the source it declined to tap.
	done := []rules.OracleDecision{{Step: 0, GorgeKind: "choose", First: "Tap Llanowar Elves for mana", Options: 2,
		Via: "resolve fallback", Picks: []string{"Done"}, PickIdx: []int{1}}}
	if got, changed := MayYes(sc, done); changed || len(got.Steps[0].Answers) != 0 {
		t.Fatalf("MayYes re-pinned a deliberate non-first fallback: %+v", got.Steps)
	}
}

// TestXMQueueCensus pins the option-kind to queue mapping directly, so a kind
// added to the routing without a fixture still trips a named assertion.
func TestXMQueueCensus(t *testing.T) {
	cases := []struct {
		kind, label, want string
	}{
		{"search", "Wastes", "target"},
		{"exilecost", "Wastes", "target"},
		{"trigger_cost_pay", "Pay {2}", "skip"},
		{"mana", "Pay 3 life", "skip"},
		{"mana", "Add W", "choice"},
		{"mana", "Add U", "choice"},
		{"permanent", "Grizzly Bears (b)", "target"},
		{"player", "p1", "target"},
		{"opponent_choice", "Player 2", "choice"},
		{"type", "Artifact", "choice"},
		{"color", "White", "choice"},
		{"yes", "Yes — discard", "choice"},
		{"pile-a", "First pile", "choice"},
		{"altaddcost", "Sacrifice 1 permanent", "choice"},
		{"card", "Wastes", "choice"},
		{"trigger", "When this enters, do a thing.", "choice"},
		{"graveyard", "Wastes", "choice"},
	}
	for _, c := range cases {
		if got := xmQueue(c.kind, c.label); got != c.want {
			t.Errorf("xmQueue(%q,%q) = %q, want %q", c.kind, c.label, got, c.want)
		}
	}
}

// TestPickKindSeparatesPlayerTargetFromOpponentChoice: a player target and the
// controller's which-opponent ask share the option kind "player"; only the
// latter (ResumeKind "opp_pick") reaches XMage's choice queue.
func TestPickKindSeparatesPlayerTargetFromOpponentChoice(t *testing.T) {
	target := rules.OracleDecision{PickKinds: []string{"player"}, Resume: "target"}
	pick := rules.OracleDecision{PickKinds: []string{"player"}, Resume: "opp_pick"}
	if got := pickKind(target, 0); got != "player" {
		t.Fatalf("pickKind(player target) = %q, want player", got)
	}
	if got := pickKind(pick, 0); got != "opponent_choice" {
		t.Fatalf("pickKind(opp_pick) = %q, want opponent_choice", got)
	}
}

// TestForcedChainSubTargetIsNotScripted: a CR 603.3d chain link with exactly
// one legal target is chosen by XMage without asking (Mechanical Mobster,
// Thorin, Mountain-king); a root trigger target is still scripted.
func TestForcedChainSubTargetIsNotScripted(t *testing.T) {
	sub := rules.OracleDecision{Step: 0, Kind: "target", Options: 1, Min: 1, Max: 1, Resume: "trig_sub",
		Picks: []string{"Mechanical Mobster (a)"}, PickRefs: []string{"p0:Mechanical Mobster"}, PickKinds: []string{"permanent"}}
	if got := XAnswers([]rules.OracleDecision{sub}, 1, nil); got != nil {
		t.Fatalf("forced chain sub-target = %#v, want nothing scripted", got)
	}
	root := sub
	root.Resume = "target"
	if got := XAnswers([]rules.OracleDecision{root}, 1, nil); got == nil {
		t.Fatal("a forced root trigger target must still be scripted")
	}
}

// TestNamedOpponentSearchUsesChoiceQueue pins Ancient Vendetta's shape
// (measured on the std pass): the card-name dialog is scripted even when gorge
// offered one name, and a search of another player's library is a choice.
func TestNamedOpponentSearchUsesChoiceQueue(t *testing.T) {
	ds := []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Min: 1, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"Wastes"}, PickIdx: []int{0}, PickKinds: []string{"name"}},
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 39, Min: 0, Max: 4, Picks: []string{"Wastes"}, PickRefs: []string{"p1:Wastes#9"}, PickIdx: []int{0}, PickKinds: []string{"search"}},
	}
	want := [][]XAnswer{{{0, "choice", "Wastes"}, {0, "choice", "Wastes"}, {0, "choice", "[choice_skip]"}}}
	if got := XAnswers(ds, 1, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v", got, want)
	}
}
