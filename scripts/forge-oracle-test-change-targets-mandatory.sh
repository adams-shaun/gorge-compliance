#!/usr/bin/env bash
# forge-oracle-test-change-targets-mandatory.sh -- driver test for a mandatory
# ChangeTargets redirect's resolution-time retarget pick (xproof cluster
# xpt-6b85f89a): p0's Revolutionary Rebuff targets Reach Through Mists; p1
# casts Redirect Lightning (additional cost 5 life) targeting the Rebuff; when
# Redirect resolves, gorge's effChangeTargets poses a one-candidate choose ask
# (CR 115.7a: a mandatory change cannot keep the old target, so the Mists is
# not offered) retargeting the Rebuff to Redirect Lightning (still a
# nonartifact spell on the stack, CR 608.2). The pick carries no option label,
# so it travels in pick_refs alone; gorge marks the decision retarget.
#
# The driver consumes the pick through StepMachine.retargetTargets at Forge's
# chooseNewTargetsFor (no cast-time confirm: the card is not Optional$), the
# Rebuff then counters Redirect Lightning, and p1's graveyard ends
# [Redirect Lightning] with the Mists still in hand. The pre-retarget driver
# strict-missed "Missing new targets answer at step 11" and kept the old
# target, leaving [Redirect Lightning, Reach Through Mists] in p1's graveyard.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the named test passed, not just the exit status.
# Usage: forge-oracle-test-change-targets-mandatory.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-change-targets-mandatory}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-change-targets-mandatory: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/change-targets-mandatory.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.ChangeTargetsMandatoryTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in mandatoryChangeTargetsAppliesTheRetargetPick; do
  grep -qx "PASS ChangeTargetsMandatoryTest.$t" "$log" || fail "ChangeTargetsMandatoryTest.$t did not pass; the pinned driver is missing the mandatory ChangeTargets retarget fix"
done
want=1
passes=$(grep -c '^PASS ChangeTargetsMandatoryTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "ChangeTargetsMandatoryTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
