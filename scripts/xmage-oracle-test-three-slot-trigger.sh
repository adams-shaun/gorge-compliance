#!/usr/bin/env bash
# Check that a trigger with three "up to one" targets (Volatile Arsonist:
# creature, player, planeswalker) gets one queued answer per slot, so each
# takes 1 damage. XMage's card at the pin damages only the first target
# (docs/upstream-bugs.md), so the fixed card from
# docs/upstream-patches/xmage-volatile-arsonist-each-target.patch is compiled
# ahead of the build unless the pinned card already has the fix. Requires the
# read-only XMage build, like a full replay.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/three-slot-trigger-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
card=Mage.Sets/src/mage/cards/v/VolatileArsonist.java
overlay="$out.card"
rm -rf "$overlay" "$overlay.src"
mkdir -p "$overlay" "$overlay.src/$(dirname "$card")"
if ! grep -q EachTargetPointer "$root/mage/$card"; then
  cp "$root/mage/$card" "$overlay.src/$card"
  (cd "$overlay.src" && patch -s -p1 <"$here/docs/upstream-patches/xmage-volatile-arsonist-each-target.patch")
  javac -J-Xmx512m -nowarn -d "$overlay" -cp "$cp" "$overlay.src/$card"
fi
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayThreeSlotTriggerTest.java"
# A full replay reads the card DB at ./db and the deck files from the test
# root, so run in a scratch directory beside OUT whose db is a private copy
# (the build root is read-only, and a shared H2 db cannot serve two JVMs).
run_dir="$out.cwd"
rm -rf "$run_dir"
mkdir -p "$run_dir/db"
trap 'rm -rf "$run_dir" "$overlay.src"' EXIT
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run_dir/"
done
cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run_dir/db/"
cd "$run_dir"
java -Xmx1g -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$overlay:$out:$cp" \
  org.mage.test.oracle.ScenarioReplayThreeSlotTriggerTest \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/fixtures"
