#!/usr/bin/env bash
# forge-oracle-test-targets-below-min.sh -- driver unit test for a target ask
# whose legal candidates cannot reach min (xproof cluster xpt-6e2fff25).
# Kitsune, Dragon's Daughter attacks alone (p0) against a lone Grizzly Bears
# (p1) and deals 6: its trigger "you may exchange control of two other target
# creatures controlled by different players" (TargetMin$ 2 | TargetMax$ 2 |
# TargetsWithDifferentControllers) has one other creature on the board, so the
# required number of targets cannot be chosen and the ability is removed
# without going on the stack (CR 603.3d with 601.2c). gorge logs no target
# ask; Forge's TargetRestrictions.hasCandidates is true with one candidate, so
# Forge still asks, and the driver used to strict-miss "Missing target answer
# at step 9". The driver now mirrors TargetSelection's own gate (Forge cancels
# the ability when even the largest legal set cannot reach min) and declines
# without a miss. The same request with a second other creature (a Grizzly
# Bears on p0's battlefield) has enough candidates for the different-
# controllers pair and keeps the strict miss.
#
# TargetsBelowMinTest (fork fixtures targets-below-min/) replays the find and
# the control through the driver in-process, then the find's request goes
# through scripts/forge-oracle-run.sh, the exporter/runner path production
# uses. It fails before the fix with the exact strict miss.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts all three named tests passed (3 runs, 0 failures), not
# just the exit status.
# Usage: forge-oracle-test-targets-below-min.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-targets-below-min}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh
fixture=$forge/forge/forge-oracle/fixtures/targets-below-min/kitsune-fewer-targets.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-targets-below-min: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"
[ -s "$fixture" ] || fail "no $fixture: the Forge build predates the below-min target decline"

log=$out/targets-below-min.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.TargetsBelowMinTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in oneOtherCreatureDropsTheTriggerWithoutAMiss oneOtherCreatureExchangesNothing twoCreaturesKeepsTheStrictMiss; do
  grep -qx "PASS TargetsBelowMinTest\.$t" "$log" || fail "TargetsBelowMinTest.$t did not pass; the pinned driver is missing the below-min target decline"
done
want=3
passes=$(grep -c '^PASS TargetsBelowMinTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "TargetsBelowMinTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" \
  || fail "TestNG summary is not $want run / $want passes / 0 failures"

"$here/scripts/forge-oracle-run.sh" "$fixture" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-targets-below-min: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
notes = row.get("notes") or []
if not any(n.startswith("target step 9: fewer candidates than min") for n in notes):
    fail("the trigger was not declined for a below-min candidate count; notes = %r" % (notes,))
print("PASS targets-below-min: Kitsune's two-target exchange trigger is dropped without a miss, no exchange")
PY
