#!/usr/bin/env bash
# Check the driver's explicit cast/activate payment against a real {1}{G}
# cost payment, without a game: the recorded multiset decides the colour split,
# an unusable multiset falls back to auto-payment, and the runCode payment hooks
# work when TestPlayer hands the payload the inner computer player.
# Requires the read-only XMage build, like the driver's javac checks.
set -euo pipefail
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
out=${1:-.ds4/scratch/payment-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
out=$(cd "$out" && pwd)
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayPaymentTest.java"
# The driver's constructor scans the card list into ./db, so run there: the
# scratch output stays in .ds4/scratch and the worktree stays clean.
cd "$out"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayPaymentTest
