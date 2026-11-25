package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// optionalTriggerSearchDecline reports a declined library search that is the
// body of a triggered ability Oracle words "you may search ...", where the
// Forge script carries no OptionalDecider$ (Hexhaven Invigorator:
// `DB$ ChangeZone | Origin$ Library | ChangeNum$ X`). Forge has no "may" to
// decline there, so gorge's only decline is the Min-0 search pick coming back
// empty. XMage builds the same card as an optional triggered ability: the
// trigger's chooseUse takes the "no" and no search follows, so the "up to"
// target_skip the generic declined pair adds is left over ("(Targets of
// PlayerB) Count are not equal (found [[target_skip]])").
//
// The source is resolved by counterPickSource, the stack snapshot at the
// decision's own step: the trigger on the stack names its printed index, and
// the trigger's description must say "you may search". A look-at-the-top "up
// to" pick (Zimone's Experiment) is not a library-search trigger and keeps the
// measured pair.
func optionalTriggerSearchDecline(reg *cards.Registry, sc Scenario, g rules.OracleResult, d rules.OracleDecision) bool {
	if reg == nil || d.Step < 0 || len(d.Picks) != 0 || d.Min != 0 || d.Max < 1 || d.Kind != "choose_n" {
		return false
	}
	switch d.Resume {
	case "search", "hidden_pick", "choice", "":
	default:
		return false
	}
	obj, matched, onStack := counterPickSource(reg, sc, g, d, libraryChangeZoneNoDecider)
	if !matched || !onStack || obj.Kind != "ability" || obj.Trigger == "" {
		return false
	}
	idx, err := strconv.Atoi(obj.Trigger)
	if err != nil || idx < 0 || d.Step >= len(g.Snapshots) {
		return false
	}
	src := counterPickSourceRef(reg, g.Snapshots[d.Step], obj.Source)
	c, ok := reg.Lookup(oraclediffRefName(src))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, src) {
		if f == nil || idx >= len(f.Triggers) {
			continue
		}
		tr := f.Triggers[idx]
		if counterPickInChain(tr.Effect, libraryChangeZoneNoDecider) &&
			strings.Contains(strings.ToLower(tr.Params["TriggerDescription"]), "you may search") {
			return true
		}
	}
	return false
}

// libraryChangeZoneNoDecider is a ChangeZone out of the library with no
// OptionalDecider$ of its own (so Forge's "may" is only the Min-0 pick).
func libraryChangeZoneNoDecider(sa *cards.SA) bool {
	return sa.API == "ChangeZone" && strings.EqualFold(strings.TrimSpace(sa.Params["Origin"]), "Library") &&
		strings.TrimSpace(sa.Params["OptionalDecider"]) == ""
}
