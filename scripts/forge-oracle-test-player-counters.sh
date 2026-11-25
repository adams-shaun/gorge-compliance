#!/usr/bin/env bash
# forge-oracle-test-player-counters.sh -- driver test for a seat's setup
# "player_counters" (xproof cluster xpt-a32b5388, Demon of Dark Schemes): the
# scenario setup gives p0 [{kind: ENERGY, n: 4}] so the {2}{B}, Pay {E}{E}{E}{E}
# ability can start, gorge's setup snapshot reports counters ENERGY=4, and the
# Forge driver used to read life, speed, the commander fields and the zones but
# never player_counters: the player started with no energy and the activation
# stopped with "was not put on the stack (cost unpaid ...)". Each entry is the
# COMPLETE count of its kind (CR 122.1; energy is a player counter, CR 107.14),
# placed as recorded state with no event and no trigger; an unknown, keyword or
# negative entry fails loudly.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the five named tests passed, not just the exit status.
# Usage: forge-oracle-test-player-counters.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-player-counters}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-player-counters: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/player-counters.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.PlayerCountersSetupTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in energyReachesThePlayerAndTheSetupSnapshot everyKindIsTheCompleteCount anUnknownKindFailsLoudly aKeywordCounterIsNotAPlayerCounter aNegativeCountFailsLoudly; do
  grep -qx "PASS PlayerCountersSetupTest\.$t" "$log" || fail "PlayerCountersSetupTest.$t did not pass; the pinned driver does not place setup player_counters"
done
want=5
passes=$(grep -c '^PASS PlayerCountersSetupTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "PlayerCountersSetupTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
echo "PASS setup player_counters reach the seat and its setup snapshot"
