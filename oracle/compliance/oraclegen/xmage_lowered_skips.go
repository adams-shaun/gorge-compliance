package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// LoweredTargetSkips derives the XMage explicit-skip plan of one cast step of
// a LOWERED item, where no fixture says which of the card's target slots were
// left empty: only gorge's own target decisions do. gorge poses no ask for a
// slot that has no legal candidate, so a cast that names fewer targets than
// the face has slots filled some of them and the rest are the omitted ones
// (Relive the Past with only a land in the graveyard: the artifact and
// enchantment objects are empty, the land object takes the one target).
//
// The posed decisions are matched to slots by order, keeping only the
// assignments in which no decision's offered candidates are ones the slot's
// own filter rules out (filterRejects). The plan is derived only when exactly
// one such assignment exists; an ambiguous cast keeps the driver's own
// handling (nil), as does any shape targetSkips will not vouch for. step is
// the index of the cast step in sc.Steps and ds holds the result of running
// sc.
func LoweredTargetSkips(reg *cards.Registry, f *cards.Face, sc Scenario, ds []rules.OracleDecision, step int) []XTargetSkip {
	if f == nil || step < 0 || step >= len(sc.Steps) {
		return nil
	}
	for _, a := range f.Abilities {
		if a.Kind == "SP" && a.API == "Charm" {
			// The slot list holds only the first mode's chain.
			return nil
		}
	}
	slots := SlotSpecs(f)
	if len(slots) < 2 {
		return nil
	}
	for _, s := range slots {
		if SlotIsStack(s.Filter) {
			// A stack slot shifts the slot indices the plan names.
			return nil
		}
	}
	var posed []rules.OracleDecision
	for _, d := range ds {
		if d.Via == "target" && d.Step == step {
			posed = append(posed, d)
		}
	}
	omitted, ok := omittedSlots(reg, slots, posed)
	if !ok || len(omitted) == 0 {
		return nil
	}
	plan := targetSkips(slots, omitted, sc, ds, map[int]bool{step: true})
	if step >= len(plan) {
		return nil
	}
	return plan[step]
}

// omittedSlots is the slot indices no posed decision filled, when the posed
// decisions fit the slots in exactly one order-preserving way. ok is false
// for no fit, an ambiguous fit, or a cast that posed an ask for every slot
// (nothing is omitted, so there is nothing to derive).
func omittedSlots(reg *cards.Registry, slots []Slot, posed []rules.OracleDecision) ([]int, bool) {
	k, p := len(slots), len(posed)
	if p == 0 || p >= k {
		return nil, false
	}
	var found []int
	count := 0
	assign := make([]int, p)
	var walk func(d, from int)
	walk = func(d, from int) {
		if count > 1 {
			return
		}
		if d == p {
			count++
			found = append([]int(nil), assign...)
			return
		}
		for s := from; s <= k-(p-d); s++ {
			if !slotFits(reg, slots[s], posed[d]) {
				continue
			}
			assign[d] = s
			walk(d+1, s+1)
		}
	}
	walk(0, 0)
	if count != 1 {
		return nil, false
	}
	taken := make(map[int]bool, p)
	for _, s := range found {
		taken[s] = true
	}
	var omitted []int
	for s := 0; s < k; s++ {
		if !taken[s] {
			omitted = append(omitted, s)
		}
	}
	return omitted, true
}

// slotFits reports whether a decision could have been the slot's ask: none of
// the candidates it offered (the picks when it recorded no offer list) is
// ruled out by the slot's filter. A candidate the filter cannot be evaluated
// on counts as fitting, so an unknown never prunes an assignment.
func slotFits(reg *cards.Registry, s Slot, d rules.OracleDecision) bool {
	refs := d.OptionRefs
	if len(refs) == 0 {
		refs = d.PickRefs
	}
	for _, ref := range refs {
		if filterRejects(reg, s.Filter, ref) {
			return false
		}
	}
	return true
}

// loweredTypeWords are the card types a filter's head word names.
var loweredTypeWords = map[string]bool{
	"artifact": true, "battle": true, "creature": true, "enchantment": true, "instant": true,
	"kindred": true, "land": true, "planeswalker": true, "sorcery": true,
}

// offBattlefieldZones are the @zone suffixes whose cards carry only their
// printed characteristics: no continuous effect (Mycosynth Lattice, a crewed
// Vehicle, Animate Artifact) changes a card's types there, so the printed
// types decide whether a filter accepts it.
var offBattlefieldZones = map[string]bool{"graveyard": true, "hand": true, "library": true, "exile": true}

// filterRejects reports whether a Forge ValidTgts filter (with its @zone
// suffix) certainly rules out the card a scenario ref names: every
// alternative's head type is not one of the card's types, or an alternative's
// "non<Word>" qualifier names a type or subtype the card has. It never
// guesses: a ref that names no registry card (a player, a token, an ability),
// a card of several faces (a split card's combined types, a double-faced
// card's current face are not the printed front), a filter with no
// off-battlefield zone (a permanent's current types may differ from the
// printed ones) and any qualifier it does not read are not rejections.
func filterRejects(reg *cards.Registry, filter, ref string) bool {
	name, ok := refCardName(ref)
	if !ok {
		return false
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) != 1 {
		return false
	}
	f := c.Faces[0]
	filter, zone, hasZone := strings.Cut(filter, "@")
	if !hasZone {
		return false
	}
	for _, z := range strings.Split(zone, ",") {
		if !offBattlefieldZones[strings.ToLower(strings.TrimSpace(z))] {
			return false
		}
	}
	for _, alt := range strings.Split(filter, ",") {
		if !altRejects(alt, f) {
			return false
		}
	}
	return true
}

// altRejects is filterRejects for one comma-separated alternative.
func altRejects(alt string, f *cards.Face) bool {
	parts := strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' })
	if len(parts) == 0 {
		return false
	}
	if base := strings.ToLower(strings.TrimSpace(parts[0])); loweredTypeWords[base] && !hasType(f, base) {
		return true
	}
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if len(part) > 3 && strings.EqualFold(part[:3], "non") && hasType(f, part[3:]) {
			return true
		}
	}
	return false
}

// refCardName is the card name a scenario ref ("p0:Forest#7") names; ok is
// false for a player, a token, an ability or anything else that is no card.
func refCardName(ref string) (string, bool) {
	i := strings.IndexByte(ref, ':')
	if i < 2 || ref[0] != 'p' || strings.Trim(ref[1:i], "0123456789") != "" {
		return "", false
	}
	n := ref[i+1:]
	if strings.HasPrefix(n, "token:") || strings.HasPrefix(n, "ability:") {
		return "", false
	}
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n, n != ""
}
