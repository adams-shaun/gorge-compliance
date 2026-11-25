#!/usr/bin/env bash
# forge-oracle-test-unaffordable-unless-cost.sh -- driver unit test for an
# unaffordable UnlessCost$ (xproof cluster xpt-25d9fc42): Nimble Hobbit
# attacks with no mana and no Food, so neither choice its attack trigger's
# GenericChoice offers (PayW's {2}{W}, PaySacFood's sacrifice) is payable.
# gorge's GenericChoiceAvailable drops both and logs no unless_pay decision
# (CR 118.3: a player cannot pay a cost without the resources to pay it in
# full), so declining is the one legal answer. Forge's ChooseGenericEffect
# filters with Cost.canPay, which does not check mana, so PayW survives and
# AbilityUtils.handleUnlessCost calls ScriptedController.payCostToPreventEffect;
# with no answer it used to strict-miss "Missing unless-pay answer at step 6".
# The driver now notes "unless-pay step N: unaffordable, declined" and returns
# false without a miss. A second case gives p0 three Plains: the cost is then
# payable, no answer was taken, and the ask must STILL be a strict miss (that
# is a real decision-shape gap, not affordability). The request is the
# forge-export of the find's scenario with the attack rewritten through a
# pass_to, so it carries no Forge card script.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts both named tests passed (2 runs, 0 failures), not just
# the exit status.
# Usage: forge-oracle-test-unaffordable-unless-cost.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-unaffordable-unless-cost}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-unaffordable-unless-cost: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/unaffordable-unless-cost.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.UnlessCostUnaffordableTest \
  bash "$runner" 2>&1 | tee "$log"

grep -qx "PASS UnlessCostUnaffordableTest.unaffordableUnlessCostIsDeclinedWithoutAMiss" "$log" \
  || fail "UnlessCostUnaffordableTest.unaffordableUnlessCostIsDeclinedWithoutAMiss did not pass; the pinned driver is missing the unaffordable unless-cost decline"
grep -qx "PASS UnlessCostUnaffordableTest.affordableUnlessCostWithNoAnswerIsStillAMiss" "$log" \
  || fail "UnlessCostUnaffordableTest.affordableUnlessCostWithNoAnswerIsStillAMiss did not pass; the affordability boundary is wrong"
want=2
passes=$(grep -c '^PASS UnlessCostUnaffordableTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "UnlessCostUnaffordableTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" \
  || fail "TestNG summary is not $want run / $want passes / 0 failures"
