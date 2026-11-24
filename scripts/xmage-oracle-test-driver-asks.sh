#!/usr/bin/env bash
# Check the driver asks (active p1 pass pair, battlefield move, game over,
# mixed must-attack, face-down attacker, declined dig, alternative cast,
# promised gift, multi-candidate library search).
# Requires the read-only XMage build, like the driver's javac checks.
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=${1:-.coverage/xmage-driver/driver-asks-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
src="$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java"
# The helper checks below cannot see the funnel call sites: a merge that
# dropped the wiring would still compile and pass them. Pin every target-ask
# entry point here -- the plain Target funnel, the Cards/TargetCard funnel and
# TargetAmount -- and the empty-ask decline those same funnels use.
lone_calls=$(/usr/bin/grep -c '= unscriptedLoneTarget(\|= unscriptedLoneAmountTarget(' "$src" || true)
empty_calls=$(/usr/bin/grep -c 'if (unscriptableEmptyAsk(' "$src" || true)
if [ "${lone_calls:-0}" -lt 3 ] || [ "${empty_calls:-0}" -lt 3 ]; then
  echo "FAIL driver asks: target-ask funnels are not wired ($lone_calls lone/empty-amount call sites, $empty_calls empty-ask call sites; want 3 each)" >&2
  exit 1
fi
# The one search funnel TargetCardInLibrary.choose reaches.
search_calls=$(/usr/bin/grep -c '= searchPickChoice(' "$src" || true)
if [ "${search_calls:-0}" -lt 1 ]; then
  echo "FAIL driver asks: the TargetCardInLibrary funnel is not wired to searchPickChoice ($search_calls call sites, want 1)" >&2
  exit 1
fi
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayDriverAsksTest.java"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null \
  -DscenarioReplay.source="$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  -cp "$out:$cp" org.mage.test.oracle.ScenarioReplayDriverAsksTest
