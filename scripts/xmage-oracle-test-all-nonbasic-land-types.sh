#!/usr/bin/env bash
# Check the snapshot's "is every nonbasic land type" branch (Planar Nexus):
# the flag grants the whole SubTypeSet.NonBasicLandType enum, but XMage does
# not materialise those subtypes, so the row must add them itself. A basic
# Mountain is the control that the branch is keyed on the flag, not on every
# land. No H2 or game replay: the real PlanarNexus effect runs on a FakeGame.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/all-nonbasic-land-types-out}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayAllNonbasicLandTypesTest.java"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayAllNonbasicLandTypesTest
