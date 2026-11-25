package oraclegen

import (
	"regexp"
	"strconv"

	"github.com/adams-shaun/gorge/engine/rules"
)

// dieResultLabel is the label of one option of gorge's choose-one-result ask
// (engine/effects/dice.go, RollDice with ChosenSVar$): "die <k>: result <r>",
// k the 1-based die in roll order, r that die's (modified) result.
var dieResultLabel = regexp.MustCompile(`^die (\d+): result (-?\d+)$`)

// DieResultPick reads gorge's choose-one-result decision (Valiant Endeavor,
// "Roll two d6 and choose one result"): a choose_n resumed as "roll" with one
// pick. die is the chosen die's 1-based place in roll order and result its
// value. ok is false for any other decision or a pick that is not a die label.
func DieResultPick(d rules.OracleDecision) (die, result int, ok bool) {
	if d.Kind != "choose_n" || d.Resume != "roll" || len(d.Picks) != 1 {
		return 0, 0, false
	}
	m := dieResultLabel.FindStringSubmatch(d.Picks[0])
	if m == nil {
		return 0, 0, false
	}
	die, _ = strconv.Atoi(m[1])
	result, _ = strconv.Atoi(m[2])
	return die, result, true
}
