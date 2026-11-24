package oraclegen

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The "any player may have <source> deal N damage to them" punisher is one
// unless-pay rider that XMage implements as a chooseUse offer to EVERY player
// in range, without stopping at the first acceptance (Browbeat's
// BrowbeatDrawEffect, Barbarian Bully, Book Burning, Breaking Point). Gorge
// asks its UnlessPayer$ Player payers in seat order and stops once one pays
// (engine/effects/unless.go's unlessProceed), so a payer XMage still asks
// after the first acceptance has no recorded decision. Scripting one boolean
// per recorded decision therefore leaves that later ask to TestPlayer's AI:
// the find measured XMage's second player accepting the damage (p1.life 15)
// where gorge's stopped at the first payer (p1.life 20), plus the strict
// "Missing CHOICE def ... PlayerA".
//
// The gate is the rider's IR, not a card name: UnlessPayer$ Player with a
// DamageYou<N> unless cost. The corpus has exactly four such carriers
// (Barbarian Bully, Book Burning, Breaking Point, Browbeat), and every one's
// XMage class loops the range without a break, so a new sibling of the shape
// is covered by the same gate. A Player-payer rider with another cost is NOT
// expanded: Rhystic Scrying's {2} loop breaks on the first payer, so its
// recorded decisions already match XMage's asks (and Minds Aglow/Mana Charged
// Dragon ask a different question through the same player walk).

// unlessDamageAllPlayers reports whether f carries the rider above: any
// printed or SVar ability with UnlessPayer$ Player and a DamageYou<N> unless
// cost.
func unlessDamageAllPlayers(f *cards.Face) bool {
	if f == nil {
		return false
	}
	match := func(params map[string]string) bool {
		if !strings.EqualFold(strings.TrimSpace(params["UnlessPayer"]), "Player") {
			return false
		}
		_, ok := effects.ParseDamageUnlessCost(strings.TrimSpace(params["UnlessCost"]))
		return ok
	}
	for _, sa := range f.Abilities {
		if sa != nil && match(sa.Params) {
			return true
		}
	}
	for _, k := range svarKeys(f) {
		if match(svarParams(f.SVars[k])) {
			return true
		}
	}
	return false
}

// unlessAllPlayerDecisions marks the index of every unless-pay decision whose
// resolving source carries the all-player damage rider. The source is read
// from the decision's own option refs (the engine records the resolving
// object on each option), falling back to the step's stack/cast sources, so
// the gate never guesses from the question's wording or the card name.
func unlessAllPlayerDecisions(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, ds []rules.OracleDecision) map[int]bool {
	out := map[int]bool{}
	if reg == nil {
		return out
	}
	for i, d := range ds {
		if d.Resume != "unless_pay" && d.Resume != "unless_decline" {
			continue
		}
		if unlessAllPlayerAt(reg, sc, snaps, d) {
			out[i] = true
		}
	}
	return out
}

// unlessAllPlayerAt resolves a decision's source faces and reports whether any
// of them carries the all-player damage rider.
func unlessAllPlayerAt(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision) bool {
	for _, name := range decisionSourceNames(sc, snaps, d) {
		card, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		for _, f := range card.Faces {
			if unlessDamageAllPlayers(f) {
				return true
			}
		}
	}
	return false
}

// decisionSourceNames lists the cards a decision can have resolved from: the
// resolving object's refs on its options/picks, then the step's stack and
// action sources. Lower-cased and deduplicated, the spelling the face-keyed
// tables use.
func decisionSourceNames(sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision) []string {
	var out []string
	seen := map[string]bool{}
	add := func(ref string) {
		name := strings.ToLower(strings.TrimSpace(oraclediffRefName(ref)))
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, ref := range d.OptionRefs {
		add(ref)
	}
	for _, ref := range d.PickRefs {
		add(ref)
	}
	for _, name := range stepSourceNames(sc, snaps, d.Step) {
		add(name)
	}
	return out
}

// allPlayerUnlessAnswers is the answer stream for an all-player damage
// punisher's unless election: one boolean per payer seat in seat order,
// emitted once from the group's first decision. A seat gorge recorded answers
// its own polarity; a seat it never asked (an earlier payer accepted, so
// gorge stopped) answers no, because XMage offers the choice to every payer
// and nobody after the accepting one accepted. nil for a decision outside the
// group or after the group's first index, so the callers cannot double-script
// the stream.
func (r *answerRouting) allPlayerUnlessAnswers(i int) []XAnswer {
	if !r.allPlayerUnless[i] {
		return nil
	}
	first := i
	bySeat := map[int]string{}
	for j, d := range r.ds {
		if !r.allPlayerUnless[j] || d.Step != r.ds[i].Step || !sameUnlessSource(r.ds[i], d) {
			continue
		}
		if j < first {
			first = j
		}
		bySeat[d.Seat] = unlessPolarity(d)
	}
	if first != i {
		return nil
	}
	seats := r.seats
	if seats < 1 {
		seats = 2
	}
	out := make([]XAnswer, 0, seats)
	for seat := 0; seat < seats; seat++ {
		answer, ok := bySeat[seat]
		if !ok {
			answer = "no"
		}
		out = append(out, XAnswer{seat, "choice", answer})
	}
	return out
}

// sameUnlessSource reports whether two unless-pay decisions resolved from the
// same source object(s). The option refs carry the resolving object; two
// different punishers resolving in one step must not share a payer stream.
// An empty ref list on either side is treated as the same source, keeping the
// expansion usable for a transcript that did not record identities.
func sameUnlessSource(a, b rules.OracleDecision) bool {
	as, bs := refSet(a.OptionRefs), refSet(b.OptionRefs)
	if len(as) == 0 || len(bs) == 0 || len(as) != len(bs) {
		return len(as) == 0 || len(bs) == 0
	}
	for ref := range as {
		if !bs[ref] {
			return false
		}
	}
	return true
}

// refSet is a duplicate-insensitive set of object refs.
func refSet(refs []string) map[string]bool {
	m := make(map[string]bool, len(refs))
	for _, ref := range refs {
		m[ref] = true
	}
	return m
}

// svarKeys returns the face's SVar names in sorted order, so walking them can
// never depend on map iteration order.
func svarKeys(f *cards.Face) []string {
	keys := make([]string, 0, len(f.SVars))
	for k := range f.SVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
