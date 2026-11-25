#!/usr/bin/env bash
# Check whose options a pass checkpoint's offered set and priority name: after
# p0's lone pass on an empty stack the non-active seat holds priority
# (CR 117.3d), so the step-0 snapshot offers that seat's options (empty when it
# has none), not the passer's. The xproof a-realm-reborn / greenhouse-rickety-
# gazebo static0.0 find shape. Requires the read-only XMage build, like a full
# replay: the constructor opens the H2 card DB, so run from a scratch cwd with
# a private copy, as scripts/xmage-oracle-run.sh does.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/offered-checkpoint-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayOfferedCheckpointTest.java"
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
  org.mage.test.oracle.ScenarioReplayOfferedCheckpointTest
