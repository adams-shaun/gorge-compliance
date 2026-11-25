#!/usr/bin/env bash
# forge-oracle-test-counter-any-mixed-kinds.sh -- driver unit test for a
# remove-any-type counter removal over two counter kinds (CR 122.1).
#
# Replays the exported xproof request xs:XP:mabel-bitter-recluse:cast:s1
# (fixture forge-oracle/fixtures/unmapped/counter-remove-any-mixed-kinds.jsonl)
# through the fork driver in strict mode: Mabel, Bitter Recluse removes
# LOYALTY, LOYALTY, P1P1 from Ajani Resolute (two loyalty, one +1/+1), so
# Ajani is in p1's graveyard after step 3 (CR 704.5i), with no harness miss
# and no unconsumed gorge decision. chooseCounterType used to take the first
# LOYALTY pick, so the amount ask counted one loyalty and Ajani survived.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-counter-any-mixed-kinds.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-counter-any-mixed-kinds}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-counter-any-mixed-kinds: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"
fixture=$forge/forge/forge-oracle/fixtures/unmapped/counter-remove-any-mixed-kinds.jsonl
[ -s "$fixture" ] || fail "no $fixture: the Forge build predates the mixed-kinds driver fix"

FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.UnmappedAskTest \
  bash "$runner"
