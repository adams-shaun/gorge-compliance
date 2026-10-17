package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// MayPlayRiderSub is the level-B sub-family of the Static$ True SpellCast
// rider a play-permission static carries: "If you cast a spell this way, that
// creature enters with a finality counter on it" (Intrepid Paleontologist,
// Noctis, Leonardo, Hundred-Battle Veteran) or "... enters with a +1/+1
// counter" (Undead Sprinter). The trigger never uses the stack, so it is not
// observed as a stack entry: its cause is the cast the same face's MayPlay
// static permits, resolved, and its observation is the permanent entering
// with the rider's counter (or tapped). Exported so the recipe and this
// classifier share one spelling.
const MayPlayRiderSub = "trigger.spell-cast-mayplay-rider"

// classifyMayPlayRider names a Static$ True SpellCast line whose filter reads
// the cast's MayPlay provenance (ValidSA$ Spell.MayPlaySource, or ValidCard$
// Card.CastSa Spell.MayPlaySource) on a face that carries the Continuous
// MayPlay$ static granting that cast, and whose Execute$ is an Effect whose
// replacement modifies how the cast permanent enters. ok is false for every
// other trigger.
func classifyMayPlayRider(f *cards.Face, t *cards.Trigger) (sub string, ok bool) {
	if t.ModeKind() != cards.TriggerSpellCast || !strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return "", false
	}
	filter := t.ParamStr(cards.PKValidCard) + "," + t.ParamStr(cards.PKValidSA)
	if !strings.Contains(filter, "MayPlaySource") {
		return "", false
	}
	if _, ok := MayPlayRiderStatic(f); !ok {
		return "", false
	}
	if !mayPlayRiderEntersModified(f, t) {
		return "", false
	}
	return MayPlayRiderSub, true
}

// MayPlayRiderStatic returns the index into f.Statics of the Continuous
// MayPlay$ static whose permitted cast the rider reads.
func MayPlayRiderStatic(f *cards.Face) (int, bool) {
	for i := range f.Statics {
		st := &f.Statics[i]
		if st.ModeKind() == cards.StaticContinuous && st.HasParam(cards.PKMayPlay) {
			return i, true
		}
	}
	return 0, false
}

// mayPlayRiderEntersModified reports that the rider's Effect installs an
// enters replacement whose body puts a counter on, or taps, the entering
// permanent: the visible consequence the recipe observes.
func mayPlayRiderEntersModified(f *cards.Face, t *cards.Trigger) bool {
	eff := f.SVars[t.ParamStr(cards.PKExecute)]
	if !strings.Contains(eff, "DB$ Effect") {
		return false
	}
	for _, part := range strings.Split(eff, "|") {
		k, v, _ := strings.Cut(part, "$")
		if strings.TrimSpace(k) != "ReplacementEffects" {
			continue
		}
		for _, repl := range strings.Split(v, ",") {
			body := f.SVars[strings.TrimSpace(repl)]
			if !strings.Contains(body, "Destination$ Battlefield") {
				continue
			}
			_, with, _ := strings.Cut(body, "ReplaceWith$")
			with = strings.TrimSpace(strings.SplitN(with, "|", 2)[0])
			w := f.SVars[with]
			if strings.Contains(w, "DB$ PutCounter") || strings.Contains(w, "DB$ Tap") {
				return true
			}
		}
	}
	return false
}
