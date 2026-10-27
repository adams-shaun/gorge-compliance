// Package classify turns the comparison of one lowered xengine item against
// one follower engine's replay of it into a Finding: one class per (item,
// engine), first match wins (DESIGN §4.1). It reuses oraclediff.CompareOpts
// unchanged, so the first difference is the one every other oracle pass
// reports; the classifier only decides what KIND of difference it is.
package classify

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The classes, in the order Classify tries them.
const (
	ClassLowerGap      = "lower_gap"
	ClassXMageLacks    = "xmage_lacks"
	ClassHarness       = "harness"
	ClassDecisionShape = "decision_shape"
	ClassRNG           = "rng"
	ClassProjectionGap = "projection_gap"
	ClassDiverge       = "diverge"
	ClassAgree         = "agree"
)

// Classes lists every class, in report order.
var Classes = []string{
	ClassAgree, ClassDiverge, ClassRNG, ClassProjectionGap, ClassDecisionShape,
	ClassHarness, ClassXMageLacks, ClassLowerGap,
}

// Finding is the classified outcome for one item and one follower engine.
type Finding struct {
	ID     string `json:"id"`
	Engine string `json:"engine"`
	Class  string `json:"class"`
	// Checkpoint and Field are the first difference (CompareOpts); Gorge and
	// Other its two values (Other is the follower's).
	Checkpoint string `json:"checkpoint,omitempty"`
	Field      string `json:"field,omitempty"`
	Gorge      string `json:"gorge,omitempty"`
	Other      string `json:"other,omitempty"`
	Msg        string `json:"msg,omitempty"`
	// Step is the scenario step of the checkpoint (-1 for "setup" and for a
	// finding with no checkpoint).
	Step int `json:"step"`
}

// GapFinding is the lower_gap finding of an item whose self-check did not
// pass. Such an item is never sent to a follower, so it has no engine.
func GapFinding(item lower.Lowered, c lower.Check) Finding {
	return Finding{
		ID: item.Item.ID, Class: ClassLowerGap, Step: c.Step,
		Field: c.Field, Gorge: c.Gorge, Other: c.Tape,
		Msg: fmt.Sprintf("self-check %s at step %d", c.Status, c.Step),
	}
}

// Classify compares gorge's result g for item with the follower's x and
// classes the difference. t is the tape the item was lowered from (it may be
// nil, in which case nothing is ever an rng finding). engine is "xmage" or
// "forge".
func Classify(item lower.Lowered, t *game.Tape, g rules.OracleResult, x oraclediff.XResult, engine string) Finding {
	f := Finding{ID: item.Item.ID, Engine: engine, Step: -1}
	v := oraclediff.CompareOpts(g, nil, x, item.Item.Compare, item.Item.Ignore...)
	f.Checkpoint, f.Field, f.Gorge, f.Other = v.Checkpoint, v.Field, v.Gorge, v.XMage
	f.Step = stepOf(v.Checkpoint)

	switch {
	case v.Status == oraclediff.XMageLacks ||
		(v.Status == oraclediff.Harness && engine == "xmage" && xmageLacks(v.Msg, t)):
		f.Class, f.Msg = ClassXMageLacks, v.Msg
	case v.Status == oraclediff.Harness:
		who := v.Engine
		if who == "xmage" {
			who = engine // CompareOpts names the follower "xmage" whichever it is
		}
		f.Class, f.Msg = ClassHarness, who+": "+v.Msg
	case (x.StrictMiss != "" || x.Leftover != "") && !trailingMiss(x, len(item.Item.Steps), engine):
		f.Class = ClassDecisionShape
		f.Msg = strings.TrimSpace("strict_miss: " + x.StrictMiss + " leftover: " + x.Leftover)
	case v.Status == oraclediff.Agree:
		f.Class = ClassAgree
	case rngField(v.Field) && rngDrawn(item, t, f.Step):
		f.Class = ClassRNG
	case projectionGap(v):
		f.Class = ClassProjectionGap
	default:
		f.Class = ClassDiverge
	}
	return f
}

// missStep matches the step of the only two strict misses Forge poses past the
// item's last step: "Missing cleanup discard answer at step 12: ..." and
// "Missing yesno answer at step 6: optional trigger ...". Any other kind
// (order, surveil, target, mode, ...) is a real shape finding wherever it sits.
var missStep = regexp.MustCompile(`^Missing (?:cleanup discard answer at step (\d+)|yesno answer at step (\d+): optional trigger)`)

// trailingMiss reports a Forge strict miss that is only a question Forge poses
// after the item's last step and nothing else: a cleanup discard when the hand
// is over seven, or an optional trigger's yes/no once the last pass lets the
// trigger resolve. Forge asks it inside that last pass, while gorge stops at
// the pass with the same decision pending and unlogged, so no checkpoint
// observes it and it says nothing about decision shape. The comparison of
// every snapshot then decides the class. A leftover (gorge answered something
// Forge never asked) is a real shape difference, so it is never trailing, and
// a miss at any step the item scripts, or of any other kind, is not either.
func trailingMiss(x oraclediff.XResult, steps int, engine string) bool {
	if engine != "forge" || x.Leftover != "" || x.StrictMiss == "" {
		return false
	}
	m := missStep.FindStringSubmatch(x.StrictMiss)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1] + m[2])
	return err == nil && n == steps
}

// Credited reports whether an item with this class counts toward the
// "compared" coverage counter: the follower replayed it to a verdict about
// the engine (agree or any divergence), not a gap in a driver or the lowering.
func Credited(class string) bool {
	switch class {
	case ClassAgree, ClassDiverge, ClassRNG, ClassProjectionGap, ClassDecisionShape:
		return true
	}
	return false
}

// stepOf is the scenario step of a checkpoint name: "setup" is -1,
// "step N (op)" is N, anything else -1.
func stepOf(cp string) int {
	rest, ok := strings.CutPrefix(cp, "step ")
	if !ok {
		return -1
	}
	n, _, _ := strings.Cut(rest, " ")
	i, err := strconv.Atoi(n)
	if err != nil {
		return -1
	}
	return i
}

// xmageLacks reports an XMage database miss of any card the tape's decks
// hold (the item has no single scenario card).
func xmageLacks(msg string, t *game.Tape) bool {
	if t == nil {
		return false
	}
	for _, d := range t.Decks {
		for _, n := range d.Cards {
			if oraclediff.XMageLacksCard(msg, n) {
				return true
			}
		}
	}
	return false
}

// rngField is true for the fields a shuffle or a random pick changes: a
// library's top cards, a hand, a graveyard's order.
func rngField(field string) bool {
	for _, s := range []string{".library_top", ".hand", ".graveyard"} {
		if strings.HasSuffix(field, s) {
			return true
		}
	}
	return false
}

// rngDrawn reports an engine RNG draw on the tape between the last agreeing
// checkpoint and the diverging step: the draws committed by the entries the
// step d carries out (all those before the setup checkpoint, with the genesis
// shuffle, when d is the setup).
func rngDrawn(item lower.Lowered, t *game.Tape, d int) bool {
	if t == nil || len(t.Entries) == 0 {
		return false
	}
	lo, hi := item.FirstEntry, item.FirstEntry-1
	if d < 0 {
		if t.GenesisRNG > 0 {
			return true
		}
		lo = 0
	} else {
		for i := d - 1; i >= 0 && i < len(item.StepEntry); i-- {
			if e := item.StepEntry[i]; e >= 0 {
				lo = e + 1
				break
			}
		}
		hi = lo - 1
		if d < len(item.StepEntry) && item.StepEntry[d] >= 0 {
			hi = item.StepEntry[d]
		}
	}
	for i := max(lo, 0); i <= hi && i < len(t.Entries); i++ {
		if t.Entries[i].RNG > 0 {
			return true
		}
	}
	return false
}

// projectionGap is a difference the comparison vocabulary cannot decide yet:
// an unmapped keyword ("other:<kw>") on either side, or tokens the engines
// name differently (DESIGN §4: such fields are gaps, never divergences).
func projectionGap(v oraclediff.Verdict) bool {
	if strings.Contains(v.Gorge, "other:") || strings.Contains(v.XMage, "other:") {
		return true
	}
	return v.Field == "permanents" && tokenNameGap(v.Gorge, v.XMage)
}

// tokenNameGap: every permanent only one side has is a token (the comparison
// renders a token's name as "token"; the remaining difference is its type
// list, the engines' token vocabulary), and once the type lists are dropped
// the two sides' lines are the same.
func tokenNameGap(g, x string) bool {
	if g == "" || x == "" {
		return false
	}
	gl, xl := strings.Split(g, "; "), strings.Split(x, "; ")
	if len(gl) != len(xl) {
		return false
	}
	strip := func(ls []string) (out []string, ok bool) {
		for _, l := range ls {
			if !strings.Contains(l, " token [") {
				return nil, false
			}
			if i, j := strings.IndexByte(l, '['), strings.IndexByte(l, ']'); i >= 0 && j > i {
				l = l[:i] + l[j+1:]
			}
			out = append(out, l)
		}
		return out, true
	}
	gs, ok1 := strip(gl)
	xs, ok2 := strip(xl)
	if !ok1 || !ok2 {
		return false
	}
	sort.Strings(gs)
	sort.Strings(xs)
	return strings.Join(gs, "\n") == strings.Join(xs, "\n")
}
