package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Payment-sensitive mana sources.
//
// A cast's `mana` is seeded into the pool, so nothing taps. For most sources
// that does not matter, but some mana abilities do more than add mana, and the
// test asserts the extra:
//
//   - a counter cost: Wall of Roots' "Put a -0/-1 counter on it: Add {G}"
//     moves its power/toughness;
//   - a rider on the mana (the Forge `Adds*` parameters: Hall of the Bandit
//     Lord's haste, Boseiju's "can't be countered"), which only the mana that
//     the source's own ability made carries.
//
// XMage pays the cast by activating those abilities. The translator states the
// same payment: it emits a gorge `activate` step for each such source the cast
// needs, and seeds only the rest of the cost. A source is "needed" only when
// the other sources cannot pay for the cast alone, because only then does the
// choice of payer not depend on XMage's auto-payment order.
var reManaCounterCost = regexp.MustCompile(`\b(?:AddCounter|PutCounter|SubCounter|RemoveCounter)<`)

// reFlickerText matches a spell that exiles a permanent and returns it, so the
// permanent is a new object that can activate its mana ability again.
var reFlickerText = regexp.MustCompile(`(?is)\bexile\b[^.]*?(?:,\s*then return\b|\.\s*(?:then )?return (?:it|that card|the exiled card)\b)`)

// sensSource is one payment-sensitive mana source a seat controls: every
// untapped same-named permanent set up on its battlefield.
type sensSource struct {
	name   string
	refs   []string
	index  int  // the mana ability's index in the face's abilities
	colour byte // its single fixed colour; 0 when the ability asks or varies
	limit  bool // ActivationLimit: once per turn
	tap    bool // {T} cost
}

// sensitiveManaSA returns the first mana ability of f whose activation does
// more than add mana, or nil.
func sensitiveManaSA(f *cards.Face) (int, *cards.SA) {
	for i, sa := range f.Abilities {
		if sa == nil || sa.API != "Mana" {
			continue
		}
		if reManaCounterCost.MatchString(sa.ParamStr(cards.PKCost)) {
			return i, sa
		}
		for k := range sa.Params {
			if strings.HasPrefix(k, "Adds") {
				return i, sa
			}
		}
	}
	return -1, nil
}

// fixedColour is the one colour sa always produces, or 0.
func fixedColour(sa *cards.SA) byte {
	prod := strings.Fields(sa.Params["Produced"])
	if len(prod) != 1 || len(prod[0]) != 1 || !strings.Contains("WUBRGC", prod[0]) {
		return 0
	}
	if a := sa.Params["Amount"]; a != "" && a != "1" {
		return 0
	}
	return prod[0][0]
}

// sensitiveSources lists the payment-sensitive sources of seat p that are
// untapped on the setup battlefield, in setup order.
func (t *translator) sensitiveSources(p int) []*sensSource {
	var out []*sensSource
	tapped := map[string]bool{}
	for _, ac := range t.setup[p] {
		if ac.zone == "battlefield" && ac.tapped {
			tapped[ac.name] = true
		}
	}
	seen := map[string]bool{}
	for _, ac := range t.setup[p] {
		if ac.zone != "battlefield" || ac.tapped || seen[ac.name] || tapped[ac.name] {
			continue
		}
		seen[ac.name] = true
		nm, r := xmageName(ac.name)
		if r != nil {
			continue
		}
		c, ok := t.lookup(nm)
		if !ok {
			continue
		}
		for _, f := range c.Faces {
			if f == nil || !sameName(f.Name, nm) {
				continue
			}
			i, sa := sensitiveManaSA(f)
			if sa == nil {
				continue
			}
			ss := &sensSource{name: ac.name, index: i, colour: fixedColour(sa),
				limit: sa.HasParam(cards.PKActivationLimit), tap: hasTapCost(sa.ParamStr(cards.PKCost))}
			for _, pl := range t.places[p] {
				if pl.zone == "battlefield" && sameName(pl.name, ac.name) {
					ss.refs = append(ss.refs, refStr(p, pl.name, pl.ord))
				}
			}
			out = append(out, ss)
			break
		}
	}
	return out
}

// sourceFree reports whether the source permanent ref can activate its mana
// ability again at turn: a {T} cost needs an untap of its controller since the
// last use, an activation limit a later turn, and a flicker since resets both.
func (em *emitter) sourceFree(ref string, seat, turn int, ss *sensSource) bool {
	k, used := em.sensUsed[ref]
	if !used {
		return true
	}
	if k < 0 {
		return true // flickered since the last use
	}
	if ss.limit && k >= turn {
		return false
	}
	if ss.tap {
		for u := k + 1; u <= turn; u++ {
			if em.t.active(u) == seat {
				return true
			}
		}
		return false
	}
	return true
}

// noteFlicker makes every source ref a flicker spell targeted a new object.
func (em *emitter) noteFlicker(card string, targets []string) {
	c, ok := em.t.lookup(card)
	if !ok {
		return
	}
	for _, f := range c.Faces {
		if f != nil && reFlickerText.MatchString(f.Oracle) {
			for _, tg := range targets {
				if _, used := em.sensUsed[tg]; used {
					em.sensUsed[tg] = -1
				}
			}
			return
		}
	}
}

// castManaPaying is castMana for a spell some payment-sensitive source of the
// seat could pay for. It returns the mana to seed plus the activation steps
// that pay the rest, or the plain castMana result and no steps when the
// payer is not determined by the cost: then the source is recorded as paid
// from the pool, which keeps the old shape refusals armed for it.
func (em *emitter) castManaPaying(seat int, name string, x int) (string, []*step, *refusal) {
	plain := func(srcs []*sensSource) (string, []*step, *refusal) {
		for _, ss := range srcs {
			em.t.markSourceUnpaid(ss.name)
		}
		m, r := em.castMana(seat, name, x)
		if len(srcs) > 0 {
			em.noteSpent(seat, len(m))
		}
		return m, nil, r
	}
	srcs := append(em.t.sensitiveSources(seat), em.cloned[seat]...)
	if len(srcs) == 0 {
		return plain(nil)
	}
	c, ok := em.t.lookup(name)
	if !ok || len(c.Faces) == 0 {
		return plain(srcs)
	}
	cost := cardManaCost(c, name)
	slots, r := costSlots(cost, x)
	if r != nil {
		return "", nil, r
	}
	skip := map[string]bool{}
	for _, ss := range srcs {
		if ss.colour == 0 {
			return plain(srcs) // an asked or varying colour: not stated here
		}
		skip[ss.name] = true
	}
	ms := em.t.manaSourcesSkipping(seat, em.entered[seat], skip)
	if ms.loose || ms.spent != "" || em.t.castsForAlternativeCost(c) {
		return plain(srcs)
	}
	// Earlier casts since the seat's last untap already spent the oldest
	// of the other sources: the pool-seeded mana tapped nothing, so count it.
	if n := em.spentSinceUntap(seat); n > 0 {
		ms.srcs = ms.srcs[min(n, len(ms.srcs)):]
	}
	covered, r := manaFromCostSources(cost, x, ms.srcs, false)
	if r != nil || len(slots) == len(covered) {
		return plain(srcs)
	}
	deficit := len(slots) - len(covered)
	type payer struct {
		ss  *sensSource
		ref string
	}
	var pay []payer
	for _, ss := range srcs {
		for _, ref := range ss.refs {
			if len(pay) < deficit && em.sourceFree(ref, seat, em.turn, ss) {
				pay = append(pay, payer{ss, ref})
			}
		}
	}
	if len(pay) < deficit {
		return plain(srcs)
	}
	rest := slots
	for _, p := range pay {
		var ok bool
		if rest, ok = dropSlot(rest, p.ss.colour); !ok {
			return plain(srcs)
		}
	}
	mana, r := manaFromCostSources(slotsCost(rest), 0, ms.srcs, false)
	if r != nil || len(mana) != len(rest) {
		return plain(srcs)
	}
	em.noteSpent(seat, len(mana))
	var steps []*step
	paid := map[string]bool{}
	for _, p := range pay {
		idx := p.ss.index
		steps = append(steps, &step{Op: "activate", Seat: seat, Card: p.ref, AbilityIndex: &idx, payment: true})
		paid[p.ss.name] = true
	}
	for _, ss := range srcs {
		if paid[ss.name] {
			em.t.markSourcePaid(ss.name)
		} else {
			em.t.markSourceUnpaid(ss.name)
		}
	}
	return mana, steps, nil
}

// commitPayment emits the activation steps ahead of the cast s, and records
// the turn each source was used on.
func (em *emitter) commitPayment(seat int, steps []*step) {
	for _, st := range steps {
		em.add(st)
		if em.sensUsed == nil {
			em.sensUsed = map[string]int{}
		}
		em.sensUsed[st.Card] = em.turn
	}
}

func (t *translator) markSourcePaid(name string) {
	if t.sourcePaid == nil {
		t.sourcePaid = map[string]bool{}
	}
	t.sourcePaid[name] = true
}

func (t *translator) markSourceUnpaid(name string) {
	if t.sourceUnpaid == nil {
		t.sourceUnpaid = map[string]bool{}
	}
	t.sourceUnpaid[name] = true
}

// sourceStatedPaid reports whether every cast paid the named source by
// activating it, so a rider or counter assert on it is played, not refused.
func (t *translator) sourceStatedPaid(name string) bool {
	return t.sourcePaid[name] && !t.sourceUnpaid[name]
}

// dropSlot removes one slot a mana of colour pays: a fixed slot of that
// colour, else a generic one, else a hybrid slot that includes it.
func dropSlot(slots []costSlot, colour byte) ([]costSlot, bool) {
	pick := -1
	for i, sl := range slots {
		if sl.fixed == colour && colour != 0 {
			pick = i
			break
		}
	}
	for i, sl := range slots {
		if pick < 0 && sl.fixed == 0 && sl.hybrid == "" {
			pick = i
		}
	}
	for i, sl := range slots {
		if pick < 0 && sl.hybrid != "" && strings.IndexByte(sl.hybrid, colour) >= 0 {
			pick = i
		}
	}
	if pick < 0 {
		return nil, false
	}
	out := append([]costSlot(nil), slots[:pick]...)
	return append(out, slots[pick+1:]...), true
}

// slotsCost renders slots back to a cost string manaFromCostSources reads.
func slotsCost(slots []costSlot) string {
	var toks []string
	generic := 0
	for _, sl := range slots {
		switch {
		case sl.fixed != 0:
			toks = append(toks, string(sl.fixed))
		case sl.hybrid != "":
			toks = append(toks, strings.Join(strings.Split(sl.hybrid, ""), "/"))
		default:
			generic++
		}
	}
	if generic > 0 {
		toks = append([]string{strconv.Itoa(generic)}, toks...)
	}
	return strings.Join(toks, " ")
}

// noteClonedSource records a land that entered as a copy of a
// payment-sensitive source (Vesuva copying Hall of the Bandit Lord): it has
// that source's mana ability, so a later cast may be paid by activating it.
// The copied land is named by one of the play's choices.
func (em *emitter) noteClonedSource(a *action, ref string) {
	for _, ch := range a.choices {
		name, ok := strings.CutPrefix(ch.pick, "name:")
		if ch.target || !ok {
			continue
		}
		c, ok := em.t.lookup(name)
		if !ok {
			continue
		}
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			i, sa := sensitiveManaSA(f)
			if sa == nil {
				continue
			}
			em.cloned[a.seat] = append(em.cloned[a.seat], &sensSource{name: refName(ref), refs: []string{ref}, index: i,
				colour: fixedColour(sa), limit: sa.HasParam(cards.PKActivationLimit), tap: hasTapCost(sa.ParamStr(cards.PKCost))})
			return
		}
	}
}

// untapEpoch is the latest turn up to turn on which seat untapped.
func untapEpoch(seat, turn, seats int) int {
	for u := turn; u > 1; u-- {
		if activeSeat(u, seats) == seat {
			return u
		}
	}
	return 1
}

// noteSpent records n mana of the seat's other sources seeded by a cast.
func (em *emitter) noteSpent(seat, n int) {
	ep := untapEpoch(seat, em.turn, em.t.seats())
	if em.spent[seat].epoch != ep {
		em.spent[seat] = spentMana{epoch: ep}
	}
	em.spent[seat].n += n
}

// spentSinceUntap is how many of the seat's other sources earlier casts
// seeded since its last untap.
func (em *emitter) spentSinceUntap(seat int) int {
	if em.spent[seat].epoch != untapEpoch(seat, em.turn, em.t.seats()) {
		return 0
	}
	return em.spent[seat].n
}

type spentMana struct{ epoch, n int }

// playsAllAtOnce reports whether the card's spell lets its controller cast
// every exiled card in one go (Villainous Wealth's "DB$ Play | Amount$ All").
// Gorge poses that as ONE modes ask whose pick lists all the cards cast; XMage
// asks a pick and a yes/no per card, so with two or more casts the answers do
// not map one to one.
func (t *translator) playsAllAtOnce(name string) bool {
	c, ok := t.lookup(name)
	if !ok {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if sa != nil && sa.API == "Play" && sa.Params["Amount"] == "All" {
				return true
			}
		}
		for _, sv := range f.SVars {
			if strings.Contains(sv, "DB$ Play") && reAmountAll.MatchString(sv) {
				return true
			}
		}
	}
	return false
}

var reAmountAll = regexp.MustCompile(`Amount\$ All\b`)
