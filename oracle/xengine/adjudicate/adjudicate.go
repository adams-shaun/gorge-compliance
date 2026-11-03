// Package adjudicate gives every xengine item that diverged from a follower a
// three-way verdict (gorge, XMage, Forge) with the existing patterns
// (oraclediff.ThreeWay), and groups the gorge_suspect findings into
// ticket drafts. It adds no comparator and
// no pattern: the three pairwise legs are oraclediff.CompareOpts, unchanged.
//
// Nothing here rules: a GF|X item yields a note, never an xmage_wrong ruling or
// a verdict row (Forge runs gorge's own scripts, so Forge
// agreeing with gorge is weak evidence). Output stays out of the tree; the one thing
// that can be committed, the -ledger, carries our labels and field names only.
package adjudicate

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The adjudication labels. An item whose pattern has no
// label (F harness, INCONSISTENT, AGREE3) keeps the class xe-6 gave it.
const (
	LabelGorgeSuspect   = "gorge_suspect"
	LabelXMageSuspect   = "xmage_suspect"
	LabelForgeSuspect   = "forge_suspect"
	LabelThreeWay       = "three_way"
	LabelGorgeForgeOnly = "gorge_forge_only"
)

// Label maps a pattern to its label; "" for a pattern that has none.
func Label(p oraclediff.Pattern) string {
	switch p {
	case oraclediff.ForgeXMageG:
		return LabelGorgeSuspect
	case oraclediff.GorgeForgeX:
		return LabelXMageSuspect
	case oraclediff.GorgeXMageF:
		return LabelForgeSuspect
	case oraclediff.AllDiffer:
		return LabelThreeWay
	case oraclediff.GorgeForgeNoX, oraclediff.GorgeNoForgeNoX:
		return LabelGorgeForgeOnly
	}
	return ""
}

// Eligible reports whether an item with these xe-6 classes (XMage's and
// Forge's) is adjudicated: at least one engine diverged, and the other either
// agreed, diverged, or did not compare at all (harness, not_sent, xmage_lacks,
// turn_cap, forge_skips_damage_step, which ThreeWay turns into the GF|- / G|F|-
// patterns or F harness). Anything else
// (rng, projection_gap, decision_shape, payment_choice, lower_gap on either
// side) keeps its class: the difference there is not yet a statement about an
// engine.
func Eligible(xClass, fClass string) bool {
	if xClass != classify.ClassDiverge && fClass != classify.ClassDiverge {
		return false
	}
	okX := func(c string) bool {
		switch c {
		case classify.ClassDiverge, classify.ClassAgree, classify.ClassHarness, classify.ClassNotSent, classify.ClassXMageLacks:
			return true
		}
		return false
	}
	okF := func(c string) bool {
		switch c {
		case classify.ClassDiverge, classify.ClassAgree, classify.ClassHarness, classify.ClassTurnCap, classify.ClassForgeSkipsDamageStep:
			return true
		}
		return false
	}
	return okX(xClass) && okF(fClass)
}

// Legs are the three pairwise verdicts of one item.
type Legs struct{ GX, GF, FX oraclediff.Verdict }

// Item adjudicates one item: g is gorge's stored result, xRow and fRow the
// XMage and Forge rows. The Forge row is compared with gorge as any follower
// row is, and with XMage through oraclediff.ForgeOracleResult, the item's own
// comparison fields and ignore list throughout.
func Item(item oraclegen.Item, g rules.OracleResult, xRow, fRow oraclediff.XResult) (oraclediff.Pattern, string) {
	p, _ := ItemLegs(item, g, xRow, fRow)
	return p, Label(p)
}

// ItemLegs is Item with the pairwise verdicts that decided it.
//
// Both followers differing from gorge are compared up to the first checkpoint either
// differs at. The engines have parted there, and the followers then replay the rest
// of a game gorge alone played, so a later difference between them says nothing.
func ItemLegs(item oraclegen.Item, g rules.OracleResult, xRow, fRow oraclediff.XResult) (oraclediff.Pattern, Legs) {
	g, xRow, fRow = cutToPartial(item, g, classify.SkipEarlyCasts(item, g, xRow), fRow)
	p, legs := itemLegs(item, g, xRow, fRow)
	x, xok := firstDifference(g, legs.GX)
	f, fok := firstDifference(g, legs.GF)
	if !xok || !fok || p != oraclediff.AllDiffer {
		return p, legs
	}
	n := min(x, f) + 1
	g.Snapshots = g.Snapshots[:n]
	xRow.Snapshots = xRow.Snapshots[:n]
	fRow.Snapshots = fRow.Snapshots[:n]
	return itemLegs(item, g, xRow, fRow)
}

func itemLegs(item oraclegen.Item, g rules.OracleResult, xRow, fRow oraclediff.XResult) (oraclediff.Pattern, Legs) {
	gx := oraclediff.CompareOpts(g, nil, xRow, item.Compare, item.Ignore...)
	gf := oraclediff.CompareOpts(g, nil, fRow, item.Compare, item.Ignore...)
	forgeHarness := fRow.Harness != ""
	// The F/X leg needs both engines' snapshots: it is skipped when Forge
	// harnessed, or gorge/XMage did not compare (ThreeWay reads that as the
	// GF|- patterns). The same gate cmd/oraclediff adjudicate uses.
	var fx oraclediff.Verdict
	if !forgeHarness && gf.Status != oraclediff.Harness &&
		(gx.Status == oraclediff.Agree || gx.Status == oraclediff.Diverge) {
		fx = oraclediff.CompareOpts(oraclediff.ForgeOracleResult(fRow), nil, xRow, item.Compare, item.Ignore...)
	}
	tw := oraclediff.ThreeWay(gx, gf, fx, forgeHarness, false)
	return tw.Pattern, Legs{GX: gx, GF: gf, FX: fx}
}

// firstDifference is the snapshot a diverging leg first differs at; ok is false for
// a leg that agreed or differed in no snapshot.
func firstDifference(g rules.OracleResult, v oraclediff.Verdict) (i int, ok bool) {
	if v.Status != oraclediff.Diverge {
		return 0, false
	}
	for i, s := range g.Snapshots {
		if s.Checkpoint == v.Checkpoint {
			return i, true
		}
	}
	return 0, false
}

// cutToPartial cuts all three results to the checkpoints a follower that stopped
// after a difference reached (classify.Partial), so that the legs compare the same
// stretch of the game. A row that stopped with no difference keeps its harness.
func cutToPartial(item oraclegen.Item, g rules.OracleResult, xRow, fRow oraclediff.XResult) (rules.OracleResult, oraclediff.XResult, oraclediff.XResult) {
	n := len(g.Snapshots)
	_, _, xPart := classify.Partial(item, g, xRow)
	_, _, fPart := classify.Partial(item, g, fRow)
	if xPart {
		n = min(n, len(xRow.Snapshots))
		xRow.Harness = ""
	}
	if fPart {
		n = min(n, len(fRow.Snapshots))
		fRow.Harness = ""
	}
	if !xPart && !fPart {
		return g, xRow, fRow
	}
	cut := func(r oraclediff.XResult) oraclediff.XResult {
		if r.Harness == "" && len(r.Snapshots) > n {
			r.Snapshots = r.Snapshots[:n]
		}
		return r
	}
	g.Snapshots = g.Snapshots[:n]
	return g, cut(xRow), cut(fRow)
}
