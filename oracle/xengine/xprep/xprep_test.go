package xprep

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifestDir = "../../compliance/manifests"

type window struct {
	l lower.Lowered
	g rules.OracleResult
}

// okWindows plays the first n FDN games (the xengine window tests' pairs and
// seeds) and returns every window whose lowering gorge's own runner
// reproduced, with gorge's result for it.
func okWindows(t *testing.T, n int) (*cards.Registry, *decks.Spellings, []window) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []window
	for g := 0; g < n; g++ {
		pair := decks.Pair(cands, cov.New(), "FDN", 0, g, 7)
		ls, _, _, err := lower.Windows(reg, pair, uint64(100+g), game.Options{Set: "FDN"}, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			if c := lower.CheckWindow(reg, l); c.Status == lower.StatusOK {
				out = append(out, window{l, c.Result})
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no lower_ok window")
	}
	return reg, sp, out
}

// prepared is every ok window Prepare serves; a refusal is logged and counted.
func prepared(t *testing.T, n int) (*cards.Registry, *decks.Spellings, []window, []oraclegen.Item) {
	t.Helper()
	reg, sp, ws := okWindows(t, n)
	var served []window
	var items []oraclegen.Item
	reasons := map[string]int{}
	for _, w := range ws {
		it, ref := Prepare(reg, sp, w.l, w.g)
		if ref != nil {
			if !slices.Contains(Reasons, ref.Reason) {
				t.Errorf("%s: reason %q is not in the closed list", ref.ID, ref.Reason)
			}
			reasons[ref.Reason]++
			continue
		}
		served = append(served, w)
		items = append(items, it)
	}
	t.Logf("%d ok windows, %d prepared, refused by reason %v", len(ws), len(served), reasons)
	if len(served) == 0 {
		t.Fatal("no window was prepared")
	}
	return reg, sp, served, items
}

// TestPrepareFillsAnswers: every prepared item has one answer list per step,
// and the answers scripted are gorge's own decisions less the declarations (the
// attack and block steps carry those) and less the target decisions
// ChooseTargets moved onto a cast or activate step. An arrange that sends every
// looked-at card to the graveyard is one answer per card.
func TestPrepareFillsAnswers(t *testing.T) {
	_, _, ws, items := prepared(t, 2)
	var moved, scripted int
	for k, it := range items {
		g := ws[k].g
		if len(it.XAnswers) != len(it.Steps) {
			t.Errorf("%s: %d xmage_answers for %d steps", it.ID, len(it.XAnswers), len(it.Steps))
			continue
		}
		_, castSteps := oraclegen.ChooseTargets(ws[k].l.Item.Scenario, g.Decisions)
		want := 0
		for _, d := range g.Decisions {
			switch {
			case d.Kind == "attackers" || d.Kind == "blockers":
			case d.Via == "target" && castSteps[d.Step]:
				moved++
			case d.GorgeKind == "arrange" && oraclegen.ArrangeEmptyGraveyard(d):
				// Every looked-at card to the graveyard: one choice answer
				// per card (XMage's selection IS the cards to graveyard).
				want += len(d.ArrangeLabels)
			default:
				want++
			}
		}
		got := 0
		for _, a := range it.XAnswers {
			got += len(a)
		}
		scripted += got
		if got != want {
			t.Errorf("%s: %d scripted answers, want %d (decisions %d)", it.ID, got, want, len(g.Decisions))
		}
	}
	// The sample must hold both a moved target and a scripted answer, or the
	// count proves nothing.
	if moved == 0 || scripted == 0 {
		t.Errorf("sample has %d moved targets and %d scripted answers", moved, scripted)
	}
}

// TestPrepareDropsTypedDualColour: a basic-typed dual land (Godless Shrine,
// Plains and Swamp) has one intrinsic "{T}: Add {X}." ability per basic type.
// XMage selects the fixed-colour ability by that text and asks no colour, so
// the colour answer gorge's runner recorded for the activation would be left
// over; the Temple-style Combo ability (one printed ability, XMage one ability
// per colour) keeps its existing clear. A non-colour choice on the typed dual's
// own step and a card choice on a later step are not the mana colour and must
// survive.
func TestPrepareDropsTypedDualColour(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	zero, one := 0, 1
	sc := oraclegen.Scenario{
		Name: "typed dual and combo mana",
		Why:  "a basic-typed dual's fixed-colour mana text asks XMage no colour",
		Setup: map[string]oraclegen.Seat{
			"p0": {Battlefield: []string{"Godless Shrine", "Temple of Deceit"}},
		},
		Steps: []oraclegen.Step{
			{Op: "activate", Seat: 0, Card: "p0:Godless Shrine", AbilityIndex: &one},
			{Op: "activate", Seat: 0, Card: "p0:Temple of Deceit", AbilityIndex: &zero,
				Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"Add B"}}}},
			{Op: "pass", Seat: 0},
		},
	}
	l := lower.Lowered{Item: oraclegen.Item{ID: "xe:test:typed-dual", Scenario: sc}}
	res, err := rules.RunOracleScenarioJSON(reg, l.Item.Raw())
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	// A non-colour choice on the typed dual's own step (a cost card pick) and
	// a card choice on the later step: neither names a colour, so the drop
	// must keep both.
	const cardChoice = "Captain Jean-Luc Picard"
	for _, step := range []int{0, 2} {
		res.Decisions = append(res.Decisions, rules.OracleDecision{
			Step: step, Seat: 0, Kind: "choose_n", Options: 2,
			Picks: []string{cardChoice}, PickIdx: []int{0},
		})
	}
	it, ref := Prepare(reg, sp, l, res)
	if ref != nil {
		t.Fatalf("Prepare refused: %+v", ref)
	}
	if got := it.XAbility[0]; got != "{T}: Add {B}." {
		t.Fatalf("typed dual prefix = %q, want %q", got, "{T}: Add {B}.")
	}
	isColour := func(v string) bool {
		switch v {
		case "White", "Blue", "Black", "Red", "Green":
			return true
		}
		return false
	}
	for _, a := range it.XAnswers[0] {
		if a.Kind == "choice" && isColour(a.Value) {
			t.Errorf("typed dual step keeps colour answer %q", a.Value)
		}
	}
	if !slices.Contains(it.XAnswers[0], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: cardChoice}) {
		t.Errorf("typed dual step lost the non-colour choice answer: %+v", it.XAnswers[0])
	}
	if got := it.XAbility[1]; got != "{T}: Add {B}" {
		t.Fatalf("combo prefix = %q, want %q", got, "{T}: Add {B}")
	}
	if len(it.XAnswers[1]) != 0 {
		t.Errorf("combo step answers = %+v, want none", it.XAnswers[1])
	}
	if !slices.Contains(it.XAnswers[2], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: cardChoice}) {
		t.Errorf("later step lost the card choice answer: %+v", it.XAnswers[2])
	}
	// The form is every fixed single colour (with the period); the Combo text
	// (no period), a colourless Add and a longer line are not fixed colour
	// texts. A queued colour naming another colour is reported, not dropped.
	for _, want := range []string{"W", "U", "B", "R", "G"} {
		p := "{T}: Add {" + want + "}."
		if got, ok := fixedColourStep([]string{p}, 0); !ok || got != want {
			t.Errorf("fixedColourStep(%q) = %q, %v", p, got, ok)
		}
	}
	for _, p := range []string{"{T}: Add {B}", "{T}: Add {C}.", "{T}: Add {B}. Draw a card."} {
		if got, ok := fixedColourStep([]string{p}, 0); ok {
			t.Errorf("fixedColourStep(%q) = %q, want no match", p, got)
		}
	}
	if _, bad := dropFixedColourAnswers([]oraclegen.XAnswer{{Kind: "choice", Value: "Black"}}, "W"); bad != "Black" {
		t.Errorf("mismatched answer = %q, want Black", bad)
	}
	if !slices.Contains(Reasons, ReasonColourMismatch) {
		t.Errorf("reason %q is not in the closed list", ReasonColourMismatch)
	}
}

// TestPrepareAbilityPrefixes: every activate step of a prepared item has a
// prefix, and no other step has one; an activation XMage has no text for (a
// token's ability, a card the corpus does not know) is a refusal, not a blank.
func TestPrepareAbilityPrefixes(t *testing.T) {
	reg, sp, ws, items := prepared(t, 2)
	acts := 0
	for _, it := range items {
		for i, st := range it.Steps {
			if st.Op != "activate" {
				if len(it.XAbility) > i && it.XAbility[i] != "" {
					t.Errorf("%s step %d (%s) has prefix %q", it.ID, i, st.Op, it.XAbility[i])
				}
				continue
			}
			acts++
			if len(it.XAbility) != len(it.Steps) || it.XAbility[i] == "" {
				t.Errorf("%s step %d activates %s with no xmage_ability", it.ID, i, st.Card)
			}
		}
	}
	if acts == 0 {
		t.Fatal("sample has no activate step")
	}
	for k, w := range ws {
		l := w.l
		l.Item.Steps = slices.Clone(l.Item.Steps)
		for i, st := range l.Item.Steps {
			if st.Op != "activate" {
				continue
			}
			for _, card := range []string{"p0:token:Treasure", "p0:No Such Card"} {
				l.Item.Steps[i].Card = card
				if _, ref := Prepare(reg, sp, l, w.g); ref == nil || ref.Reason != ReasonAbilityUnknown {
					t.Errorf("%s: activating %q is not refused %s (%v)", l.Item.ID, card, ReasonAbilityUnknown, ref)
				}
			}
			l.Item.Steps[i].Card = st.Card
			l.Item.Steps[i].AbilityIndex = nil
			if _, ref := Prepare(reg, sp, l, w.g); ref == nil || ref.Reason != ReasonAbilityUnknown {
				t.Errorf("%s: an activation with no ability index is not refused (%v)", l.Item.ID, ref)
			}
			break
		}
		if k >= 2 {
			break
		}
	}
}

// TestPrepareRefusesSpelling: an item holding a card XMage spells differently
// is refused, whichever zone it sits in; the same item without it is served.
func TestPrepareRefusesSpelling(t *testing.T) {
	reg, sp, ws, _ := prepared(t, 1)
	var odd []string
	for _, name := range []string{"Lim-D\u00fbl's Vault", "Fire"} {
		if sp.Differs(reg, name) {
			odd = append(odd, name)
		}
	}
	if len(odd) == 0 {
		t.Fatal("no known card with a differing XMage spelling")
	}
	w := ws[0]
	for _, name := range odd {
		l := w.l
		l.Item.Setup = map[string]oraclegen.Seat{}
		for k, s := range w.l.Item.Setup {
			l.Item.Setup[k] = s
		}
		s := l.Item.Setup["p1"]
		s.Graveyard = append(slices.Clone(s.Graveyard), name)
		l.Item.Setup["p1"] = s
		_, ref := Prepare(reg, sp, l, w.g)
		if ref == nil || ref.Reason != ReasonSpelling || !strings.Contains(ref.Detail, name) {
			t.Errorf("%s in a graveyard: got %+v, want %s", name, ref, ReasonSpelling)
		}
	}
	if _, ref := Prepare(reg, sp, w.l, w.g); ref != nil {
		t.Errorf("control item refused: %+v", ref)
	}
}

// TestPassPositionsFirstStrikeDamage: the two damage steps of a first-strike
// combat, sharing gorge's one step name, come apart at the non-active seat's
// pass on an empty stack, with a trigger resolving in the first of them; a
// combat with no first striker has the one step.
func TestPassPositionsFirstStrikeDamage(t *testing.T) {
	striker := []rules.OracleSnapPerm{{Attacking: true, Keywords: []string{"First Strike"}}}
	plain := []rules.OracleSnapPerm{{Attacking: true}}
	trigger := []rules.OracleSnapStack{{Kind: "ability", Source: "p0:X"}}
	at := func(step string, perms []rules.OracleSnapPerm, stack []rules.OracleSnapStack) rules.OracleSnapshot {
		return rules.OracleSnapshot{Turn: 1, Step: step, Permanents: perms, Stack: stack}
	}
	pass := func(seat int) oraclegen.Step { return oraclegen.Step{Op: "pass", Seat: seat} }

	type want struct {
		step  string
		moved bool
	}
	cases := []struct {
		name  string
		steps []oraclegen.Step
		snaps []rules.OracleSnapshot
		want  []want
	}{{
		name:  "first striker with a trigger in the first step",
		steps: []oraclegen.Step{pass(1), pass(0), pass(1), pass(0), pass(1), pass(0), pass(1)},
		snaps: []rules.OracleSnapshot{
			at("declare-blockers", striker, nil),
			at("combat-damage", striker, trigger),
			at("combat-damage", striker, trigger),
			at("combat-damage", striker, nil),
			at("combat-damage", striker, nil),
			at("combat-damage", striker, nil),
			at("combat-damage", striker, nil),
			at("end-combat", striker, nil),
		},
		want: []want{
			{oraclegen.StepFirstStrikeDamage, true},
			{oraclegen.StepFirstStrikeDamage, false},
			{oraclegen.StepFirstStrikeDamage, false},
			{oraclegen.StepFirstStrikeDamage, false},
			{"combat-damage", true},
			{"combat-damage", false},
			{"end-combat", true},
		},
	}, {
		name:  "no first striker",
		steps: []oraclegen.Step{pass(1), pass(0), pass(1)},
		snaps: []rules.OracleSnapshot{
			at("declare-blockers", plain, nil),
			at("combat-damage", plain, nil),
			at("combat-damage", plain, nil),
			at("end-combat", plain, nil),
		},
		want: []want{{"combat-damage", true}, {"combat-damage", false}, {"end-combat", true}},
	}}
	for _, c := range cases {
		got := passPositions(c.steps, c.snaps)
		if len(got) != len(c.want) {
			t.Fatalf("%s: %d positions, want %d", c.name, len(got), len(c.want))
		}
		for i, w := range c.want {
			if got[i].Step != w.step || got[i].Moved != w.moved {
				t.Errorf("%s: pass %d: %s moved=%v, want %s moved=%v", c.name, i, got[i].Step, got[i].Moved, w.step, w.moved)
			}
		}
	}
}

// TestPreparePassPositions: a prepared item has one xmage_pos entry per step,
// set exactly on the pass steps and equal to gorge's checkpoint after the step;
// Moved marks a pass that changed the turn or step and Resolves one that
// removed a stack object, and no pass is both (the object resolves first).
// An item naming a card no manifest lists is Forge's alone: XMage could not place it.
func TestPrepareRefusesACardNoXMageSetLists(t *testing.T) {
	reg, sp, ws, _ := prepared(t, 1)
	w := ws[0]
	l := w.l
	l.Item.Setup = map[string]oraclegen.Seat{}
	for k, s := range w.l.Item.Setup {
		l.Item.Setup[k] = s
	}
	const absent = "A-Blood Artist" // a digital rebalance, in Forge's corpus and no XMage set
	s := l.Item.Setup["p1"]
	s.Graveyard = append(slices.Clone(s.Graveyard), absent)
	l.Item.Setup["p1"] = s
	_, ref := Prepare(reg, sp, l, w.g)
	if ref == nil || ref.Reason != ReasonNoXMageCard || !strings.Contains(ref.Detail, absent) {
		t.Fatalf("refusal %+v, want %s naming %q", ref, ReasonNoXMageCard, absent)
	}
	if _, ref := Prepare(reg, sp, w.l, w.g); ref != nil && ref.Reason == ReasonNoXMageCard {
		t.Fatalf("an item of listed cards was refused: %+v", ref)
	}
}

func TestPreparePassPositions(t *testing.T) {
	_, _, ws, items := prepared(t, 2)
	var moved, resolves int
	for k, it := range items {
		if len(it.XPos) != len(it.Steps) {
			t.Errorf("%s: %d xmage_pos for %d steps", it.ID, len(it.XPos), len(it.Steps))
			continue
		}
		snaps := ws[k].g.Snapshots
		for i, st := range it.Steps {
			p := it.XPos[i]
			if (st.Op == "pass") != (p != nil) {
				t.Errorf("%s step %d (%s): xmage_pos %v", it.ID, i, st.Op, p)
				continue
			}
			if p == nil {
				continue
			}
			after := snaps[i+1]
			step := p.Step
			if step == oraclegen.StepFirstStrikeDamage {
				step = "combat-damage"
			}
			if p.Turn != after.Turn || step != after.Step || p.Active != after.Active {
				t.Errorf("%s step %d: position %+v, gorge stood at turn %d %s active %d", it.ID, i, *p, after.Turn, after.Step, after.Active)
			}
			if p.Moved && p.Resolves {
				t.Errorf("%s step %d: a pass that moved the step cannot also resolve", it.ID, i)
			}
			if p.Resolves && len(snaps[i].Stack) == 0 {
				t.Errorf("%s step %d: resolves with an empty stack", it.ID, i)
			}
			if p.Moved {
				moved++
			}
			if p.Resolves {
				resolves++
			}
		}
	}
	t.Logf("%d passes moved the step, %d resolved an object", moved, resolves)
	if moved == 0 {
		t.Error("sample has no pass that moves the step")
	}
}

func TestStackLost(t *testing.T) {
	a := rules.OracleSnapStack{Kind: "spell", Source: "p0:Shock", Controller: 0}
	b := rules.OracleSnapStack{Kind: "ability", Source: "p0:Imp", Controller: 0}
	for _, c := range []struct {
		name          string
		before, after []rules.OracleSnapStack
		want          bool
	}{
		{"both empty", nil, nil, false},
		{"unchanged", []rules.OracleSnapStack{a}, []rules.OracleSnapStack{a}, false},
		{"emptied", []rules.OracleSnapStack{a}, nil, true},
		{"resolved, trigger pushed", []rules.OracleSnapStack{a}, []rules.OracleSnapStack{b}, true},
		{"pushed only", []rules.OracleSnapStack{a}, []rules.OracleSnapStack{a, b}, false},
		{"one of two copies left", []rules.OracleSnapStack{a, a}, []rules.OracleSnapStack{a}, true},
	} {
		if got := stackLost(c.before, c.after); got != c.want {
			t.Errorf("%s: stackLost = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestPlayStepUnlessPays: a land played as a step answers the as-enters "pay
// 2 life?" on XMage's CHOICE queue. The same paid cost on a pass step keeps its
// numeric mode answer, and a decline or a play-step modal ETB is untouched.
func TestPlayStepUnlessPays(t *testing.T) {
	play := oraclegen.Step{Op: "play", Seat: 0, Card: "p0:Sacred Foundry"}
	pass := oraclegen.Step{Op: "pass", Seat: 0}
	paid := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes",
		Options: 2, Min: 1, Max: 1,
		Picks: []string{"Pay 2 life"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Sacred Foundry"}, PickKinds: []string{"mode"},
		Resume: "unless_pay",
	}
	decline := paid
	decline.Picks = []string{"Don't pay"}
	decline.PickIdx = []int{1}
	modal := paid
	modal.Resume = "modes"
	modal.Picks = []string{"Create a 1/1 token"}
	passPaid := paid
	passPaid.Step = 1

	for _, c := range []struct {
		name  string
		steps []oraclegen.Step
		d     rules.OracleDecision
		want  [][]oraclegen.XAnswer
	}{
		{
			"play step paid",
			[]oraclegen.Step{play},
			paid,
			[][]oraclegen.XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}}},
		},
		{
			"play step decline",
			[]oraclegen.Step{play},
			decline,
			[][]oraclegen.XAnswer{{{Seat: 0, Kind: "choice", Value: "no"}}},
		},
		{
			"pass step paid",
			[]oraclegen.Step{play, pass},
			passPaid,
			[][]oraclegen.XAnswer{nil, {{Seat: 0, Kind: "mode", Value: "1"}}},
		},
		{
			"play step modal ETB",
			[]oraclegen.Step{play},
			modal,
			[][]oraclegen.XAnswer{{{Seat: 0, Kind: "mode", Value: "1"}}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := playStepUnlessPays(c.steps, []rules.OracleDecision{c.d})
			sc := oraclegen.Scenario{Steps: c.steps}
			as := oraclegen.XAnswersForScenario(nil, rules.OracleResult{Decisions: got}, sc, nil, nil)
			if !reflect.DeepEqual(as, c.want) {
				t.Errorf("answers = %+v, want %+v", as, c.want)
			}
		})
	}
}

func TestIsTape(t *testing.T) {
	for id, want := range map[string]bool{
		"xe:FDN:0:1:tape":    true,
		"xe:FDN:0:1:tape:t2": true,
		"xe:FDN:0:1:w3":      false,
		"xe:FDN:0:1:tape2x":  false,
		"xe:FDN:0:1:tape:tx": false,
	} {
		if got := IsTape(id); got != want {
			t.Errorf("IsTape(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestComboColourPrefix(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	face := func(name string) *cards.Face {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("no card %q", name)
		}
		return c.Faces[0]
	}
	black := oraclegen.Step{Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"B"}}}}
	if got := comboColourPrefix("{T}", face("Temple of Deceit"), 0, black); got != "{T}: Add {B}" {
		t.Errorf("a two-colour tap: %q", got)
	}
	labelled := oraclegen.Step{Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"Add U"}}}}
	if got := comboColourPrefix("{T}", face("Temple of Deceit"), 0, labelled); got != "{T}: Add {U}" {
		t.Errorf("an answer by the option's label: %q", got)
	}
	if got := comboColourPrefix("{T}", face("Temple of Deceit"), 0, oraclegen.Step{}); got != "{T}" {
		t.Errorf("no colour was asked: %q", got)
	}
	if got := comboColourPrefix("{T}: Add {B}.", face("Swamp"), 0, oraclegen.Step{}); got != "{T}: Add {B}." {
		t.Errorf("a one-colour tap: %q", got)
	}
}

// TestPrepareModalTriggerMode: a window cut at the pass that hands priority
// away still has the permanent's spell on the stack, so gorge records no mode
// decision for its modal enters trigger while XMage resolves it and asks MODE
// first, then the mode's target. Prepare completes the trigger from gorge's
// own continued replay onto the window's last step. A window where gorge did
// answer the mode keeps exactly that one answer.
func TestPrepareModalTriggerMode(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	// The card's own mode label, read from the corpus rather than pasted into
	// the test (the Forge script's text stays out of this repository).
	var surveil string
	if c, ok := reg.Lookup("Federation Probe"); ok {
		for label, n := range oraclegen.ModeNumbers(c.Faces[0]) {
			if n == 2 {
				surveil = label
			}
		}
	}
	if surveil == "" {
		t.Fatal("Federation Probe has no second mode label")
	}
	// build is the Federation Probe window of xe:RAND:0:15:w1: one land, tap,
	// cast, then the pass that hands priority to the opponent. recorded adds
	// the opponent's pass and answers the trigger's mode, so gorge decides it
	// inside the window.
	build := func(recorded bool) (lower.Lowered, rules.OracleResult) {
		t.Helper()
		steps := []oraclegen.Step{
			{Op: "play", Seat: 0, Card: "p0:Island"},
			{Op: "activate", Seat: 0, Card: "p0:Island", AbilityIndex: &zero},
			{Op: "cast", Seat: 0, Card: "p0:Federation Probe"},
			{Op: "pass", Seat: 0},
		}
		if recorded {
			steps = append(steps, oraclegen.Step{Op: "pass", Seat: 1,
				Answers: []oraclegen.Answer{{Kind: "modes", Pick: []string{surveil}}}})
		}
		it := oraclegen.Item{
			ID:       "xe:test:modal-trigger-mode",
			Template: "xengine-window",
			Payment:  []string{"", "", "U", ""},
			Scenario: oraclegen.Scenario{
				Name: "Federation Probe at a cut pass", Turn: 1,
				Why: "a modal enters trigger is put on the stack after the window's last checkpoint",
				Setup: map[string]oraclegen.Seat{
					"p0": {
						Hand:       []string{"Island", "Federation Probe", "Plains", "Plains", "Plains", "Island"},
						LibraryTop: []string{"Command Decision", "Seven of Nine, Unassimilated", "Plains", "Lieutenant Commander Data", "Make It So", "Conquest Mission", "Island"},
						Life:       int32p(20),
					},
					"p1": {
						Hand:       []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp"},
						LibraryTop: []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp"},
						Life:       int32p(20),
					},
				},
				Steps: steps,
			},
		}
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Fails) > 0 {
			t.Fatalf("gorge rejects the window: %v", res.Fails)
		}
		return lower.Lowered{Item: it}, res
	}
	modeAnswers := func(it oraclegen.Item) []oraclegen.XAnswer {
		var out []oraclegen.XAnswer
		for _, as := range it.XAnswers {
			for _, a := range as {
				if a.Kind == "mode" {
					out = append(out, a)
				}
			}
		}
		return out
	}
	t.Run("cut before the trigger", func(t *testing.T) {
		l, g := build(false)
		if len(g.Decisions) != 0 {
			t.Fatalf("precondition: the cut recorded decisions %+v", g.Decisions)
		}
		it, ref := Prepare(reg, sp, l, g)
		if ref != nil {
			t.Fatalf("refused: %+v", ref)
		}
		last := len(it.XAnswers) - 1
		as := it.XAnswers[last]
		// Federation Probe is itself a legal target, so gorge's fallback takes
		// mode 1 ("Target creature gets +1/+1") and then targets the Probe.
		if len(as) < 2 || as[0].Kind != "mode" || as[0].Value != "1" {
			t.Fatalf("last step answers = %+v, want mode 1 first", as)
		}
		targeted := false
		for _, a := range as[1:] {
			if a.Kind == "target" && a.Value == "Federation Probe" {
				targeted = true
			}
		}
		if !targeted {
			t.Errorf("last step answers = %+v, want the mode's Federation Probe target", as)
		}
		if got := modeAnswers(it); len(got) != 1 || got[0].Seat != 0 {
			t.Errorf("mode answers = %+v, want exactly the completed one", got)
		}
	})
	t.Run("gorge decided the mode", func(t *testing.T) {
		l, g := build(true)
		if len(g.Decisions) == 0 {
			t.Fatal("precondition: the window recorded no mode decision")
		}
		it, ref := Prepare(reg, sp, l, g)
		if ref != nil {
			t.Fatalf("refused: %+v", ref)
		}
		got := modeAnswers(it)
		if len(got) != 1 || got[0].Value != "2" {
			t.Fatalf("mode answers = %+v, want exactly gorge's Surveil answer 2", got)
		}
	})
}

// int32p is a *int32 for a scenario's Life.
func int32p(v int32) *int32 { return &v }
