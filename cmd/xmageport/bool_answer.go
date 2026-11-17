package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// XMage asks a boolean choice (setChoice(player, true|false)) of its own
// prompt; gorge asks the same decision as a two-option choose or modes ask
// ("Pay 1 per age" / "Sacrifice X", a villainous choice's two texts) that
// has no yes/no-shaped option, so the runner's abstract "yes"/"no" never
// lands. mapBoolAnswer re-expresses the boolean as one of the two options.

// dumpOption is one option of a pending decision, as the runner's
// not-offered failure prints it.
type dumpOption struct {
	Index int
	Kind  string
	Label string
	Mode  string
}

var reDumpOption = regexp.MustCompile(`\[(\d+) (\S+) ("(?:[^"\\]|\\.)*") obj=\d+ pl=\d+ att=\d+ mode=([^\]\s]*)`)

// parseOptionDump reads the option list out of a "not offered ...: <dump>"
// failure line.
func parseOptionDump(line string) []dumpOption {
	var out []dumpOption
	for _, m := range reDumpOption.FindAllStringSubmatch(line, -1) {
		i, _ := strconv.Atoi(m[1])
		l, err := strconv.Unquote(m[3])
		if err != nil {
			l = m[3]
		}
		out = append(out, dumpOption{Index: i, Kind: m[2], Label: l, Mode: m[4]})
	}
	return out
}

// mapBoolAnswer maps a "yes"/"no" pick onto a decision of exactly two
// options, neither yes/no-shaped: XMage's true is the first choice and false
// the second. A cumulative-upkeep style ask (exactly one option whose Kind
// ends "_pay"; the "_manual" payment variant is not counted) is matched by
// Kind, not position: the payment is yes, the other option (the sacrifice)
// is no. Reports false for anything else.
func mapBoolAnswer(pick string, opts []dumpOption) (string, bool) {
	label, _, ok := mapBoolPair(pick, opts)
	return label, ok
}

// mapBoolPair is mapBoolAnswer that also returns the label of the OTHER
// option, so a caller can flip the mapping (see tryBoolFlips).
func mapBoolPair(pick string, opts []dumpOption) (label, other string, ok bool) {
	p := strings.ToLower(pick)
	if p != "yes" && p != "no" {
		return "", "", false
	}
	// "choose mana sources" variants of a payment are the same answer as
	// the payment (cumulative_pay_manual): not a third choice
	var base []dumpOption
	for _, o := range opts {
		if !strings.HasSuffix(o.Kind, "_manual") {
			base = append(base, o)
		}
	}
	if opts = base; len(opts) != 2 {
		return "", "", false
	}
	for _, o := range opts {
		k, l := strings.ToLower(o.Kind), strings.ToLower(o.Label)
		if k == "yes" || k == "no" || k == "accept" || k == "decline" || strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "no") {
			return "", "", false // a yes/no ask: the plain answer already lands
		}
	}
	yes, no := opts[0], opts[1]
	if a, b := strings.HasSuffix(opts[0].Kind, "_pay"), strings.HasSuffix(opts[1].Kind, "_pay"); a != b {
		if b {
			yes, no = opts[1], opts[0]
		}
	}
	o, x := no, yes
	if p == "yes" {
		o, x = yes, no
	}
	name := func(o dumpOption) string {
		if o.Label == "" {
			return o.Kind
		}
		return o.Label
	}
	return name(o), name(x), true
}

// boolFlips counts, and optionally inverts, the booleans a run writes as
// options of a two-option ask: XMage's true is the first choice only by
// convention, and some asks (Sigurd's "put another counter", Indomitable
// Might's "as though not blocked", Emeria Shepherd's "battlefield") list the
// affirmative option second. A run maps its seq-th boolean inverted when bit
// seq of mask is set; runResult replays a failing run with each mask (see
// runResult), keeping the first that reproduces every XMage assert.
type boolFlips struct {
	mask uint
	seq  int // booleans mapped so far
}

func (f *boolFlips) next() (invert bool) {
	invert = f.mask&(1<<uint(f.seq)) != 0
	f.seq++
	return invert
}

type fallbackAsk struct {
	Seat int
	Kind string
}

var (
	reTranscriptStep = regexp.MustCompile(`^step [a-z_]+(?: |$)`)
	reFallbackAsk    = regexp.MustCompile(`^\s+\[\w+ fallback\] p(\d) (\w+) ->`)
)

// fallbackAsks lists, per step index, the decisions the runner answered by
// its fallback during that step: the asks no scenario answer consumed.
func fallbackAsks(transcript []string) map[int][]fallbackAsk {
	out := map[int][]fallbackAsk{}
	step := -1
	for _, l := range transcript {
		if reTranscriptStep.MatchString(l) {
			step++
			continue
		}
		if m := reFallbackAsk.FindStringSubmatch(l); m != nil && step >= 0 {
			s, _ := strconv.Atoi(m[1])
			out[step] = append(out[step], fallbackAsk{Seat: s, Kind: m[2]})
		}
	}
	return out
}

var reModesAsk = regexp.MustCompile(`^\s+\[(?:\w+ fallback|answer)\] p(\d) modes ->`)

// modesAsks lists, per step index, the seats a modes decision was posed to
// during that step, whether the runner answered it from a scenario answer or
// by its fallback.
func modesAsks(transcript []string) map[int][]int {
	out := map[int][]int{}
	step := -1
	for _, l := range transcript {
		if reTranscriptStep.MatchString(l) {
			step++
			continue
		}
		if m := reModesAsk.FindStringSubmatch(l); m != nil && step >= 0 {
			s, _ := strconv.Atoi(m[1])
			out[step] = append(out[step], s)
		}
	}
	return out
}

// boolLeftovers lists the abstract yes/no picks a not-offered or unconsumed
// placement failure names.
func boolLeftovers(line string) []string {
	isBool := func(p string) bool { p = strings.ToLower(p); return p == "yes" || p == "no" }
	var out []string
	if nm := reNotOffered.FindStringSubmatch(line); nm != nil {
		if isBool(nm[1]) {
			out = append(out, nm[1])
		}
		return out
	}
	i := strings.Index(line, "[")
	if i < 0 || !strings.Contains(line, "unconsumed answer(s)") {
		return nil
	}
	var left []answer
	if json.Unmarshal([]byte(line[i:]), &left) != nil {
		return nil
	}
	for _, l := range left {
		if len(l.Pick) == 1 && isBool(l.Pick[0]) {
			out = append(out, l.Pick[0])
		}
	}
	return out
}

const probePick = "\x00bool-answer-probe"

// probeOptions learns the options of the first choose/modes decision of
// kind that step n poses: it queues an answer no option matches and reads
// the runner's not-offered dump. nil when the step poses no such ask.
func probeOptions(reg *cards.Registry, cur *portScenario, n int, kind string) []dumpOption {
	cp := *cur
	cp.Steps = append([]*step(nil), cur.Steps...)
	s := *cp.Steps[n]
	s.Answers = append(append([]answer(nil), s.Answers...), answer{Kind: kind, Pick: []string{probePick}})
	cp.Steps[n] = &s
	res, _, err := execScenario(reg, &cp)
	if err != nil {
		return nil
	}
	prefix := "step " + strconv.Itoa(n) + " ("
	want := "not offered for " + kind + ":"
	for _, f := range res.Fails {
		if strings.HasPrefix(f, prefix) && strings.Contains(f, "harness: answer ") && strings.Contains(f, want) {
			return parseOptionDump(f[strings.Index(f, want):])
		}
	}
	return nil
}

// A GenericChoice with an unless cost (Starseer Mentor: "loses 3 life unless
// they sacrifice a nonland permanent or discard a card") is two modes asks in
// gorge -- which cost, then pay it or not (modes of kind unless_pay /
// unless_decline) -- where XMage asks booleans: "pay the cost?", and with two
// costs "sacrifice?" (true: the first cost, false: the other).
func isUnlessPay(opts []dumpOption) bool {
	for _, o := range opts {
		if o.Mode == "unless_pay" || o.Mode == "unless_decline" {
			return true
		}
	}
	return false
}

// mapUnlessChain rewrites the seat's boolean answers at step n as the two
// modes answers of an unless-cost GenericChoice. Reports whether it did.
func mapUnlessChain(reg *cards.Registry, cur *portScenario, groups []*group, n int, seat int) bool {
	type pos struct {
		g *group
		i int
	}
	var bools []pos
	for _, g := range groups {
		for i, a := range g.answers {
			if a.Kind == "any" && len(a.Pick) == 1 && (a.Seat == nil || *a.Seat == seat) {
				if p := strings.ToLower(a.Pick[0]); p == "yes" || p == "no" {
					bools = append(bools, pos{g, i})
				}
			}
		}
	}
	if len(bools) == 0 || len(bools) > 2 {
		return false
	}
	// step n without the booleans: the probes must reach the modes asks
	cp := *cur
	cp.Steps = append([]*step(nil), cur.Steps...)
	s := *cp.Steps[n]
	s.Answers = nil
	for _, a := range cur.Steps[n].Answers {
		if p := strings.ToLower(a.Pick[0]); !(a.Kind == "any" && len(a.Pick) == 1 && (p == "yes" || p == "no")) {
			s.Answers = append(s.Answers, a)
		}
	}
	cp.Steps[n] = &s
	opts1 := probeOptions(reg, &cp, n, "modes")
	if len(opts1) == 0 || len(opts1) > 2 || isUnlessPay(opts1) {
		return false
	}
	first := opts1[0]
	if len(opts1) == 2 && len(bools) == 2 && strings.ToLower(groupAnswer(bools[0]).Pick[0]) == "yes" && strings.ToLower(groupAnswer(bools[1]).Pick[0]) == "no" {
		first = opts1[1]
	}
	s2 := s
	s2.Answers = append(append([]answer(nil), s.Answers...), answer{Kind: "modes", Pick: []string{first.Label}, Seat: &seat})
	cp.Steps[n] = &s2
	opts2 := probeOptions(reg, &cp, n, "modes")
	if len(opts2) != 2 || !isUnlessPay(opts2) {
		return false
	}
	pay := strings.ToLower(groupAnswer(bools[0]).Pick[0]) == "yes"
	var second dumpOption
	for _, o := range opts2 {
		if (pay && o.Mode == "unless_pay") || (!pay && o.Mode == "unless_decline") {
			second = o
		}
	}
	if second.Label == "" {
		return false
	}
	g0 := bools[0].g
	// the first boolean becomes the two modes answers; the second (which
	// cost) is absorbed into the first pick
	g0.answers = append(g0.answers[:bools[0].i:bools[0].i], append([]answer{
		{Kind: "modes", Pick: []string{first.Label}, Seat: &seat},
		{Kind: "modes", Pick: []string{second.Label}, Seat: &seat}}, g0.answers[bools[0].i+1:]...)...)
	g0.kinds = append(g0.kinds[:bools[0].i:bools[0].i], append([]int{0, 0}, g0.kinds[bools[0].i+1:]...)...)
	if len(bools) == 2 {
		g1, i1 := bools[1].g, bools[1].i
		if g1 == g0 && i1 > bools[0].i {
			i1++ // the first boolean became two answers
		}
		g1.answers = append(g1.answers[:i1:i1], g1.answers[i1+1:]...)
		g1.kinds = append(g1.kinds[:i1:i1], g1.kinds[i1+1:]...)
	}
	return true
}

func groupAnswer(p struct {
	g *group
	i int
}) answer {
	return p.g.answers[p.i]
}

// mapBoolLeftover rewrites, in the groups placed at step n, abstract yes/no
// answers as the matching option of the two-option choose/modes asks step n
// poses (asks: the step's fallback decisions). At most one answer is
// rewritten per (seat, kind): the probe shows the FIRST such ask only, so a
// second same-kind ask (a real yes/no after it, say) is learned from the next
// run, once the first is consumed. Reports whether it rewrote any.
func mapBoolLeftover(reg *cards.Registry, cur *portScenario, groups []*group, n int, asks []fallbackAsk, flips *boolFlips) bool {
	mapped := false
	tried := map[fallbackAsk]bool{}
	for _, ak := range asks {
		if (ak.Kind != "choose" && ak.Kind != "modes") || tried[ak] {
			continue
		}
		tried[ak] = true
		if ak.Kind == "modes" && mapUnlessChain(reg, cur, groups, n, ak.Seat) {
			mapped = true
			continue
		}
		var opts []dumpOption
		probed := false
	find:
		for _, g := range groups {
			for i, a := range g.answers {
				if a.Kind != "any" || len(a.Pick) != 1 || (a.Seat != nil && *a.Seat != ak.Seat) {
					continue
				}
				if p := strings.ToLower(a.Pick[0]); p != "yes" && p != "no" {
					continue
				}
				if !probed {
					opts, probed = probeOptions(reg, cur, n, ak.Kind), true
				}
				label, other, ok := mapBoolPair(a.Pick[0], opts)
				if !ok {
					break find
				}
				if flips != nil && flips.next() {
					label = other
				}
				g.answers[i] = answer{Kind: ak.Kind, Pick: []string{label}, Seat: a.Seat}

				mapped = true
				break find
			}
		}
	}
	return mapped
}
