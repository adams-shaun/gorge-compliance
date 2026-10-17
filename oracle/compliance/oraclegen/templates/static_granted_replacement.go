// Level-B observation of a static whose AddReplacementEffect$ grants a
// REPLACEMENT effect, observed by causing the event the granted replacement
// rewrites (ticket agent-20261009T094023Z-cd4b2f74; the static slice the
// granted-ability, granted-trigger, granted-static and donor templates left as
// the named "grants a replacement effect" skip). The outer static is a plain
// Mode$ Continuous whose Affected$ names the HOST the granted replacement
// lands on (the card itself for both DFT rows); the granted SVar body is
// parsed with the same parser a printed R: line uses.
//
// Two shapes are served, one per DFT census row:
//
//   - DamageDone, a DB$ ReplaceEffect body that raises the held DamageAmount
//     (Far Fortune, End Boss' max-speed "a source you control would deal
//     damage ... deals that much damage plus 1 instead"): p0 casts Shock at
//     p1 and resolves it; p1 ends at 17 (Shock's 2 plus the granted 1), and
//     at 18 in the speed-0 control.
//   - Draw, a DB$ Draw body that draws more (Vnwxt, Verbose Host' max-speed
//     "if you would draw a card, draw two cards instead"): p0 casts
//     Divination (draw two) and resolves it; p0's hand grows by 4 (each of
//     the two draws replaced by a draw of two), and by 2 in the speed-0
//     control.
//
// Each observation supplies the outer static's gate through withMaxSpeed and
// proves the grant's doing with a gate-off control (the same board and steps
// at speed 0). An observation that does not hold gorge-side is never served,
// so the row keeps its named grant skip.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// staticGrantedReplacementItem is the AddReplacementEffect$ observation entry
// point. ok is false when the granted body is an event class or body no shape
// here serves; the caller then falls through to its named grant skip.
func staticGrantedReplacementItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	raw := strings.TrimSpace(st.ParamStr(cards.PKAddReplacementEffect))
	if raw == "" {
		return oraclegen.Item{}, false
	}
	for _, nm := range cards.SplitGrantNames(raw) {
		r, ok := cards.ParseReplacementLine(f.SVars[nm])
		if !ok {
			continue
		}
		switch r.EventKind() {
		case cards.ReplDamageDone:
			if it, ok := grantedReplacementDamageItem(reg, f, name, req, st, r); ok {
				return it, true
			}
			if it, ok := grantedReplacementPreventItem(reg, f, name, req, st, r); ok {
				return it, true
			}
		case cards.ReplUntap:
			if it, ok := grantedReplacementUntapItem(reg, f, name, req, st, r); ok {
				return it, true
			}
		case cards.ReplDraw, cards.ReplDrawCards:
			if it, ok := grantedReplacementDrawItem(reg, f, name, req, st, r); ok {
				return it, true
			}
		}
	}
	return oraclegen.Item{}, false
}

// grantedReplacementDamageItem serves a granted DamageDone replacement whose
// ReplaceWith$ body is a DB$ ReplaceEffect. The observation proves the body
// RAISED the damage: a Shock (2) to p1 ends p1 at 17 with the grant, 18 in the
// speed-0 control.
func grantedReplacementDamageItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, r cards.Repl) (oraclegen.Item, bool) {
	with := cards.ResolveSVar(f.SVars, strings.TrimSpace(r.ParamStr(cards.PKReplaceWith)))
	if with == nil || with.API != "ReplaceEffect" {
		return oraclegen.Item{}, false
	}
	shock, ok := castProbe(reg, "Shock", "p1")
	if !ok || !oraclegen.XMageKnown("Shock") {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{shock, {Op: "resolve"}}
	sc := staticScenario(f, name, []string{name}, []string{"Shock"}, steps)
	withMaxSpeed(sc.Setup)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	// The grant really raised the damage: 20 - (2 + 1) = 17.
	if life, ok := snapshotLife(res.Snapshots[len(res.Snapshots)-1], 1); !ok || life != 17 {
		return oraclegen.Item{}, false
	}
	// Gate-off control: the same Shock at speed 0 must land its printed 2.
	ctl := sc
	ctl.Setup = cloneSeats(sc.Setup)
	p0 := ctl.Setup["p0"]
	p0.Speed = 0
	ctl.Setup["p0"] = p0
	cres, cok := runStatic(reg, ctl)
	if !cok || len(cres.Fails) != 0 || len(cres.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if life, ok := snapshotLife(cres.Snapshots[len(cres.Snapshots)-1], 1); !ok || life != 18 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"614.1a", "616.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// grantedReplacementDrawItem serves a granted Draw replacement whose
// ReplaceWith$ body draws more cards. The observation proves the body drew
// MORE: a Divination (draw two) leaves p0's hand `draw` above the speed-0
// control's (the grant replaces each of the two draws with a draw of two).
func grantedReplacementDrawItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, r cards.Repl) (oraclegen.Item, bool) {
	with := cards.ResolveSVar(f.SVars, strings.TrimSpace(r.ParamStr(cards.PKReplaceWith)))
	if with == nil || with.API != "Draw" {
		return oraclegen.Item{}, false
	}
	pool, why := oraclegen.PoolFor(probeCost(reg, cantDrawProbe))
	if why != "" {
		return oraclegen.Item{}, false
	}
	draw, dwhy := probeDrawCount(reg, cantDrawProbe)
	if dwhy != "" {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + cantDrawProbe, Mana: pool},
		{Op: "resolve"},
	}
	hand := func(res rules.OracleResult) int {
		if len(res.Snapshots) == 0 || len(res.Snapshots[len(res.Snapshots)-1].Players) == 0 {
			return -1
		}
		return len(res.Snapshots[len(res.Snapshots)-1].Players[0].Hand)
	}
	// Gate-off control: the same Divination at speed 0 draws its printed two.
	ctl := staticScenario(f, name, []string{name}, []string{cantDrawProbe}, steps)
	cres, cok := runStatic(reg, ctl)
	if !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	base := hand(cres)
	if base < 0 {
		return oraclegen.Item{}, false
	}
	// Gate on: each of the two draws is replaced by a draw of two, so the
	// hand ends `draw` (the control's growth) above the control's own total.
	sc := staticScenario(f, name, []string{name}, []string{cantDrawProbe}, steps)
	withMaxSpeed(sc.Setup)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	if got := hand(res); got != base+draw {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"614.1a", "616.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// snapshotLife reads seat's life from snap, ok=false when the seat is absent.
func snapshotLife(snap rules.OracleSnapshot, seat int) (int32, bool) {
	for _, p := range snap.Players {
		if p.Seat == seat {
			return p.Life, true
		}
	}
	return 0, false
}
