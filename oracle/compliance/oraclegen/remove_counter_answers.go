package oraclegen

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
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
// "counter_kind" is the engine's resume role, not a card identity: PutCounter
// poses "choose a kind to put" with the same ResumeKind, and a fixed-count
// RemoveCounter Any poses it whenever the carrier has more than one kind.
// XMage turns only the RemoveCounter Any/All UP-TO ask into a multi-amount
// (RemoveUpToAmountCountersEffect); the other two are plain Choice dialogs.
// The fold therefore also requires the ask shape (one option per present
// counter kind plus the trailing Done option) and accepts only a carrier
// whose counters did not grow across the step. An ask that matches neither is
// left untouched on the generic choice route, so the next sibling sharing the
// resume kind is safe by default instead of needing a card list.
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

// removeCounterKindDecision reports one counter-kind election pick. The
// engine reuses the "counter_kind" ResumeKind for RemoveCounter's per-counter
// election AND for PutCounter's "choose a kind to put" asks
// (effects/counters_put.go and counters_put_existing.go), so this predicate
// is only a candidate filter: foldRemoveCounterKindAnswers additionally
// requires the remove-up-to shape before it scripts amounts.
func removeCounterKindDecision(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && d.Resume == "counter_kind"
}

// A "Remove a counter from {this}" / "Remove <N> counters from {this}"
// ACTIVATION COST is the other XMage multi-amount: RemoveCountersSourceCost's
// any-counter arm (Mage .../costs/common/RemoveCountersSourceCost.java pay())
// always calls player.getMultiAmount(..., toChoose, 0, amount, amount,
// REMOVE_COUNTERS, game) over every counter kind on the source, even a single
// one. gorge poses the same choice as a KChoose per removed counter only when
// the source carries more than one kind; with one kind it picks it unasked,
// so there is no decision to fold (CR 118.3, 122.8: the payer still chose the
// counters). The amount per kind is the source's checkpoint delta across the
// activate step: costs are paid before the ability resolves, so the activate
// step itself carries exactly the counters the cost removed.

// removeCounterCostAsk names one activate step whose XMage rule text prices an
// any-counter source-removal cost. Ref is the source permanent's scenario ref
// (the activate step's Card).
type removeCounterCostAsk struct {
	Step int
	Seat int
	Ref  string
}

// removeCounterCostAsks finds every activate step whose printed ability's
// XMage rule-text prefix is an any-counter source-removal cost. The prefix is
// re-derived through XMageAbility, the same mapping the activate template and
// xprep write to Item.XAbility, so every XAnswersForScenario caller gets the
// routing without threading a new parameter through its 100+ call sites. Only
// a printed ability qualifies: a granted or keyword-granted alias, and a face
// whose text is ambiguous, fail closed.
func removeCounterCostAsks(reg *cards.Registry, sc Scenario, g rules.OracleResult) []removeCounterCostAsk {
	if reg == nil || len(g.Snapshots) == 0 {
		return nil
	}
	it := Item{Scenario: Scenario{Setup: sc.Setup}}
	var out []removeCounterCostAsk
	for i, st := range sc.Steps {
		if st.Op != "activate" || st.AbilityIndex == nil || st.Card == "" {
			continue
		}
		if _, kind := ResolveActivatedAbility(reg, it, i, st, g); kind != ActivatedPrinted {
			continue
		}
		face, ok := printedStepFace(reg, it, i, st, g)
		if !ok {
			continue
		}
		prefixes, why := XMageAbility(face)
		if why != "" {
			continue
		}
		prefix, ok := prefixes[*st.AbilityIndex]
		if !ok || !removeCounterCostPrefix(prefix) {
			continue
		}
		out = append(out, removeCounterCostAsk{Step: i, Seat: st.Seat, Ref: st.Card})
	}
	return out
}

// removeCounterCostPrefix reports whether an XMage ability prefix contains an
// any-counter source-removal cost. RemoveCountersSourceCost's any arm prints
// "remove " + CardUtil.numberToText(n, "a") + " counter(s) from {this}" and
// asks the multi-amount dialog; its Counter overload instead names the kind
// ("remove two +1/+1 counters from {this}") and removes without a dialog.
// Requiring exactly "<amount> counter(s)" therefore selects the dialog arm by
// its printed role, not by a card list: a new counter kind or amount needs no
// new case (the amount is not read from the text anyway, the checkpoint delta
// is), while a kind-named or variable ("X time counters") cost cannot match.
func removeCounterCostPrefix(prefix string) bool {
	for _, part := range strings.Split(prefix, ", ") {
		rest, ok := strings.CutPrefix(part, "Remove ")
		if !ok {
			continue
		}
		rest, ok = strings.CutSuffix(rest, " from {this}")
		if !ok {
			continue
		}
		words := strings.Fields(rest)
		if len(words) != 2 || (words[1] != "counter" && words[1] != "counters") {
			continue
		}
		if counterCostAmountWord(words[0]) {
			return true
		}
	}
	return false
}

// counterCostAmountWord reports whether w is an amount XMage's
// CardUtil.numberToText emits: "a" for one, the words for 2..20, or digits
// above 20.
func counterCostAmountWord(w string) bool {
	switch w {
	case "a", "an", "one", "two", "three", "four", "five", "six", "seven",
		"eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen",
		"fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty":
		return true
	}
	if w == "" {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// removeCounterCostPick reports one decision that is a unit of an activate
// step's any-counter source-removal cost. The engine's wildcard SubCounter arm
// (cast_subcounter.go wildcardCounterAsk) poses one KChoose per removed unit
// with option kind "subcounter", no ResumeKind, and the label
// "<source> (<kind> counter)"; its filtered "from among" twin shares the
// option kind but prints a bare permanent name, so the label's parenthesised
// kind is what selects the source-anchored cost. The picked kind and, when
// recorded, the object ref must name the ask's own source: a second
// SubCounter cost in the same step must stay on the generic route. This is
// deliberately NOT removeCounterKindDecision: the effect fold's
// "counter_kind" resume role is never what a cost pick carries.
func removeCounterCostPick(d rules.OracleDecision, c removeCounterCostAsk, kinds []string) bool {
	if d.Kind != "choose_n" || len(d.PickKinds) != 1 || d.PickKinds[0] != "subcounter" {
		return false
	}
	if len(d.Picks) != 1 {
		return false
	}
	kind, ok := counterKindFromPickLabel(d.Picks[0])
	if !ok || !containsCounterKind(kinds, canonicalCounterKind(kind)) {
		return false
	}
	if len(d.PickRefs) > 0 && d.PickRefs[0] != "" && d.PickRefs[0] != c.Ref {
		return false
	}
	return true
}

// counterKindFromPickLabel extracts the engine counter kind from a SubCounter
// option label ("<permanent name> (<kind> counter)", cast_subcounter.go's
// wildcardCounterAsk and its mana-cost twin). The parenthesised kind is the
// only place the option carries it: the label is what the generic choice route
// would otherwise script verbatim, which is why the cost's pick has to route
// here instead.
func counterKindFromPickLabel(s string) (string, bool) {
	s = strings.TrimSpace(s)
	const suffix = " counter)"
	if !strings.HasSuffix(s, suffix) {
		return "", false
	}
	open := strings.LastIndex(s, " (")
	if open < 0 {
		return "", false
	}
	kind := strings.TrimSpace(s[open+2 : len(s)-len(suffix)])
	if kind == "" {
		return "", false
	}
	return kind, true
}

// foldRemoveCounterCostAnswers returns, per activate step, the amount answers
// XMage's RemoveCountersSourceCost multi-amount reads, and the set of
// subcounter decision indices it owns. The amounts are the source's counter
// delta across the step (a kind left at zero still gets its explicit X=0); the
// message order is XMage's HashMap order (counterKindMessageOrder), using the
// step's counter-kind picks to recover the engine's counter order for
// same-bucket ties. A lone counter kind poses no engine decision, so its
// answers are emitted by the caller's step-order fallback; a multi-kind source
// does pose picks, which this owns so the raw label never reaches XMage's
// choice queue.
func foldRemoveCounterCostAnswers(ds []rules.OracleDecision, snaps []rules.OracleSnapshot, costs []removeCounterCostAsk) (map[int][]XAnswer, map[int]bool) {
	answers := map[int][]XAnswer{}
	owned := map[int]bool{}
	for _, c := range costs {
		if c.Step < 0 || c.Step >= len(snaps) {
			continue
		}
		before, beforeCounts, ok := counterSnapshotKinds(&snaps[c.Step], c.Ref)
		if !ok || len(before) == 0 {
			continue
		}
		var afterSnap *rules.OracleSnapshot
		if c.Step+1 < len(snaps) {
			afterSnap = &snaps[c.Step+1]
		}
		_, afterCounts, _ := counterSnapshotKinds(afterSnap, c.Ref)
		removed := make(map[string]int, len(before))
		total := 0
		for _, kind := range before {
			if n := int(beforeCounts[kind] - afterCounts[kind]); n > 0 {
				removed[kind] = n
				total += n
			}
		}
		if total == 0 {
			// No counters left the source across this step, so no cost was
			// paid and XMage would not pose the dialog: leave every decision
			// on the generic route.
			continue
		}
		var run []rules.OracleDecision
		var picked []int
		for i, d := range ds {
			if d.Step == c.Step && d.Seat == c.Seat && removeCounterCostPick(d, c, before) {
				run = append(run, d)
				picked = append(picked, i)
			}
		}
		var as []XAnswer
		for _, kind := range counterKindMessageOrder(before, run) {
			as = append(as, XAnswer{c.Seat, "amount", strconv.Itoa(removed[kind])})
		}
		answers[c.Step] = as
		for _, i := range picked {
			owned[i] = true
		}
	}
	return answers, owned
}

// removeCounterDonePicked reports a pick of the remove-up-to "Done" option.
// Its presence is a positive RemoveCounter signal (PutCounter has no Done
// option), and it terminates the engine's election loop.
func removeCounterDonePicked(d rules.OracleDecision) bool {
	for k, kind := range d.PickKinds {
		if kind == "remove_counter_done" {
			return true
		}
		if k < len(d.Picks) && strings.EqualFold(strings.TrimSpace(d.Picks[k]), "done") {
			return true
		}
	}
	return false
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
			// A Done pick ends the engine's election loop, so the next
			// counter_kind decision (if any) is a new ask, not a pick of
			// this one.
			if removeCounterDonePicked(ds[j-1]) {
				break
			}
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
		// Only the RemoveCounter Any/All up-to ask becomes an XMage
		// multi-amount; a PutCounter kind pick and a fixed-count RemoveCounter
		// ask stay on the generic choice route. Require the carrier and the
		// up-to option shape before folding, so an unrecognised counter_kind
		// ask is scripted exactly as it was before this routing existed.
		before, after, ok := removeCounterTarget(ds, ds[i:j], snaps, ds[i], counts)
		if !ok {
			i = j
			continue
		}
		kinds := append([]string(nil), before...)
		for _, kind := range after {
			if !containsCounterKind(kinds, kind) {
				kinds = append(kinds, kind)
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
// normCounter does, so a pick label and a snapshot key compare. A SubCounter
// option label ("<permanent> (<kind> counter)") carries its kind in
// parentheses; unwrap it first so a cost pick and an effect pick compare the
// same way.
func canonicalCounterKind(s string) string {
	if kind, ok := counterKindFromPickLabel(s); ok {
		s = kind
	}
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
//
// A candidate is only accepted when the run has the RemoveCounter Any/All
// up-to shape (see removeCounterUpToShape) and its counters do not increase
// across the step. Together those reject the two other asks that share the
// engine's "counter_kind" ResumeKind: PutCounter's "choose a kind to put"
// (its options come from the card's kind list, not from the carrier's
// counters, and the carrier gains counters) and fixed-count RemoveCounter Any
// on a multi-kind carrier (options are the present kinds with no Done, and
// XMage asks RemoveCounterTargetEffect's Choice instead of a multi-amount).
func removeCounterTarget(all []rules.OracleDecision, run []rules.OracleDecision, snaps []rules.OracleSnapshot, anchor rules.OracleDecision, counts map[string]int) (before, after []string, ok bool) {
	if len(snaps) == 0 {
		return nil, nil, false
	}
	step := anchor.Step
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
	for _, d := range all {
		if d.Step == step && d.Seat == anchor.Seat && d.Kind == "target" {
			for _, ref := range d.PickRefs {
				add(ref)
			}
		}
	}
	// A spell's target is chosen on its cast step, so take the nearest
	// preceding target decision of the same seat too.
	prev := -1
	for _, d := range all {
		if d.Seat != anchor.Seat || d.Kind != "target" || d.Step >= step || d.Step < 0 {
			continue
		}
		if d.Step > prev {
			prev = d.Step
		}
	}
	if prev >= 0 {
		for _, d := range all {
			if d.Seat == anchor.Seat && d.Kind == "target" && d.Step == prev {
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
		if !fits || !counterCountsNonIncreasing(bCounts, aCounts) ||
			!removeCounterUpToShape(run, bCounts) {
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

// counterCountsNonIncreasing reports whether no counter kind on the carrier
// grew across the step. A RemoveCounter never adds, so a carrier that gained
// any counters cannot be this run's target (a PutCounter ask picked it).
func counterCountsNonIncreasing(before, after map[string]int32) bool {
	for kind, n := range after {
		if n > before[kind] {
			return false
		}
	}
	return true
}

// removeCounterUpToShape reports whether every decision in run is one step of
// the engine's RemoveCounter Any/All up-to election. That ask offers exactly
// the carrier's still-present counter kinds (one option each) plus a trailing
// "Done" option, so its option count is present+1 and each pick is one of
// those present kinds. Fixed-count RemoveCounter Any offers present kinds
// only, and a PutCounter kind pick offers the card's kind list, so neither
// matches.
func removeCounterUpToShape(run []rules.OracleDecision, before map[string]int32) bool {
	remaining := make(map[string]int32, len(before))
	for kind, n := range before {
		remaining[kind] = n
	}
	for _, d := range run {
		present := 0
		for _, n := range remaining {
			if n > 0 {
				present++
			}
		}
		if d.Options != present+1 {
			return false
		}
		for k, label := range d.Picks {
			if k < len(d.PickKinds) && d.PickKinds[k] == "remove_counter_done" {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(label), "done") {
				continue
			}
			kind := canonicalCounterKind(label)
			if kind == "" || remaining[kind] <= 0 {
				return false
			}
			remaining[kind]--
		}
	}
	return true
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
