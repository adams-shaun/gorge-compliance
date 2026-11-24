#!/usr/bin/env bash
# Check the answers to an optional attack trigger whose may gorge folded into its
# body's pick or cost: Preeminent Captain with no Soldier in hand and Myr Battlesphere
# with no other Myr get no (nothing logged), Captain with a picked Soldier gets yes.
# Requires the read-only XMage build, like the driver's javac checks.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/folded-optional-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayFoldedOptionalTest.java"
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
  org.mage.test.oracle.ScenarioReplayFoldedOptionalTest \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/fixtures"
