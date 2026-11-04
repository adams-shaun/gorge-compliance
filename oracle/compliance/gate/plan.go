package gate

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Need is what a compliance pass must do for one generated scenario
// (rerun only
// stale rows).
type Need int

const (
	// Fresh: the committed verdict is for this exact scenario at this
	// XMAGE_REF and driver, it passes, and gorge still meets its frozen
	// expectation. Nothing to do; the gate re-checks it.
	Fresh Need = iota
	// Rediff: the verdict must be recomputed and XMage's result for this
	// exact scenario is already cached, so no Java runs.
	Rediff
	// Replay: the verdict must be recomputed and XMage has to replay it.
	Replay
)

func (n Need) String() string {
	switch n {
	case Fresh:
		return "fresh"
	case Rediff:
		return "rediff"
	}
	return "replay"
}

// PlanItem decides what a pass does for one scenario. A row is stale when
// it is missing, was recorded for another scenario, XMAGE_REF or driver,
// does not pass (diverge, harness, gorge_wrong: a gorge fix may have flipped
// it), or passes but gorge no longer meets its frozen expectation. reason
// names which.
func PlanItem(reg *cards.Registry, it oraclegen.Item, row compliance.VerdictRow, have bool, ref, driver string, cache oraclediff.Cache) (Need, string) {
	sha := ItemSHA(it)
	reason := ""
	switch {
	case !have:
		reason = "no verdict"
	case row.FromForge():
		// A Forge-only verdict never satisfies the XMage pass: XMage must
		// replay or re-diff the scenario so its comparison can supersede it.
		reason = "forge verdict (no XMage comparison yet)"
	case row.ScenarioSHA != sha:
		reason = "scenario changed"
	case row.XMageRef != ref:
		reason = "xmage_ref changed"
	case row.Status == compliance.StatusXMageLacks:
		// XMage's card database lacks the card, so the committed row is
		// terminal: there is nothing to replay.
		return Fresh, ""
	case row.Status != compliance.StatusAgree && row.Status != compliance.StatusXMageWrong:
		reason = "status " + row.Status
	default:
		// XMage's side is a function of the scenario, XMAGE_REF and the
		// driver source: a driver edit can move any passing row.
		if row.XMageDriver != driver {
			reason = "xmage_driver changed"
		} else if ok, why := StillMeets(reg, it, row); !ok {
			reason = why
		}
	}
	if reason == "" {
		return Fresh, ""
	}
	if _, ok := cache.Get(sha); ok {
		return Rediff, reason
	}
	return Replay, reason
}

// StillMeets reports whether gorge at this head still produces the result a
// passing verdict row froze, and if not, why. A row checks its frozen
// fields; a legacy row its whole-snapshot hash.
func StillMeets(reg *cards.Registry, it oraclegen.Item, row compliance.VerdictRow) (bool, string) {
	if len(row.Frozen) == 0 && row.CanonSHA == "" {
		return false, "no frozen expectation"
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		return false, "gorge replay: " + err.Error()
	}
	if len(row.Frozen) > 0 {
		if ok, why := oraclediff.MeetsOpts(row.Frozen, res, it.Compare, it.Ignore...); !ok {
			return false, "gorge no longer meets the frozen expectation: " + why
		}
		return true, ""
	}
	if Hash([]byte(oraclediff.CanonicalOpts(res.Snapshots, it.Compare, it.Ignore...))) != row.CanonSHA {
		return false, "gorge no longer meets the frozen expectation"
	}
	return true, ""
}

// Freeze replays a generated scenario in gorge and returns its frozen
// expectation (oraclediff.Freeze).
func Freeze(reg *cards.Registry, it oraclegen.Item) ([]compliance.Frozen, error) {
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		return nil, err
	}
	return oraclediff.FreezeOpts(res, it.Compare, it.Ignore...), nil
}
