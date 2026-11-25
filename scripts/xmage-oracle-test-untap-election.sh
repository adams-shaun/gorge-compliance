#!/usr/bin/env bash
# Driver test: the Whip Vine untap cut (CR 502.3) — find xs:XP:whip-vine:f0a0:s1
# (xpt-4782660c). A pass ends turn 1 with Whip Vine's optional untap choice
# pending in turn 2's untap step, so gorge's cut position is (2, untap);
# phaseOfGorgeStep had no untap case and the driver threw
# "pass position in unknown step untap" before replaying anything. The untap
# step grants no priority (CR 500.4), so the queued (2, UNTAP) checkpoint can
# only fire from the ask: chooseUse's untap-election branch flushes the
# runCode actions, answers leave-tapped (gorge left the choice pending) and
# pauses the game, the row complete at 23 snapshots. Requires the read-only
# XMage build, like a full replay.
# Usage: xmage-oracle-test-untap-election.sh [OUTDIR]
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/untap-election-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayUntapElectionTest.java"
# The fixture replay reads the card DB at ./db and the deck files from the test
# root, so run in a scratch directory beside OUT whose db is a private copy
# (the build root is read-only, and a shared H2 db cannot serve two JVMs).
run_dir="$out.cwd"
rm -rf "$run_dir"
mkdir -p "$run_dir/db"
trap 'rm -rf "$run_dir"' EXIT
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run_dir/"
done
cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run_dir/db/"
cd "$run_dir"
java -Xmx1g -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayUntapElectionTest \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/fixtures"
