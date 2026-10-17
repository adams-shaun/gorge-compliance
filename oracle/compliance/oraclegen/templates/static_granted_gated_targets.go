package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// gatedGrantTarget is the fixture a gated self grant's targeted ability needs
// before the engine offers it (CR 602.2b/601.2c: an ability with a target is
// activatable only when a legal target exists): Slick Imitator's "Copy target
// spell you control" needs a spell of p0's on the stack, Adagia, Windswept
// Bastion's "copy target artifact or enchantment you control" an artifact on
// p0's battlefield. The zero value is "no fixture"; the gate-off control keeps
// the same fixture, so the offer is still the gate's doing.
type gatedGrantTarget struct {
	battlefield []string
	hand        []string
	steps       []oraclegen.Step
}

// gatedGrantTargetSpell is the spell cast to sit on the stack as the target,
// and gatedGrantTargetArtifact the permanent placed as one: neither has a
// target or a choice of its own.
const (
	gatedGrantTargetSpell    = "Accumulated Knowledge"
	gatedGrantTargetArtifact = "Ornithopter"
)

// gatedGrantTargetFor reads the granted ability's ValidTgts$; ok is false when
// it names a target this fixture cannot provide (the caller keeps its
// untargeted attempt).
func gatedGrantTargetFor(reg *cards.Registry, sa *cards.SA) (gatedGrantTarget, bool) {
	tgts := sa.ParamStr(cards.PKValidTgts)
	if tgts == "" {
		return gatedGrantTarget{}, false
	}
	words := affectedWords(tgts)
	switch {
	case strings.EqualFold(sa.ParamStr(cards.PKTargetType), "Spell") && hasWord(words, "YouCtrl"):
		st, ok := castProbe(reg, gatedGrantTargetSpell)
		if !ok {
			return gatedGrantTarget{}, false
		}
		return gatedGrantTarget{hand: []string{gatedGrantTargetSpell}, steps: []oraclegen.Step{st}}, true
	case hasWord(words, "Artifact") && !hasWord(words, "OppCtrl"):
		if _, ok := reg.Lookup(gatedGrantTargetArtifact); !ok {
			return gatedGrantTarget{}, false
		}
		return gatedGrantTarget{battlefield: []string{gatedGrantTargetArtifact}}, true
	}
	return gatedGrantTarget{}, false
}
