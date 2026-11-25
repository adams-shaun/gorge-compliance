package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// CastDivisions is castDivisions for callers outside the package (the Forge
// export, whose driver contract reads the "damage_split" decision).
func CastDivisions(reg *cards.Registry, sc Scenario, res rules.OracleResult) []rules.OracleDecision {
	return castDivisions(reg, sc, res)
}

// castDivisions is castDivisionsFor for the Forge contract (xmage false).
func castDivisions(reg *cards.Registry, sc Scenario, res rules.OracleResult) []rules.OracleDecision {
	return castDivisionsFor(reg, sc, res, false)
}

// castDivisionsFor restores the "damage_split" decision a divided spell's
// shares travel in. Since gorge cr-g04 (CR 601.2d) a spell announces its
// DividedAsYouChoose$ division while it is cast: gorge asks only when the
// division is free (two or more targets and more points than targets), as a
// cast-time choose whose picks are "divide" options, one per target; a lone
// target, or as many points as targets, is filled without an ask. Before it,
// every divided DealDamage spell posed a resolution-time "damage_split"
// KChoose, which both consumers of the decisions read: XAnswers scripts it as
// XMage's "<ref>^X=<share>" TargetAmount answers, and the Forge driver
// contract (cmd/oraclediff forge.go) sums it. The spell's root target ask
// stays unmarked (Divided 0), so the decisions alone cannot tell a forced
// division from an undivided spell; the card's own ability does.
//
// For each cast step whose card's spell ability divides (DealDamage always;
// PutCounter and PreventDamage only when gorge posed the division ask, as
// those never posed a damage_split before -- except the XMage answer path
// below, which always marks them), the cast-time "divide" ask is
// removed and a "damage_split" decision carrying the announced shares is
// inserted: in the Forge view at the cast step, right after the root target
// ask (the Forge driver allocates the division while targeting and takes it by
// exact step); in the XMage view at the head of the step the spell resolves
// in, where gorge used to pose it. Every other decision list is returned
// unchanged.
//
// xmage is the XMage script's view. A cast whose targets travel in the step's
// answers (a lowered xproof item) scripts that root target ask itself, at the
// cast step, so XMage's TargetAmount reads the shares there: the copied root
// ask is marked Divided with the total, and the division ask (if gorge posed
// one) stays beside it, so dividedTargetAnswers emits one "<ref>^X=<share>" per
// target from the two; no split is inserted. The Forge
// contract keeps the root ask unmarked (the driver sums the split), so the
// Forge callers pass false. The Forge view inserts no split for a lone target
// whose division gorge filled without an ask: the driver consumes a split only
// when Forge poses a division, and Forge never asks for a one-target
// allocation, so restoring the forced lone-target division would leave it
// unconsumed.
func castDivisionsFor(reg *cards.Registry, sc Scenario, res rules.OracleResult, xmage bool) []rules.OracleDecision {
	ds := castSpellDivisions(reg, sc, res, xmage)
	if xmage {
		// A divided TRIGGER's root ask is the XMage view's alone (see
		// markPlacedTriggerDivisions); the Forge contract stays untouched.
		ds = markPlacedTriggerDivisions(reg, sc, res, ds)
	}
	return ds
}

// castSpellDivisions is the cast-step half of castDivisionsFor.
func castSpellDivisions(reg *cards.Registry, sc Scenario, res rules.OracleResult, xmage bool) []rules.OracleDecision {
	ds := res.Decisions
	if reg == nil {
		return ds
	}
	var out []rules.OracleDecision
	for k, st := range sc.Steps {
		if st.Op != "cast" || st.Card == "" {
			continue
		}
		api, total, ok := spellDivision(reg, st)
		if !ok {
			continue
		}
		src := ds
		if out != nil {
			src = out
		}
		i := castRootTarget(src, k)
		if i < 0 {
			continue
		}
		t := src[i]
		n := len(t.PickRefs)
		j := divideAsk(src, i)
		if !xmage && n == 1 && j < 0 {
			// A lone target takes the whole total and has one legal
			// division (CR 601.2d), so gorge asks nothing. Forge does
			// not pose a division for one target either, so a
			// synthesized Forge split would be a decision its driver
			// can never consume. The XMage view still needs the root
			// ask's share (marked below).
			continue
		}
		shares := make([]int, n)
		for x := range shares {
			shares[x] = 1
		}
		switch {
		case j >= 0:
			for _, p := range src[j].PickIdx {
				if p >= 0 && p < n {
					shares[p]++
				}
			}
		case xmage && t.Via == "answer" && total > 0 && total >= n:
			// XMage's TargetAmount always scripts a divided target ask as
			// "<ref>^X=<share>", including a lone target whose division
			// gorge filled without posing an ask (CR 601.2d). Mark the
			// root ask for every divided API, before the DealDamage-only
			// arm below, which is the Forge contract: Forge never asks
			// for a one-target allocation, so the Forge view keeps
			// skipping it.
			shares[0] += total - n
		case api != "DealDamage":
			continue
		case total > 0 && total >= n:
			shares[0] += total - n
		case n > 1:
			// No ask and no literal total: the only forced division of
			// n > 1 targets is one point each (CR 601.2d).
		default:
			continue
		}
		// The Forge driver allocates the division while the spell is
		// cast (CR 601.2d), at the cast step, and takes it by exact step.
		// The XMage view keeps the resolve step, where gorge used to pose
		// the split.
		splitStep := k
		if xmage {
			splitStep = resolveStepOf(sc, res.Snapshots, k)
		}
		split := rules.OracleDecision{Step: splitStep, Seat: t.Seat,
			Kind: "choose_n", GorgeKind: "choose", Resume: "damage_split", Via: "cast division",
			Options: n, Picks: []string{}, PickIdx: []int{}, PickRefs: []string{}}
		for x, s := range shares {
			label := oraclediffRefName(t.PickRefs[x])
			if x < len(t.Picks) && t.Picks[x] != "" && isSeat(t.PickRefs[x]) {
				label = t.Picks[x]
			}
			for ; s > 0; s-- {
				split.Picks = append(split.Picks, label)
				split.PickIdx = append(split.PickIdx, x)
				split.PickRefs = append(split.PickRefs, t.PickRefs[x])
			}
		}
		split.Min, split.Max = len(split.PickIdx), len(split.PickIdx)
		if xmage && t.Via == "answer" {
			// The root ask carries the shares itself (see above): mark it
			// divided and leave the division ask where it is, which
			// dividedTargetAnswers pairs with it; no split is inserted.
			marked := append([]rules.OracleDecision(nil), src...)
			marked[i].Divided = len(split.PickIdx)
			out = marked
			continue
		}
		next := make([]rules.OracleDecision, 0, len(src)+1)
		inserted := false
		for x, d := range src {
			if x == j {
				continue
			}
			if !xmage && x == i {
				// Forge: right after the root target ask, where the
				// division ask sat.
				next = append(next, d, split)
				inserted = true
				continue
			}
			if !inserted && d.Step >= split.Step && x > i {
				next = append(next, split)
				inserted = true
			}
			next = append(next, d)
		}
		if !inserted {
			next = append(next, split)
		}
		out = next
	}
	if out == nil {
		return ds
	}
	return out
}

// spellDivision reports the API and literal total (0 when the total is not a
// literal) of the targeted DividedAsYouChoose$ spell ability of the face the
// cast step casts, the shapes gorge announces at cast (engine/rules
// cast_division.go castDivisionAPI). A Charm's modes are sub-abilities, never
// the root, and keep the resolution-time split. Only the cast face is read: a
// card's other face keeps its own spell ability, which this cast did not
// announce (an Adventure's PutCounter is never the front creature's).
func spellDivision(reg *cards.Registry, st Step) (string, int, bool) {
	c, ok := reg.Lookup(oraclediffRefName(st.Card))
	if !ok {
		return "", 0, false
	}
	f := castStepFace(c, st)
	if f == nil {
		return "", 0, false
	}
	for _, sa := range f.Abilities {
		if sa == nil || sa.Kind != "SP" {
			continue
		}
		switch sa.API {
		case "DealDamage", "PutCounter", "PreventDamage":
		default:
			continue
		}
		if strings.TrimSpace(sa.Params["DividedAsYouChoose"]) == "" || !effects.TargetsOf(sa).Targeted() {
			continue
		}
		return sa.API, effects.DividedTotal(sa), true
	}
	return "", 0, false
}

// castStepFace is the face cast step st casts. An alternate-face cast mode
// flips the card to its second face before the cast (engine/rules cast_begin.go
// and cast_window.go: the Adventure/Omen spell half, a Room's other door, an
// aftermath or split half, a transformed defeated battle, a disturb or
// converted back face), while adventure_recast returns the card to its main
// face and every other mode casts the main face. A step's ref may also name an
// alternate face, which the runner binds to that face's offer
// (engine/rules/oracle_run_faces.go); that face is selected when the card
// carries it (a prepared copy's spell face is named this way).
func castStepFace(c *cards.Card, st Step) *cards.Face {
	if c == nil || len(c.Faces) == 0 {
		return nil
	}
	switch st.CastMode {
	case "adventure_alt", "omen_alt", "room_alt", "split_alt", "aftermath", "defeat_cast", "disturb", "converted":
		if len(c.Faces) > 1 {
			return c.Faces[1]
		}
		return c.Faces[0]
	case "adventure_recast":
		return c.Faces[0]
	}
	name := cards.NormalizeName(oraclediffRefName(st.Card))
	for _, f := range c.Faces {
		if f != nil && cards.NormalizeName(f.Name) == name {
			return f
		}
	}
	return c.Faces[0]
}

// castRootTarget is the index of cast step k's own root target ask: the
// first target decision of that step, answered from the step's target list
// (via "target", the generator's shape) or from its answers (via "answer",
// a lowered xproof item whose cast carries its targets as an answer). Only
// that first ask is the cast's: a later trigger's target in the same step
// follows it and is never taken.
func castRootTarget(ds []rules.OracleDecision, k int) int {
	for i, d := range ds {
		if d.Step == k && d.Kind == "target" && (d.Via == "target" || d.Via == "answer") && d.Resume == "" && d.Divided == 0 && len(d.PickRefs) > 0 {
			return i
		}
	}
	return -1
}

// divideAsk is the index of the cast-time division ask that follows target
// ask i (same step and seat, every option a "divide" pick), or -1.
func divideAsk(ds []rules.OracleDecision, i int) int {
	for j := i + 1; j < len(ds) && ds[j].Step == ds[i].Step; j++ {
		d := ds[j]
		if d.Seat != ds[i].Seat || d.Kind != "choose_n" || len(d.PickKinds) == 0 {
			continue
		}
		all := true
		for _, k := range d.PickKinds {
			if k != "divide" {
				all = false
				break
			}
		}
		if all {
			return j
		}
	}
	return -1
}

// resolveStepOf is the first step after cast step k at whose checkpoint the
// spell cast at k is no longer on the stack (snapshot k'+1 follows step k'),
// or k+1 when the snapshots do not say.
func resolveStepOf(sc Scenario, snaps []rules.OracleSnapshot, k int) int {
	name := oraclediffRefName(sc.Steps[k].Card)
	for s := k + 1; s < len(sc.Steps) && s+1 < len(snaps); s++ {
		onStack := false
		for _, o := range snaps[s+1].Stack {
			if o.Kind == "spell" && oraclediffRefName(o.Source) == name {
				onStack = true
				break
			}
		}
		if !onStack {
			return s
		}
	}
	return k + 1
}
