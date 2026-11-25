#!/usr/bin/env bash
# forge-oracle-test-graveyard-transit-order.sh -- driver unit test for an
# unpinned simultaneous zone order.
#
# Sorin, Lord of Innistrad's -6 destroys p1's Crimson Kobolds and Tibalt, the
# Fiend-Blooded, then a ChangeZoneAll returns them to the battlefield in the
# SAME resolution. Both cards are in neither checkpoint, so forge_order.go
# cannot derive a CR 404.3 graveyard_order pin, and Forge still asks "order to
# Graveyard" (and the return asks "order to Battlefield"). StepMachine.orderCards
# used to strict-miss both asks with an empty leftover. It now keeps
# Forge's offered order for an unpinned ask at exactly a graveyard, a
# battlefield or a hand: Wrenn and Seven's -8 returns the planeswalker and
# seven Forests to the hand, whose snapshot order is unobservable
# (SnapshotWriter sorts hand names), so no pin can exist for it either. Every
# other zone (library arrange/dig, exile, command, stack, sideboard) still
# strict-misses, and a pin a checkpoint can see is still consumed and still
# orders the ask.
#
# OrderCardsTest.unpinnedGraveyardOrderKeepsForgeOrder,
# .unpinnedBattlefieldOrderKeepsForgeOrder, .unpinnedHandOrderKeepsForgeOrder,
# .unpinnedOtherZoneStillLooseMisses and .pinnedGraveyardOrderStillApplies pin
# both halves. The wrapper asserts the 14-test count and each of those five
# names, not just the class's exit
# status: a pin that predates these tests (c9419 has nine) would otherwise run
# green and silently drop the coverage. Needs the out-of-tree build at
# FORGE_ORACLE_REF (make
# forge-oracle-setup), like the other forge-oracle-test-*.sh. Usage:
# forge-oracle-test-graveyard-transit-order.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-graveyard-transit-order}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-graveyard-transit-order: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/order-cards.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.OrderCardsTest \
  bash "$runner" 2>&1 | tee "$log"

want=14
passes=$(grep -c '^PASS OrderCardsTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "OrderCardsTest passed $passes tests, want $want: the pinned driver is missing the transit-order tests"
for t in unpinnedGraveyardOrderKeepsForgeOrder unpinnedBattlefieldOrderKeepsForgeOrder \
  unpinnedHandOrderKeepsForgeOrder unpinnedOtherZoneStillLooseMisses \
  pinnedGraveyardOrderStillApplies; do
  grep -qx "PASS OrderCardsTest\.$t" "$log" || fail "OrderCardsTest.$t did not pass; the pinned driver is missing the transit-order fix"
done
