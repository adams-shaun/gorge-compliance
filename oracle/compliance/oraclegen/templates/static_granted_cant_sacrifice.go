package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// grantedCantSacrificeItem serves a granted "This token can't be sacrificed"
// (an AddStaticAbility$ whose body is Mode$ CantSacrifice) on a card that
// makes its own tokens with a built-in sacrifice: Zurgo, Thunder's Decree's
// Mobilize ("create ... tapped and attacking Warrior creature tokens.
// Sacrifice them at the beginning of the next end step", CR 702.181a) grants
// the Warrior tokens "can't be sacrificed" during its controller's end step
// (Phases$ End of Turn). The card attacks, play passes to the end step, where
// the delayed sacrifice trigger is on the stack, and the trigger resolves:
// the sacrifice attempt is the observation (CR 701.21a), and the tokens are
// still on the battlefield at the last checkpoint.
//
// Gorge decides, as everywhere in this file: the item is served only when its
// replay shows the delayed trigger on the stack at the end-step checkpoint
// with the tokens on the battlefield, and the same tokens still there after
// it resolved. Any other card (no Mobilize, a grant the attack does not make
// observable) returns false and keeps its named skip.
func grantedCantSacrificeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !f.IsCreature() || req.Face != 0 || !faceHasKeywordPrefix(f, "Mobilize") {
		return oraclegen.Item{}, false
	}
	if !strings.Contains(strings.ToLower(st.ParamStr(cards.PKAffected)), "token") {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 0, Attackers: []string{"p0:" + name}, Defender: "p1"},
		{Op: "pass_to", Seat: 0, Step: "end"},
		{Op: "resolve", Seat: 0},
	}
	sc := staticScenario(f, name, []string{name}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) != 4 {
		return oraclegen.Item{}, false
	}
	// snaps: setup, attack, pass_to end, resolve.
	atEnd, after := res.Snapshots[2], res.Snapshots[3]
	n := ownTokens(atEnd)
	if n == 0 || atEnd.Step != "end" || !triggerOnStack(atEnd, "p0:"+name) {
		return oraclegen.Item{}, false
	}
	if after.Step != "end" || len(after.Stack) != 0 || ownTokens(after) != n {
		return oraclegen.Item{}, false
	}
	return grantedStaticItem(f, name, req, sc, res, []string{"701.21a", "702.181a", "611.3"}, false), true
}

// ownTokens counts p0's tokens on the battlefield in snap.
func ownTokens(snap rules.OracleSnapshot) int {
	n := 0
	for _, p := range snap.Permanents {
		if p.Token && p.Controller == 0 {
			n++
		}
	}
	return n
}

// faceHasKeywordPrefix reports whether one of f's keyword lines starts with
// head ("Mobilize" matches "Mobilize:2").
func faceHasKeywordPrefix(f *cards.Face, head string) bool {
	for _, k := range f.Keywords {
		if strings.HasPrefix(k, head) {
			return true
		}
	}
	return false
}
