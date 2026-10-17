// The three-seat LosesGame recipe (levelb.LosesGameSub): "when a player /
// an opponent / the chosen player loses the game". In the two-seat scenario
// every other recipe plays, p1's loss ends the game (CR 104.2a), so the
// trigger never resolves anywhere a snapshot could show it. The recipe seats
// a second opponent (oraclegen.ThreeSeats): p1 loses to a Shock at 2 life,
// leaves the game (CR 800.4a), and the game goes on for p0 and p2, so the
// trigger's effect -- Shinryu's "you win the game" ends it, a life gain or a
// counter is a field on the board -- is a compared field.
//
// The trigger's player filter decides the board before the cause:
//
//   - any player / an opponent / another player (ValidPlayer$ absent,
//     Player, Player.Opponent, Player.Other): the card stands on p0's
//     battlefield, on its back face for a face-1 trigger, as every trigger
//     recipe places it;
//   - the chosen player (Player.Chosen): the choice is made by the card
//     itself (Shinryu's "As this creature transforms into Shinryu, choose an
//     opponent"), which a back-face setup placement never runs. The board is
//     then the card's own front-face trigger scenario that ends with the card
//     on the requirement's face (Zenos's "when the chosen creature leaves the
//     battlefield, transform"), replayed with three seats.
//
// TestLosesGameThreeSeatCensus pins which cards of the declared sets the
// recipe serves and why the rest are not.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// losesGameVictimLife is p1's starting life: the Shock cause's 2 damage
// takes it to 0, a state-based loss (CR 704.5a).
const losesGameVictimLife int32 = 2

// losesGameFires serves one LosesGameSub requirement.
func losesGameFires(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger loses-game: " + why}
	}
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Triggers) {
		return skip("index " + req.Slot)
	}
	t := &f.Triggers[idx]
	var boards []oraclegen.Item
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKValidPlayer)), "Player.Chosen") {
		boards = chosenPlayerBoards(reg, f, name, req)
		if len(boards) == 0 {
			return skip("no front-face scenario of the card ends on " + f.Name + " with its player chosen")
		}
	} else {
		for _, fx := range triggerFixtures(reg, f, t) {
			sc := triggerScenario(f, name, triggerCause{}, req, nil, &fx)
			boards = append(boards, oraclegen.Item{Scenario: sc})
		}
	}
	fired := false
	for _, board := range boards {
		sc, ok := losesGameCause(reg, board.Scenario)
		if !ok {
			continue
		}
		if !losesGameTriggerFires(reg, f, name, req, sc) {
			continue
		}
		fired = true
		if it, ok := losesGameServe(reg, f, name, req, board, sc); ok {
			return it, nil
		}
	}
	if !fired {
		return skip("did not fire")
	}
	return skip("no scenario gorge plays through")
}

// chosenPlayerBoards are the card's own served face-0 trigger scenarios whose
// last checkpoint shows it standing on face f under p0: the enabler that
// transforms it, running the "as it transforms, choose an opponent"
// replacement on the way.
func chosenPlayerBoards(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) []oraclegen.Item {
	c, ok := reg.Lookup(name)
	if !ok || req.Face == 0 {
		return nil
	}
	var out []oraclegen.Item
	for _, r := range levelb.Requirements(c) {
		if r.Family != "trigger" || r.Face != 0 || r.Gap != "" || r.Sub == levelb.LosesGameSub {
			continue
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			continue
		}
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Snapshots) == 0 || !standsOnFace(res.Snapshots[len(res.Snapshots)-1], f.Name) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// standsOnFace reports a p0 permanent named face in the snapshot.
func standsOnFace(s rules.OracleSnapshot, face string) bool {
	for _, p := range s.Permanents {
		if p.Controller == 0 && strings.EqualFold(p.Name, face) {
			return true
		}
	}
	return false
}

// losesGameCause seats the board's game for three (ThreeSeats), puts p1 at
// losesGameVictimLife and appends p0's Shock at p1.
func losesGameCause(reg *cards.Registry, board oraclegen.Scenario) (oraclegen.Scenario, bool) {
	shock, ok := castProbe(reg, shockProbe, "p1")
	if !ok {
		return board, false
	}
	sc := board
	sc.Players = oraclegen.ThreeSeats
	sc.Setup = make(map[string]oraclegen.Seat, len(board.Setup)+1)
	for k, v := range board.Setup {
		// A shallow copy keeps every seat field (back_face, life, speed);
		// only p0's hand and p1's life change, and the hand is re-sliced.
		sc.Setup[k] = v
	}
	p0, p1 := sc.Setup["p0"], sc.Setup["p1"]
	p0.Hand = append(append([]string(nil), p0.Hand...), shockProbe)
	life := losesGameVictimLife
	p1.Life = &life
	sc.Setup["p0"], sc.Setup["p1"] = p0, p1
	sc.Steps = append(append([]oraclegen.Step(nil), board.Steps...), shock)
	return sc, true
}

// losesGameTriggerFires plays the cause with each seat passing once, so the
// Shock resolves, p1 loses, and the checkpoint after p2's pass holds the
// trigger on the stack (the emitted item resolves it instead: the XMage
// driver's pass pairs are two-seat).
func losesGameTriggerFires(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, sc oraclegen.Scenario) bool {
	probe := sc
	probe.Steps = append(append([]oraclegen.Step(nil), sc.Steps...),
		oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1}, oraclegen.Step{Op: "pass", Seat: 2})
	_, res, ok := oraclegen.Settle(reg, probe)
	return ok && abilityOnStack(res.Snapshots, stackSourceWants(reg, name, f), stackSlot(req))
}

// losesGameServe settles the cause, rewrites targets to gorge's picks and
// scripts XMage's answers from the three-seat replay. The board item's
// parallel slices and comparison options carry over, padded to the new steps.
func losesGameServe(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, board oraclegen.Item, sc oraclegen.Scenario) (oraclegen.Item, bool) {
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	if res, ok = oraclegen.PlaysThrough(reg, sc); !ok {
		return oraclegen.Item{}, false
	}
	if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
		if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
			sc, res = yes, res2
		}
	}
	it := oraclegen.NewLevelBItem(name, req.Key, TriggerFires.Version, []string{"603.2", "800.4a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	if board.ID != "" {
		// A served board item's own steps keep the answers its template
		// scripted (a recipe's answer post-processing, such as a choice
		// XMage poses as a target); only the cause's steps take the
		// three-seat replay's answers.
		xa := make([][]oraclegen.XAnswer, len(sc.Steps))
		copy(xa, board.XAnswers)
		for i := len(board.Steps); i < len(sc.Steps) && i < len(it.XAnswers); i++ {
			xa[i] = it.XAnswers[i]
		}
		it.XAnswers = xa
	}
	it.Compare = append([]string(nil), board.Compare...)
	it.Ignore = append([]string(nil), board.Ignore...)
	if len(board.XAbility) > 0 {
		it.XAbility = make([]string, len(sc.Steps))
		copy(it.XAbility, board.XAbility)
	}
	if len(board.XTargetSkips) > 0 {
		it.XTargetSkips = make([][]oraclegen.XTargetSkip, len(sc.Steps))
		copy(it.XTargetSkips, board.XTargetSkips)
	}
	for i, st := range sc.Steps {
		if stepNeedsXAbility(st) != (i < len(it.XAbility) && it.XAbility[i] != "") {
			return it, false
		}
	}
	return it, true
}
