#!/usr/bin/env bash
# forge-oracle-test-discard-may-election.sh -- driver unit test for "you may
# discard a card" (DB$ Discard | Mode$ TgtChoose | Optional$ True; Evie Frye,
# Glacial Dragonhunt). Forge asks chooseCardsToDiscardFrom for 0..1; gorge poses
# a yes/no election (effDiscardTgtChoose, resume discard_may) and then the card
# pick, which it skips when forced. The driver must consume the election: yes
# discards the lone card (no strict miss, no leftover), no discards nothing, and
# a yes with two cards then reads gorge's card pick. DiscardMayElectionTest
# replays the exported xs:XP:evie-frye:f0a0:s1 request for all three.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-discard-may-election.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-discard-may-election}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-discard-may-election: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.DiscardMayElectionTest \
  bash "$runner"
