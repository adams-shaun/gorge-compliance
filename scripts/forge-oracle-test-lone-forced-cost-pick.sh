#!/usr/bin/env bash
# forge-oracle-test-lone-forced-cost-pick.sh -- driver unit test for a forced
# lone card cost that must leave a queued pick for the ask that follows (xproof
# clusters xpt-5e60af5c / xpt-f2f006f3): The Gitrog, Ravenous Ride's saddle
# trigger pays "Sacrifice a creature that saddled it" with Memnite as the only
# saddler, so the pick is forced and gorge logs none. The queued p0:Swamp pick
# answers the later "put up to X land cards from your hand onto the battlefield"
# ask of the same step; StepMachine.pickObjects used to consume it for the
# Sacrifice cost and strict-miss ("gorge pick p0:Swamp is not among Forge's 1
# options"). A forced pick now takes a queued pick only when its refs are among
# the options.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the test passed (1 run, 0 failures), not just the exit status.
# Usage: forge-oracle-test-lone-forced-cost-pick.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-lone-forced-cost-pick}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-lone-forced-cost-pick: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/lone-forced-cost-pick.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.LoneForcedCostPickTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames the
# class): assert the named test passed, not just the exit status.
grep -qx "PASS LoneForcedCostPickTest\.gitrogLoneSacrificeLeavesLandPick" "$log" || fail "LoneForcedCostPickTest.gitrogLoneSacrificeLeavesLandPick did not pass; the pinned driver is missing the lone forced cost pick fix"
want=1
passes=$(grep -c '^PASS LoneForcedCostPickTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "LoneForcedCostPickTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
