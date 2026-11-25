package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// xanswersForScenario translates a composite gorge election ("Yes — discard" /
// "Yes — put into the battlefield") into XMage's direct card selection. The
// card is taken from the verified before/after checkpoint, never guessed from
// the first library card: a spell can draw, reveal or replace that card before
// the election is answered. Other elections remain ordinary choices.
// castSteps is passed through to xanswers so a cast step's own targets are not
// scripted a second time.
func xanswersForScenario(reg *cards.Registry, res rules.OracleResult, sc Scenario, modes map[string]int, castSteps map[int]bool) [][]XAnswer {
	ds := liveNamedSameNames(castDivisionsFor(reg, sc, res, true), res.Snapshots)
	for i := range ds {
		d := &ds[i]
		if d.Kind != "choose_n" || d.Options != 2 || len(d.Picks) != 1 ||
			pickKind(*d, 0) != "yes" || len(d.PickRefs) != 1 || d.Picks[0] != d.PickRefs[0] {
			continue
		}
		var zone string
		switch {
		case d.Picks[0] == "Yes — discard":
			zone = "graveyard"
		case strings.HasPrefix(d.Picks[0], "Yes — put into the battlefield"):
			zone = "battlefield"
		default:
			continue
		}
		// If the engine offered the subsequent object choice, use that
		// recorded pick instead of inferring it from the resulting board.
		followup := false
		for j := i + 1; j < len(ds) && ds[j].Step == d.Step; j++ {
			pick := &ds[j]
			if pick.Seat != d.Seat || !compositeCardPick(*pick, zone) {
				continue
			}
			followup = true
			if len(pick.ObjectPicks) > 0 {
				d.Kind = pick.Kind
				d.GorgeKind = pick.GorgeKind
				d.Picks = make([]string, len(pick.ObjectPicks))
				d.PickRefs = append([]string(nil), pick.ObjectPicks...)
				d.PickKinds = make([]string, len(pick.ObjectPicks))
				for k, ref := range pick.ObjectPicks {
					d.Picks[k] = oraclediffRefName(ref)
					d.PickKinds[k] = "card"
					if zone == "graveyard" {
						d.PickKinds[k] = "discard"
					}
				}
				d.Max = pick.Max
				pick.Kind = "composite_election" // emitted once from the correlated picks
			} else {
				// Older transcripts still carry the selection on the follow-up.
				d.Kind = "composite_election" // no XMage yes/no dialog
			}
			break
		}
		if followup {
			continue
		}
		names := movedCards(res.Snapshots, sc, d.Step, d.Seat, zone)
		if len(names) == 0 {
			continue // no observable selected card: do not invent one
		}
		d.Picks, d.PickRefs = names, append([]string(nil), names...)
		d.PickKinds = make([]string, len(names))
		for k := range d.PickKinds {
			d.PickKinds[k] = "card"
		}
		// A TargetCardInLibrary(0, MAX) choice is not completed by the
		// first pick: makeChoose needs an explicit stop after the subset.
		// A DigUntil found move (gorge's diguntil_move) is all or nothing,
		// so its yes picks every found card: XMage's choose over the
		// revealed cards then has no target left (isChoiceCompleted) and
		// would never read the stop (Aurora Awakener, measured "(Choices
		// of PlayerA) Count are not equal (found [[choice_skip]])").
		if zone == "battlefield" && d.Resume != "diguntil_move" {
			d.Max = len(names) + 1
		} else {
			d.Max = len(names)
		}
	}
	collapseRoomLock(ds)
	unposeCollectEvidenceDeclines(reg, ds)
	// A permanent's Support (its enters-the-battlefield trigger or an
	// activated ability) is a counter_pick in gorge but a TARGET on XMage's
	// SupportAbility/activated ability, declared when the object is announced,
	// so its answers belong on the announcement step rather than the
	// resolution step (support_pick.go). An instant or sorcery's Support is
	// not in this loop: since gorge 5326ad5128 it is a cast target decision.
	// Activate steps (and cast steps of a permanent whose trigger is announced
	// by the cast) can carry a Step.Targets preset that would fill the
	// support slot, the ability's FIRST target, and make XMage skip its own
	// "up to" ask entirely; clear the preset and let every target of the
	// activation come from the answer queue, support answers first. The
	// Scenario value shares its Steps slice with the caller, so the cleared
	// preset is what the item serializes.
	supports := map[int]supportAnswers{}
	for i, d := range ds {
		sa, ok := supportPick(reg, sc, res, d)
		if !ok {
			continue
		}
		supports[i] = sa
		if sa.step >= 0 && sa.step < len(sc.Steps) &&
			(sc.Steps[sa.step].Op == "cast" || sc.Steps[sa.step].Op == "activate") {
			sc.Steps[sa.step].Targets = nil
			if castSteps != nil {
				delete(castSteps, sa.step)
			}
		}
	}
	if castSteps == nil {
		castSteps = stepsWithOwnTargets(sc)
	}
	castSteps = withoutDividedActivations(castSteps, ds, sc)
	// A Dig decision's XMage dialog selects an endpoint that depends on the
	// source card's IR (DestinationZone2): the exile family inverts gorge's
	// pick, every other family keeps it. Resolve the source once per step from
	// the scenario and the stack snapshots, so the answer router never has to
	// infer the family from the option count alone.
	digs := map[int]digSource{}
	for _, d := range ds {
		if d.Resume != "dig" {
			continue
		}
		if _, done := digs[d.Step]; done {
			continue
		}
		if src, ok := digSourceForStep(reg, sc, res.Snapshots, d.Step); ok {
			digs[d.Step] = src
		}
	}
	casts := map[int]bool{}
	for i, st := range sc.Steps {
		if st.Op == "cast" && !castXSizesCost(reg, st.Card) {
			casts[i] = true
		}
	}
	bounces := map[int]bool{}
	for _, d := range ds {
		if ownBounceShape(d) {
			if _, done := bounces[d.Step]; !done {
				bounces[d.Step] = ownBounceStep(reg, sc, res.Snapshots, d.Step)
			}
		}
	}
	// A mandatory hidden pick of N cards from the controller's own graveyard
	// is XMage's ExileCardFromOwnGraveyardControllerEffect for the proven
	// faces: it reads the pick from the TARGET queue as one "^"-joined target
	// definition, one segment per card (own_graveyard_exile_source.go). The
	// source resolves from the step's stack, as ownBounce's does; both proven
	// carriers pose the effect from an SVar (a granted trigger, a delayed
	// trigger), so the face-level shape is the authority.
	ownGraveyardExile := map[int]bool{}
	for _, d := range ds {
		if !ownGraveyardExileShape(d) {
			continue
		}
		if _, done := ownGraveyardExile[d.Step]; !done {
			ownGraveyardExile[d.Step] = ownGraveyardExileStep(reg, sc, res.Snapshots, d.Step)
		}
	}
	tapFirst := map[int]bool{}
	for _, d := range ds {
		if d.Resume == "taporuntap" {
			if _, done := tapFirst[d.Step]; !done {
				tapFirst[d.Step] = tapThenUntapStep(sc, res.Snapshots, d.Step)
			}
		}
	}
	// XMage asks the "card ORDER to put … from the TOP" dialog only for a
	// hand_move whose effect is implemented with anyOrder=true. Verify that
	// per resolving source instead of scripting the answers for every top
	// batch: Brainstorm and Dream Cache auto-top without any order ask.
	orderAsks := map[int]bool{}
	for i, d := range ds {
		if HandMoveBatch(d) && HandMoveOrderAsk(reg, sc, res.Snapshots, d) {
			orderAsks[i] = true
		}
	}
	searchZones := searchZoneAsks(reg, sc, res.Snapshots, ds)
	// A Bolster tie election is a counter_pick in gorge but XMage's
	// BolsterEffect asks it through chooseTarget: mark the decisions whose
	// resolving source carries PutCounter's Bolster$ parameter, so pickQueue
	// scripts their picks on the TARGET queue (bolster_pick.go).
	bolsters := map[int]bool{}
	for i, d := range ds {
		if bolsterTiePick(reg, sc, res, d) {
			bolsters[i] = true
		}
	}
	// A Craft keyword's material exile is an "exilecost" pick in gorge but
	// XMage's CraftAbility chooses it through chooseTarget: mark the
	// decisions whose activated ability carries Keyword$ Craft, so pickQueue
	// keeps their picks on the TARGET queue (craft_cost_pick.go).
	crafts := map[int]bool{}
	for i, d := range ds {
		if craftMaterialPick(reg, sc, res, d) {
			crafts[i] = true
		}
	}
	optionalTriggerSearch := map[int]bool{}
	for i, d := range ds {
		if optionalTriggerSearchDecline(reg, sc, res, d) {
			optionalTriggerSearch[i] = true
		}
	}
	sources := map[int][]string{}
	for _, d := range ds {
		if _, done := sources[d.Step]; !done {
			sources[d.Step] = stepSourceNames(sc, res.Snapshots, d.Step)
		}
	}
	// The all-player damage punisher's unless election (Browbeat and its
	// siblings): XMage offers its chooseUse to every payer even after one
	// accepts, so the answers expand to one boolean per seat (unless_payers.go).
	allPlayerUnless := unlessAllPlayerDecisions(reg, sc, res.Snapshots, ds)
	seats := sc.Players
	if seats < 1 {
		seats = 2
	}
	// A chooseUse whose yes branch moves the looked-at window to the library
	// bottom asks a follow-up card order XMage poses and gorge does not
	// (choose_dialog_order.go, Petals of Insight).
	chooseUseOrders := chooseUseBottomOrders(reg, sc, res.Snapshots, ds)
	out := xanswersSetup(ds, len(sc.Steps), modes, castSteps, sc.Setup, res.Flips, xanswersExtra{sources: sources, allPlayerUnless: allPlayerUnless, seats: seats, chooseUseOrders: chooseUseOrders, digs: digs, pips: phyrexianPipPlan(reg, res, sc), casts: casts, activates: activationXAnnounces(reg, sc, res), removeCounterCosts: removeCounterCostAsks(reg, sc, res), searchZones: searchZones, forage: forageExiles(ds, res.Snapshots), ownBounce: bounces, ownGraveyardExile: ownGraveyardExile, tapThenUntap: tapFirst, bolster: bolsters, optionalTriggerSearch: optionalTriggerSearch, craft: crafts, supports: supports, snaps: res.Snapshots, orderAsks: orderAsks})
	// The deterministic trample assignment XMage poses as a multi-amount
	// dialog is no decision the engine recorded, so it is synthesized from
	// the scenario's own attack/block steps and leads the pass_to step that
	// crosses combat damage (combat_damage_answers.go).
	prependCombatDamageAnswers(res, sc, out)
	return out
}

// compositeCardPick distinguishes the follow-up card selector from any other
// same-seat object-valued decision in the step. ObjectPicks alone is not a role:
// targets, costs, and replacement choices also record object identities.
func compositeCardPick(d rules.OracleDecision, zone string) bool {
	if d.Kind != "choose_n" && d.Kind != "mode" {
		return false
	}
	want := "card"
	if zone == "graveyard" {
		want = "discard"
	}
	if len(d.ObjectPicks) == 0 {
		return len(d.PickKinds) > 0 && d.PickKinds[0] == want
	}
	if len(d.PickKinds) != len(d.ObjectPicks) {
		return false
	}
	for _, kind := range d.PickKinds {
		if kind != want {
			return false
		}
	}
	return true
}

// movedCards finds cards newly in the indicated destination during one step.
// For discard, the resolving spell itself also goes to the graveyard; remove
// exactly one occurrence of its name rather than mistaking it for the pick.
// If a step introduces several other objects, do not guess which one was
// chosen: the generator's ordinary fallback remains visible to the oracle.
func movedCards(snaps []rules.OracleSnapshot, sc Scenario, step, seat int, zone string) []string {
	if step < 0 || step+1 >= len(snaps) || step >= len(sc.Steps) || seat < 0 ||
		seat >= len(snaps[step].Players) || seat >= len(snaps[step+1].Players) {
		return nil
	}
	before, after := snaps[step], snaps[step+1]
	var old, next []string
	if zone == "graveyard" {
		old, next = before.Players[seat].Graveyard, after.Players[seat].Graveyard
	} else {
		for _, p := range before.Permanents {
			if p.Owner == seat {
				old = append(old, p.Ref)
			}
		}
		for _, p := range after.Permanents {
			if p.Owner == seat {
				next = append(next, p.Ref)
			}
		}
	}
	counts := make(map[string]int, len(old))
	for _, name := range old {
		counts[name]++
	}
	var added []string
	for _, name := range next {
		if counts[name] > 0 {
			counts[name]--
		} else {
			added = append(added, name)
		}
	}
	// A permanent spell enters during its resolve step too; it is not
	// the object selected by the enter trigger. Likewise an instant or
	// sorcery enters the graveyard after resolving.
	for j := step - 1; j >= 0; j-- {
		if sc.Steps[j].Op != "cast" {
			continue
		}
		spell := oraclediffRefName(sc.Steps[j].Card)
		for k, name := range added {
			if oraclediffRefName(name) == spell {
				added = append(added[:k], added[k+1:]...)
				break
			}
		}
		break
	}
	if zone == "battlefield" {
		for i, ref := range added {
			for _, p := range after.Permanents {
				if p.Ref == ref {
					added[i] = p.Name
					break
				}
			}
		}
	}
	if len(added) != 1 { // unrelated moves or an ambiguous multi-card selection
		return nil
	}
	return added
}

// stepsWithOwnTargets marks the cast/activate steps that carry their own
// Targets, for a caller that rewrote none (castSteps nil). The driver queues
// a step's Targets itself, so its target decisions must not be scripted a
// second time: the spare answer is consumed by the next target ask
// (Meltstrider's Resolve's "Hill Giant" answered its own enters trigger's
// opponent-creature target; Choreographed Sparks' "p1" answered its
// TargetSpell), which rejects it.
func stepsWithOwnTargets(sc Scenario) map[int]bool {
	out := map[int]bool{}
	for i, st := range sc.Steps {
		if (st.Op == "cast" || st.Op == "activate") && len(st.Targets) > 0 {
			out[i] = true
		}
	}
	return out
}

// withoutDividedActivations drops from castSteps every activate step whose own
// target ask is divided (DividedAsYouChoose$: The Earth Crystal's "distribute
// two +1/+1 counters among one or two target creatures"), so xanswersSetup
// scripts that ask in XMage's TargetAmount form, one "<ref>^X=<share>" per
// recipient with gorge's own shares (dividedTargetAnswers). XMage's
// chooseTargetAmount rejects a bare alias ("Must be target amount, but found
// unknown target"), and the driver queues an activation's step targets bare,
// so it skips them when the step scripts a "^X=" answer. Cast steps keep their
// castSpell route ("^"-joined names), so their scenario bytes are unchanged.
// The input map is never mutated.
func withoutDividedActivations(castSteps map[int]bool, ds []rules.OracleDecision, sc Scenario) map[int]bool {
	var out map[int]bool
	for _, d := range ds {
		if d.Kind != "target" || d.Via != "target" || d.Resume == "copy_targets" || d.Divided <= 0 || len(d.PickRefs) == 0 {
			continue
		}
		if d.Step < 0 || d.Step >= len(sc.Steps) || sc.Steps[d.Step].Op != "activate" || !castSteps[d.Step] {
			continue
		}
		if out == nil {
			out = make(map[int]bool, len(castSteps))
			for k, v := range castSteps {
				out[k] = v
			}
		}
		delete(out, d.Step)
	}
	if out == nil {
		return castSteps
	}
	return out
}

// unposeCollectEvidenceDeclines clears CostPosed on a decline-only triggered
// collect evidence cost ("you may collect evidence 4" over a graveyard short
// of it: Surveillance Monitor, Izoni, Center of the Web). gorge's runner marks
// such a decline posed (rules.xmageAlwaysAsksCost reads collect evidence as a
// priceable cost with no non-mana component), but XMage's CollectEvidenceCost
// has a real canPay, so DoIfCostPaid poses no chooseUse and a scripted "no"
// is left over: "(Choices of PlayerA) Count are not equal (found [No])".
// The decision names its source by ref; a source whose triggers pay no
// CollectEvidence cost keeps the runner's mark.
func unposeCollectEvidenceDeclines(reg *cards.Registry, ds []rules.OracleDecision) {
	if reg == nil {
		return
	}
	for i := range ds {
		d := &ds[i]
		if !loneTriggerCostDecline(*d) {
			continue
		}
		ref := ""
		switch {
		case len(d.ObjectPicks) > 0:
			ref = d.ObjectPicks[0]
		case len(d.PickRefs) > 0:
			ref = d.PickRefs[0]
		}
		if ref == "" {
			continue
		}
		if c, ok := reg.Lookup(oraclediffRefName(ref)); ok && triggerCollectsEvidence(c) {
			d.CostPosed = false
		}
	}
}

// triggerCollectsEvidence reports whether any face's triggered ability (or a
// sub-ability it chains to) pays a CollectEvidence cost.
func triggerCollectsEvidence(c *cards.Card) bool {
	for _, f := range c.Faces {
		for _, t := range f.Triggers {
			for sa := t.Effect; sa != nil; sa = sa.Sub {
				if strings.HasPrefix(strings.TrimSpace(sa.Params["Cost"]), "CollectEvidence") ||
					strings.Contains(sa.Params["Cost"], " CollectEvidence") {
					return true
				}
			}
		}
	}
	return false
}

// forageExiles maps each accepted forage election (a trigger_cost_pay pick
// labelled "Forage") to the three cards its exile arm moved from the payer's
// graveyard to exile on the decision's step. gorge exiles the top three
// graveyard cards without posing a pick (rules' forageSettle), so the
// checkpoint is the only record of which cards; XMage asks them
// ("TargetCardInYourGraveyard: Select cards from your graveyard (selected 0
// of 3, min 3)", Bushy Bodyguard) and strict-missed with nothing scripted. A
// step that moved any other number of cards (the Food arm, a second exile on
// the step) is left unscripted rather than guessed.
func forageExiles(ds []rules.OracleDecision, snaps []rules.OracleSnapshot) map[int][]string {
	var out map[int][]string
	for i, d := range ds {
		if len(d.Picks) != 1 || pickKind(d, 0) != "trigger_cost_pay" || !strings.EqualFold(strings.TrimSpace(d.Picks[0]), "Forage") {
			continue
		}
		if d.Step < 0 || d.Step+1 >= len(snaps) || d.Seat < 0 ||
			d.Seat >= len(snaps[d.Step].Players) || d.Seat >= len(snaps[d.Step+1].Players) {
			continue
		}
		before, after := snaps[d.Step].Players[d.Seat], snaps[d.Step+1].Players[d.Seat]
		exiled := addedNames(before.Exile, after.Exile)
		left := addedNames(after.Graveyard, before.Graveyard)
		if len(exiled) != 3 || !sameNames(exiled, left) {
			continue
		}
		if out == nil {
			out = map[int][]string{}
		}
		out[i] = exiled
	}
	return out
}

// loneTriggerCostSacrifice maps an accepted trigger-cost pay whose label is a
// sacrifice (The Gitrog, Ravenous Ride: "Sacrifice creature that saddled it
// this turn") and whose sacrifice the engine folded -- one legal candidate, so
// gorge records no pick for it -- to that candidate's name. The checkpoint is
// the only record: the pay decision's ObjectPicks name the source, not the
// sacrificed creature, and gorge poses no pick when the choice is forced.
//
// XMage's card-specific effect folds the "you may" into a TargetSacrifice(0, 1)
// whose makeChoose reads the CHOICE queue, so without the name the effect's
// own hand pick (the land) is consumed by the sacrifice ask: "Found wrong
// choice command (invalid target or miss skip command): Swamp" (xpt-77cb30f4,
// xpt-cfdb102c). Emitting the checkpoint's move instead -- the same evidence
// forageExiles uses -- covers every sacrifice trigger cost whose lone pick the
// engine folded, not just this card.
//
// A step that also records an explicit sacrifice pick is left to that pick's
// own routing, and anything but one battlefield->graveyard move by the payer
// (a token, a simultaneous wipe, an unclear before-checkpoint) is left
// unscripted rather than guessed.
func loneTriggerCostSacrifice(ds []rules.OracleDecision, snaps []rules.OracleSnapshot) map[int][]string {
	posed := map[[2]int]bool{}
	for _, e := range ds {
		for _, k := range e.PickKinds {
			if k == "sacrifice" {
				posed[[2]int{e.Step, e.Seat}] = true
			}
		}
	}
	var out map[int][]string
	for i, d := range ds {
		if len(d.Picks) != 1 || pickKind(d, 0) != "trigger_cost_pay" ||
			!strings.Contains(strings.ToLower(strings.TrimSpace(d.Picks[0])), "sacrifice") {
			continue
		}
		if d.Step < 0 || d.Step+1 >= len(snaps) || posed[[2]int{d.Step, d.Seat}] {
			continue
		}
		before, after := snaps[d.Step], snaps[d.Step+1]
		if d.Seat < 0 || d.Seat >= len(before.Players) || d.Seat >= len(after.Players) {
			continue
		}
		moved := addedNames(before.Players[d.Seat].Graveyard, after.Players[d.Seat].Graveyard)
		if len(moved) != 1 {
			continue
		}
		name := moved[0]
		if !controlsPermanent(before.Permanents, d.Seat, name) || controlsPermanent(after.Permanents, d.Seat, name) {
			continue
		}
		if out == nil {
			out = map[int][]string{}
		}
		out[i] = []string{name}
	}
	return out
}

// controlsPermanent reports whether seat controls a permanent of that name in
// the checkpoint.
func controlsPermanent(perms []rules.OracleSnapPerm, seat int, name string) bool {
	for _, p := range perms {
		if p.Controller == seat && p.Name == name {
			return true
		}
	}
	return false
}

// addedNames is the multiset difference next - old, in next's order.
func addedNames(old, next []string) []string {
	counts := map[string]int{}
	for _, n := range old {
		counts[n]++
	}
	var added []string
	for _, n := range next {
		if counts[n] > 0 {
			counts[n]--
			continue
		}
		added = append(added, n)
	}
	return added
}

// sameNames reports whether a and b hold the same names, ignoring order.
func sameNames(a, b []string) bool {
	return len(a) == len(b) && len(addedNames(a, b)) == 0
}

// activationXAnnounces maps each activation step to the number of X
// announcements its costs pose. The ability is resolved through
// ResolveActivatedAbility first, so a granted activation is not read against
// the aliased printed index. A mana {X} contributes one announce (Talon of
// Pain, Chimeric Staff, Crypt Rats); a separate non-mana variable cost that
// announces its own X contributes another (Ludevic, Necrogenius's
// ExileXFromYourGraveCost). A counter-removal cost that reuses the mana X
// (Talon's Remove X charge counters) does not announce again.
func activationXAnnounces(reg *cards.Registry, sc Scenario, g rules.OracleResult) map[int]int {
	if reg == nil {
		return nil
	}
	it := Item{Scenario: Scenario{Setup: sc.Setup}}
	out := map[int]int{}
	for i, st := range sc.Steps {
		if st.Op != "activate" || st.AbilityIndex == nil {
			continue
		}
		sa, kind := ResolveActivatedAbility(reg, it, i, st, g)
		switch kind {
		case ActivatedGrantedUnresolved:
			continue
		case ActivatedPrinted:
			face, ok := printedStepFace(reg, it, i, st, g)
			if !ok {
				continue
			}
			sa = face.Abilities[*st.AbilityIndex]
		}
		if sa == nil {
			continue
		}
		if n := activationXAnnounceCount(sa.Params["Cost"]); n > 0 {
			out[i] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// activationXAnnounceCount counts the announceX asks an activation cost poses:
// one per mana {X}, plus one per non-mana variable cost whose count is X. The
// counter-removal family reuses the mana announce (XMage's
// TalonOfPainRemoveVariableCountersSourceCost overrides announceXValue), so it
// is not counted again; XMin<N> is the VariableManaCost bound marker, not a
// cost of its own. A token forgeCostSymbols itself renders as a {X}
// (Waterbend<X>) is the mana announce above, not a second one beside it.
func activationXAnnounceCount(cost string) int {
	n := 0
	for _, sym := range forgeCostSymbols(cost) {
		if sym == "{X}" {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	for _, tok := range forgeCostTokens(cost) {
		if _, ok := forgeManaSymbol(tok); ok {
			continue
		}
		if _, ok := loyaltyTokenCount(tok); ok || strings.HasPrefix(tok, "XMin") {
			continue
		}
		head, payload, _ := strings.Cut(tok, "<")
		payload = strings.TrimSuffix(payload, ">")
		parts := strings.Split(payload, "/")
		if len(parts) == 0 || !strings.EqualFold(strings.TrimSpace(parts[0]), "X") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(head), "subcounter") || tokenRendersManaX(tok) {
			// Source-counter removal and Waterbend<X> both reuse the mana X:
			// the former overrides XMage's announceXValue, the latter is the
			// {X} forgeCostSymbols rendered for the rule text above.
			continue
		}
		if strings.EqualFold(head, "Waterbend") {
			// Waterbend<X> is itself the {X} forgeCostSymbols counted: XMage
			// asks it once, through getAmount (Katara, Water Tribe's Hope).
			continue
		}
		n++
	}
	return n
}

// tokenRendersManaX reports whether forgeCostSymbols turns tok itself into a
// {X}, so the mana count that renders the rule text already counted its
// announce. Asking the renderer (rather than naming the token) keeps every
// future token it maps to {X} from being counted a second time.
func tokenRendersManaX(tok string) bool {
	for _, sym := range forgeCostSymbols(tok) {
		if sym == "{X}" {
			return true
		}
	}
	return false
}

// castXSizesCost reports a spell whose X also sizes a non-mana part of its
// cost (Abandon Hope's "Cost$ X 1 B Discard<X/Card>"). XMage bounds such an X
// by that part (an empty hand leaves only X = 0) and TestPlayer.announceX
// returns a fixed X unasked, so a forced announce there stays unscripted
// (castAnnouncesX): a scripted "X=0" was left over (Geometer's Arthropod and
// Matterbending Mage, casting Abandon Hope with an empty hand).
func castXSizesCost(reg *cards.Registry, ref string) bool {
	if reg == nil || ref == "" {
		return false
	}
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range c.Faces {
		for _, sa := range f.Abilities {
			if sa.Kind == "SP" && strings.Contains(sa.Params["Cost"], "<X") {
				return true
			}
		}
	}
	return false
}
