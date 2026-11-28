package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// materialize returns a scenario copy with every answer group placed at its
// current step (base + offset).
func materialize(sc *portScenario, groups []*group) *portScenario {
	cp := *sc
	cp.Steps = make([]*step, len(sc.Steps))
	for i, s := range sc.Steps {
		c := *s
		c.Targets = append([]string(nil), s.Targets...)
		c.Answers = append([]answer(nil), s.Answers...)
		cp.Steps[i] = &c
	}
	for _, g := range groups {
		i := g.at + g.offset
		if i >= len(cp.Steps) {
			i = len(cp.Steps) - 1
		}
		cp.Steps[i].Targets = append(cp.Steps[i].Targets, g.targets...)
		cp.Steps[i].Answers = append(cp.Steps[i].Answers, g.placed()...)
	}
	return &cp
}

var reStepFail = regexp.MustCompile(`^(?:after )?step (\d+) \(`)

// placementFailSteps returns the step indices whose failure is about an
// answer or target the step did not consume.
func placementFailSteps(fails []string) map[int]bool {
	out := map[int]bool{}
	for _, f := range fails {
		m := reStepFail.FindStringSubmatch(f)
		if m == nil {
			continue
		}
		if strings.Contains(f, "unconsumed answer(s)") || strings.Contains(f, "unused target(s)") ||
			(strings.Contains(f, "harness: answer ") && strings.Contains(f, "not offered")) ||
			(strings.Contains(f, "harness: target ") && strings.Contains(f, "not offered")) {
			n, _ := strconv.Atoi(m[1])
			out[n] = true
		}
	}
	return out
}

var verbose bool

// runOne runs sc the way audit.TestXMagePortScenarios does (the audit entry
// point in XMage-fixture mode), so the log-only replay verdict is known too:
// a scenario whose replay differs from its live game cannot be committed.
func runOne(reg *cards.Registry, sc *portScenario) (fails, transcript []string, replayDiff string, err error) {
	res, raw, err := execScenario(reg, sc)
	if err != nil {
		return nil, nil, "", err
	}
	if verbose {
		fmt.Println(string(raw))
		fmt.Println(strings.Join(res.Transcript, "\n"))
		fmt.Println(strings.Join(res.Fails, "\n"))
	}
	return res.Fails, res.Transcript, res.ReplayDiff, nil
}

// execScenario runs sc without printing; probes use it so -v shows only the
// scenario's own runs.
func execScenario(reg *cards.Registry, sc *portScenario) (*rules.OracleAuditResult, []byte, error) {
	raw, err := json.Marshal(scenarioBody(sc))
	if err != nil {
		return nil, nil, err
	}
	res, err := rules.RunOracleAuditScenarioJSON(reg, raw, true)
	if err != nil {
		return nil, nil, err
	}
	return &res, raw, nil
}

var reFallback = regexp.MustCompile(`\[\w+ fallback\] p\d (\w+) ->`)

var reContinueAck = regexp.MustCompile(`^\s+\[\w+ fallback\] p\d choose -> \["Continue"\]`)

// hasContinueAck reports whether the runner answered a bare "Continue"
// acknowledgement by its fallback.
func hasContinueAck(transcript []string) bool {
	for _, l := range transcript {
		if reContinueAck.MatchString(l) {
			return true
		}
	}
	return false
}

var reDeclarationAnswer = regexp.MustCompile(`^\s+\[answer\] p\d (?:attackers|blockers) ->`)

// declarationAnswer reports whether a stated choice was consumed by an attack
// or block declaration. The tests declare those with attack()/block(), which
// become attack/block steps, so an answer taken by the declaration decision
// is a choice gorge never asked (XMage asked which creature to move an Aura
// to; gorge has one candidate), carried to the next decision that would take
// it: the run is not the game the test states.
func declarationAnswer(transcript []string) bool {
	for _, l := range transcript {
		if reDeclarationAnswer.MatchString(l) {
			return true
		}
	}
	return false
}

var reSetupFallback = regexp.MustCompile(`^\s+\[setup fallback\] p\d (\w+) ->`)

// setupDecision returns the first decision kind the runner answered by its
// fallback while building the setup (before step 0), or "". An empty attack
// or block declaration is not a choice a test states.
func setupDecision(transcript []string) string {
	for _, l := range transcript {
		if m := reSetupFallback.FindStringSubmatch(l); m != nil && m[1] != "attackers" && m[1] != "blockers" {
			return m[1]
		}
	}
	return ""
}

// aiChoiceFallback returns the first decision kind the runner answered by
// its fallback that is a real game choice (not an empty attack/block
// declaration, which the test states with attack/block calls).
func aiChoiceFallback(transcript []string) string {
	for _, l := range transcript {
		if m := reFallback.FindStringSubmatch(l); m != nil && m[1] != "attackers" && m[1] != "blockers" {
			return m[1]
		}
	}
	return ""
}

var reCountMismatch = regexp.MustCompile(`harness: submit \w+ \[\d+\]: expected (\d+)\.\.\d+ choices, got 1`)

// mergePicks folds n consecutive single-pick answers of g into one n-pick
// answer: XMage answers "choose two" with one setChoice per object, gorge
// asks it once. Reports whether it merged.
func mergePicks(g *group, n int) bool {
	single := func(a answer) bool {
		p := strings.ToLower(a.Pick[0])
		return len(a.Pick) == 1 && p != "yes" && p != "no" && a.Kind != "modes"
	}
	for i := 0; i+n <= len(g.answers); i++ {
		ok := true
		for j := i; j < i+n; j++ {
			ok = ok && single(g.answers[j])
		}
		if !ok {
			continue
		}
		var picks []string
		for j := i; j < i+n; j++ {
			picks = append(picks, g.answers[j].Pick[0])
		}
		g.answers[i] = answer{Kind: "any", Pick: picks, Seat: g.answers[i].Seat}
		g.answers = append(g.answers[:i+1], g.answers[i+n:]...)
		g.kinds = append(g.kinds[:i+1], g.kinds[i+n:]...)
		g.kinds[i] = 0
		return true
	}
	return false
}

// skipEmptyDeclaration turns an empty attack/block step gorge never poses
// (no creature that can attack, or nothing able to block an unblockable
// attacker) into a plain walk to that step. Reports whether it changed.
func skipEmptyDeclaration(s *step, line string) bool {
	switch {
	case s.Op == "block" && len(s.Blocks) == 0 && strings.Contains(line, "no blockers decision pending"):
		act := s.blockActive
		if act == "" {
			act = pref(1 - s.Seat)
		}
		*s = step{Op: "pass_to", Step: stepNames[sDeclareBlockers], Active: act, Expect: s.Expect, Answers: s.Answers, Targets: s.Targets}
		return true
	case s.Op == "attack" && len(s.Attackers) == 0 && strings.Contains(line, "passed declare-attackers without being asked"):
		*s = step{Op: "pass_to", Step: stepNames[sDeclareAttackers], Active: pref(s.Seat), Expect: s.Expect, Answers: s.Answers, Targets: s.Targets}
		return true
	}
	return false
}

// scenarioBody drops the wrapper fields rules' strict decoder does not know.
func scenarioBody(sc *portScenario) any {
	type body struct {
		Name    string           `json:"name"`
		Turn    *int             `json:"turn,omitempty"`
		CR      []string         `json:"cr"`
		Why     string           `json:"why"`
		Players int              `json:"players,omitempty"`
		Setup   map[string]*seat `json:"setup"`
		Steps   []*step          `json:"steps"`
		Expect  []*expect        `json:"expect"`
	}
	return body{sc.Name, sc.Turn, sc.CR, sc.Why, sc.Players, sc.Setup, sc.Steps, sc.Expect}
}

var reNotOffered = regexp.MustCompile(`harness: answer "((?:[^"\\]|\\.)*)" not offered for (\w+):`)

// answerKinds lists the decision kinds an XMage choice may be answering, in
// the order tried: "any" first, then the specific kinds, so a choice the
// wildcard hands to the wrong decision (an attackers ask) is retried on
// the kinds a setChoice can mean.
func answerKinds(a answer) []string {
	if a.Kind != "any" {
		return []string{a.Kind}
	}
	p := strings.ToLower(a.Pick[0])
	switch {
	case p == "yes" || p == "no":
		return []string{"any", "trigger_optional", "choose", "replacement", "modes"}
	case isSeatRef(p) || len(p) > 2 && isSeatRef(p[:2]) && p[2] == ':':
		return []string{"any", "choose", "target", "replacement", "arrange", "modes"}
	}
	return []string{"any", "choose", "modes", "replacement", "trigger_order"}
}

// splitUnaskedTail moves g.answers[i:] to a new group at step n+1 (or drops
// them when n is the last step), leaving the answers a decision already
// consumed where they are. Moving the whole group instead would unplace the
// consumed answers too.
func splitUnaskedTail(r *result, g *group, i, n, steps int) bool {
	if i <= 0 || i >= len(g.answers) {
		return false
	}
	tail := &group{answers: append([]answer(nil), g.answers[i:]...), kinds: make([]int, len(g.answers)-i), at: n + 1}
	g.answers, g.kinds = g.answers[:i:i], g.kinds[:i:i]
	if n+1 < steps {
		r.Groups = append(r.Groups, tail)
	} else {
		r.Dropped += len(tail.answers)
	}
	return true
}

// refBareName strips a runner ref ("p0:Llanowar Elves#2") to its card name;
// ok is false for anything that is not a ref.
func refBareName(pick string) (string, bool) {
	i := strings.IndexByte(pick, ':')
	if i != 2 || !isSeatRef(pick[:2]) {
		return "", false
	}
	n := pick[i+1:]
	if j := strings.LastIndexByte(n, '#'); j >= 0 && allDigits(n[j+1:]) {
		n = n[:j]
	}
	return n, true
}

// rewriteOfferedLabel fixes an answer gorge reports as not offered for kind
// because XMage names a card where the ask labels it differently:
//
//   - a card REF ("p0:Llanowar Elves") against an ask whose options carry the
//     bare card name ("choose a card name", a hidden pick from the library):
//     the pick becomes that label;
//   - a chapter number ("X=2") against read ahead's chapter ask: the pick
//     becomes that chapter's label ("Chapter II");
//   - a card pick against an optional ask's leading Yes/No pair ("you may put
//     a creature card from among them"): XMage states only the card, so the
//     "Yes" that opens the optional is inserted before it.
//
// It reports whether it changed g.
func rewriteOfferedLabel(g *group, i int, kind string, opts []dumpOption) bool {
	a := g.answers[i]
	if label, ok := readAheadLabel(a.Pick[0], opts); ok {
		g.answers[i] = answer{Kind: kind, Pick: []string{label}, Seat: a.Seat}
		return true
	}
	name, ok := refBareName(a.Pick[0])
	if !ok || len(opts) == 0 {
		return false
	}
	for _, o := range opts {
		if strings.EqualFold(o.Label, name) {
			g.answers[i] = answer{Kind: kind, Pick: []string{o.Label}, Seat: a.Seat}
			return true
		}
	}
	if len(opts) == 2 {
		yes, no := -1, -1
		for j, o := range opts {
			switch strings.ToLower(o.Label) {
			case "yes":
				yes = j
			case "no":
				no = j
			}
			// an optional trigger's cost ask ("Sacrifice 1 creature" /
			// "Do not pay", Industrial Advancement) is the same leading pair:
			// XMage states only the object paid, the pay option precedes it
			switch o.Kind {
			case "trigger_cost_pay":
				yes = j
			case "trigger_cost_decline":
				no = j
			}
		}
		if yes >= 0 && no >= 0 {
			g.answers = append(g.answers[:i:i], append([]answer{{Kind: kind, Pick: []string{opts[yes].Label}, Seat: a.Seat}}, g.answers[i:]...)...)
			g.kinds = append(g.kinds[:i:i], append([]int{0}, g.kinds[i:]...)...)
			return true
		}
	}
	return false
}

func (g *group) placed() []answer {
	out := make([]answer, len(g.answers))
	for i, a := range g.answers {
		ks := answerKinds(a)
		k := 0
		if i < len(g.kinds) && g.kinds[i] < len(ks) {
			k = g.kinds[i]
		}
		if g.tail && ks[k] == "any" && k+1 < len(ks) {
			// an unbound choice answers a game choice, never the attack
			// or block declaration the walk passes first
			k++
		}
		out[i] = answer{Kind: ks[k], Pick: a.Pick, Seat: a.Seat}
	}
	return out
}

// takeLeftovers removes from g the answers/targets a leftover failure lists
// and returns them as a new group (nil when none matched). A choice that
// reaches the last step unconsumed is dropped: gorge never poses it, and the
// expectations still come from XMage, so dropping it cannot make a wrong
// game pass -- it only lets the asserts judge the game.
func takeLeftovers(g *group, line string) *group {
	i := strings.Index(line, "[")
	if i < 0 {
		return nil
	}
	out := &group{}
	if strings.Contains(line, "unconsumed answer(s)") {
		var left []answer
		if json.Unmarshal([]byte(line[i:]), &left) != nil {
			return nil
		}
		for _, l := range left {
			// The runner consumes a group's answers in order, so of several
			// answers with one pick (XMage's two setChoice(true): "use the
			// ability", then "pay life instead of mana") the LATER one is the
			// one left over; taking the first would move an answer a decision
			// already consumed and leave the unconsumed one in place.
			for j := len(g.answers) - 1; j >= 0; j-- {
				a := g.answers[j]
				if len(a.Pick) > 0 && len(l.Pick) > 0 && a.Pick[0] == l.Pick[0] {
					out.answers = append(out.answers, a)
					out.kinds = append(out.kinds, 0)
					g.answers = append(g.answers[:j:j], g.answers[j+1:]...)
					g.kinds = append(g.kinds[:j:j], g.kinds[j+1:]...)
					break
				}
			}
		}
	} else {
		var left []string
		if json.Unmarshal([]byte(line[i:]), &left) != nil {
			return nil
		}
		for _, l := range left {
			for j, x := range g.targets {
				if x == l {
					out.targets = append(out.targets, x)
					g.targets = append(g.targets[:j:j], g.targets[j+1:]...)
					break
				}
			}
		}
	}
	if len(out.answers)+len(out.targets) == 0 {
		return nil
	}
	return out
}

// targetAskKind picks the decision kind an XMage addTarget can answer when a
// step poses a card-choice ask instead of a target ask: the test framework's
// target queue carries both real targets and card choices (a library search,
// an encode, a surveil), and gorge poses those as choose/arrange/modes.
var reOptionSeat = regexp.MustCompile(`\[\d+ \S+ "(?:[^"\\]|\\.)*" obj=\d+ pl=(\d+)`)

// optionSeat reads the seat the pending decision's options are posed to out
// of a not-offered dump.
func optionSeat(line string) (int, bool) {
	m := reOptionSeat.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// pickSeat reads the seat a pN: pick names.
func pickSeat(pick string) (int, bool) {
	if len(pick) >= 2 && isSeatRef(pick[:2]) {
		return int(pick[1] - '0'), true
	}
	return 0, false
}

// reorderAnswerForSeat moves the first answer of the asked seat to position i
// (swapping it with the pick the runner read there). Reports whether it did.
func reorderAnswerForSeat(g *group, i, seat int) bool {
	for j := i + 1; j < len(g.answers); j++ {
		if len(g.answers[j].Pick) == 0 {
			continue
		}
		if s, ok := pickSeat(g.answers[j].Pick[0]); ok && s == seat {
			g.answers[i], g.answers[j] = g.answers[j], g.answers[i]
			g.kinds[i], g.kinds[j] = g.kinds[j], g.kinds[i]
			return true
		}
	}
	return false
}

func targetAskKind(asks []fallbackAsk) (string, bool) {
	for _, k := range []string{"choose", "arrange", "modes"} {
		for _, a := range asks {
			if a.Kind == k {
				return k, true
			}
		}
	}
	return "", false
}

// paymentChooseOnly reports whether every choose/arrange/modes ask of a step
// is a mana-payment choose ("Pay U": gorge's cost plumbing). An XMage
// addTarget can only have been meant for a card choice, so re-homing it onto
// such a choose is the unused-target-payment-choose shape, not a fix.
func paymentChooseOnly(asks []fallbackAsk) bool {
	seen := false
	for _, a := range asks {
		if a.Kind != "choose" && a.Kind != "arrange" && a.Kind != "modes" {
			continue
		}
		seen = true
		if !a.Pay {
			return false
		}
	}
	return seen
}

// mergeUnconsumedCardPick folds a leftover card choice into the earlier
// single-pick answer of the same step: XMage states one addTarget per card for
// an "and/or" search while gorge poses one multi-pick choose, so the first
// pick is consumed and the second is left over -- the two are one answer.
func mergeUnconsumedCardPick(g *group, line string) bool {
	i := strings.Index(line, "[")
	if i < 0 {
		return false
	}
	var left []answer
	if json.Unmarshal([]byte(line[i:]), &left) != nil {
		return false
	}
	merged := false
	for _, l := range left {
		if len(l.Pick) != 1 {
			continue
		}
		if _, ok := refBareName(l.Pick[0]); !ok {
			continue
		}
		ai, li := -1, -1
		for j, a := range g.answers {
			if len(a.Pick) == 0 {
				continue
			}
			if a.Pick[0] == l.Pick[0] {
				li = j
				continue
			}
			// A repeated pick of one multi-pick card choice (Garnet's
			// "each of any number of Sagas", an "and/or" search) is one
			// answer; a card pick consumed and another left over at the
			// same step are the two picks of that one answer.
			if ai < 0 && (a.Kind == "any" || a.Kind == "choose") {
				if _, ok := refBareName(a.Pick[0]); ok {
					ai = j
				}
			}
		}
		if ai < 0 || li < 0 || li < ai {
			continue
		}
		// One choose decision per seat is a separate answer, not two picks
		// of one multi-pick ask (Gandalf's "each player sacrifices a
		// creature": p0's and p1's picks are different decisions).
		a, b := g.answers[ai], g.answers[li]
		if a.Seat != nil && b.Seat != nil && *a.Seat != *b.Seat {
			continue
		}
		if as, aok := pickSeat(a.Pick[0]); aok {
			if bs, bok := pickSeat(b.Pick[0]); bok && as != bs {
				continue
			}
		}
		// Distinct card names at one step are separate choose decisions
		// (Juxtapose's tied creature and tied artifact), unless both came
		// from addTarget, which states one multi-pick search as one
		// addTarget per card. A repeated pick of the same card (Garnet's
		// "each of any number of Sagas", written with #2 ordinals) is one
		// answer too.
		if a.Kind != "choose" {
			an, _ := refBareName(a.Pick[0])
			bn, _ := refBareName(b.Pick[0])
			if !strings.EqualFold(an, bn) {
				continue
			}
		}
		g.answers[ai].Kind = "choose"
		g.answers[ai].Pick = append(g.answers[ai].Pick, l.Pick[0])
		g.answers = append(g.answers[:li:li], g.answers[li+1:]...)
		g.kinds = append(g.kinds[:li:li], g.kinds[li+1:]...)
		merged = true
	}
	return merged
}

// convertUnusedTargetsToAnswers re-homes the unused targets listed in line as
// answers of kind k, so a card choice XMage states with addTarget reaches the
// choose/arrange/modes ask it was meant for. It reports whether it moved any.
func convertUnusedTargetsToAnswers(g *group, line, kind string) bool {
	i := strings.Index(line, "[")
	if i < 0 {
		return false
	}
	var left []string
	if json.Unmarshal([]byte(line[i:]), &left) != nil {
		return false
	}
	moved := false
	for _, l := range left {
		for j, x := range g.targets {
			if x != l {
				continue
			}
			g.targets = append(g.targets[:j:j], g.targets[j+1:]...)
			g.answers = append(g.answers, answer{Kind: kind, Pick: []string{l}})
			g.kinds = append(g.kinds, 0)
			moved = true
			break
		}
	}
	return moved
}

// runResult plays r once, then -- when the run still fails an assert and
// mapped booleans onto two-option asks -- replays it with each inversion of
// up to three of those mappings (XMage's true is the first option only by
// convention), keeping the first run that reproduces every XMage assert.
func runResult(reg *cards.Registry, r *result) {
	orig, sc0 := cloneGroups(r.Groups), r.Scenario
	runResultMask(reg, r, 0)
	if r.Status == "fail-expect" && r.nflips > 0 && r.nflips <= 3 {
		for mask := uint(1); mask < 1<<uint(r.nflips); mask++ {
			r2 := *r
			r2.Groups, r2.Scenario, r2.Dropped, r2.nflips = cloneGroups(orig), sc0, 0, 0
			runResultMask(reg, &r2, mask)
			if r2.Status == "pass" {
				*r = r2
				r.Scenario.Why += " A boolean XMage answers is the affirmative option of a two-option gorge ask that is listed second."
				return
			}
			if shapeRefusalApplies(&r2) {
				*r = r2
				refuseShape(r)
				return
			}
		}
	}
	if shapeRefusalApplies(r) {
		refuseShape(r)
	}
}

// shapeRefusalApplies reports whether r's failing run is the translator shape
// r.shape names, and nothing harder. A replay diff or a harness failure is
// the audit gate's own verdict and outranks a fail-expect row, so a shape can
// never hide it; a row triaged as a known gorge bug keeps its fail-expect.
func shapeRefusalApplies(r *result) bool {
	if r.activeShape() == nil || r.Status != "fail-expect" {
		return false
	}
	if r.Scenario != nil && knownBugs[r.Scenario.XMage] != "" {
		return false
	}
	if sh := r.activeShape(); sh.onlyIf != nil && !sh.onlyIf(r.Fails) {
		return false
	}
	return r.ReplayDiff == "" && !anyHarness(r.Fails)
}

// activeShape is the shape refusal a failing run converts to: the one the
// translator derived from the test, else the one the run's transcript showed.
func (r *result) activeShape() *refusal {
	if r.shape != nil {
		return r.shape
	}
	return r.runShape
}

// refuseShape converts a failing run into the shape refusal r.shape names and
// drops the scenario, so writeCommit does not write it.
func refuseShape(r *result) {
	sh := r.activeShape()
	r.Code, r.Detail, r.Status = sh.Code, sh.Detail, "refused"
	r.Scenario = nil
}

func cloneGroups(gs []*group) []*group {
	out := make([]*group, len(gs))
	for i, g := range gs {
		c := *g
		c.targets = append([]string(nil), g.targets...)
		c.answers = append([]answer(nil), g.answers...)
		c.kinds = append([]int(nil), g.kinds...)
		c.xvals = append([]xans(nil), g.xvals...)
		out[i] = &c
	}
	return out
}

func runResultMask(reg *cards.Registry, r *result, mask uint) {
	sc := r.Scenario
	r.runShape = nil
	for _, g := range r.Groups {
		g.kinds = make([]int, len(g.answers))
	}
	var fails, transcript []string
	var replayDiff string
	flips := &boolFlips{mask: mask}
	for iter := 0; iter < 40; iter++ {
		cur := materialize(sc, r.Groups)
		f, tr, rd, err := runOne(reg, cur)
		transcript, replayDiff = tr, rd
		ngroups := len(r.Groups)
		if err != nil {
			fails = []string{"decode: " + err.Error()}
			break
		}
		fails = f
		if trimRestrictedAttack(sc, tr) {
			continue
		}
		asks := fallbackAsks(tr)
		progress := false
		// movedTo marks the steps an answer was moved onto in this pass: an
		// unused target there is judged after the answer lands, not before.
		movedTo := map[int]bool{}
		bump := func(g *group) bool {
			if g.at+g.offset+1 < len(sc.Steps) {
				g.offset++
				movedTo[g.at+g.offset] = true
				for i := range g.kinds {
					g.kinds[i] = 0
				}
				progress = true
				return true
			}
			return false
		}
		for _, line := range f {
			m := reStepFail.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			n, _ := strconv.Atoi(m[1])
			if n < len(sc.Steps) && skipEmptyDeclaration(sc.Steps[n], line) {
				progress = true
				continue
			}
			var at []*group
			for _, g := range r.Groups[:ngroups] {
				if g.at+g.offset == n {
					at = append(at, g)
				}
			}
			if len(at) == 0 {
				continue
			}
			if len(boolLeftovers(line)) > 0 {
				// an XMage boolean gorge asks as a two-option choose or
				// modes: place it as that ask's option, not as "yes"/"no"
				if mapBoolLeftover(reg, cur, at, n, asks[n], flips) {
					progress = true
					continue
				}
			}
			if cm := reCountMismatch.FindStringSubmatch(line); cm != nil {
				want, _ := strconv.Atoi(cm[1])
				for _, g := range at {
					if want > 1 && mergePicks(g, want) {
						progress = true
						break
					}
				}
				continue
			}
			if nm := reNotOffered.FindStringSubmatch(line); nm != nil {
				done := false
				for _, g := range at {
					for i, a := range g.answers {
						if done || a.Pick[0] != nm[1] {
							continue
						}
						// gorge and XMage need not ask the same seat's
						// card choice first (Juxtapose asks the defender
						// before the caster). The pick for the asked seat
						// is in the group, just at another position: put
						// it where the runner reads answers from.
						if nm[2] == "choose" || nm[2] == "modes" {
							if askSeat, ok := optionSeat(line); ok {
								if ps, ok := pickSeat(a.Pick[0]); ok && ps != askSeat {
									if reorderAnswerForSeat(g, i, askSeat) {
										progress, done = true, true
										continue
									}
								}
							}
						}
						if rewriteOfferedLabel(g, i, nm[2], parseOptionDump(line)) {
							progress, done = true, true
							continue
						}
						ks := answerKinds(a)
						k := g.kinds[i] + 1
						for k < len(ks) && ks[k] == nm[2] {
							k++
						}
						if k < len(ks) {
							g.kinds[i] = k
							progress = true
						} else if i > 0 && splitUnaskedTail(r, g, i, n, len(sc.Steps)) {
							// the answers before i were consumed (the runner
							// takes a group in order): only i and what follows
							// is unasked at this step, so only that moves on
							progress = true
						} else if !bump(g) && g.tail {
							// a choice of a method with no action that no
							// step can place: drop it, as before it had a
							// home (the asserts still judge the game)
							r.Dropped += len(g.answers)
							g.answers, g.kinds = nil, nil
							progress = true
						}
						done = true
					}
				}
				if !done {
					for _, g := range at {
						bump(g)
					}
				}
				continue
			}
			if strings.Contains(line, "unconsumed answer(s)") || strings.Contains(line, "unused target(s)") {
				// XMage's addTarget queue also answers card-choice asks
				// (a library search, an encode, a surveil). Gorge poses
				// those as choose/arrange/modes, which cannot consume a
				// step target, so an unused target that matches such an
				// ask is re-homed as its answer before the leftover is
				// moved on to the next step.
				if strings.Contains(line, "unused target(s)") {
					if movedTo[n] {
						// An answer XMage states before this target (the clone
						// election, then Gilded Drake's exchange target) moves
						// onto this step in the same pass and takes the first
						// choose ask; converting the target now would answer
						// that ask with the target instead.
						progress = true
						continue
					}
					if kind, ok := targetAskKind(asks[n]); ok && paymentChooseOnly(asks[n]) {
						// The only choose is gorge's mana-payment plumbing
						// ("Pay U"), which no addTarget answers: the targets
						// stay on the step (where the run may still reproduce
						// every assert) and move on as leftovers, and the
						// row refuses only if it then fails.
						r.shape = refuse("unused-target-payment-choose", "an XMage addTarget can only answer a card choice; the step poses only a mana-payment choose")
					} else if ok {
						for _, g := range at {
							if convertUnusedTargetsToAnswers(g, line, kind) {
								progress = true
							}
						}
						if progress {
							continue
						}
					}
				}
				// A repeated pick of one choose ask (XMage's addTarget per
				// card for an "and/or" search) is one multi-pick answer:
				// the first pick is consumed and the leftover rides it.
				if strings.Contains(line, "unconsumed answer(s)") {
					for _, g := range at {
						if mergeUnconsumedCardPick(g, line) {
							progress = true
						}
					}
					if progress {
						continue
					}
				}
				// Only the LEFTOVERS move on: what this step consumed stays.
				for _, g := range at {
					left := takeLeftovers(g, line)
					if left == nil {
						continue
					}
					progress = true
					if n+1 < len(sc.Steps) {
						left.at = n + 1
						movedTo[n+1] = true
						r.Groups = append(r.Groups, left)
					} else {
						r.Dropped += len(left.answers) + len(left.targets)
					}
				}
				continue
			}
			if strings.Contains(line, "harness: target ") && strings.Contains(line, "not offered") {
				for _, g := range at {
					bump(g)
				}
			}
		}
		if !progress && len(f) > 0 {
			// An unless-cost GenericChoice's two modes asks: the boolean is
			// consumed by the first (by a substring of its text), so no
			// placement failure names it -- only the second ask falls back.
			ma := modesAsks(tr)
			steps := make([]int, 0, len(ma))
			for n := range ma {
				steps = append(steps, n)
			}
			sort.Ints(steps)
		chain:
			for _, n := range steps {
				var at []*group
				for _, g := range r.Groups {
					if g.at+g.offset == n {
						at = append(at, g)
					}
				}
				if len(at) == 0 || n >= len(sc.Steps) {
					continue
				}
				for _, seat := range ma[n] {
					if mapUnlessChain(reg, cur, at, n, seat) {
						progress = true
						break chain
					}
				}
			}
		}
		if !progress {
			break
		}
	}
	// freeze the placement into the scenario
	final := materialize(sc, r.Groups)
	r.XAnswers = map[int][]xans{}
	for _, g := range r.Groups {
		i := min(g.at+g.offset, len(sc.Steps)-1)
		r.XAnswers[i] = append(r.XAnswers[i], g.xvals...)
	}
	r.Scenario = final
	r.Groups = nil
	r.Fails = fails
	r.nflips = flips.seq
	if r.Dropped > 0 {
		r.Scenario.Why += fmt.Sprintf(" %d XMage choice(s) gorge never asks were dropped.", r.Dropped)
	}
	if len(fails) > 0 && r.Dropped > 0 && r.Strict && knownBugs[sc.XMage] == "" {
		// The test states choices gorge never asks where XMage does, or
		// asks where XMage does not, so the answers found no home:
		//   - a permanent in the setup triggers before the first step (a
		//     turn-1 upkeep trigger) and no step can carry an answer to the
		//     setup, so the runner took its fallback;
		//   - gorge poses a bare "Continue" acknowledgement of a
		//     look-at-top reveal (Birthing Ritual) that XMage never asks,
		//     which the stated picks are then offered to.
		// Neither refuses here: a replay diff or a harness failure is the
		// audit gate's own verdict and outranks them, and the boolean-mask
		// retries may still reproduce every assert. runResult applies the
		// refusal after those, through shapeRefusalApplies.
		if k := setupDecision(transcript); k != "" {
			r.runShape = refuse("setup-decision", "a %s decision is posed before the first step", k)
		} else if hasContinueAck(transcript) {
			r.runShape = refuse("continue-acknowledgement", "gorge poses a bare Continue acknowledgement XMage never asks")
		}
	}
	if len(fails) > 0 && r.runShape == nil && knownBugs[sc.XMage] == "" && declarationAnswer(transcript) {
		r.runShape = refuse("choice-consumed-by-declaration", "an XMage choice gorge never asks was taken by an attack or block declaration")
	}
	if k := aiChoiceFallback(transcript); len(fails) > 0 && !r.Strict && k != "" && knownBugs[sc.XMage] == "" {
		// Without setStrictChooseMode(true) XMage's AI answered what the
		// test does not state; the runner's fallback is not that AI, so
		// the failure says nothing about gorge -- unless triage traced it
		// to a gorge bug (known_bugs.tsv), which keeps it as a known_bug.
		if alt, why, ok := tryFallbackAlternatives(reg, r.Scenario, transcript); ok {
			// another answer to that one decision reproduces every XMage
			// assert: keep it, written down on the fallback's step
			r.Scenario = alt
			r.Scenario.Why += why
			r.Fails, r.ReplayDiff, r.Status = nil, "", "pass"
			return
		}
		r.Code, r.Detail, r.Status = "nonstrict-ai-choice", "unstated "+k+" answered by the runner's fallback", "refused"
		r.Scenario = nil
		return
	}
	r.ReplayDiff = replayDiff
	r.Status = decideStatus(fails, replayDiff)
}

// decideStatus maps a finished run to a result status. A non-empty replayDiff
// (the live game and its log-only replay disagree) is a hard failure of the
// audit gate whatever the expectations say, so it outranks pass and
// fail-expect; writeCommit writes only pass and known_bug rows, and
// fail-replay is neither.
func decideStatus(fails []string, replayDiff string) string {
	switch {
	case anyHarness(fails):
		return "fail-harness"
	case replayDiff != "":
		return "fail-replay"
	case len(fails) == 0:
		return "pass"
	default:
		return "fail-expect"
	}
}

func anyHarness(fails []string) bool {
	for _, f := range fails {
		if strings.Contains(f, "harness:") || strings.Contains(f, "unconsumed answer(s)") || strings.Contains(f, "unused target(s)") || strings.HasPrefix(f, "decode:") {
			return true
		}
	}
	return false
}

func runAll(reg *cards.Registry, results []*result) {
	work := make(chan *result)
	var wg sync.WaitGroup
	n := runtime.GOMAXPROCS(0)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range work {
				func() {
					defer func() {
						if p := recover(); p != nil {
							r.Fails = []string{fmt.Sprintf("harness: panic %v", p)}
							r.Status = "fail-harness"
						}
					}()
					runResult(reg, r)
				}()
			}
		}()
	}
	for _, r := range results {
		if r.Scenario != nil {
			work <- r
		}
	}
	close(work)
	wg.Wait()
}

// driverLine renders r's scenario as an input line for the XMage oracle
// driver (scripts/xmage-oracle-run.sh): gorge-only `pass` steps and gorge
// answer placements are removed, and XMage gets the test's ORIGINAL choices
// (xmage_answers) and activateAbility texts (xmage_ability) instead.
func driverLine(r *result) ([]byte, error) {
	type dstep struct {
		*step
		Answers []answer `json:"answers,omitempty"`
	}
	var steps []any
	var xab []string
	var xan [][]xans
	var carry []xans
	for i, s := range r.Scenario.Steps {
		carry = append(carry, r.XAnswers[i]...)
		if s.Op == "pass" || s.payment {
			continue
		}
		c := *s
		c.Answers = nil
		steps = append(steps, dstep{step: &c})
		xab = append(xab, s.XAbility)
		xan = append(xan, carry)
		carry = nil
	}
	if len(carry) > 0 && len(xan) > 0 {
		xan[len(xan)-1] = append(xan[len(xan)-1], carry...)
	}
	for i := range xan {
		if xan[i] == nil {
			xan[i] = []xans{}
		}
	}
	sc := r.Scenario
	return json.Marshal(map[string]any{
		"name": r.Class + "#" + r.Method, "cr": sc.CR, "why": sc.Why, "setup": sc.Setup,
		"steps": steps, "expect": sc.Expect, "xmage_ability": xab, "xmage_answers": xan,
	})
}
