#!/usr/bin/env bash
# forge-oracle-test-twopiles-one-card-split.sh -- driver unit test for a one-card
# TwoPiles split (CR 700.3): Epiphany at the Drownyard cast with X = 0 reveals one
# card. gorge poses no split (one partition) and logs only the opponent's pile
# pick; Forge's TwoPilesEffect still asks the separator "Divide cards into two
# piles" (1 option, 0..1), which the driver answers with the card as pile 1 when
# no gorge object pick is queued. The exported xproof request
# xs:XP:epiphany-at-the-drownyard:cast:s1 must replay in strict mode with no
# harness miss and no unconsumed gorge decision.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts both tests passed (2 run, 0 failures), not just the exit status.
# Usage: forge-oracle-test-twopiles-one-card-split.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-twopiles-one-card-split}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-twopiles-one-card-split: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/twopiles-one-card.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.TwoPilesOneCardSplitTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames the
# class): assert each named test passed, not just the exit status.
for t in oneCardSplitWithNoGorgeSplitPutsTheCardInPileOne onlyTheLoneTwoPilesAskQualifies; do
  grep -qx "PASS TwoPilesOneCardSplitTest\.$t" "$log" || fail "TwoPilesOneCardSplitTest.$t did not pass; the pinned driver is missing the one-card TwoPiles split fix"
done
want=2
passes=$(grep -c '^PASS TwoPilesOneCardSplitTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "TwoPilesOneCardSplitTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
