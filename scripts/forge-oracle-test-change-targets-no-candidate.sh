#!/usr/bin/env bash
# forge-oracle-test-change-targets-no-candidate.sh -- driver test for a mandatory
# ChangeTargets redirect with no legal new target (xproof cluster xpt-5b82851f:
# Deflection, Shunt, Swerve): p1's Aerial Volley targets only its Ornithopter;
# p0's Deflection targets the Volley; p1 then Bounce Offs the Ornithopter to
# hand. When Deflection resolves no creature with flying is on the battlefield,
# so the Volley's target stays unchanged (CR 115.7a) and gorge poses nothing.
#
# Forge still calls chooseNewTargetsFor (the change is mandatory). The driver
# counts the candidates Forge's own selection would offer and, with none, keeps
# the targets with a "new targets: no legal candidate" note instead of the strict
# miss "Missing new targets answer at step 15". A legal new target with no gorge
# decision still misses.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the named test passed, not just the exit status.
# Usage: forge-oracle-test-change-targets-no-candidate.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-change-targets-no-candidate}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-change-targets-no-candidate: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/change-targets-no-candidate.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.ChangeTargetsNoCandidateTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in noLegalNewTargetKeepsTheTargetWithoutAMiss legalNewTargetWithNoGorgeDecisionIsStillAMiss; do
  grep -qx "PASS ChangeTargetsNoCandidateTest.$t" "$log" || fail "ChangeTargetsNoCandidateTest.$t did not pass; the pinned driver is missing the mandatory ChangeTargets retarget fix"
done
want=2
passes=$(grep -c '^PASS ChangeTargetsNoCandidateTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "ChangeTargetsNoCandidateTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
