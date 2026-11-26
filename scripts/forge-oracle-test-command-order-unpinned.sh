#!/usr/bin/env bash
# forge-oracle-test-command-order-unpinned.sh -- driver unit test for an
# unpinned simultaneous command-zone order.
#
# ScriptedController builds the "order to " + destinationZone ask for every
# destination zone, so an "order to Command" ask is reachable (no scripted
# card is known to order to Command yet; this is latent). But a command zone
# has no order the game state can see: Forge's SnapshotWriter.commandZone
# sorts the names (Collections.sort(names)) and gorge's digest sorts a
# Commander game's Command names too ("sorted; Commander games"), so
# forge_order.go can never derive a pin for the ask, and
# StepMachine.orderCards used to strict-miss it with an empty leftover. It
# now keeps Forge's offered order for an unpinned ask at exactly a graveyard,
# a battlefield, a hand, an exile or a command zone, the same fix the hand
# and exile asks got. Every other zone (library arrange/dig, stack,
# sideboard) still strict-misses, and a pin a checkpoint can see is still
# consumed and still orders the ask.
#
# OrderCardsTest.unpinnedCommandOrderKeepsForgeOrder is red on the previous
# pin 1dea7b45ce0 and green here. The wrapper asserts the 19-test count and
# the command name, not just the class's exit status: a pin that predates the
# test would otherwise run green and silently drop the coverage. Needs the
# out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup), like the
# other forge-oracle-test-*.sh. Usage:
# forge-oracle-test-command-order-unpinned.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-command-order-unpinned}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-command-order-unpinned: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/order-cards.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.OrderCardsTest \
  bash "$runner" 2>&1 | tee "$log"

want=19
passes=$(grep -c '^PASS OrderCardsTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "OrderCardsTest passed $passes tests, want $want: the pinned driver is missing the command-order test"
for t in unpinnedGraveyardOrderKeepsForgeOrder unpinnedBattlefieldOrderKeepsForgeOrder \
  unpinnedHandOrderKeepsForgeOrder unpinnedExileOrderKeepsForgeOrder \
  unpinnedCommandOrderKeepsForgeOrder \
  unpinnedOtherZoneStillLooseMisses pinnedGraveyardOrderStillApplies; do
  grep -qx "PASS OrderCardsTest\.$t" "$log" || fail "OrderCardsTest.$t did not pass; the pinned driver is missing the unpinned-order fix"
done
