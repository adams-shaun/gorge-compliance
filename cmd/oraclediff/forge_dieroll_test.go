package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestForgeRolledDie: the Forge request carries every die roll the lowered
// item recorded, on the step that made it, so the driver can force the same
// natural roll instead of rolling its own. The roll is not a gorge decision
// (Forge poses no decision for a die), so it must not be exported as one: a
// decision the driver never consumes would be an unconsumed leftover. The
// side field is also absent from Raw(), which is all gorge's runner reads.
// The driver-side half of the behaviour -- that MyRandom.setRandom's wrapper
// forwards the bounded draw to the installed generator -- is pinned by
// forge-oracle's ForcedDieRollTest.productionSetRandomForwardsTheBound.
func TestForgeRolledDie(t *testing.T) {
	roll := oraclegen.DieRoll{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 3}
	it := oraclegen.Item{
		ID:       "xe:die-roll",
		Template: lower.Template,
		DieRolls: []oraclegen.DieRoll{roll},
		Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
			{Op: "pass", Seat: 0},
			{Op: "activate", Seat: 0, Card: "p0:Ebony Fly"},
		}},
	}
	rq := forgeBuildRequest(nil, it, rules.OracleResult{}, nil, true)
	if !reflect.DeepEqual(rq.Item.DieRolls, []oraclegen.DieRoll{roll}) {
		t.Fatalf("request die rolls %+v, want %+v", rq.Item.DieRolls, roll)
	}
	if len(rq.GorgeDecisions) != 0 {
		t.Fatalf("die roll exported as a gorge decision: %+v", rq.GorgeDecisions)
	}
	if raw := string(rq.Item.Raw()); bytes.Contains([]byte(raw), []byte("die_rolls")) {
		t.Fatalf("Raw sends the Forge side field to gorge's runner: %s", raw)
	}
	b, err := json.Marshal(rq)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"die_rolls":[{"step":1,"seat":0,"sides":6,"natural":3,"result":3}]`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("request JSON lost the forced roll:\n%s", b)
	}
}

// forgeOracleDieRollRef is the driver commit the die-roll contract was run
// against: 7f287e888f adds ForcedDieRandom, d2939e9b61 makes MyRandom forward
// the bounded draw to it, and the current FORGE_ORACLE_REF (95020b95d4 = the same driver re-picked onto upstream and merged with the fork's gorge branch; the die-roll commits are 880a93f52d and 8922a729bb there; 2aff202bc9 = b33362f600 plus
// a resolution-time payment hook that does not touch dice; becb01adc6 = 95020b95d4 plus the setup-token keyword-counter commit, die-roll code unchanged; 0791efdeb1 = becb01adc6 plus the coin-flip force, die-roll code unchanged and
// ForcedDieRollTest re-run green beside the new ForcedCoinFlipTest; 963576e5d2 = 96acdef442 plus the waterbend_generic tap pick consumed by the scripted controller, die-roll code unchanged,
// ForcedDieRollTest and ForcedCoinFlipTest re-run green on it; ff2049dc97 = 963576e5d2 plus the MayPlay land play outside the hand,
// die-roll code unchanged, the driver's unit tests re-run green on it; cda262a7ef = ff2049dc97 plus the OriginAlternative$/endure/same-name-order mapping, die-roll code unchanged,
// ForcedDieRollTest and the full driver suite (including LandPlayMayPlayTest, DecisionQueueTest and the AltSourceAnswer/OrderCards behavior tests) re-run green on it; f01192ae90 = cda262a7ef plus the remove-counter amount, Blight cost, lone-Chooser and pile-pick mapping, die-roll code unchanged, the full driver suite (including ForcedDieRollTest and ForcedCoinFlipTest) re-run green on it; c92f40d4f49 = f01192ae90 plus the counter-amount ask routing fix (only RemoveCounter takes the per-counter picks; CountersPutEffect counter_upto stays numeric) and its regression test, die-roll code unchanged, the full driver suite (121 tests) re-run green on it; 8b2a5e3141f = c92f40d4f49 plus the forced GenericChoice mode and different-name library order mappings, die-roll code unchanged, the full driver suite (124 tests) re-run green on it; 19b0c2902c5 = c92f40d4f49 plus the Phyrexian life payment in applyManaToCost, die-roll code unchanged, the full driver suite (123 tests) re-run green on it; f789f1e46fb = the clean merge of 8b2a5e3141f and 19b0c2902c5, die-roll code unchanged, the full driver suite (126 tests, including ForcedDieRollTest, PhyrexianLifePaymentTest and LoneModeLibraryOrderTest) re-run green on it) contains both. TestForgeRolledDie only sees the Go request and stays green if the pin
// is reverted to 0958a1aca2, where Forge rolls its own d6 again; this pin check
// is the repo-side half. A bump must re-run the pinned driver's
// ForcedDieRollTest and update the constant deliberately.
const forgeOracleDieRollRef = "f789f1e46fb61b039a82d0a7ad89920da676cf97"

// TestForgeOracleRefPinsForcedDieRoll fails when the Makefile's default driver
// pin moves off the commit whose forced-die behaviour was verified. It reads
// the committed Makefile rather than the environment: the pin is what a pass
// resolves when FORGE_ORACLE_REF is unset.
func TestForgeOracleRefPinsForcedDieRoll(t *testing.T) {
	// go test runs in the package directory. Walk up rather than using
	// runtime.Caller: test-targets builds with -trimpath, so the caller path
	// is the import path, not a file on disk.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var b []byte
	for {
		b, err = os.ReadFile(filepath.Join(dir, "Makefile"))
		if err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no Makefile above %s: %v", dir, err)
		}
		dir = parent
	}
	m := regexp.MustCompile(`(?m)^FORGE_ORACLE_REF\s*\?=\s*(\S+)`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s/Makefile has no FORGE_ORACLE_REF default", dir)
	}
	if got := string(m[1]); got != forgeOracleDieRollRef {
		t.Fatalf("FORGE_ORACLE_REF = %s, want %s: re-run the pinned driver's ForcedDieRollTest and update forgeOracleDieRollRef deliberately", got, forgeOracleDieRollRef)
	}
}
