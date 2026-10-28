// Package adjudicate gives every xengine item that diverged from a follower a
// three-way verdict (gorge, XMage, Forge) with the existing section 8.1
// patterns (oraclediff.ThreeWay), and groups the gorge_suspect findings into
// ticket drafts (DESIGN xengine section 4.1, 10 O3). It adds no comparator and
// no pattern: the three pairwise legs are oraclediff.CompareOpts, unchanged.
//
// Nothing here rules: a GF|X item yields a note, never an xmage_wrong ruling or
// a verdict row (Forge design D4: Forge runs gorge's own scripts, so Forge
// agreeing with gorge is weak evidence). Output stays off-repo; the one thing
// that can be committed, the -ledger, carries our labels and field names only.
package adjudicate

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The adjudication labels (DESIGN section 4.1). An item whose pattern has no
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
// agreed, diverged, or did not compare at all (harness, xmage_lacks, which
// ThreeWay turns into the GF|- / G|F|- patterns or F harness). Anything else
// (rng, projection_gap, decision_shape, payment_choice, lower_gap on either
// side) keeps its class: the difference there is not yet a statement about an
// engine.
func Eligible(xClass, fClass string) bool {
	if xClass != classify.ClassDiverge && fClass != classify.ClassDiverge {
		return false
	}
	okX := func(c string) bool {
		switch c {
		case classify.ClassDiverge, classify.ClassAgree, classify.ClassHarness, classify.ClassXMageLacks:
			return true
		}
		return false
	}
	okF := func(c string) bool {
		switch c {
		case classify.ClassDiverge, classify.ClassAgree, classify.ClassHarness:
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
func ItemLegs(item oraclegen.Item, g rules.OracleResult, xRow, fRow oraclediff.XResult) (oraclediff.Pattern, Legs) {
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
