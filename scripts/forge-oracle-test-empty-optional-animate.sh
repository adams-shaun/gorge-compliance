#!/usr/bin/env bash
# forge-oracle-test-empty-optional-animate.sh -- driver unit test for an
# Optional$ Animate confirm with nothing to animate (xproof cluster
# xpt-fc2992c4): Sita Varma, Masked Racer's Exhaust with X = 0 and no other
# creature. "You may have the base power and toughness of each other creature
# you control become ..." is an election with no board effect either way
# (CR 608.2d), so gorge poses no ask and logs no yes/no; Forge's AnimateEffect
# still calls confirmAction("It has base power and toughness 2/2 until end of
# turn."). The driver answers no when no yes/no is queued and no battlefield
# card would be animated, and keeps the strict miss when one would be (the same
# request with a Grizzly Bears on p0's battlefield).
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts both tests passed (2 run, 0 failures), not just the exit status.
# Usage: forge-oracle-test-empty-optional-animate.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-empty-optional-animate}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-empty-optional-animate: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/empty-optional-animate.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.EmptyOptionalAnimateConfirmTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames the
# class): assert each named test passed, not just the exit status.
for t in noOtherCreatureDeclinesTheConfirm anotherCreatureKeepsTheStrictMiss; do
  grep -qx "PASS EmptyOptionalAnimateConfirmTest\.$t" "$log" || fail "EmptyOptionalAnimateConfirmTest.$t did not pass; the pinned driver is missing the empty Optional\$ Animate confirm fix"
done
want=2
passes=$(grep -c '^PASS EmptyOptionalAnimateConfirmTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "EmptyOptionalAnimateConfirmTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
