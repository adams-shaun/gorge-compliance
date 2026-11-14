package adjudicate

// The fourth column: phase.rs (P) beside XMage (X) and Forge (F). The
// three-way pattern, its label, findings.jsonl, the ledger and the ticket
// drafts are computed exactly as before; this file only adds where P stands
// (oraclediff.NWay over G, F, P and X), written to fourway.jsonl and
// patterns4.tsv when the adjudication is given phase's diff.
//
// G/F/X pairs are the three-way legs, unchanged. P is compared with each of
// them over the stretch the three-way verdict was decided on: up to the first
// checkpoint where XMage or Forge differs from gorge, else the whole compared
// stretch. A P row that stopped before that point says nothing about it: it is
// "alone" when it had already parted from gorge, and uncompared otherwise. A P
// that lacks a card the item names is "unsupported" (mark "?"), never a
// disagreement.

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	followers "github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// fourLetters are the followers of the four-way pattern, and their names.
var (
	fourLetters = []string{"F", "P", "X"}
	fourNames   = map[string]string{"F": "forge", "P": "phase", "X": "xmage"}
)

// FourRow is one item's four-way line in fourway.jsonl: ids, classes,
// patterns and field names only, never a snapshot value.
type FourRow struct {
	ID string `json:"id"`
	// Card is the card of the diverging step (XMage's finding, else Forge's,
	// else phase's).
	Card string `json:"card,omitempty"`
	// Four is the oraclediff.NWay pattern over G, F, P and X.
	Four string `json:"four"`
	// Label is the four-way label (gorge_suspect, phase_suspect, split, ...).
	Label string `json:"label,omitempty"`
	// Three is the item's three-way pattern, as findings.jsonl has it.
	Three string `json:"three"`
	// Phase is where phase.rs stands: with_gorge, with_others, alone,
	// unsupported or uncompared.
	Phase  string `json:"phase"`
	XClass string `json:"xmage_class"`
	FClass string `json:"forge_class"`
	PClass string `json:"phase_class"`
	// GPField is the first field P differs from gorge at, on the compared
	// stretch; "" when they agree there or P did not compare.
	GPField string `json:"gp_field,omitempty"`
	// Note says why P did not compare, when it did not.
	Note string `json:"note,omitempty"`
}

// EligibleFour reports whether an item enters the four-way table: at least one
// follower diverged, and none is in a class that is not yet a statement about
// an engine (rng, projection_gap, decision_shape, payment_choice, lower_gap).
func EligibleFour(xClass, fClass, pClass string) bool {
	if xClass != classify.ClassDiverge && fClass != classify.ClassDiverge && pClass != classify.ClassDiverge {
		return false
	}
	ok := func(c string, also ...string) bool {
		switch c {
		case classify.ClassDiverge, classify.ClassAgree, classify.ClassHarness, classify.ClassCrewTypeDependency:
			return true
		}
		for _, a := range also {
			if c == a {
				return true
			}
		}
		return false
	}
	return ok(xClass, classify.ClassNotSent, classify.ClassXMageLacks) &&
		ok(fClass, classify.ClassTurnCap, classify.ClassForgeSkipsDamageStep) &&
		ok(pClass, classify.ClassNotSent, classify.ClassXMageLacks, classify.ClassTurnCap, classify.ClassPhaseSkipsAttackStep)
}

// phaseUnsupported reports a phase finding that is a card phase lacks: its
// preflight refused the item (not_sent with reason "phase_lacks_card"), or
// the database missed a card at run time (xmage_lacks, the classifier's
// "lacks a card" class for every follower).
func phaseUnsupported(f classify.Finding) bool {
	switch f.Class {
	case classify.ClassXMageLacks:
		return true
	case classify.ClassNotSent:
		return strings.HasSuffix(f.Msg, "_lacks_card")
	}
	return false
}

// Four places phase.rs's row beside an item's three-way verdict. xf, ff and pf
// are the three followers' xe-6 findings, which decide what did not compare.
func Four(item oraclegen.Item, g rules.OracleResult, xRow, fRow, pRow oraclediff.XResult, xf, ff, pf classify.Finding) (oraclediff.NWayVerdict, FourRow) {
	p3, legs, c := cutLegs(item, g, xRow, fRow)
	row := FourRow{ID: item.ID, Three: string(p3), XClass: xf.Class, FClass: ff.Class, PClass: pf.Class}
	marks := map[string]string{}
	// X and F: compared when their leg with gorge was.
	compared := func(v oraclediff.Verdict) bool {
		return v.Status == oraclediff.Agree || v.Status == oraclediff.Diverge
	}
	if !compared(legs.GX) {
		marks["X"] = oraclediff.MarkUncompared
		if xf.Class == classify.ClassXMageLacks || legs.GX.Status == oraclediff.XMageLacks {
			marks["X"] = oraclediff.MarkUnsupported
		}
	}
	if !compared(legs.GF) || c.F.Harness != "" || p3 == oraclediff.ForgeHarness {
		marks["F"] = oraclediff.MarkUncompared
	}
	if p3 == oraclediff.Inconsistent {
		// gorge's own side failed: no engine is placed.
		row.Four, row.Phase = string(oraclediff.Inconsistent), oraclediff.PhaseUncompared
		return oraclediff.NWayVerdict{Pattern: row.Four, Inconsistent: true}, row
	}
	// The stretch the three-way verdict was decided on.
	L := len(c.G.Snapshots)
	d := -1
	for _, v := range []oraclediff.Verdict{legs.GX, legs.GF} {
		if i, ok := firstDifference(c.G, v); ok && (d < 0 || i < d) {
			d = i
		}
	}
	if d >= 0 {
		L = d + 1
	}
	pv := map[string]oraclediff.Verdict{}
	switch {
	case phaseUnsupported(pf):
		marks["P"] = oraclediff.MarkUnsupported
		row.Note = pf.Msg
	case pf.Class != classify.ClassAgree && pf.Class != classify.ClassDiverge && pf.Class != classify.ClassCrewTypeDependency:
		marks["P"] = oraclediff.MarkUncompared
		row.Note = "phase " + pf.Class
	default:
		pr := pRow
		if fl, _ := followers.Lookup("phase"); fl.TapsWithCast {
			pr = classify.SkipEarlyCasts(item, g, pr)
		}
		if _, cp, ok := classify.Partial(item, g, pr); ok {
			pr = cp
		} else if pr.Harness != "" {
			marks["P"] = oraclediff.MarkUncompared
			row.Note = "phase harness"
			break
		}
		gs := c.G
		if m := len(pr.Snapshots); m < L {
			// P stopped before the decisive checkpoint: it counts only if it
			// had parted from gorge already.
			gs.Snapshots = gs.Snapshots[:min(m, len(gs.Snapshots))]
			v := oraclediff.CompareOpts(gs, nil, pr, item.Compare, item.Ignore...)
			if v.Status != oraclediff.Diverge {
				marks["P"] = oraclediff.MarkUncompared
				row.Note = "phase stopped before the decisive checkpoint"
				break
			}
			row.GPField = v.Field
			pv["G"], pv["F"], pv["X"] = v, oraclediff.Verdict{Status: oraclediff.Diverge}, oraclediff.Verdict{Status: oraclediff.Diverge}
			break
		}
		cutTo := func(r oraclediff.XResult) oraclediff.XResult {
			if len(r.Snapshots) > L {
				r.Snapshots = r.Snapshots[:L]
			}
			return r
		}
		gs.Snapshots = gs.Snapshots[:L]
		pr = cutTo(pr)
		pv["G"] = oraclediff.CompareOpts(gs, nil, pr, item.Compare, item.Ignore...)
		if pv["G"].Status == oraclediff.Diverge {
			row.GPField = pv["G"].Field
		}
		if _, out := marks["F"]; !out {
			pv["F"] = oraclediff.CompareOpts(oraclediff.ForgeOracleResult(cutTo(c.F)), nil, pr, item.Compare, item.Ignore...)
		}
		if _, out := marks["X"]; !out {
			pv["X"] = oraclediff.CompareOpts(oraclediff.ForgeOracleResult(pr), nil, cutTo(c.X), item.Compare, item.Ignore...)
		}
	}
	eq := func(a, b string) (bool, bool) {
		var v oraclediff.Verdict
		switch a + b {
		case "GF":
			v = legs.GF
		case "GX":
			v = legs.GX
		case "FX":
			v = legs.FX
		case "GP", "FP", "PX":
			o := a
			if a == "P" {
				o = b
			}
			v = pv[o]
		}
		if !compared(v) {
			return false, false
		}
		return v.Status == oraclediff.Agree, true
	}
	nv := oraclediff.NWay(fourLetters, marks, eq, fourNames)
	row.Four, row.Label, row.Phase = nv.Pattern, nv.Label, nv.Vote("P")
	div := xf
	switch {
	case xf.Class == classify.ClassDiverge:
	case ff.Class == classify.ClassDiverge:
		div = ff
	default:
		div = pf
	}
	row.Card = cardOf(item, div)
	return nv, row
}
