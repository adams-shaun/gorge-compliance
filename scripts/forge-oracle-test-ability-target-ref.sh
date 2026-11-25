#!/usr/bin/env bash
# forge-oracle-test-ability-target-ref.sh -- driver test for a stack-ability target ref (xproof
# cluster xpt-1d873918, Azorius Guildmage): p1 activates Dark Sphere ({T}, Sacrifice: a non-mana
# ability, so it uses the stack, CR 113.3b and 602.2) and p0 answers with Azorius Guildmage's
# "{2}{U}: Counter target activated ability" aimed at "p1:ability:Dark Sphere" (gorge's grammar
# for the k-th pending ability on the stack, oracle_run.go splitRef). The driver resolved a
# target ref only to a player or a card, so the activation was refused ("was not put on the
# stack"). AbilityTargetRefTest replays the exported request and requires the Guildmage's
# ability to sit on Dark Sphere's, then the stack empty once both pass; it also pins the ref
# grammar and the oldest-first "#k" ordinal.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the three named tests passed, not just the exit status.
# Usage: forge-oracle-test-ability-target-ref.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-ability-target-ref}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-ability-target-ref: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/ability-target-ref.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.AbilityTargetRefTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in guildmageCountersDarkSpheresActivatedAbility abilityRefGrammar nthPendingCountsOldestFirst; do
  grep -qx "PASS AbilityTargetRefTest\.$t" "$log" || fail "AbilityTargetRefTest.$t did not pass; the pinned driver cannot bind a pN:ability:Name target ref"
done
want=3
passes=$(grep -c '^PASS AbilityTargetRefTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "AbilityTargetRefTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
