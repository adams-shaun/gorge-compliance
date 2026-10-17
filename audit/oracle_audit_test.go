package audit_test

// Oracle-text card audit (gorge docs/design/2026-09-27-oracle-text-card-audit.md).
//
// Every scenario states what a card does according to its PRINTED (Oracle)
// text and the Comprehensive Rules, written by an author who never read the
// card's Forge script. gorge's runner builds a real two-seat game around real
// corpus cards, drives it only through the actions a player has (cast,
// activate, attack, block, pass priority, answer a decision), and asserts
// observable state: zones, life, P/T, keywords, counters, the stack, and what
// the engine offers. A failure therefore means the engine+script disagrees
// with the printed card, whichever side is wrong.
//
// The runner's only non-player actions are SETUP (placing named cards into
// zones and setting life totals before turn 1, as logged events) and the
// "mana"/"move"/"life" ops, which stand in for an unspecified outside
// effect. All of them are logged, so every scenario still replays from its
// log alone (OracleAuditResult.ReplayDiff).
//
// Nothing here embeds card script or Oracle text: scenarios name cards and
// the corpus is read at run time (the GPL boundary).

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards/oracletext"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestOracleScenarioFilesWellFormed needs no corpus, so it runs where the
// scenario author works -- a checkout deliberately WITHOUT .cards, so the
// author cannot read a script or fit expectations to engine output. It checks
// the schema (gorge's runner rejects unknown fields), the family directory,
// unique scenario names, the step vocabulary, ref syntax, and the authoring
// rules (every scenario states why and cr, and asserts something).
func TestOracleScenarioFilesWellFormed(t *testing.T) {
	t.Parallel()
	files := loadOracleFiles(t)
	divergent := oracleDivergent(t)
	scenarioFamily := map[string]string{}
	scenarioFile := map[string]string{}
	for p, f := range files {
		for _, raw := range f.Scenarios {
			h := headOf(t, p, raw)
			key := f.Card + "/" + h.Name
			// The same card/scenario in both repositories (gorge's residual
			// directory and this one's) is a copy that would drift.
			if prev, dup := scenarioFile[key]; dup && strings.HasPrefix(prev, "gorge:") != strings.HasPrefix(p, "gorge:") {
				t.Errorf("%s: scenario %q is also in %s (a scenario lives in exactly one repository)", p, key, prev)
			}
			scenarioFile[key] = p
			scenarioFamily[key] = f.Family
		}
	}
	for key, row := range divergent {
		fam, ok := scenarioFamily[key]
		switch {
		case !ok:
			t.Errorf("ratchet row %q names no scenario", key)
		case row.family != fam:
			t.Errorf("ratchet row %q is in %s but the scenario is family %q", key, row.src, fam)
		}
	}
	for key, row := range oracleUnconsumed(t) {
		fam, ok := scenarioFamily[key]
		switch {
		case !ok:
			t.Errorf("%s row %q names no scenario", row.src, key)
		case row.family != fam:
			t.Errorf("unconsumed row %q is in %s but the scenario is family %q", key, row.src, fam)
		}
	}
	for p, f := range files {
		names := map[string]bool{}
		for _, raw := range f.Scenarios {
			h := headOf(t, p, raw)
			where := p + ": " + h.Name
			if h.Name == "" || names[h.Name] {
				t.Errorf("%s: scenario name empty or duplicated", where)
			}
			names[h.Name] = true
			if h.Why == "" || len(h.CR) == 0 {
				t.Errorf("%s: every scenario states its Oracle/CR reasoning (why, cr)", where)
			}
			if len(h.Expect) == 0 {
				hasStepExpect := false
				for _, st := range h.Steps {
					hasStepExpect = hasStepExpect || len(st.Expect) > 0
				}
				if !hasStepExpect {
					t.Errorf("%s: asserts nothing", where)
				}
			}
			for _, prob := range rules.CheckOracleScenarioJSON(raw) {
				t.Errorf("%s: %s", where, prob)
			}
		}
	}
}

// TestOracleAudit runs every Oracle-text scenario against the real corpus.
// Filter with -run 'TestOracleAudit/<Card>/<scenario>'.
func TestOracleAudit(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	files := loadOracleFiles(t)
	divergent := oracleDivergent(t)
	unconsumed := oracleUnconsumed(t)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	for _, p := range paths {
		f := files[p]
		c, ok := reg.Lookup(f.Card)
		if !ok {
			t.Errorf("%s: %q not in the corpus", p, f.Card)
			continue
		}
		if f.OracleSHA != "" && f.OracleSHA != oracletext.Digest(c) {
			t.Errorf("%s: Oracle text changed since the scenarios were written (oracle_sha %s, corpus %s): re-derive them", p, f.OracleSHA, oracletext.Digest(c))
		}
		for _, raw := range f.Scenarios {
			h := headOf(t, p, raw)
			key := f.Card + "/" + h.Name
			// seen is filled by the parent loop, never by a subtest closure:
			// a -run filter skips closures, and the presence check below
			// answers a static question (TestOracleUnconsumedPresenceIgnoresSubtestFilter).
			seen[key] = true
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				res, err := rules.RunOracleAuditScenarioJSON(reg, raw, false)
				if err != nil {
					t.Fatalf("%s: %v", p, err)
				}
				// Every scenario must also replay from its log alone: a
				// divergence here is an engine replay bug (or a runner write
				// outside emit), independent of the Oracle verdict and never
				// ratcheted.
				if res.ReplayDiff != "" {
					t.Errorf("log-only replay differs:\n%s", res.ReplayDiff)
				}
				fails, transcript := res.Fails, res.Transcript
				row, isKnown := divergent[key]
				known := row.reason
				if isKnown && row.family != f.Family {
					t.Errorf("ratchet row for %s is in %s but the scenario is family %q", key, row.src, f.Family)
				}
				if os.Getenv("ORACLE_AUDIT_TRACE") != "" {
					t.Logf("transcript:\n    %s", strings.Join(transcript, "\n    "))
				}
				// Split the fail list: a leftover step answer is excused only
				// by the shrinking unconsumed ratchet, and only the leftover
				// itself. Any other fail on the same scenario is still reported.
				var leftovers, otherFails []string
				for _, f := range fails {
					if strings.Contains(f, rules.OracleUnconsumedMarker) {
						leftovers = append(leftovers, f)
					} else {
						otherFails = append(otherFails, f)
					}
				}
				unc, isUnc := unconsumed[key]
				if isUnc && len(leftovers) == 0 {
					t.Errorf("stale unconsumed-answer row (the scenario now consumes every step answer; delete its row from %s): %s", unc.src, unc.reason)
				}
				if isUnc && len(leftovers) > 0 {
					t.Logf("known unconsumed answer: %s\n  observed: %s", unc.reason, strings.Join(leftovers, "\n  observed: "))
				} else {
					otherFails = append(otherFails, leftovers...)
				}
				fails = otherFails
				switch {
				case len(fails) == 0 && isKnown:
					t.Errorf("stale known divergence (the scenario now passes; delete its row from %s): %s", row.src, known)
				case len(fails) > 0 && isKnown:
					t.Logf("known divergence: %s\n  observed: %s", known, strings.Join(fails, "\n  observed: "))
				case len(fails) > 0:
					t.Errorf("%s [%s] CR %v\n  Oracle-derived: %s\n  FAIL: %s\n  transcript:\n    %s",
						f.Card, f.Family, h.CR, h.Why, strings.Join(fails, "\n  FAIL: "), strings.Join(transcript, "\n    "))
				}
			})
		}
	}
	t.Cleanup(func() {
		for key, row := range divergent {
			if !seen[key] {
				t.Errorf("%s row %q names no scenario", row.src, key)
			}
		}
		for key, row := range unconsumed {
			if !seen[key] {
				t.Errorf("%s row %q names no scenario", row.src, key)
			}
		}
	})
}
