package lower

import (
	"strconv"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// ChooseDieResultAsNumber is decisions with each choose-one-result answer
// ("Roll two d6 and choose one result", Valiant Endeavor; gorge's choose_n
// resumed as "roll") reshaped into the ask Forge poses: chooseNumber over the
// rolled values, answered with the chosen value itself. gorge's pick is the
// label "die 1: result 3" of the die kept, which a number ask cannot read, so
// left as is the number ask is a strict miss and the pick an unconsumed
// leftover.
//
// The pick becomes the bare result ("3") of kind "number", with no option
// indices or refs, on the same step and seat. The input slice is not
// modified; a list with no such answer is returned as is.
func ChooseDieResultAsNumber(decisions []rules.OracleDecision) []rules.OracleDecision {
	var out []rules.OracleDecision
	for i, d := range decisions {
		_, result, ok := oraclegen.DieResultPick(d)
		if !ok {
			continue
		}
		if out == nil {
			out = append([]rules.OracleDecision(nil), decisions...)
		}
		d.Picks = []string{strconv.Itoa(result)}
		d.PickKinds = []string{"number"}
		d.PickIdx = nil
		d.PickRefs = nil
		d.PickRefsInexact = nil
		d.ObjectPicks = nil
		d.OptionRefs = nil
		out[i] = d
	}
	if out == nil {
		return decisions
	}
	return out
}

// DropTiedDieResultChoices is decisions without the choose-one-result answers
// whose dice all rolled the same result: XMage poses its chooseUse only when
// the two results differ (ValiantEndeavorEffect), so a scripted answer for a
// tie would be left unconsumed. rolls are the item's recorded die rolls; a
// decision's dice are those recorded on its step and seat. A decision with
// fewer than two recorded dice is kept. The input slice is not modified.
func DropTiedDieResultChoices(decisions []rules.OracleDecision, rolls []oraclegen.DieRoll) []rules.OracleDecision {
	var out []rules.OracleDecision
	dropped := false
	for _, d := range decisions {
		if _, _, ok := oraclegen.DieResultPick(d); ok && tiedDice(rolls, d.Step, d.Seat) {
			dropped = true
			continue
		}
		out = append(out, d)
	}
	if !dropped {
		return decisions
	}
	return out
}

// tiedDice reports at least two recorded dice on step by seat, all with one
// result.
func tiedDice(rolls []oraclegen.DieRoll, step, seat int) bool {
	n, first := 0, 0
	for _, r := range rolls {
		if r.Step != step || r.Seat != seat {
			continue
		}
		v := r.Result
		if v == 0 {
			v = r.Natural // an unmodified roll omits its result
		}
		if n > 0 && v != first {
			return false
		}
		first = v
		n++
	}
	return n >= 2
}
