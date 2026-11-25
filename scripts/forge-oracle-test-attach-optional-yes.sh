#!/usr/bin/env bash
# forge-oracle-test-attach-optional-yes.sh -- driver unit tests for the
# Optional$ Attach confirms and the other unmapped asks of
# UnmappedAsksJoinDirectionAttachTest (xproof cluster xpt-0ae740c1 added the
# Bruna, Light of Alabaster case). Bruna's RepeatEach attaches each Aura that can
# enchant it (CR 303.4), so gorge poses one yes/no per Aura (resume
# attach_optional, no card ref) where Heavenly Blademaster gets one card pick
# (attach_subset); the driver answers Forge's per-attachment confirm from the
# yes/no, and a build that read only card refs answered every confirm no
# ("attach pick step 10: []").
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# The wrapper asserts each named test passed (6 run, 0 failures), not just the exit status.
# Usage: forge-oracle-test-attach-optional-yes.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-attach-optional-yes}
mkdir -p "$out"

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh

fail() { echo "forge-oracle-test-attach-optional-yes: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"

log=$out/attach-optional-yes.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.UnmappedAsksJoinDirectionAttachTest \
  bash "$runner" 2>&1 | tee "$log"

# TestNG can exit zero with no executed test (a pin that predates or renames the
# class): assert each named test passed, not just the exit status.
for t in brunaAttachOptionalYesAttaches heavenlyBlademasterAttachesTheNamedAttachment nahiriLoneEquipmentPickIsTheYes nahiriEmptyPickDeclinesTheAttach; do
  grep -qx "PASS UnmappedAsksJoinDirectionAttachTest\.$t" "$log" || fail "UnmappedAsksJoinDirectionAttachTest.$t did not pass; the pinned driver is missing the Optional\$ Attach confirm answers"
done
want=6
passes=$(grep -c '^PASS UnmappedAsksJoinDirectionAttachTest\.' "$log" || true)
[ "$passes" -eq "$want" ] || fail "UnmappedAsksJoinDirectionAttachTest passed $passes tests, want $want"
grep -q "Total tests run: $want, Passes: $want, Failures: 0" "$log" || fail "TestNG summary is not $want run / $want passes / 0 failures"
