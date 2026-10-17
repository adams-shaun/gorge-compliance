// Causes for the zone-change residue shapes (ticket g17, wave 3 "trigger
// with no recipe"): a "when the chosen creature leaves" trigger
// (ChosenCardStrict), a library-to-graveyard trigger, and a
// "from anywhere" put-into-the-graveyard trigger. Each has a turn-1 cause:
//
//   - library to graveyard: a mill probe aimed at p0 puts land cards from
//     p0's library into its graveyard.
//   - from anywhere to the graveyard: a destroy probe sends a p0 permanent
//     there; a trigger whose IsPresent$ demands a counter on the source
//     starts with it.
//
// The engine stays the authority on every filter; a cause that cannot fire
// keeps the residue's named skip.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// millProbes are the spells that put cards from p0's library into p0's
// graveyard; the scenario library is the runner's Wastes fill, so a mill
// lands land cards.
var millProbes = []string{"Tome Scour"}

// residueCauses builds the causes of one zone-change-residue shape. ok is
// false when the trigger is none of them (the named skip stands). A
// ChosenCardStrict filter is served when the card's own ETB chooses an
// opposing creature (chosenCardLeavesCauses).
func residueCauses(reg *cards.Registry, name string, t *cards.Trigger, filter string) ([]triggerCause, string, bool) {
	origin := strings.ToLower(t.ParamStr(cards.PKOrigin))
	dest := strings.ToLower(t.ParamStr(cards.PKDestination))
	switch {
	case strings.Contains(strings.ToLower(filter), "chosencardstrict"):
		return chosenCardLeavesCauses(reg, name, t)
	case strings.Contains(origin, "library") && strings.Contains(dest, "graveyard"):
		return libraryToGraveyardCauses(reg, name, t)
	case strings.Contains(dest, "graveyard"):
		return anyToGraveyardCauses(reg, name, t)
	}
	return nil, "", false
}

// libraryToGraveyardCauses mills p0 with the mill probes, putting land cards
// from the library into p0's graveyard. It refuses a trigger whose body's
// ChangeType$ spec carries a predicate the filter grammar does not know: the
// trigger would fire and its body would silently move nothing, so the
// scenario would compare a no-op against XMage's move. Hedge Shredder's
// Card.TriggeredCards used to be such a predicate and kept the named skip
// until 8e9f1097c registered it; it is served now.
func libraryToGraveyardCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	for _, body := range triggerBodyLines(t) {
		for _, spec := range changeTypeSpecs(body) {
			if len(effects.UnknownPredicates(spec)) > 0 {
				return nil, "", false
			}
		}
	}
	var out []triggerCause
	for _, p := range millProbes {
		if c, ok := castCause(reg, name, p, "p0"); ok {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "", false
	}
	return out, "", true
}

// triggerBodyLines is the trigger's Execute body's SVar text and its inline
// effect line.
func triggerBodyLines(t *cards.Trigger) []string {
	var out []string
	if t.Effect != nil && t.Effect.Line != "" {
		out = append(out, t.Effect.Line)
	}
	if ex := t.ParamStr(cards.PKExecute); ex != "" {
		out = append(out, ex)
	}
	return out
}

// changeTypeSpecs extracts each ChangeType$ value of a DB$ line.
func changeTypeSpecs(line string) []string {
	var out []string
	for _, field := range strings.Split(line, "|") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(field), "ChangeType$ "); ok {
			out = append(out, v)
		}
	}
	return out
}

// anyToGraveyardCauses destroys a p0 permanent, putting it into p0's
// graveyard from anywhere. A trigger whose IsPresent$ gates on a counter on
// the source may already satisfy the gate with what the source enters with
// (its own ETB counters); the cause without setup counters runs first so a
// source that would die at the gate is not killed by them.
func anyToGraveyardCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	counters := map[string]map[string]int{}
	if kind, n, gated := levelb.SelfCounterGate(t.ParamStr(cards.PKIsPresent)); gated {
		counters = map[string]map[string]int{"__SOURCE__": {kind: n}}
	}
	var out []triggerCause
	for _, withCounters := range []bool{false, true} {
		for _, p := range destroyProbes {
			c, ok := castCause(reg, name, p, "p0:"+bearsProbe)
			if !ok {
				continue
			}
			c.battlefield = []string{bearsProbe}
			if withCounters {
				c.counters = counters
			}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "", false
	}
	return out, "", true
}

// chosenCardLeavesCauses serves "When the chosen creature leaves the
// battlefield" (Zenos yae Galvus: ValidCard$ Card.ChosenCardStrict, Origin$
// Battlefield) on a card whose own ETB chooses the creature (DB$ ChooseCard
// | Choices$ Creature.OppCtrl). The card is cast first, so its ETB chooses
// p1's baseline Grizzly Bears -- the only opposing creature -- and a probe
// whose destination the trigger admits then removes that creature. ok is
// false for any other ChosenCardStrict shape.
func chosenCardLeavesCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || !strings.EqualFold(t.ParamStr(cards.PKOrigin), "Battlefield") {
		return nil, "", false
	}
	f := c.Faces[0]
	if !f.IsCreature() || !selfETBChoosesOpposingCreature(f) {
		return nil, "", false
	}
	var out []triggerCause
	for _, probe := range zoneProbes(t.ParamStr(cards.PKDestination)) {
		cs, ok := castCause(reg, name, probe, "p1:"+bearsProbe)
		if !ok {
			continue
		}
		cs.castSelfX = true
		cs.choicesAsTargets = true
		out = append(out, cs)
	}
	if len(out) == 0 {
		return nil, "", false
	}
	return out, "", true
}

// selfETBChoosesOpposingCreature reports a self-ETB trigger on f whose body
// is a ChooseCard over Creature.OppCtrl.
func selfETBChoosesOpposingCreature(f *cards.Face) bool {
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		if tr.ModeKind() != cards.TriggerChangesZone || !strings.EqualFold(tr.ParamStr(cards.PKDestination), "Battlefield") ||
			!strings.EqualFold(tr.ParamStr(cards.PKValidCard), "Card.Self") {
			continue
		}
		body := f.SVars[tr.ParamStr(cards.PKExecute)]
		if strings.Contains(body, "DB$ ChooseCard") && strings.Contains(body, "Choices$ Creature.OppCtrl") {
			return true
		}
	}
	return false
}

// choicesAsTargetAnswers rewrites the item's object "choice" answers as
// "target" answers naming the decision's exact pick: Zenos's ETB "choose a
// creature an opponent controls" is a target ask in XMage, so a choice-queue
// answer is consumed by the wrong ask (measured lb4-gtrig: "Found wrong
// choice command ... TargetOpponent"). A player pick stays a choice:
// Shinryu's "choose an opponent" is XMage's player.choose over a
// TargetOpponent, which reads the choice queue by player name (strict replay,
// lb4w2-3seat: "Missing CHOICE def ... TargetOpponent" with the pick queued
// as a target).
func choicesAsTargetAnswers(xa [][]oraclegen.XAnswer, ds []rules.OracleDecision) [][]oraclegen.XAnswer {
	for step := range xa {
		for i := range xa[step] {
			a := &xa[step][i]
			if a.Kind != "choice" || a.Value == "yes" || a.Value == "no" || isSeatAnswer(a.Value) {
				continue
			}
			a.Kind = "target"
			for _, d := range ds {
				if d.Step == step && len(d.ObjectPicks) == 1 && len(d.PickRefs) == 1 {
					a.Value = d.PickRefs[0]
				}
			}
		}
	}
	return xa
}

// isSeatAnswer reports an answer value naming a player ("p0", "p1", "p2").
func isSeatAnswer(v string) bool {
	if len(v) < 2 || v[0] != 'p' {
		return false
	}
	for _, r := range v[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
