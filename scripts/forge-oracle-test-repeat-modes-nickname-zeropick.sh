#!/usr/bin/env bash
# forge-oracle-test-repeat-modes-nickname-zeropick.sh -- driver unit test for
# repeated Confluence modes (CR 700.2d), a NICKNAME trigger-order label and a
# zero-option card ask that used to consume the following ChoosePlayer pick.
#
# Replays four exported xproof requests through the fork driver in strict mode:
# Verdant Confluence binds Return, Search, Return; Wretched Confluence binds
# the same draw mode three times; Uro's sacrifice and draw ETB triggers bind
# from a NICKNAME label in gorge's order; Dawnbreak Reclaimer's empty
# graveyard card asks consume no pick and its lone-opponent ChoosePlayer still
# consumes p1. Every case must have no harness miss and no unconsumed gorge
# decision.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-repeat-modes-nickname-zeropick.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-repeat-modes-nickname-zeropick}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-repeat-modes-nickname-zeropick: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.RepeatModesNicknameZeroPickTest \
  bash "$runner"
