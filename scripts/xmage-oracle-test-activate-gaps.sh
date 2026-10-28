#!/usr/bin/env bash
# Check the driver's manual pool payment and the Saddle/Crew single-object cost
# chooser end to end, through the real XMage engine. The payment window must
# spend any pool unit for the generic part even when gorge's recorded multiset
# names no unit of the remaining colour, and a one-object Tap cost chooser must
# consume the scripted choice. Requires the read-only XMage build, like the
# driver's javac checks.
set -euo pipefail
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
out=${1:-.ds4/scratch/activate-gaps-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayActivateGapsTest.java"
# The constructor opens XMage's H2 card DB at ./db, which must be writable:
# run in a scratch cwd whose db is a private copy and whose other Mage.Tests
# entries link back to the read-only build.
run="$out/cwd"
rm -rf "$run"
mkdir -p "$run/db"
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run/"
done
cp "$tests/db/cards.h2.mv.db" "$run/db/"
cd "$run"
java -Xmx1g -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayActivateGapsTest
