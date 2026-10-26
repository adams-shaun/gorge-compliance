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

// castDivisions restores the "damage_split" decision a divided spell's
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
// those never posed a damage_split before), the cast-time "divide" ask is
// removed and a "damage_split" decision carrying the announced shares is
// inserted at the head of the step the spell resolves in, where gorge used to
// pose it. Every other decision list is returned unchanged.
func castDivisions(reg *cards.Registry, sc Scenario, res rules.OracleResult) []rules.OracleDecision {
	ds := res.Decisions
	if reg == nil {
		return ds
	}
	var out []rules.OracleDecision
	for k, st := range sc.Steps {
		if st.Op != "cast" || st.Card == "" {
			continue
		}
		api, total, ok := spellDivision(reg, oraclediffRefName(st.Card))
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
		split := rules.OracleDecision{Step: resolveStepOf(sc, res.Snapshots, k), Seat: t.Seat,
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
		next := make([]rules.OracleDecision, 0, len(src)+1)
		inserted := false
		for x, d := range src {
			if x == j {
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
// literal) of the targeted DividedAsYouChoose$ spell ability of the named
// card, the shapes gorge announces at cast (engine/rules cast_division.go
// castDivisionAPI). A Charm's modes are sub-abilities, never the root, and
// keep the resolution-time split.
func spellDivision(reg *cards.Registry, name string) (string, int, bool) {
	c, ok := reg.Lookup(name)
	if !ok {
		return "", 0, false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
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
	}
	return "", 0, false
}

// castRootTarget is the index of cast step k's own root target ask: the
// first target decision of that step answered from the step's target list.
func castRootTarget(ds []rules.OracleDecision, k int) int {
	for i, d := range ds {
		if d.Step == k && d.Kind == "target" && d.Via == "target" && d.Resume == "" && d.Divided == 0 && len(d.PickRefs) > 0 {
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
