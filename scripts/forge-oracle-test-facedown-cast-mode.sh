#!/usr/bin/env bash
# forge-oracle-test-facedown-cast-mode.sh -- driver test for gorge's face-down
# cast modes (xproof cluster xpt-716677db): a cast_mode "morphed", "megamorphed"
# or "disguised" (CR 702.37c, 702.168a, 702.169a) hits StepMachine.spellForMode's
# default, so the driver takes the card's printed spell: Forge refuses it where
# the printed cost exceeds the mana gorge paid (Belltoll Dragon megamorphed for
# {3} over three Islands: "step 3 (cast) p0:Belltoll Dragon was not put on the
# stack") and casts face up where it does not (Sleuth Instructor {1}{W}).
#
# The driver now maps all three modes to the one face-down spell Forge builds
# per printed keyword (CardFactoryUtil.abilityCastFaceDown, a Spell with
# setCastFaceDown(true) and Cost {3}; Megamorph builds it with the same Morph
# key). The fixtures (fork fixtures/morph/) are exported xs:XP requests cut
# after the cast: no harness row, the card face-down 2/2 on the stack, out of
# the hand, the pool empty and all three lands tapped.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the named tests passed, not just the exit status.
# Usage: forge-oracle-test-facedown-cast-mode.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-facedown-cast-mode}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-facedown-cast-mode: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/facedown-cast-mode.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.FaceDownCastModeTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in megamorphedCastIsAFaceDownTwoTwo morphedCastIsAFaceDownTwoTwo disguisedCastIsAFaceDownTwoTwo; do
  grep -qx "PASS FaceDownCastModeTest.$t" "$log" || fail "FaceDownCastModeTest.$t did not pass; the pinned driver is missing the face-down cast mode fix"
done
want=3
passes=$(grep -c '^PASS FaceDownCastModeTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "FaceDownCastModeTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
echo "forge-oracle-test-facedown-cast-mode: OK"
