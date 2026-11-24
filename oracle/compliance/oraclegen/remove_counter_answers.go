package oraclegen

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// RemoveCounter's CounterType$ Any / All election is one XMage multi-amount
// dialog, not one makeChoose per counter.
//
// gorge poses the election as one KChoose per counter removed (engine
// ResumeKind "counter_kind", option kind "counter"), so an "up to three
// counters" effect on a permanent holding two loyalty counters records two
// picks: [LOYALTY], [LOYALTY]. XMage's
// RemoveUpToAmountCountersEffect instead calls
// getMultiAmountWithIndividualConstraints once, with one message per counter
// kind the target holds (CounterType keys), and TestPlayer reads one "X=<n>"
// choice per message. Passing the raw kind labels through therefore fails
// with "Missing choice in multi amount" (Mabel, Bitter Recluse removing two
// loyalty counters from Ajani Resolute).
//
// foldRemoveCounterKindAnswers folds each consecutive run of those picks into
// one amount answer per kind message. The kind set is the target's own
// counter set at the checkpoint BEFORE the step (a kind left at zero after a
// full removal must still get its "X=0"), and the target is re-derived from
// the decisions and snapshots, never from a card name: a same-step target
// decision first, then the nearest preceding target decision, then the
// permanent/player whose counters the step actually changed.
//
// Message order: XMage builds the list as new ArrayList<>(counterMap.keySet())
// over a java.util.HashMap, so it is bucket order of the Java String hash, not
// the engine's counter-slice order. counterKindMessageOrder reproduces that
// order (default 16-bucket table, resizing at 3/4 load, insertion order inside
// a bucket). The engine's own counter order is reconstructed from the runs'
// picked option indices (each pick's index is its position among the kinds
// still present), which supplies the HashMap's insertion order for the
// same-bucket ties; a run that stops early with unconstrained kinds falls back
// to a sorted order, still deterministic.

// removeCounterKindDecision reports one RemoveCounter Any/All counter-kind
// election pick.
func removeCounterKindDecision(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && d.Resume == "counter_kind"
}

// foldRemoveCounterKindAnswers returns the amount answers keyed by the first
// decision of each run, and the set of every decision index consumed by a run.
func foldRemoveCounterKindAnswers(ds []rules.OracleDecision, snaps []rules.OracleSnapshot) (map[int][]XAnswer, map[int]bool) {
	answers := map[int][]XAnswer{}
	owned := map[int]bool{}
	for i := 0; i < len(ds); {
		if !removeCounterKindDecision(ds[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(ds) && removeCounterKindDecision(ds[j]) &&
			ds[j].Step == ds[i].Step && ds[j].Seat == ds[i].Seat {
			j++
		}
		run := ds[i:j]
		counts := map[string]int{}
		var picked []string
		for _, d := range run {
			for k, label := range d.Picks {
				if k < len(d.PickKinds) && d.PickKinds[k] == "remove_counter_done" {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(label), "done") {
					continue
				}
				kind := canonicalCounterKind(label)
				if kind == "" {
					continue
				}
				if counts[kind] == 0 {
					picked = append(picked, kind)
				}
				counts[kind]++
			}
		}
		var kinds []string
		if before, after, ok := removeCounterTarget(ds, i, snaps, counts); ok {
			kinds = append(kinds, before...)
			for _, kind := range after {
				if !containsCounterKind(kinds, kind) {
					kinds = append(kinds, kind)
				}
			}
		}
		for _, kind := range picked {
			if !containsCounterKind(kinds, kind) {
				kinds = append(kinds, kind)
			}
		}
		var as []XAnswer
		for _, kind := range counterKindMessageOrder(kinds, run) {
			as = append(as, XAnswer{ds[i].Seat, "amount", strconv.Itoa(counts[kind])})
		}
		if len(as) > 0 {
			answers[i] = as
		}
		for k := i; k < j; k++ {
			owned[k] = true
		}
		i = j
	}
	return answers, owned
}

// canonicalCounterKind normalizes a gorge counter kind the way the runner's
// normCounter does, so a pick label and a snapshot key compare.
func canonicalCounterKind(s string) string {
	t := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	switch t {
	case "+1/+1", "P1P1":
		return "P1P1"
	case "-1/-1", "M1M1":
		return "M1M1"
	}
	return t
}

// displayCounterName is XMage's CounterType name for a gorge kind: the nine
// power/toughness counters use CardUtil.getBoostCountAsStr's signed form,
// every other kind is the lowercase name CounterType declares.
func displayCounterName(kind string) string {
	if kind == "P1P1" {
		return "+1/+1"
	}
	if kind == "M1M1" {
		return "-1/-1"
	}
	if len(kind) == 4 && (kind[0] == 'P' || kind[0] == 'M') && (kind[2] == 'P' || kind[2] == 'M') &&
		kind[1] >= '0' && kind[1] <= '9' && kind[3] >= '0' && kind[3] <= '9' {
		pSign, tSign := kind[0], kind[2]
		pNum, tNum := kind[1:2], kind[3:4]
		if (pSign == 'P' || pSign == 'M') && (tSign == 'P' || tSign == 'M') {
			p := pNum
			if pSign == 'P' {
				if tSign == 'M' && pNum == "0" {
					p = "-0"
				} else {
					p = "+" + pNum
				}
			} else {
				p = "-" + pNum
			}
			t := tNum
			if tSign == 'P' {
				if pSign == 'M' && tNum == "0" {
					t = "-0"
				} else {
					t = "+" + tNum
				}
			} else {
				t = "-" + tNum
			}
			return p + "/" + t
		}
	}
	return strings.ToLower(kind)
}

// removeCounterTarget finds the permanent or player whose counters the run
// removes from, and returns that carrier's counter kinds before and after the
// step (canonicalized, positive counts only). counts is the removals per kind
// observed in the run, used to choose among candidate refs.
func removeCounterTarget(ds []rules.OracleDecision, i int, snaps []rules.OracleSnapshot, counts map[string]int) (before, after []string, ok bool) {
	if len(snaps) == 0 {
		return nil, nil, false
	}
	step := ds[i].Step
	if step < 0 || step >= len(snaps) {
		return nil, nil, false
	}
	beforeSnap := snaps[step]
	var afterSnap *rules.OracleSnapshot
	if step+1 < len(snaps) {
		afterSnap = &snaps[step+1]
	}

	var candidates []string
	seen := map[string]bool{}
	add := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		candidates = append(candidates, ref)
	}
	// Same-step target decisions are the ordinary case (a trigger's target).
	for _, d := range ds {
		if d.Step == step && d.Seat == ds[i].Seat && d.Kind == "target" {
			for _, ref := range d.PickRefs {
				add(ref)
			}
		}
	}
	// A spell's target is chosen on its cast step, so take the nearest
	// preceding target decision of the same seat too.
	prev := -1
	for _, d := range ds {
		if d.Seat != ds[i].Seat || d.Kind != "target" || d.Step >= step || d.Step < 0 {
			continue
		}
		if d.Step > prev {
			prev = d.Step
		}
	}
	if prev >= 0 {
		for _, d := range ds {
			if d.Seat == ds[i].Seat && d.Kind == "target" && d.Step == prev {
				for _, ref := range d.PickRefs {
					add(ref)
				}
			}
		}
	}
	// The checkpoint delta is the authority when no target decision reached
	// this run (a retimed entry trigger): any carrier whose counters changed.
	for _, p := range beforeSnap.Permanents {
		if counterCountsDiffer(p.Counters, counterCountsAt(afterSnap, p.Ref)) {
			add(p.Ref)
		}
	}
	for _, p := range beforeSnap.Players {
		ref := seatRef(p.Seat)
		if counterCountsDiffer(p.Counters, counterCountsAt(afterSnap, ref)) {
			add(ref)
		}
	}

	var fallbackBefore, fallbackAfter []string
	haveFallback := false
	for _, ref := range candidates {
		b, bCounts, ok := counterSnapshotKinds(&beforeSnap, ref)
		if !ok || len(b) == 0 {
			continue
		}
		a, aCounts, _ := counterSnapshotKinds(afterSnap, ref)
		fits, exact := true, true
		for kind, n := range counts {
			if bCounts[kind] < int32(n) {
				fits = false
				break
			}
			if bCounts[kind]-aCounts[kind] != int32(n) {
				exact = false
			}
		}
		if !fits {
			continue
		}
		if exact {
			return b, a, true
		}
		if !haveFallback {
			fallbackBefore, fallbackAfter = b, a
			haveFallback = true
		}
	}
	if haveFallback {
		return fallbackBefore, fallbackAfter, true
	}
	return nil, nil, false
}

// counterSnapshotKinds returns a carrier's counter kinds and counts at a
// checkpoint. A "pN" ref is a player; anything else is a permanent.
func counterSnapshotKinds(snap *rules.OracleSnapshot, ref string) ([]string, map[string]int32, bool) {
	if snap == nil {
		return nil, nil, false
	}
	var cs map[string]int32
	found := false
	if isSeat(ref) {
		seat, ok := seatOfRef(ref)
		if !ok {
			return nil, nil, false
		}
		for _, p := range snap.Players {
			if p.Seat == seat {
				cs = p.Counters
				found = true
				break
			}
		}
	} else {
		for _, p := range snap.Permanents {
			if p.Ref == ref {
				cs = p.Counters
				found = true
				break
			}
		}
	}
	if !found {
		return nil, nil, false
	}
	counts := make(map[string]int32, len(cs))
	var kinds []string
	for k, n := range cs {
		kind := canonicalCounterKind(k)
		if kind == "" || n <= 0 {
			continue
		}
		counts[kind] += n
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds, counts, true
}

func counterCountsAt(snap *rules.OracleSnapshot, ref string) map[string]int32 {
	_, counts, ok := counterSnapshotKinds(snap, ref)
	if !ok {
		return nil
	}
	return counts
}

func counterCountsDiffer(before, after map[string]int32) bool {
	for k, n := range before {
		if after[k] != n {
			return true
		}
	}
	return false
}

func containsCounterKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// counterKindMessageOrder puts kinds in the order XMage's
// getMultiAmountWithIndividualConstraints messages them.
func counterKindMessageOrder(kinds []string, run []rules.OracleDecision) []string {
	if len(kinds) <= 1 {
		return kinds
	}
	ordered := append([]string(nil), kinds...)
	if engine, ok := reconstructCounterKindOrder(kinds, run); ok {
		ordered = engine
	} else {
		sort.Strings(ordered)
	}
	return xmageCounterMapOrder(ordered)
}

type counterKindPick struct {
	kind string
	idx  int
}

// reconstructCounterKindOrder recovers the engine's counter-slice order from
// the runs' picks: each pick's option index is its position among the kinds
// still present, so any permutation reproducing every index is the order the
// permanent listed them. When more than one fits (a run that stopped early),
// the lexicographically smallest fit is returned: deterministic, and the
// same-bucket ties it resolves are the only place the order is otherwise
// unknowable.
func reconstructCounterKindOrder(kinds []string, run []rules.OracleDecision) ([]string, bool) {
	if len(kinds) > 8 {
		return nil, false
	}
	var picks []counterKindPick
	for _, d := range run {
		for k, label := range d.Picks {
			if k < len(d.PickKinds) && d.PickKinds[k] == "remove_counter_done" {
				break
			}
			if strings.EqualFold(strings.TrimSpace(label), "done") {
				break
			}
			kind := canonicalCounterKind(label)
			if kind == "" || !containsCounterKind(kinds, kind) {
				continue
			}
			if k >= len(d.PickIdx) {
				continue
			}
			picks = append(picks, counterKindPick{kind: kind, idx: d.PickIdx[k]})
		}
	}
	if len(picks) == 0 {
		return nil, false
	}
	base := append([]string(nil), kinds...)
	sort.Strings(base)
	var fit []string
	var permute func(cur, rest []string) bool
	permute = func(cur, rest []string) bool {
		if len(rest) == 0 {
			if counterOrderFits(cur, picks) {
				fit = append([]string(nil), cur...)
				return true
			}
			return false
		}
		for i := range rest {
			nextRest := append(append([]string(nil), rest[:i]...), rest[i+1:]...)
			if permute(append(cur, rest[i]), nextRest) {
				return true
			}
		}
		return false
	}
	if permute(nil, base) {
		return fit, true
	}
	return nil, false
}

func counterOrderFits(order []string, picks []counterKindPick) bool {
	rem := append([]string(nil), order...)
	for _, p := range picks {
		pos := -1
		for i, k := range rem {
			if k == p.kind {
				pos = i
				break
			}
		}
		if pos < 0 || pos != p.idx {
			return false
		}
		rem = append(rem[:pos], rem[pos+1:]...)
	}
	return true
}

// xmageCounterMapOrder reproduces java.util.HashMap's keySet iteration order
// for a default-constructed map: buckets ascending, insertion order inside a
// bucket. keys is the insertion order (the engine's counter order).
func xmageCounterMapOrder(keys []string) []string {
	capacity := 16
	for len(keys) > capacity*3/4 {
		capacity *= 2
	}
	type entry struct {
		kind   string
		bucket int
		pos    int
	}
	entries := make([]entry, len(keys))
	for i, kind := range keys {
		entries[i] = entry{kind: kind, bucket: int(javaCounterHash(displayCounterName(kind)) & uint32(capacity-1)), pos: i}
	}
	sort.SliceStable(entries, func(a, b int) bool {
		if entries[a].bucket != entries[b].bucket {
			return entries[a].bucket < entries[b].bucket
		}
		return entries[a].pos < entries[b].pos
	})
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.kind
	}
	return out
}

// javaCounterHash is String.hashCode spread the way HashMap spreads it:
// h ^ (h >>> 16).
func javaCounterHash(s string) uint32 {
	var h int32
	for _, r := range s {
		h = 31*h + int32(r)
	}
	u := uint32(h)
	return u ^ (u >> 16)
}
