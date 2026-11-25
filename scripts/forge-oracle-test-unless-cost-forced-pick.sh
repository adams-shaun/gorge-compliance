#!/usr/bin/env bash
# forge-oracle-test-unless-cost-forced-pick.sh -- driver unit test for a paid
# UnlessCost$ card cost whose option list is exactly the amount (xproof cluster
# xpt-4561863a): Ill-Timed Explosion's Discard<2/Card> from the two drawn cards
# and Rent Is Due's "tap two untapped creatures" with exactly Ghostly Dancers and
# Nyxborn Courser. gorge's unless_pay answer elects the payment, an elected
# payment pays the whole cost (CR 118.12, 601.2h), and gorge logs no card pick
# for a forced one, so Forge must ask amount..amount, not 0..amount. A third
# replay (a three-card hand) checks a logged discard pick is still consumed.
# Each replay must be strict with no harness miss and no unconsumed decision.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-unless-cost-forced-pick.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-unless-cost-forced-pick}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-unless-cost-forced-pick: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.UnlessCostForcedPickTest \
  bash "$runner"
