#!/usr/bin/env bash
# forge-oracle-test-setup-life-replacement.sh -- driver test for a seat's setup
# "life" (xproof cluster xpt-57a0a28c, Angel of Vitality): the scenario setup
# gives p0 life 25 with Angel of Vitality on the battlefield ("If you would gain
# life, you gain that much life plus 1 instead"), gorge's setup snapshot reports
# life 25, and the Forge driver used to write the life with Player.setLife after
# the battlefield was placed. Forge turns a raise into gainLife and a drop into
# loseLife (CR 119.5), so the battlefield's replacement (CR 614) made it 26 and a
# "whenever an opponent loses life" trigger fired on a setup life below 20. A
# scenario's starting life is a state, not an event: the driver now writes every
# seat's life before any permanent exists.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the four named tests passed, not just the exit status.
# Usage: forge-oracle-test-setup-life-replacement.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-setup-life-replacement}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-setup-life-replacement: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/setup-life-replacement.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.SetupLifeReplacementTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in aLifeGainReplacementOnTheBattlefieldDoesNotChangeTheSetupLife theReplacementStillAppliesToALaterRealGain aLifeLossTriggerDoesNotSeeTheSetupLife aSecondSeatsReplacementDoesNotChangeItsSetupLife; do
  grep -qx "PASS SetupLifeReplacementTest\.$t" "$log" || fail "SetupLifeReplacementTest.$t did not pass; the pinned driver writes the setup life through gain/loss replacements"
done
want=4
passes=$(grep -c '^PASS SetupLifeReplacementTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "SetupLifeReplacementTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
echo "PASS the setup life is a state: no gain/loss replacement or trigger touches it"
