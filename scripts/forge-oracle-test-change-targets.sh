#!/usr/bin/env bash
# forge-oracle-test-change-targets.sh -- driver test for an Optional$
# ChangeTargets confirm on a spell with no targets (xproof cluster
# xpt-bf9a34cd): p1 casts Reach Through Mists ("Draw a card", no targets) and
# p0 casts Redirect ("You may choose new targets for target spell"), which
# targets it. A spell with no targets has nothing to choose anew (CR 115.7),
# so gorge's effChangeTargets skips the declaration and logs no decision;
# Forge's ChangeTargetsEffect still asks "Do you want to change targets of
# Reach Through Mists?" (its own TODO). The driver answers no when no yes/no
# is queued and no subject spell or subability holds a target.
#
# The targeted half is the same Redirect targeting a Shock that owns a target:
# gorge logs a retarget choose ("Choose new target"), so the driver answers the
# confirm from it (an empty pick is a recorded decline; a pick answers yes and
# StepMachine.retargetTargets applies it), instead of strict-missing. A target
# with a queued yes/no still takes that answer, and one with no answer keeps
# the strict miss.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the five named tests passed, not just the exit status.
# Usage: forge-oracle-test-change-targets.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-change-targets}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-change-targets: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/change-targets.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.ChangeTargetsConfirmTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in targetlessSpellDeclinesTheConfirm targetedSpellConsumesTheRetargetDecision targetedSpellAppliesTheRetargetPick targetedSpellConsumesTheQueuedYesNo targetedSpellWithoutAnAnswerStaysStrictMiss; do
  grep -qx "PASS ChangeTargetsConfirmTest\.$t" "$log" || fail "ChangeTargetsConfirmTest.$t did not pass; the pinned driver is missing the empty Optional\$ ChangeTargets confirm fix"
done
want=5
passes=$(grep -c '^PASS ChangeTargetsConfirmTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "ChangeTargetsConfirmTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
