#!/usr/bin/env bash
# Replay two fixture windows through the production ScenarioReplay and assert
# that a mana ability is activated on the permanent the step names:
# Hidden Grotto (not the untapped Island, pool C) and Swamp#2 (not Swamp#1).
# Requires the read-only XMage build, like the driver's other javac checks.
set -euo pipefail
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
out=$(realpath -m "${1:-.ds4/scratch/mana-scope-driverout}")
here=$(cd "$(dirname "$0")/.." && pwd)
fixture="$here/oracle/xmageoracle/test/fixtures/mana-scope.jsonl"
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayManaAbilityScopedToCardTest.java"
# The game's H2 card database resolves from its working directory, and the
# XMage build tree is read-only: run from a scratch cwd with a private copy of
# the DB, as scripts/xmage-oracle-run.sh does for parallel replays.
run="$out.cwd"
rm -rf "$run"
mkdir -p "$run/db"
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run/"
done
cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run/db/"
cd "$run"
systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=600% -- \
  "$JAVA_HOME/bin/java" -Xmx1g -XX:ActiveProcessorCount=2 \
  -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayManaAbilityScopedToCardTest "$fixture"
