package main

// phase.rs as the fourth column of `oraclediff adjudicate` (-phase-diff).
// The three-way pattern, the ledger and every existing report field are
// computed as before; this adds where phase.rs stands to each classified
// report row, from its follower-diff rows (the G/P leg), its result cache (the
// P/F and P/X legs) and its follower-export refusals (unsupported).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// phaseLeg is phase.rs's side of an adjudication.
type phaseLeg struct {
	rows    map[string]ForgeRow // follower-diff -engine phase, by scenario sha
	cache   oraclediff.Cache    // phase.rs results by request sha; Dir "" = none
	refused map[string]string   // follower-export refusals: item id -> reason
}

// legs3 are the three-way legs of one scenario.
type legs3 struct {
	gx, gf, fx   oraclediff.Verdict
	forgeHarness bool
}

func loadPhaseLeg(diffPath, cacheDir, prepPath string) (*phaseLeg, error) {
	rows, _, err := readForgeRows(diffPath)
	if err != nil {
		return nil, err
	}
	ph := &phaseLeg{rows: rows, cache: oraclediff.Cache{Dir: cacheDir}, refused: map[string]string{}}
	if prepPath == "" {
		prepPath = filepath.Join(filepath.Dir(diffPath), "phase-prep.jsonl")
		if _, err := os.Stat(prepPath); errors.Is(err, os.ErrNotExist) {
			return ph, nil
		}
	}
	err = readLines(prepPath, func(b []byte) error {
		var r struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		ph.refused[r.ID] = r.Reason
		return nil
	})
	return ph, err
}

func comparedLeg(v oraclediff.Verdict) bool {
	return v.Status == oraclediff.Agree || v.Status == oraclediff.Diverge
}

// phaseLacks reports a G/P verdict that is phase.rs missing a card.
func phaseLacks(v oraclediff.Verdict) bool {
	return v.Status == oraclediff.XMageLacks || (v.Status == oraclediff.Harness && strings.Contains(v.Msg, "phase lacks card"))
}

// place fills rep's phase.rs column. fsnap and xsnap are Forge's and XMage's
// results when the F/X leg was compared (nil otherwise).
func (ph *phaseLeg) place(rep *reportRow, it oraclegen.Item, sha string, l legs3, fsnap, xsnap *oraclediff.XResult) {
	marks := map[string]string{}
	if l.forgeHarness || !comparedLeg(l.gf) {
		marks["F"] = oraclediff.MarkUncompared
	}
	if !comparedLeg(l.gx) {
		marks["X"] = oraclediff.MarkUncompared
		if l.gx.Status == oraclediff.XMageLacks {
			marks["X"] = oraclediff.MarkUnsupported
		}
	}
	var gp, fp, px oraclediff.Verdict
	pr, have := ph.rows[sha]
	switch reason, refused := ph.refused[it.ID]; {
	case refused && strings.HasSuffix(reason, "_lacks_card"):
		marks["P"] = oraclediff.MarkUnsupported
	case refused || !have:
		marks["P"] = oraclediff.MarkUncompared
	case phaseLacks(pr.Verdict):
		marks["P"] = oraclediff.MarkUnsupported
	case !comparedLeg(pr.Verdict):
		marks["P"] = oraclediff.MarkUncompared
	default:
		gp = pr.Verdict
		if gp.Status == oraclediff.Diverge {
			rep.GPField = gp.Field
		}
		if psnap, ok := ph.cache.Get(pr.RequestSHA); ok {
			if fsnap != nil {
				fp = oraclediff.CompareOpts(oraclediff.ForgeOracleResult(*fsnap), nil, psnap, it.Compare, it.Ignore...)
			}
			if xsnap != nil {
				px = oraclediff.CompareOpts(oraclediff.ForgeOracleResult(psnap), nil, *xsnap, it.Compare, it.Ignore...)
			}
		}
	}
	eq := func(a, b string) (bool, bool) {
		v := map[string]oraclediff.Verdict{"GF": l.gf, "GX": l.gx, "FX": l.fx, "GP": gp, "FP": fp, "PX": px}[a+b]
		if !comparedLeg(v) {
			return false, false
		}
		return v.Status == oraclediff.Agree, true
	}
	nv := oraclediff.NWay([]string{"F", "P", "X"}, marks, eq, map[string]string{"F": "forge", "P": "phase", "X": "xmage"})
	rep.Four, rep.FourLabel, rep.Phase = nv.Pattern, nv.Label, nv.Vote("P")
}
