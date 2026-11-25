#!/usr/bin/env bash
# forge-oracle-test-krrik-life-instead-of-b.sh -- driver unit test for K'rrik,
# Son of Yawgmoth's "for each {B} in a cost, you may pay 2 life rather than pay
# that mana" (Forge: PayLifeInsteadOf:B; CR 601.2h, 118.9; xproof cluster
# xpt-eaa38e91). gorge's pay_life pick on a plain {B} pip was never taken: the
# scripted controller paid only Phyrexian pips with life, so Muck Rats' cost
# stayed unpaid and the cast was refused "was not put on the stack". The pick
# now pays one black shard with 2 life while the player has the keyword (Forge's
# AI and GUI do the same); a {B} pip with no pick stays a mana payment.
# PhyrexianLifePaymentTest.krrikPaysTheBlackPipWithTwoLife replays the exported
# xs:XP:k-rrik-son-of-yawgmoth:f0t0:s1 request (Muck Rats on the stack, life 18,
# Swamp untapped); krrikWithoutALifePickStillPaysMana is the no-pick control.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the tests passed (the whole PhyrexianLifePaymentTest class, 4 run, 0 failures), not just the exit status.
# Usage: forge-oracle-test-krrik-life-instead-of-b.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-krrik-life-instead-of-b}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-krrik-life-instead-of-b: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/krrik-life-instead-of-b.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.PhyrexianLifePaymentTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames the
# class): assert the named tests passed, not just the exit status.
for t in krrikPaysTheBlackPipWithTwoLife krrikWithoutALifePickStillPaysMana; do
  grep -qx "PASS PhyrexianLifePaymentTest\.$t" "$log" || fail "PhyrexianLifePaymentTest.$t did not pass; the pinned driver is missing the K'rrik life payment"
done
want=4
passes=$(grep -c '^PASS PhyrexianLifePaymentTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "PhyrexianLifePaymentTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
