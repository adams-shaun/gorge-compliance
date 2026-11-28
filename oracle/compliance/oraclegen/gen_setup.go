package oraclegen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// mayYes queues, on the step that posed it, an answer taking option 0 for
// every decision gorge's fallback left empty although it offered options.
// A payment-tap ask (Convoke/Improvise/Waterbend/Harmonize's "Tap <name>
// for 1/for <colour>/to waterbend for 1", a creature-mana "instead of
// paying 1", a Harmonize "reduce by N") is never force-answered: the tap
// changes which mana the cast spends, and the two engines must agree on the
// payment, so the injected pick leaves gorge pool mana floating that a
// pool-paid cast spends (The Wandering Rescuer, Lofty Dreams). The empty
// fallback answer is kept: both engines then pay the whole cost from the
// pool.
func mayYes(sc Scenario, ds []rules.OracleDecision) (Scenario, bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	changed := false
	for _, d := range ds {
		if d.Step < 0 || d.Step >= len(out.Steps) || d.Options == 0 || d.First == "" ||
			d.Via == "target" || d.Via == "answer" || (d.GorgeKind != "choose" && d.GorgeKind != "target") {
			continue
		}
		if len(d.Picks) == 0 && tapPaymentAsk(d.First) {
			continue
		}
		if len(d.Picks) > 0 && (d.Picks[0] != d.First || (len(d.PickIdx) > 0 && d.PickIdx[0] != 0)) {
			// The runner's fallback deliberately answered past option 0
			// (the "done" it takes for an optional triggered cost's mana
			// window no potential mana can pay, Esoteric Duplicator):
			// pinning option 0 would tap the source it declined to tap.
			// Option 0 can share the pick's label (a CR 704.5j keep among
			// same-named legends, where the runner keeps the member that
			// survives the batch: Grub, Notorious Auntie's token), so the
			// index decides, not the label.
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]Answer(nil), st.Answers...), Answer{Kind: d.GorgeKind, Pick: []string{d.First}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// tapPaymentAsk reports an option label from pay.ConvokeAsk's optional
// tap-for-payment menu. The family is matched by label because a
// rules.OracleDecision carries no option kinds when the fallback declined
// it; every member names the tapped permanent and the contribution. A mana
// wheel's "Tap <name> for mana" and a mandatory pay ask (Min 1, so gorge's
// fallback answered it and Picks is non-empty) do not match.
func tapPaymentAsk(label string) bool {
	if !strings.HasPrefix(label, "Tap ") {
		return false
	}
	if strings.HasSuffix(label, " for 1") || strings.HasSuffix(label, " instead of paying 1") {
		return true
	}
	if strings.Contains(label, " (reduce by ") {
		return true
	}
	for _, c := range []string{"W", "U", "B", "R", "G"} {
		if strings.HasSuffix(label, " for "+c) {
			return true
		}
	}
	return false
}

// searchPicks queues, on the hidden-library-search step that posed it, an
// answer taking the search's first eligible card. Gorge's deterministic
// fallback declines an "up to N" hidden search (CR 701.19b fail-to-find),
// and XMage poses a mandatory library search (Terramorphic Expanse's
// TargetCardInLibrary 1..1) that rejects the resulting [target_skip] with
// "Wrong skip command found". A declined search whose option list is
// non-empty has a legal card, so re-running with the first eligible card
// makes both engines find the same card. An optional "you may search"
// shape poses a search_confirm yes/no first; when that confirm was declined
// no search pick is posed, so there is nothing to queue, and when it was
// accepted the search was actually made, so forcing its pick agrees with
// XMage too. The answer is only kept when the replay consumes it (see the
// caller's PlaysThrough guard), so a search XMage never reaches falls back
// to the decline pair.
func searchPicks(sc Scenario, ds []rules.OracleDecision, look bool) (Scenario, bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	changed := false
	for i, d := range ds {
		if d.Step < 0 || d.Step >= len(out.Steps) || d.Options == 0 ||
			d.Kind != "choose_n" || len(d.Picks) != 0 {
			continue
		}
		pick := ""
		switch {
		case d.Resume == "search" && d.First != "":
			pick = d.First
		case look && declinedLookPick(d) && !lookedAck(ds, i):
			// A declined two-option choose-one from an exile/library look
			// (Fireglass Mentor's "Choose one of them" over the Dig'ed exiled
			// cards): XMage poses the ask as a mandatory TargetCardInExile
			// 1..1 that rejects even the skip token ("Wrong skip command
			// found"), so gorge must take the first looked-at card too. The
			// rerun's pick is re-scripted onto XMage's target queue by
			// RetargetForcedLookPicks; the caller's PlaysThrough guard keeps
			// the rerun only when the replay consumes the answer. The look
			// flag is the serving template's card-side proof: the same
			// decision shape on an ask XMage poses as an "up to" makeChoose
			// agrees with the routing's own skip, and a forced pick there
			// would move gorge's board for nothing (measured: Speedball, New
			// Warrior). A look_ack ahead of the ask is the Zimone's
			// Experiment shape the measured declined pair answers, so it is
			// excluded here.
			pick = d.OptionRefs[0]
		case look && declinedLookedOnePick(d) && lookedAck(ds, i):
			// A declined one-card chooser after a hidden-zone look the served
			// trigger chain proves (Whiskervale Forerunner's "You may reveal a
			// creature card ... from among them"): XMage's ask is
			// player.choose(cards, TargetCardInLibrary(filter)) -- a
			// mandatory 1..1 makeChoose that rejects the skip token -- so
			// gorge must take the lone eligible card too. The pick stays on
			// XMage's choice queue (no RetargetForcedLookPicks: that is the
			// two-option exile form), and the chooseUse that follows the
			// pick is scripted from the rerun's own decisions.
			pick = d.OptionRefs[0]
		default:
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]Answer(nil), st.Answers...), Answer{Kind: d.GorgeKind, Pick: []string{pick}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// declinedLookPick is the declined shape of the exile/library look chooser:
// "choose one of them" over two looked-at objects, up to one, nothing
// picked, option 0's label empty (the options are the looked-at objects).
func declinedLookPick(d rules.OracleDecision) bool {
	return d.Resume == "choice" && d.Options == 2 && d.Min == 0 && d.Max == 1 &&
		len(d.OptionRefs) == 2 && d.First == ""
}

// declinedLookedOnePick is the declined shape of a one-card look chooser:
// "choose a card" over exactly one looked-at eligible object, up to one,
// nothing picked, no option label (the option is the object).
func declinedLookedOnePick(d rules.OracleDecision) bool {
	return d.Resume == "choice" && d.Options == 1 && d.Min == 0 && d.Max == 1 &&
		len(d.OptionRefs) == 1 && d.First == ""
}

// lookedAck reports a look_ack by the same seat in decision i's step earlier
// in the list: the Zimone's Experiment shape whose declined answer is the
// measured "no" + target_skip pair (answer_routing.go), never a forced pick.
func lookedAck(ds []rules.OracleDecision, i int) bool {
	for _, d := range ds[:i] {
		if d.Resume == "look_ack" && sameAsker(d, ds[i]) {
			return true
		}
	}
	return false
}

// RetargetForcedLookPicks rewrites the XMage answers of the forced first-card
// picks SearchPicks queued for a declined exile/library look (Fireglass
// Mentor): XMage poses that ask as a mandatory TargetCardInExile on the
// target queue, so the makeChoose choice form the generic pick path emits
// would be a wrong-queue answer. The value is the generic target answer's
// own form (targetPickValue), so a same-named pair stays disambiguated
// exactly as every target ask is. Only callers that just re-ran a scenario
// SearchPicks changed call it; a decision list with no forced pick is
// returned unchanged.
func RetargetForcedLookPicks(xa [][]XAnswer, ds []rules.OracleDecision) [][]XAnswer {
	for _, d := range ds {
		if d.Step < 0 || d.Step >= len(xa) || len(d.Picks) != 1 || d.Picks[0] != "" ||
			len(d.PickKinds) != 1 || d.PickKinds[0] != "card" ||
			d.Kind != "choose_n" || d.Resume != "choice" || d.Options != 2 ||
			d.Min != 0 || d.Max != 1 || len(d.OptionRefs) != 2 || d.First != "" ||
			len(d.PickRefs) != 1 {
			continue
		}
		choice := disambiguatedObjectChoice(d, 0, oraclediffRefName(d.PickRefs[0]))
		for i, a := range xa[d.Step] {
			if a.Seat == d.Seat && a.Kind == "choice" && a.Value == choice {
				xa[d.Step][i] = XAnswer{d.Seat, "target", targetPickValue(d, 0, oraclediffRefName(d.PickRefs[0]))}
				break
			}
		}
	}
	return xa
}

// playsThrough replays sc exactly and reports whether gorge performed
// every step and ended with an empty stack. Both leftover targets and
// answers are failures here: callers must rewrite targets to gorge's actual
// picks before replaying the generated scenario.
func playsThrough(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	res, ok := probeTargets(reg, sc)
	return res, ok && len(res.Fails) == 0
}

// probeTargets runs a preliminary fixture that may over-offer targets. Only
// unused-target failures are excused here, before chooseTargets replaces the
// fixture slots; a reversed cast, or any other failure, is never a success.
func probeTargets(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	b, _ := json.Marshal(sc)
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil || len(res.Snapshots) != len(sc.Steps)+1 || castAborted(sc, res.Snapshots, res.Transcript) {
		return res, false
	}
	for _, f := range res.Fails {
		if !strings.Contains(f, rules.OracleUnusedTargetMarker) {
			return res, false
		}
	}
	return res, len(res.Snapshots[len(res.Snapshots)-1].Stack) == 0
}

// baseline gives every cast scenario something for "up to one target"
// triggers and library searches to find: XMage poses those decisions even
// with no legal choice (and strict mode then needs a scripted skip), while
// gorge skips them. An opposing creature and a varied library top make
// both engines ask.
func baseline(setup map[string]Seat, f *cards.Face) {
	p1 := setup["p1"]
	has := false
	for _, n := range p1.Battlefield {
		if n == "Grizzly Bears" {
			has = true
		}
	}
	if !has {
		p1.Battlefield = append(p1.Battlefield, "Grizzly Bears")
	}
	setup["p1"] = p1
	if randomLibraryPick(f) {
		// An effect that CHOOSES a card from the library at random (Getaway
		// Barrel's "put a random creature card from among them onto the
		// battlefield") picks with each engine's own RNG, so a varied top made
		// the permanent's identity differ run to run (gorge Ornithopter, XMage
		// Grizzly Bears). One NAME repeated 13 times (the largest reveal the
		// generated rows inspect) makes every random pick choose the same name
		// -- the 13 entries are distinct card objects whose names match, which
		// is what the name-level frozen snapshot compares.
		p0 := setup["p0"]
		p0.LibraryTop = repeat("Grizzly Bears", 13)
		setup["p0"] = p0
	} else if searchesLibrary(f) {
		p0 := setup["p0"]
		p0.LibraryTop = []string{"Jace Beleren", "Grizzly Bears", "Forest", "Glorious Anthem", "Shock", "Plains", "Ornithopter"}
		setup["p0"] = p0
	}
}

// randomLibraryPick reports whether one of the card's effects selects a card
// from a library with AtRandom$ True (Getaway Barrel's ChooseCard). Such a
// scenario needs a single-name library top: the selection is random, and the
// two engines' RNGs do not agree.
func randomLibraryPick(f *cards.Face) bool {
	randomFromLibrary := func(line string) bool {
		return strings.Contains(line, "AtRandom$ True") &&
			(strings.Contains(line, "ChoiceZone$ Library") || strings.Contains(line, "Origin$ Library"))
	}
	for _, sa := range f.Abilities {
		if randomFromLibrary(sa.Line) {
			return true
		}
	}
	for _, body := range f.SVars {
		if randomFromLibrary(body) {
			return true
		}
	}
	for _, t := range f.Triggers {
		if t.Effect != nil && randomFromLibrary(t.Effect.Line) {
			return true
		}
	}
	return false
}

func searchesLibrary(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind == "SP" && sa.API == "Discover" {
			// Discover (CR 701.57) exiles from the top of the library until a
			// nonland card is found, so the scenario must seed a discoverable
			// card; otherwise gorge finds none and no Treasures are made while
			// XMage, whose library is not empty, creates them.
			return true
		}
		if strings.Contains(sa.Line, "Origin$ Library") {
			return true
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Origin$ Library") {
			return true
		}
	}
	for _, t := range f.Triggers {
		if t.Effect != nil && strings.Contains(t.Effect.Line, "Origin$ Library") {
			return true
		}
	}
	return false
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func withHand(s Seat, name string) Seat {
	s.Hand = append([]string{name}, s.Hand...)
	return s
}

// settle runs the cast in gorge and returns how many resolve steps empty
// the stack (at most 4); ok is false when gorge cannot cast with this
// fixture.
func settle(reg *cards.Registry, sc Scenario) (int, rules.OracleResult, bool) {
	for n := 1; n <= 4; n++ {
		try := sc
		try.Steps = append(append([]Step(nil), sc.Steps...), make([]Step, n)...)
		for i := len(sc.Steps); i < len(try.Steps); i++ {
			try.Steps[i] = Step{Op: "resolve"}
		}
		b, _ := json.Marshal(try)
		res, err := rules.RunOracleScenarioJSON(reg, b)
		if err != nil || len(res.Snapshots) == 0 {
			return 0, res, false
		}
		for _, f := range res.Fails {
			// The static fixture over-offers targets on purpose; the generator
			// rewrites each cast step to gorge's actual picks afterwards, and
			// verifies the rewrite with PlaysThrough. So the runner's
			// unused-target self-check is expected here and is not a reason to
			// reject the fixture; every other step/harness fail is.
			if strings.Contains(f, rules.OracleUnusedTargetMarker) {
				continue
			}
			if strings.HasPrefix(f, "step ") || strings.Contains(f, "harness:") {
				return 0, res, false
			}
		}
		if castAborted(try, res.Snapshots, res.Transcript) {
			continue
		}
		last := res.Snapshots[len(res.Snapshots)-1]
		if len(res.Snapshots) == len(try.Steps)+1 && len(last.Stack) == 0 {
			return n, res, true
		}
	}
	return 0, rules.OracleResult{}, false
}

// castAborted rejects a scenario where the runner reports a cast abort or a
// cast step leaves its card in its origin hand zone. CR 601.2c/733.1 reverses
// an illegal cast; treating the resulting empty stack as a successful settle
// would publish a scenario that never cast the named spell.
func castAborted(sc Scenario, snaps []rules.OracleSnapshot, transcript []string) bool {
	for _, line := range transcript {
		if strings.Contains(strings.ToLower(line), "cast aborted") {
			return true
		}
	}
	if len(snaps) != len(sc.Steps)+1 {
		return false
	}
	for i, st := range sc.Steps {
		if st.Op != "cast" {
			continue
		}
		seatRef, name, ok := strings.Cut(st.Card, ":")
		if !ok || !strings.HasPrefix(seatRef, "p") {
			continue
		}
		var seat int
		if _, err := fmt.Sscanf(seatRef, "p%d", &seat); err != nil || seat < 0 || seat >= len(snaps[i].Players) {
			continue
		}
		before := countName(snaps[i].Players[seat].Hand, name)
		after := countName(snaps[i+1].Players[seat].Hand, name)
		if before > 0 && after >= before {
			return true
		}
	}
	return false
}

func countName(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}

// chooseTargets rewrites the target-carrying step's Targets from the target
// decisions gorge's deterministic runner actually made, in order. A
// target-carrying step is a cast (XMage's castSpell) or an activated
// ability's activate step (activateAbility): both take the step's Targets
// directly. The static fixture only promises a legal candidate per slot; it
// over-offers -- an "up to N" slot gorge declines, a token slot with no token
// on the board, a slot in a mixed chain -- and XMage rejects a target list
// whose count does not match the ability's, so the scenario must carry
// exactly gorge's picks (the target decision's PickRefs, in order).
//
// castSteps is the set of step indices whose targets were taken from such a
// step (the steps held on the Item): xanswers sends those through castSpell
// or activateAbility, not through a scripted target answer, while a target
// decision posed during a resolve step still needs an XMage answer. A step
// that posed no target decision (every slot skipped, or an ability with no
// targets) has its fixture targets cleared, so the surplus never reaches
// XMage. Level-A scenarios hold only cast steps, so their bytes and
// castSteps are unchanged.
func chooseTargets(sc Scenario, ds []rules.OracleDecision) (Scenario, map[int]bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	// carriesTargets is the op set whose own targets XMage consumes: cast
	// through castSpell, activate through activateAbility.
	carriesTargets := func(op string) bool { return op == "cast" || op == "activate" }
	// A modal cast's target is posed after the mode pick, so the runner's
	// fallback target decision ("cast fallback") is deterministic but not
	// Via "target"; fold it into the step too. Left as an xanswer it sat in
	// XMage's target queue until the NEXT target ask (a copied Shock's
	// any-target), which then rejected the name (Choreographed Sparks).
	modal := castModalSteps(ds)
	chosen := map[int][]string{}
	castSteps := map[int]bool{}
	for _, d := range ds {
		if d.Kind != "target" || d.Step < 0 || d.Step >= len(out.Steps) || !carriesTargets(out.Steps[d.Step].Op) {
			continue
		}
		if d.Via != "target" && !(modal[d.Step] && out.Steps[d.Step].Op == "cast") {
			continue
		}
		// A copy's CR 707.10c new-target election is never a cast step's own
		// targets: XMage poses it as per-slot chooseUse dialogs, not through
		// castSpell, so it must not be folded into the step's fixture targets.
		if d.Resume == "copy_targets" {
			continue
		}
		chosen[d.Step] = append(chosen[d.Step], d.PickRefs...)
		castSteps[d.Step] = true
	}
	for i := range out.Steps {
		if !carriesTargets(out.Steps[i].Op) {
			continue
		}
		if _, ok := castSteps[i]; ok {
			out.Steps[i].Targets = chosen[i]
			continue
		}
		// A cast that posed no target decision: drop the fixture's surplus.
		out.Steps[i].Targets = nil
	}
	return out, castSteps
}

// arrangeEmptyGraveyard reports whether an arrange decision answered the empty
// kept set of an optional (Min 0) graveyard-destination ask at step time: the
// shape an upkeep Surveil's step-time fallback poses in a scenario that
// revisits the upkeep, where every looked-at card went to the graveyard.
func arrangeEmptyGraveyard(d rules.OracleDecision) bool {
	return arrangeEmpty(d) && d.ArrangeKind == "graveyard"
}

// arrangeEmpty reports whether an arrange decision kept none of the cards an
// optional (Min 0) ask looked at at step time, sending them all away: to the
// graveyard (surveil) or the bottom of the library (scry).
func arrangeEmpty(d rules.OracleDecision) bool {
	return d.Step >= 0 && d.Min == 0 && len(d.PickIdx) == 0 && len(d.ArrangeLabels) > 0 &&
		len(d.ArrangeLabels) == d.Options
}

func hasType(f *cards.Face, t string) bool {
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// openingHandAnswers declines the "you may begin the game with this card" ask
// for a K:MayEffectFromOpeningHand card. The runner's setup fallback otherwise
// takes option 0 ("Yes"), which starts the card on the battlefield and makes
// the later cast step "not offered"; the scenario wants the card in hand.
func openingHandAnswers(f *cards.Face) []Answer {
	if _, ok := f.KeywordParam("MayEffectFromOpeningHand"); ok {
		return []Answer{{Kind: "choose", Pick: []string{"no"}}}
	}
	return nil
}
