#!/usr/bin/env bash
# forge-oracle-test-phyrexian-processor-paylife.sh -- driver test for a
# "pay any amount of life" payment (xproof cluster xpt-b1209a2d): Phyrexian
# Processor's replacement is AB$ StoreSVar | Cost$ Mandatory PayLife<X>, so
# Forge announces the amount as the cost's X while gorge logs it as a
# choose_n with pick kind paylife and an "N life" label (0 life included, CR 119.4,
# 118.3). The driver took the X announcement only from an x decision, so the pick
# was a leftover and the AI answered ("Missing x answer at step 6: X for").
#
# The driver now binds a paylife pick to the X of a PayLife<X> cost. The
# fixture (fork fixtures/phyrexian/processor-zero-life.jsonl) is the exported
# xs:XP:phyrexian-processor:cast:s1 request: Processor enters on a 0 life
# payment, no harness row, no leftover, the seat keeps 20 life; a 3 life
# variant of the same pick ends at 17.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts the named test passed, not just the exit status.
# Usage: forge-oracle-test-phyrexian-processor-paylife.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-phyrexian-processor-paylife}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-phyrexian-processor-paylife: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/phyrexian-processor-paylife.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.PhyrexianProcessorPayLifeTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames
# the class): assert each named test passed, not just the exit status.
for t in zeroLifeIsAnnouncedAsX threeLifeIsPaidInFull; do
  grep -qx "PASS PhyrexianProcessorPayLifeTest.$t" "$log" || fail "PhyrexianProcessorPayLifeTest.$t did not pass; the pinned driver is missing the PayLife<X> paylife announcement fix"
done
want=2
passes=$(grep -c '^PASS PhyrexianProcessorPayLifeTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "PhyrexianProcessorPayLifeTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
