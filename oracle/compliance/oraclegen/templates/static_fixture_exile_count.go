// The exiled-with COUNT fixture (wave-5 group X): a self static gated on the
// number of cards exiled with the source, where the source's own exile is a
// "Survival" second-main-phase trigger that needs the source tapped (Veteran
// Survivor: "At the beginning of your second main phase, if Veteran Survivor
// is tapped, exile up to one target card from a graveyard. As long as there
// are three or more cards exiled with Veteran Survivor, it gets +3/+3 and has
// hexproof"). The fixture places the card, then for each needed card attacks
// with it (so it is tapped at the second main phase) and resolves the
// trigger on a graveyard victim, one of p0's turns at a time -- the engine's
// own move records the exiled-with link, never a setup placement.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// staticExiledWithCountVictims are the graveyard cards the Survival trigger
// exiles, one per turn: cards with no graveyard ability of their own, none
// sharing a name with the battlefield probe (a target ref binds by name).
var staticExiledWithCountVictims = []string{"Hill Giant", "Savannah Lions", "Shock"}

// staticExiledWithCountFixture builds the fixture for a self static whose
// CheckSVar$ body is ExiledWith$Amount compared GE n, on a face whose
// "tapped at your second main phase" trigger exiles a target graveyard card.
// ok is false for any other shape.
func staticExiledWithCountFixture(f *cards.Face, st cards.Static) (staticFixture, bool) {
	if !strings.EqualFold(strings.TrimSpace(st.Params["AffectedDefined"]), "Self") &&
		!hasWord(affectedWords(st.ParamStr(cards.PKAffected)), "Self") {
		return staticFixture{}, false
	}
	body := strings.TrimSpace(f.SVars[strings.TrimSpace(st.Params["CheckSVar"])])
	if !strings.EqualFold(body, "ExiledWith$Amount") {
		return staticFixture{}, false
	}
	cmp := strings.TrimSpace(st.Params["SVarCompare"])
	if !strings.HasPrefix(cmp, "GE") {
		return staticFixture{}, false
	}
	n := staticCountFrom(cmp)
	if n < 1 || n > len(staticExiledWithCountVictims) || !survivalExileTrigger(f) {
		return staticFixture{}, false
	}
	victims := staticExiledWithCountVictims[:n]
	fx := staticFixture{
		conditionPrelude: conditionPrelude{graveyard: append([]string(nil), victims...)},
		place:            true,
		reanswers:        true,
	}
	for i, v := range victims {
		if i > 0 {
			fx.afterSteps = append(fx.afterSteps, oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main1", Active: "p0"})
		}
		fx.afterSteps = append(fx.afterSteps,
			oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + f.Name}},
			oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main2"},
			oraclegen.Step{Op: "resolve", Targets: []string{"p0:" + v}})
	}
	return fx, true
}

// survivalExileTrigger reports whether the face carries the Survival shape:
// a Phase trigger at the controller's second main phase, present only while
// the card itself is tapped, whose effect exiles a TARGET card from a
// graveyard.
func survivalExileTrigger(f *cards.Face) bool {
	for _, tr := range f.Triggers {
		if tr.Mode != "Phase" || tr.Effect == nil || tr.Effect.API != "ChangeZone" {
			continue
		}
		p := tr.Params
		if !strings.EqualFold(strings.TrimSpace(p["Phase"]), "Main") || strings.TrimSpace(p["PhaseCount"]) != "2" ||
			!strings.EqualFold(strings.TrimSpace(p["PresentDefined"]), "Self") ||
			!strings.EqualFold(strings.TrimSpace(p["IsPresent"]), "Card.tapped") {
			continue
		}
		ep := tr.Effect.Params
		if strings.EqualFold(strings.TrimSpace(ep["Origin"]), "Graveyard") &&
			strings.EqualFold(strings.TrimSpace(ep["Destination"]), "Exile") &&
			strings.TrimSpace(ep["ValidTgts"]) != "" {
			return true
		}
	}
	return false
}
