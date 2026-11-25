package lower

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// planTargetSkips stamps Item.XTargetSkips for every cast step that names
// fewer targets than its card has target objects. XMage poses every target
// object of a spell in order, and an object with no legal candidate or none
// the cast chose needs an explicit "[target_skip]" at its place between the
// filled ones; a lowered step keeps only the flat target list, so without the
// plan the driver cannot tell which object a lone target fills (Relive the
// Past with only a land in the graveyard sets the land inline on the artifact
// object and XMage never finds the cast).
//
// The slots a step left empty are read off gorge's own target decisions
// (oraclegen.LoweredTargetSkips), so the item is run once; a window whose
// casts all name every slot of their card never pays for that run. A step the
// plan cannot vouch for gets no entry and the driver keeps its own handling.
func (l *Lowered) planTargetSkips(reg *cards.Registry) {
	var casts []int
	faces := map[int]*cards.Face{}
	for i, st := range l.Item.Steps {
		if st.Op != "cast" || len(st.Targets) == 0 || st.CastMode != "" {
			continue
		}
		f := castFace(reg, st.Card)
		if f == nil || len(oraclegen.SlotSpecs(f)) <= len(st.Targets) {
			continue
		}
		casts = append(casts, i)
		faces[i] = f
	}
	if len(casts) == 0 {
		return
	}
	res, err := rules.RunOracleScenarioJSON(reg, l.Item.Raw())
	if err != nil || len(res.Fails) > 0 {
		return
	}
	for _, i := range casts {
		plan := oraclegen.LoweredTargetSkips(reg, faces[i], l.Item.Scenario, res.Decisions, i)
		if len(plan) == 0 {
			continue
		}
		if l.Item.XTargetSkips == nil {
			l.Item.XTargetSkips = make([][]oraclegen.XTargetSkip, len(l.Item.Steps))
		}
		l.Item.XTargetSkips[i] = plan
	}
}

// castFace is the face a cast step's card ref names ("p0:Relive the Past"),
// or nil for a ref that is no registry card.
func castFace(reg *cards.Registry, ref string) *cards.Face {
	_, rest, ok := strings.Cut(ref, ":")
	if !ok || strings.HasPrefix(rest, "token:") || strings.HasPrefix(rest, "ability:") {
		return nil
	}
	name := refName(ref)
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return nil
	}
	for _, f := range c.Faces {
		if f.Name == name {
			return f
		}
	}
	return c.Faces[0]
}
