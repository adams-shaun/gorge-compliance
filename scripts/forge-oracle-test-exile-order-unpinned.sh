#!/usr/bin/env bash
# forge-oracle-test-exile-order-unpinned.sh -- driver unit test for an
# unpinned simultaneous exile order.
#
# Flickerform's ability exiles the creature it enchants and itself in ONE
# resolution (xproof cluster xpt-1d42dce4, xs:XP:flickerform:f0a0:s1,
# gorge~forge). Forge asks "order to Exile" for the two cards, but an exile
# has no order the game state can see: Forge's SnapshotWriter sorts an
# exile's names (Collections.sort(ex)) and gorge's digest documents "Hand and
# Exile are sorted names", so forge_order.go can never derive a pin for the
# ask. StepMachine.orderCards used to strict-miss it with an empty leftover;
# it now keeps Forge's offered order for an unpinned ask at exactly a
# graveyard, a battlefield, a hand, an exile or a command zone, the same fix
# the hand ask got (the Wrenn and Seven -8). Every other zone (library
# arrange/dig, stack, sideboard) still strict-misses, and a pin a checkpoint
# can see is still consumed and still orders the ask.
#
# OrderCardsTest.unpinnedExileOrderKeepsForgeOrder is red on the previous pin
# 4385467a9eb and green here; unpinnedOtherZoneStillLooseMisses now asserts
# Library instead of Exile. The wrapper asserts the 19-test count (the class
# gained unpinnedCommandOrderKeepsForgeOrder,
# scripts/forge-oracle-test-command-order-unpinned.sh) and the
# exile name, not just the class's exit
# status: a pin that predates the test would otherwise run green and silently
# drop the coverage. Needs the out-of-tree build at FORGE_ORACLE_REF (make
# forge-oracle-setup), like the other forge-oracle-test-*.sh. Usage:
# forge-oracle-test-exile-order-unpinned.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-exile-order-unpinned}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-exile-order-unpinned: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/order-cards.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.OrderCardsTest \
  bash "$runner" 2>&1 | tee "$log"

want=19
passes=$(grep -c '^PASS OrderCardsTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "OrderCardsTest passed $passes tests, want $want: the pinned driver is missing the exile-order test"
for t in unpinnedGraveyardOrderKeepsForgeOrder unpinnedBattlefieldOrderKeepsForgeOrder \
  unpinnedHandOrderKeepsForgeOrder unpinnedExileOrderKeepsForgeOrder \
  unpinnedCommandOrderKeepsForgeOrder \
  unpinnedOtherZoneStillLooseMisses pinnedGraveyardOrderStillApplies; do
  grep -qx "PASS OrderCardsTest\.$t" "$log" || fail "OrderCardsTest.$t did not pass; the pinned driver is missing the unpinned-order fix"
done
