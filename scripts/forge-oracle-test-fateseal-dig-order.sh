#!/usr/bin/env bash
# forge-oracle-test-fateseal-dig-order.sh -- driver unit test for Spin into
# Myth's fateseal (CR 701.29): Dig with ChangeNum$ Any and LibraryPosition$ -1.
# gorge logs one choose_n[dig] pick (Ornithopter, Plains) and no order decision;
# Forge's DigEffect still asks to order the moved cards, which the driver
# answers from that pick. The exported xproof request must replay in strict
# mode with no harness miss and no unconsumed gorge decision, and leave
# Ornithopter second from the bottom of its owner's library.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-fateseal-dig-order.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-fateseal-dig-order}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-fateseal-dig-order: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.FatesealDigOrderTest \
  bash "$runner"
