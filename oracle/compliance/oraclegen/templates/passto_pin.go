package templates

import (
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// pinPassToSteps names, on a pass_to that gorge's own replay of the item
// resolves in a LATER turn than the previous checkpoint, the step and active
// seat where gorge stopped.
//
// gorge reads a pass_to as "advance to the next such decision/step",
// whichever turn that is. XMage's replay driver (ScenarioReplay.step, case
// "pass_to") keeps the tracked turn unless the step names an active seat: a
// stepless pass_to stays in the current step (61e4bb225) and "main1" after
// main2 stays in the current turn. A pass_to that crosses into the next turn
// therefore checkpointed in the wrong turn: Archangel of Tithes' stepless
// "attackers" decision (turn 2 in gorge, 1 in XMage) and Ancient
// Adamantoise's "main1" after main2 (a "both: checkpoint" harness row). gorge
// stops at the same decision with the pin (its Decision check runs first, and
// an Active equal to the reached turn's active seat changes no stop), so the
// pinned item is kept only when its replay reproduces the original snapshots
// exactly. A same-turn pass_to is left alone: the driver tracks it already.
func pinPassToSteps(reg *cards.Registry, it *oraclegen.Item) {
	if !hasPassTo(it.Steps) {
		return
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 {
		return
	}
	at := map[string]rules.OracleSnapshot{}
	for _, s := range res.Snapshots {
		at[s.Checkpoint] = s
	}
	pinned := it.Scenario
	pinned.Steps = append([]oraclegen.Step(nil), it.Steps...)
	changed := false
	prevTurn := int32(-1)
	if s, ok := at["setup"]; ok {
		prevTurn = s.Turn
	}
	for i, st := range pinned.Steps {
		snap, ok := at[fmt.Sprintf("step %d (%s)", i, st.Op)]
		if !ok {
			continue
		}
		if st.Op == "pass_to" && driverStepNames[snap.Step] {
			if st.Step == "" && st.Decision != "" && snap.Turn != prevTurn {
				pinned.Steps[i].Step = snap.Step
				changed = true
			}
			if st.Active == "" && prevTurn >= 0 && snap.Turn != prevTurn && pinned.Steps[i].Step != "" {
				pinned.Steps[i].Active = fmt.Sprintf("p%d", snap.Active)
				changed = true
			}
		}
		prevTurn = snap.Turn
	}
	if !changed {
		return
	}
	cand := *it
	cand.Scenario = pinned
	again, err := rules.RunOracleScenarioJSON(reg, cand.Raw())
	if err != nil || len(again.Fails) != 0 || !sameSnapshots(res.Snapshots, again.Snapshots) {
		return
	}
	it.Scenario = pinned
}

// driverStepNames are the step names the XMage replay driver maps to a
// PhaseStep; any other stop is left unpinned.
var driverStepNames = map[string]bool{
	"upkeep": true, "draw": true, "main1": true, "begin-combat": true,
	"declare-attackers": true, "declare-blockers": true, "end-combat": true,
	"main2": true, "end": true,
}

func hasPassTo(steps []oraclegen.Step) bool {
	for _, st := range steps {
		if st.Op == "pass_to" {
			return true
		}
	}
	return false
}

func sameSnapshots(a, b []rules.OracleSnapshot) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(ja) == string(jb)
}
