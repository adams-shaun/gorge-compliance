#!/usr/bin/env bash
# Check that a flash spell queued for a later step of the same turn is held
# through an earlier p0/p1 pass pair: the pair resolves the seat-0 spell and
# the flash cast waits for its own step (the ECL-s106 xe:RAND:2:5:w7 shape).
# Requires the read-only XMage build, like the driver's javac checks.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/flash-held-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayFlashHeldTest.java"
# The constructor opens XMage's H2 card DB at ./db, which must be writable:
# run in a scratch cwd whose db is a private copy and whose other Mage.Tests
# entries link back to the read-only build.
run="$out/cwd"
rm -rf "$run"
mkdir -p "$run/db"
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run/"
done
cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run/db/"
cd "$run"
java -Xmx1g -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayFlashHeldTest \
  "$here/oracle/xmageoracle/test/fixtures/xe-RAND-2-5-w7.jsonl"
