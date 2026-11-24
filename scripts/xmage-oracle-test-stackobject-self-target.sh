#!/usr/bin/env bash
# Check that Katara's Reversal cast on an empty stack with only an untap target
# resolves, untaps it and does not counter itself (CR 115.5: a spell on the
# stack is an illegal target for itself, so its "counter up to four target
# spells and/or abilities" group has no candidate). Needs an XMage build at an
# XMAGE_REF that carries the TargetStackObject fix
# (docs/upstream-patches/xmage-stackobject-self-target.patch): on an older build
# the Reversal is its own one candidate, counters itself and leaves Multani's
# Presence's trigger on the stack. Requires the read-only XMage build, like a
# full replay.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/stackobject-self-target-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayStackObjectSelfTargetTest.java"
# A full replay reads the card DB at ./db and the deck files from the test
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
  org.mage.test.oracle.ScenarioReplayStackObjectSelfTargetTest \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/fixtures"
