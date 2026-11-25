package oraclegen

import (
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	costvocab "github.com/adams-shaun/gorge/engine/rules/cost"
	"github.com/adams-shaun/gorge/engine/rules/pay"
)

// pipAsk is one flexible cost pip (CR 601.2b) of a cast or activate step, in
// AnnouncePip order (hybrid, then twobrid, then phyrexian, then hybrid-
// phyrexian). asks is whether XMage poses the pip's "Pay 2 life instead of
// ..." chooseUse at all: a recorded life face always asks, and a mana face
// asks only when gorge's recorded decision offered the life face too (its
// option count is wider than the pip's colour faces). XMage's ask is gated on
// PayLifeCost.canPay, so a mana face whose life face gorge's feasibility gate
// dropped (fewer than 2 life, a CantPayLife static) is never posed and must
// not script a boolean. Hybrid and twobrid pips have no life face and never
// ask. life is whether gorge's recorded face is the two-life face (pay_life)
// -- otherwise the pip was paid with mana (pay_<colour>/pay_generic) and
// XMage's boolean answer is no.
//
// Per-pip identity is what the flat yes/no answer queue cannot carry: when a
// step pays an earlier pip with mana and a later one with life, the queue's
// lone yes lands on the earlier (mana-face) ask and the later life ask starves.
type pipAsk struct {
	asks bool
	life bool
}

// xanswersExtra is the scenario context xanswersSetup accepts from the
// callers that can derive it. The tests and the shape-only callers pass none.
type xanswersExtra struct {
	digs map[int]digSource
	pips map[int][]pipAsk
	// casts is the set of the scenario's cast step indices: a forced X
	// announcement on one is still posed by XMage (castAnnouncesX).
	casts map[int]bool
	// activates maps an activation step to the number of X announcements its
	// costs pose. One is the mana {X}; a non-mana variable cost that announces
	// its own X (Ludevic, Necrogenius's ExileXFromYourGraveCost) adds another.
	// A counter-removal cost that reuses the mana X (Talon of Pain) does not.
	activates map[int]int
	// searchZones maps a search_zones decision index to the resolved XMage ask
	// shape (search_zone_source.go). Absent means an unproven effect shape,
	// which stays unscripted.
	searchZones map[int]searchZoneAsk
	// forage maps a forage election's decision index to the graveyard
	// cards its exile arm took (forageExiles).
	forage map[int][]string
	// ownBounce marks the steps that pose an own-bounce hidden pick.
	ownBounce map[int]bool
	// ownGraveyardExile marks the steps that pose a proven own-graveyard
	// exile's mandatory hidden pick (own_graveyard_exile_source.go).
	ownGraveyardExile map[int]bool
	// tapThenUntap marks the steps whose TapOrUntap election is a
	// tap-then-untap face's (tapThenUntapStep).
	tapThenUntap map[int]bool
	// bolster marks the decisions whose counter_pick is a Bolster tie
	// election, which XMage's BolsterEffect asks through chooseTarget
	// (bolster_pick.go).
	bolster map[int]bool
	// craft marks the decisions whose exilecost pick pays a Craft keyword's
	// material cost. XMage's CraftAbility chooses its material through
	// chooseTarget, the TARGET queue, unlike every other exile cost's
	// getTargets().choose on the choice queue (craft_cost_pick.go).
	craft map[int]bool
	// supports maps a Support counter_pick decision index to the target
	// answers XMage asks at the source's announcement, not at resolution
	// (support_pick.go). A present key is owned by that route.
	supports map[int]supportAnswers
	// snaps is the gorge result's checkpoints: the destination of a
	// hand_move/library_placement decision is not a field, so the answer
	// router re-derives a top placement from the after-step library top.
	snaps []rules.OracleSnapshot
	// orderAsks marks the hand_move decisions whose effect XMage implements
	// with an explicit "card ORDER to put … from the TOP" dialog. A self
	// put-back (Brainstorm, Dream Cache) auto-tops without that ask, so its
	// order answers would be left queued for the next dialog.
	orderAsks map[int]bool
	// sources maps a decision step to the cards resolving or acting there
	// (stepSourceNames); see answerRouting.sources.
	sources map[int][]string
	// allPlayerUnless marks every unless-pay decision whose resolving face is
	// the "any player may have <source> deal N damage to them" punisher
	// (unless_payers.go); see answerRouting.allPlayerUnless.
	allPlayerUnless map[int]bool
	// seats is the match's seat count (0 keeps the two-seat game), the payer
	// range an allPlayerUnless election answers.
	seats int
	// chooseUseOrders maps a step to the card-order choices XMage asks after
	// a chooseUse whose yes branch moves the looked-at window to the library
	// bottom (choose_dialog_order.go).
	chooseUseOrders map[int][]XAnswer
}

// isPipDecision reports whether d is one of gorge's ManaAsk pip decisions
// (castasks.go's pipAsk): a one-pick choose whose option kind is a flexible
// pip face (pay_<colour>, pay_generic or pay_life). Hybrid and twobrid faces
// share these kinds with phyrexian ones, so the kind alone cannot tell a pip
// XMage asks from one it does not -- the derived cost decides that.
func isPipDecision(d rules.OracleDecision) bool {
	if d.Kind != "choose_n" || len(d.Picks) != 1 || len(d.PickKinds) != 1 {
		return false
	}
	switch pickKind(d, 0) {
	case "pay_life", "pay_generic":
		return true
	case "pay_W", "pay_U", "pay_B", "pay_R", "pay_G", "pay_C":
		return true
	}
	return false
}

// phyrexianPipPlan derives, from the corpus cost of each cast/activate step,
// which of the step's recorded pip decisions XMage poses and what face each
// was paid with. It returns nil when no step has a life-face pip decision
// (every other cost shape keeps the historical routing) or when a step's cost
// cannot be re-derived: the caller then keeps the historical single-yes
// behaviour, whose one-pip-every-pip shape is exact there.
//
// The derivation is needed because gorge's OracleDecision records only the
// picked face, not the offered menu: a mana-face pip (one XMage also asks when
// the life face was affordable) is indistinguishable from a mana-only
// feasibility pick without re-deriving the cost's pip list.
func phyrexianPipPlan(reg *cards.Registry, res rules.OracleResult, sc Scenario) map[int][]pipAsk {
	if reg == nil {
		return nil
	}
	// The pip decisions of each step, in recorded order, and whether any is
	// the life face. Only a step with a life-face pip needs the plan: with
	// every pip paid with mana, gorge's decisions and XMage's asks still
	// interleave the same (each pip ask is answered no, or was never posed).
	pips := map[int][]rules.OracleDecision{}
	anyLife := false
	for _, d := range res.Decisions {
		if d.Step < 0 || d.Step >= len(sc.Steps) || !isPipDecision(d) {
			continue
		}
		pips[d.Step] = append(pips[d.Step], d)
		if pickKind(d, 0) == "pay_life" {
			anyLife = true
		}
	}
	if !anyLife {
		return nil
	}
	plan := map[int][]pipAsk{}
	for step, ds := range pips {
		if p, ok := stepPipPlan(reg, sc, step, ds); ok {
			plan[step] = p
		}
	}
	return plan
}

// stepPipPlan is phyrexianPipPlan for one step: it re-derives the step's cost
// from the corpus, maps the recorded pip decisions onto the AnnouncePip pips
// in order, and false-falls-back when the two do not line up. The count is
// the guard that keeps a mis-derived cost (an unknown cost text, an
// X-announced pip or an ability cost the corpus stores only in part) from
// mapping a plan onto the wrong pips.
func stepPipPlan(reg *cards.Registry, sc Scenario, step int, ds []rules.OracleDecision) ([]pipAsk, bool) {
	c, ok := stepPipCost(reg, sc, step)
	if !ok {
		return nil, false
	}
	if c.AnnPipCount() != len(ds) {
		// K'rrik's PayLifeInsteadOf grant turns the plain {B} pips into
		// announceable {B/P} pips appended after the printed phyrexian ones
		// (cast_payment.go), exactly the order XMage's
		// handlePhyrexianLikeEffects asks the granted pips in.
		c = pay.AnnounceGrantedBlackPips(c)
		if c.AnnPipCount() != len(ds) {
			return nil, false
		}
	}
	plan := make([]pipAsk, len(ds))
	for i, d := range ds {
		alts := pay.AnnouncePip(c, i)
		life := pickKind(d, 0) == "pay_life"
		// The offered menu, not the printed pip, decides whether XMage asks:
		// ManaAsk drops an infeasible face and deduplicates a repeated colour,
		// so d.Options is the menu's size. The pip's colour faces are every
		// alternative that is not the life face; a mana pick was offered the
		// life face exactly when the menu is wider than those. A life face
		// always asks (gorge recorded it as the chosen payment, so it was
		// offered), including the one-option shape its gate trims to.
		colours := map[byte]struct{}{}
		hasLife := false
		for _, alt := range alts {
			switch {
			case alt.Life > 0:
				hasLife = true
			case alt.Color != 0:
				colours[alt.Color] = struct{}{}
			}
		}
		plan[i] = pipAsk{asks: life || hasLife && d.Options > len(colours), life: life}
	}
	return plan, true
}

// stepPipCost re-derives the pip-bearing cost a step announced, from the
// corpus cost text of its card's face (cast) or ability (activate). Only a
// cast step in the default cast mode and an activate step with a known
// ability index qualify; anything else keeps the historical routing.
func stepPipCost(reg *cards.Registry, sc Scenario, step int) (pay.Cost, bool) {
	st := sc.Steps[step]
	if st.Op != "cast" && st.Op != "activate" {
		return pay.Cost{}, false
	}
	if st.Op == "cast" && st.CastMode != "" {
		// A modal cast's cost is not the printed one.
		return pay.Cost{}, false
	}
	c, ok := reg.Lookup(oraclediffRefName(st.Card))
	if !ok || len(c.Faces) == 0 {
		return pay.Cost{}, false
	}
	// The stack ref names the specific face of a multi-face card
	// (stackSourceRef rewrites it), so resolve that face like digFaces does;
	// a whole-card ref keeps the front face.
	var face *cards.Face
	name := cards.NormalizeName(oraclediffRefName(st.Card))
	for _, f := range c.Faces {
		if f != nil && cards.NormalizeName(f.Name) == name {
			face = f
			break
		}
	}
	if face == nil {
		face = c.Faces[0]
	}
	if face == nil {
		// The corpus can carry a nil face (a split or meld half without one,
		// the same shape adopt/tickets.go guards): there is no cost to read.
		return pay.Cost{}, false
	}
	var raw string
	if st.Op == "cast" {
		raw = face.ManaCost
	} else {
		if st.AbilityIndex == nil || *st.AbilityIndex < 0 || *st.AbilityIndex >= len(face.Abilities) {
			return pay.Cost{}, false
		}
		raw = face.Abilities[*st.AbilityIndex].Params["Cost"]
	}
	if raw == "" {
		return pay.Cost{}, false
	}
	return costvocab.ParseCost(raw), true
}
