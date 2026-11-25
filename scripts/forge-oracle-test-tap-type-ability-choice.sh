#!/usr/bin/env bash
# forge-oracle-test-tap-type-ability-choice.sh -- driver unit test for the
# either-or additional-cost ability choice of a tap-a-named-type cost (xproof
# cluster xpt-bf618006, Disruption Protocol, gorge~forge): gorge is right (CR
# 601.2b: the caster may tap an untapped artifact OR pay {1}), but the
# ScriptedController.getAbilityToPlay verb fallback compared the CostPart class
# name against "cost" + verb ("costtap") while the class of tapXType<1/Artifact>
# is CostTapType, and Forge prints the part as "Tap 1 untapped Artifact you
# control", so gorge's pick label "Tap 1 artifact" bound nothing and the strict
# run missed ("Missing ability choice answer at step 7"). getAbilityToPlay now
# binds through an explicit verb-to-class map (tap this permanent -> CostTap,
# any other tap label -> CostTapType; the same self-vs-type split for untap,
# plus pay life/energy/mana, take damage, gain life, add counters, ...).
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the test passed (1 run, 0 failures), not just the exit status.
# Usage: forge-oracle-test-tap-type-ability-choice.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-tap-type-ability-choice}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-tap-type-ability-choice: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/tap-type-ability-choice.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.TapTypeAbilityChoiceTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames the
# class): assert the named test passed, not just the exit status.
grep -qx "PASS TapTypeAbilityChoiceTest\.disruptionProtocolTapPickBindsCostTapType" "$log" || fail "TapTypeAbilityChoiceTest.disruptionProtocolTapPickBindsCostTapType did not pass; the pinned driver is missing the tap-type ability choice fix"
want=1
passes=$(grep -c '^PASS TapTypeAbilityChoiceTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "TapTypeAbilityChoiceTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
