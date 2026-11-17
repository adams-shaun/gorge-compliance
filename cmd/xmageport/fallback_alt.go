package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Without setStrictChooseMode(true) XMage's AI answers a choice the test never
// states (a trigger order, CR 603.3b, or a single target), and the runner's
// fallback (option 0) is not that AI. When the fallback run fails an assert,
// the scenario is still a valid interaction scenario if SOME other answer to
// that one decision reproduces every XMage assert: tryFallbackAlternatives
// replays the scenario once per answer and, when one passes, returns it with
// that answer written down on the fallback's step.

const (
	// maxFallbackReplays caps the replays per method (4 triggers: 4! = 24).
	maxFallbackReplays = 24
	maxTriggerOrder    = 4
	maxTargetOptions   = 8
)

// fallbackAlternatives lists every answer (the picked option labels, in
// order) to a trigger_order ask of 2..4 triggers (all their orders) or a
// target ask of 1..8 options (each one), in enumeration order: lexicographic
// by option index. Any other ask, or one past the caps, yields nil. Answers
// with the same label sequence are listed once.
func fallbackAlternatives(kind string, opts []dumpOption) [][]string {
	var out [][]string
	seen := map[string]bool{}
	add := func(labels []string) {
		key := strings.Join(labels, "\x00")
		if !seen[key] {
			seen[key] = true
			out = append(out, labels)
		}
	}
	switch kind {
	case "trigger_order":
		if len(opts) < 2 || len(opts) > maxTriggerOrder {
			return nil
		}
		idx := make([]int, len(opts))
		for i := range idx {
			idx[i] = i
		}
		var perm func(prefix []int, rest []int)
		perm = func(prefix, rest []int) {
			if len(rest) == 0 {
				labels := make([]string, len(prefix))
				for i, p := range prefix {
					labels[i] = opts[p].Label
				}
				add(labels)
				return
			}
			for i, r := range rest {
				next := append(append([]int(nil), rest[:i]...), rest[i+1:]...)
				perm(append(append([]int(nil), prefix...), r), next)
			}
		}
		perm(nil, idx)
	case "target":
		if len(opts) < 1 || len(opts) > maxTargetOptions {
			return nil
		}
		for _, o := range opts {
			add([]string{o.Label})
		}
	}
	return out
}

// fallbackPoint is the first real decision the runner answered by its
// fallback: the step it was posed in, its seat, kind and the labels picked.
type fallbackPoint struct {
	Step, Seat int
	Kind       string
	Picked     []string
}

var (
	reFallbackPick = regexp.MustCompile(`^\s+\[\w+ fallback\] p(\d) (\w+) -> (.*)$`)
	reQuoted       = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)

// firstFallbackPoint locates the first fallback aiChoiceFallback reports. A
// fallback before the first step (the setup drive) has no step to carry an
// answer, so it reports false.
func firstFallbackPoint(transcript []string) (fallbackPoint, bool) {
	step := -1
	for _, l := range transcript {
		if reTranscriptStep.MatchString(l) {
			step++
			continue
		}
		m := reFallbackPick.FindStringSubmatch(l)
		if m == nil || m[2] == "attackers" || m[2] == "blockers" {
			continue
		}
		if step < 0 {
			return fallbackPoint{}, false
		}
		seat, _ := strconv.Atoi(m[1])
		fp := fallbackPoint{Step: step, Seat: seat, Kind: m[2], Picked: []string{}}
		for _, q := range reQuoted.FindAllString(m[3], -1) {
			s, err := strconv.Unquote(q)
			if err != nil {
				s = q
			}
			fp.Picked = append(fp.Picked, s)
		}
		return fp, true
	}
	return fallbackPoint{}, false
}

// tryFallbackAlternatives replays sc once per alternative answer to the first
// fallback decision of the failed run that produced transcript. It returns
// the scenario with the first passing answer written on the fallback's step,
// the why sentence to append, and ok; ok is false when no answer passes or
// the fallback is not a trigger order / single target.
func tryFallbackAlternatives(reg *cards.Registry, sc *portScenario, transcript []string) (*portScenario, string, bool) {
	fp, ok := firstFallbackPoint(transcript)
	if !ok || (fp.Kind != "trigger_order" && fp.Kind != "target") {
		return nil, "", false
	}
	opts := probeOptions(reg, sc, fp.Step, fp.Kind)
	if fp.Step >= len(sc.Steps) || (fp.Kind == "trigger_order" && len(fp.Picked) != len(opts)) ||
		(fp.Kind == "target" && len(fp.Picked) != 1) {
		return nil, "", false
	}
	alts := fallbackAlternatives(fp.Kind, opts)
	var first *portScenario
	passes, tried, replays := 0, 0, 0
	for _, labels := range alts {
		if strings.Join(labels, "\x00") == strings.Join(fp.Picked, "\x00") {
			tried++ // the fallback's own answer: the run that failed
			continue
		}
		if replays >= maxFallbackReplays {
			break
		}
		replays++
		cp := *sc
		cp.Steps = append([]*step(nil), sc.Steps...)
		s := *cp.Steps[fp.Step]
		seat := fp.Seat
		s.Answers = append(append([]answer(nil), s.Answers...), answer{Kind: fp.Kind, Pick: labels, Seat: &seat})
		cp.Steps[fp.Step] = &s
		res, _, err := execScenario(reg, &cp)
		if err != nil {
			continue
		}
		fails, tr, rd := res.Fails, res.Transcript, res.ReplayDiff
		// the answer must have landed on the intended ask (a pick is a label
		// substring, so an ambiguous label can be taken by another option)
		want := fmt.Sprintf("[answer] p%d %s -> %q", fp.Seat, fp.Kind, labels)
		landed := false
		for _, l := range tr {
			landed = landed || strings.Contains(l, want)
		}
		if !landed {
			continue
		}
		tried++
		if decideStatus(fails, rd) != "pass" || aiChoiceFallback(tr) != "" {
			continue
		}
		passes++
		if first == nil {
			first = &cp
		}
	}
	if first == nil {
		return nil, "", false
	}
	what := "trigger order"
	if fp.Kind == "target" {
		what = "target"
	}
	return first, fmt.Sprintf(" %s chosen to match XMage's unstated AI choice; %d of %d answers pass.", what, passes, tried), true
}
