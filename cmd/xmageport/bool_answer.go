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
}

var reDumpOption = regexp.MustCompile(`\[(\d+) (\S+) ("(?:[^"\\]|\\.)*") obj=`)

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
		out = append(out, dumpOption{Index: i, Kind: m[2], Label: l})
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
	p := strings.ToLower(pick)
	if p != "yes" && p != "no" {
		return "", false
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
		return "", false
	}
	for _, o := range opts {
		k, l := strings.ToLower(o.Kind), strings.ToLower(o.Label)
		if k == "yes" || k == "no" || k == "accept" || k == "decline" || strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "no") {
			return "", false // a yes/no ask: the plain answer already lands
		}
	}
	yes, no := opts[0], opts[1]
	if a, b := strings.HasSuffix(opts[0].Kind, "_pay"), strings.HasSuffix(opts[1].Kind, "_pay"); a != b {
		if b {
			yes, no = opts[1], opts[0]
		}
	}
	o := no
	if p == "yes" {
		o = yes
	}
	if o.Label == "" {
		return o.Kind, true
	}
	return o.Label, true
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

// mapBoolLeftover rewrites, in the groups placed at step n, abstract yes/no
// answers as the matching option of the two-option choose/modes asks step n
// poses (asks: the step's fallback decisions). At most one answer is
// rewritten per (seat, kind): the probe shows the FIRST such ask only, so a
// second same-kind ask (a real yes/no after it, say) is learned from the next
// run, once the first is consumed. Reports whether it rewrote any.
func mapBoolLeftover(reg *cards.Registry, cur *portScenario, groups []*group, n int, asks []fallbackAsk) bool {
	mapped := false
	tried := map[fallbackAsk]bool{}
	for _, ak := range asks {
		if (ak.Kind != "choose" && ak.Kind != "modes") || tried[ak] {
			continue
		}
		tried[ak] = true
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
				label, ok := mapBoolAnswer(a.Pick[0], opts)
				if !ok {
					break find
				}
				g.answers[i] = answer{Kind: ak.Kind, Pick: []string{label}, Seat: a.Seat}
				mapped = true
				break find
			}
		}
	}
	return mapped
}
