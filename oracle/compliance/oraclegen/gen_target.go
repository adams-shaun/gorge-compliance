package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// targetZone is the zone a target filter draws from: an explicit TgtZone$,
// else a non-battlefield Origin$, else Stack for a slot that names a spell
// or ability on the stack (TargetType$ Spell/SpellAbility/Activated/
// Triggered, or ValidTgts$ with inZoneStack).
//
// charmMode selects the narrower stack rule a charm's SVar mode needs: a
// mode whose ValidTgts$ also names a battlefield type (Icy Reception's
// "creature or legendary spell", Theorix Charm's "noncreature card") keeps
// its battlefield fixture, because the historical generator read it that
// way and its verdict is pinned to the battlefield scenario. A top-level
// Counter (Precise Redaction, Countersculpt) is routed before this by
// targetsSpell, so its broader reading is unchanged.
func targetZone(params map[string]string, charmMode bool) string {
	if z := params["TgtZone"]; z != "" {
		return z
	}
	if o := params["Origin"]; o != "" {
		// An Origin$ that names the battlefield alone is the default zone
		// and stays unsuffixed (no slot change for the common case); an
		// Origin$ that adds Stack ("Battlefield,Stack") is kept so the
		// slot records both alternatives.
		if !strings.Contains(o, "Battlefield") || strings.Contains(o, "Stack") {
			return o
		}
	}
	if charmMode {
		if charmModeTargetsStack(params) {
			return "Stack"
		}
		return ""
	}
	if abilityTargetsStack(params) {
		return "Stack"
	}
	return ""
}

// zoneNamesStack reports whether a TgtZone$ list names Stack (it may be a
// comma-separated combo such as "Stack,Battlefield").
func zoneNamesStack(z string) bool {
	for _, part := range strings.Split(z, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "Stack") {
			return true
		}
	}
	return false
}

// abilityTargetsStack reports whether an ability's target is a spell or
// ability on the stack, judged from its target vocabulary: a "Spell"-
// family TargetType$, or a ValidTgts$ naming the inZoneStack zone. An
// explicit non-stack zone (TgtZone$ / Origin$) wins, so a card targeting an
// instant card in a graveyard is not mistaken for a stack target.
func abilityTargetsStack(params map[string]string) bool {
	if strings.Contains(params["ValidTgts"], "inZoneStack") {
		return true
	}
	if params["TargetType"] == "" {
		return false
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "Spell", "SpellAbility", "Activated", "Triggered", "Instant", "Sorcery":
			return true
		}
	}
	return false
}

// charmModeTargetsStack is abilityTargetsStack narrowed for a charm SVar
// mode: an ability-type TargetType$ (SpellAbility/Activated/Triggered) is
// always stack-only, but a Spell/Instant/Sorcery target is routed to the
// stack only when its ValidTgts$ cannot be read as a battlefield permanent.
// A mode whose ValidTgts$ names a battlefield base type (Creature, Card, ...)
// keeps the battlefield fixture its committed verdict was generated for.
func charmModeTargetsStack(params map[string]string) bool {
	if strings.Contains(params["ValidTgts"], "inZoneStack") {
		return true
	}
	if params["TargetType"] == "" {
		return false
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "SpellAbility", "Activated", "Triggered":
			return true
		}
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "Spell", "Instant", "Sorcery":
			return !validTgtsNamesBattlefield(params["ValidTgts"])
		}
	}
	return false
}

// validTgtsBattlefieldBases are the ValidTgts$ base types that the
// battlefield candidate table can serve. A filter that names any of them is
// satisfiable on the battlefield, so a charm mode's Spell-family
// TargetType$ on such a filter is not forced onto the stack.
var validTgtsBattlefieldBases = map[string]bool{
	"any": true, "creature": true, "player": true, "opponent": true,
	"permanent": true, "card": true, "artifact": true, "enchantment": true,
	"land": true, "planeswalker": true,
}

// validTgtsNamesBattlefield reports whether a ValidTgts$ filter names a
// battlefield-card base type. An inZoneStack qualifier wins (the card is on
// the stack), and only the first alternative is consulted -- the fixture
// builder serves a filter from its first alternative's shape. An empty
// filter names no type and is never battlefield-satisfiable here.
func validTgtsNamesBattlefield(validTgts string) bool {
	if strings.Contains(validTgts, "inZoneStack") {
		return false
	}
	first := strings.SplitN(validTgts, ",", 2)[0]
	base := strings.ToLower(strings.SplitN(strings.TrimSpace(first), ".", 2)[0])
	return validTgtsBattlefieldBases[base]
}

// SlotIsStack reports whether a target filter (as TargetSlots/ChainSlots
// encode it) draws only from the stack. A slot that also names a non-stack
// zone ("Stack,Battlefield") is served by the ordinary fixture -- the
// battlefield candidate -- so it is not a stack slot.
func SlotIsStack(filter string) bool {
	i := strings.LastIndexByte(filter, '@')
	if i < 0 {
		return false
	}
	z := filter[i+1:]
	if !zoneNamesStack(z) {
		return false
	}
	for _, part := range strings.Split(z, ",") {
		if p := strings.TrimSpace(part); p != "" && !strings.EqualFold(p, "Stack") {
			return false
		}
	}
	return true
}

// perPlayerTargetAnswers answers a TargetsForEachPlayer$ ask: one target for
// each seat in the match, in seat order starting at 0, using the pick that
// seat's controller made or a target skip when the seat chose none. XMage's
// ForEachPlayerTargetsAdjuster poses every seat's ask, even a seat with no
// legal candidate.
func perPlayerTargetAnswers(d rules.OracleDecision) []XAnswer {
	seatPick := map[int]string{}
	for _, ref := range d.PickRefs {
		s, ok := refSeat(ref)
		if !ok {
			continue
		}
		if _, dup := seatPick[s]; dup {
			continue
		}
		v := ref
		if !isSeat(ref) {
			v = oraclediffRefName(ref)
		}
		seatPick[s] = v
	}
	n := d.SeatCount
	if n < 1 {
		n = 1
	}
	out := make([]XAnswer, 0, n)
	for s := 0; s < n; s++ {
		if v, ok := seatPick[s]; ok {
			out = append(out, XAnswer{d.Seat, "target", v})
		} else if s == d.Seat && d.PerOpponent {
			// The controller's own seat without a pick: the corpus's
			// per-player targets are "for each opponent" (Celebrate the
			// Mountain-king, Omega, Riptide Gearhulk), which XMage never
			// asks the controller for -- a skip here would be consumed as
			// the opponent's answer (measured on the std pass).
			continue
		} else {
			out = append(out, XAnswer{d.Seat, "target", "[target_skip]"})
		}
	}
	return out
}

// refSeat returns the seat a scenario ref belongs to ("p1:Grizzly Bears" or
// "p1" -> 1). It is how a per-player target answer finds the seat whose ask
// the pick answers.
func refSeat(ref string) (int, bool) {
	if !strings.HasPrefix(ref, "p") {
		return 0, false
	}
	r := ref[1:]
	if i := strings.IndexByte(r, ':'); i >= 0 {
		r = r[:i]
	}
	n, err := strconv.Atoi(r)
	if err != nil {
		return 0, false
	}
	return n, true
}

// targetDecisionAnswers scripts one target decision. A TargetsForEachPlayer$
// ask is answered per seat, a DividedAsYouChoose$ ask in XMage's divided
// "<ref>^X=<share>" form, anything else one answer per pick. Every form is
// followed by a target skip for each "up to N" chain slot the engine settled
// without posing it (XMage still asks those), and preceded by one for each
// such slot settled BEFORE this ask (leadingUnposedSkips).
func targetDecisionAnswers(r *answerRouting, i int) []XAnswer {
	d := r.ds[i]
	as := leadingUnposedSkips(d)
	switch {
	case d.Resume == "copy_targets" && d.Divided == 0 && !d.PerPlayer:
		// A copy's CR 707.10c new-target election: XMage asks one chooseUse
		// per inherited target before the pick, so this is not one target
		// dialog and never takes the trailing "up to N" skip below. A divided
		// (TargetAmount) or per-player copy keeps its existing branch: the
		// per-slot replace stream for those is unproven, so it is left alone.
		as = append(as, copyTargetAnswers(d)...)
	case d.PerPlayer:
		// TargetsForEachPlayer$ (CR 601.2c): XMage asks one target
		// per player, in seat order starting at seat 0. Answer each
		// seat with the pick its controller made, or a target skip
		// when the seat chose none.
		as = append(as, perPlayerTargetAnswers(d)...)
	case d.Divided > 0 && len(d.PickRefs) > 0:
		// A TargetAmount slot (distribute counters, divided damage): XMage's
		// chooseTargetAmount takes one "<ref>^X=<share>" per target and
		// completes when the allocation is answered.
		as = append(as, dividedTargetAnswers(r, i)...)
	default:
		for k := range d.PickRefs {
			as = append(as, singleTargetAnswer(d, k))
		}
		if len(d.PickRefs) < d.Max && !poolExhausted(d) {
			// Fewer picks than the "up to N" ask allows: XMage keeps
			// asking, so stop it with the target queue's skip token. This
			// also covers a slot gorge never posed at all (zero picks).
			as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
		}
	}
	for n := 0; n < d.UnposedSlots; n++ {
		as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
	}
	return as
}

// poolExhausted reports an ask whose picks took every candidate gorge offered:
// XMage completes such an object by itself (TargetImpl.isChoiceCompleted's
// moreSelectCount == 0), so a closing skip would be left unused ("Targets ...
// Count are not equal (found [[target_skip]])": Eddymurk Crab's "up to two
// target creatures" with itself the only creature). A decision that records
// no candidate count keeps the skip.
func poolExhausted(d rules.OracleDecision) bool {
	return d.Options > 0 && d.Options <= len(d.PickRefs)
}

// singleTargetAnswer is the TARGET-queue answer for one pick: a seat ref, an
// exact identity ref (a same-name alias, a permanent's current face, or an
// inexact ref's exact owner prefix), or the ref's name with a same-name
// marker. It is the one home for that spelling, shared by the ordinary target
// ask and a copy's per-slot replacement, so the two cannot drift.
func singleTargetAnswer(d rules.OracleDecision, k int) XAnswer {
	ref := d.PickRefs[k]
	if isSeat(ref) {
		return XAnswer{d.Seat, "target", ref}
	}
	// XMage's chooseTarget parses the same copy marker as makeChoose and
	// filters on isCopy(), so a target that shares its name with another
	// offered object needs the discriminator here too (a token copy of a
	// card, Extravagant Replication). An alias pick keeps its full ref, which
	// the driver's targetName maps to the object's @alias.
	p := ClassifySameName(d, k)
	if p.Alias != "" {
		return XAnswer{d.Seat, "target", ref}
	}
	if p.Unanswerable {
		// The ref is rank-derived, but its owner prefix may still separate
		// the candidates (two same-named cards in different graveyards). The
		// driver's alias binding scopes a non-token ref to that seat, so
		// "<seat>:<name>" reaches the picked card instead of the first
		// same-named one XMage matches by name alone (CR 601.2c).
		if owner := ownerQualifiedRef(d, k); owner != "" {
			return XAnswer{d.Seat, "target", owner}
		}
	}
	if currentFaceTargetRef(d, k) {
		// The option label names a different object than the ref -- the
		// permanent's current face (a transformed DFC, a face-down card). Its
		// current name selects the live object when no other option shares
		// it; else the ref's identity alias.
		if name, ok := uniqueCurrentFaceName(d, k); ok {
			return XAnswer{d.Seat, "target", name}
		}
		return XAnswer{d.Seat, "target", ref}
	}
	_, marker := SameNameAmbiguity(d, k)
	return XAnswer{d.Seat, "target", oraclediffRefName(ref) + marker}
}

// dividedTargetAnswers answers a divided target ask with each pick's share of
// the total. A divided target ask with several recipients is paired with the
// engine's own damage_split decision (that split's recipients are exactly the
// ask's), whose shares are then emitted here -- at the ask's own queue
// position, ahead of the chain skips its caller appends. Counters have no such
// decision: the engine deals them round-robin over the picks.
func dividedTargetAnswers(r *answerRouting, i int) []XAnswer {
	d := r.ds[i]
	n := len(d.PickRefs)
	if j, ok := r.targetSplit[i]; ok {
		return damageSplitAnswers(r.ds[j])
	}
	if j, ok := r.targetDivide[i]; ok {
		// The announced division: every target takes 1, and each pick of
		// the division ask adds 1 to the target at that option index.
		as := make([]XAnswer, 0, n)
		for k, ref := range d.PickRefs {
			share := 1
			for _, p := range r.ds[j].PickIdx {
				if p == k {
					share++
				}
			}
			as = append(as, XAnswer{d.Seat, "target", ref + "^X=" + strconv.Itoa(share)})
		}
		return as
	}
	as := make([]XAnswer, 0, n)
	for k, ref := range d.PickRefs {
		share := d.Divided / n
		if k < d.Divided%n {
			share++
		}
		if share == 0 {
			continue
		}
		// Keep the scenario ref (including #N), as the damage split does.
		as = append(as, XAnswer{d.Seat, "target", ref + "^X=" + strconv.Itoa(share)})
	}
	return as
}

// damageSplitAnswers turns a "damage_split" KChoose into one target answer
// per chosen target, its Value the ref plus "^X=<share>". The engine's split
// answer is a multiset over option indexes: a target receiving k damage has
// its index repeated k times, and PickIdx/Picks/PickRefs are parallel. Targets
// are emitted in first-appearance order of the option index, exactly as the
// engine assigns them.
func damageSplitAnswers(d rules.OracleDecision) []XAnswer {
	share := map[int]int{}
	var order []int
	for _, i := range d.PickIdx {
		if _, seen := share[i]; !seen {
			order = append(order, i)
		}
		share[i]++
	}
	ref := map[int]string{}
	for k, i := range d.PickIdx {
		if k < len(d.PickRefs) {
			ref[i] = d.PickRefs[k]
		}
	}
	as := make([]XAnswer, 0, len(order))
	for _, i := range order {
		name := ref[i]
		if name == "" {
			// A snapshot without refs: fall back to the option label, which
			// for a damage-split permanent is the card name.
			if i < len(d.Picks) {
				name = d.Picks[i]
			}
		}
		// Keep the scenario ref (including #N): XMage's targetName uses it
		// to resolve two same-name permanents to distinct setup aliases.
		as = append(as, XAnswer{d.Seat, "target", name + "^X=" + strconv.Itoa(share[i])})
	}
	return as
}

// manaAllocationAnswers turns a "mana_color" allocation (Min==Max>1) into one
// amount answer per WUBRG colour, zeroes included: XMage's
// getMultiAmountWithIndividualConstraints iterates the effect's manaSymbols in
// ColoredManaSymbol order and requires an "X=<n>" for each. A pick's colour is
// its option label ("Add W", the authority), falling back to its index mod 5
// (the engine/effects/mana_effect.go layout: unit*5 + colourIndex).
func manaAllocationAnswers(d rules.OracleDecision) []XAnswer {
	counts := map[byte]int{}
	for k := range d.Picks {
		if code, ok := allocationColour(d, k); ok {
			counts[code]++
		}
	}
	as := make([]XAnswer, 0, 5)
	for _, code := range "WUBRG" {
		as = append(as, XAnswer{d.Seat, "amount", strconv.Itoa(counts[byte(code)])})
	}
	return as
}

// allocationColour names the WUBRG colour one picked option allocates.
func allocationColour(d rules.OracleDecision, k int) (byte, bool) {
	if k < len(d.Picks) {
		if label := d.Picks[k]; strings.HasPrefix(label, "Add ") && len(label) == 5 {
			if strings.IndexByte("WUBRG", label[4]) >= 0 {
				return label[4], true
			}
		}
	}
	if k < len(d.PickIdx) {
		return "WUBRG"[d.PickIdx[k]%5], true
	}
	return 0, false
}

// hasTargetPick reports whether any picked option reaches XMage's target
// queue (a real TargetXxx), which XMage poses even when the engine offered
// exactly one legal option.
func hasTargetPick(d rules.OracleDecision) bool {
	for k := range d.Picks {
		switch pickKind(d, k) {
		case "permanent", "player", "opponent_choice":
			return true
		}
	}
	return false
}

// playerTargetHead reports whether a ValidTgts$ filter names a player rather
// than a card. A zone qualifier (Origin$/TgtZone$) describes where an effect
// finds its cards, so it must never be appended to a player target: doing so
// turned "Opponent" into "Opponent@Hand" (Cruelclaw's Heist, Ruthless
// Negotiation, Soul Search, Aggressive Negotiations), which no fixture can
// satisfy. The head is the first comma-separated alternative before its first
// '.' predicate.
func playerTargetHead(filter string) bool {
	head := strings.ToLower(strings.SplitN(strings.Split(filter, ",")[0], ".", 2)[0])
	return head == "player" || head == "opponent"
}
