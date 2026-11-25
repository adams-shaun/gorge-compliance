#!/usr/bin/env bash
# Replay a lowered xproof start whose one differing card (Bartolomé del
# Presidio, XMage "Bartolome del Presidio") rides Item.card + xmage_name, the
# way xprep stamps it: the driver must add and cast it under the XMage
# spelling and report every snapshot under the corpus spelling.
# Requires the read-only XMage build, like the driver's other javac checks.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=$(realpath -m "${1:-.coverage/xmage-driver/xproof-spelling-driverout}")
here=$(cd "$(dirname "$0")/.." && pwd)
fixture="$here/oracle/xmageoracle/test/fixtures/bartolome-xproof-spelling.jsonl"
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayXproofSpellingTest.java"
# The game's H2 card database resolves from its working directory, and the
# XMage build tree is read-only: run from a scratch cwd with a private copy of
# the DB, as scripts/xmage-oracle-run.sh does.
run="$out.cwd"
rm -rf "$run"
mkdir -p "$run/db"
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run/"
done
cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run/db/"
cd "$run"
systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% -- \
  "$JAVA_HOME/bin/java" -Xmx1g -XX:ActiveProcessorCount=2 \
  -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayXproofSpellingTest "$fixture"
