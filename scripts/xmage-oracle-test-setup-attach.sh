#!/usr/bin/env bash
# Check the driver's setup-attached link: an Aura linked to its bearer is not
# put into the graveyard by the state-based-action check (CR 704.5m) and fires
# no "becomes attached" trigger, without H2 or a game replay. Requires the
# read-only XMage build, just like the driver's javac check.
set -euo pipefail
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
out=${1:-.ds4/scratch/setup-attach-out}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplaySetupAttachTest.java"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplaySetupAttachTest
