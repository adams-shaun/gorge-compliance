#!/usr/bin/env bash
# Check the driver's helper contracts: attach asks, zone mapping, pass-run
# queue order (a recorded seat-1 resolving pass is queued on seat 1 before its
# later flash cast), stepless pass_to, card-shape probes, cast modes and
# spellings. The driver constructor opens XMage's H2 card DB, so run in a
# scratch cwd with a private copy.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/driver-contract-out}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayDriverContractTest.java"
run="$out/cwd"
rm -rf "$run"
mkdir -p "$run/db"
for e in "$tests"/*; do
  [ "$(basename "$e")" = db ] || ln -s "$e" "$run/"
done
cp "$tests/db/cards.h2.mv.db" "$run/db/"
cd "$run"
java -Xmx1g -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayDriverContractTest
