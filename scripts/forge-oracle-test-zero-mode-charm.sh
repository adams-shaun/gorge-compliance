#!/usr/bin/env bash
# forge-oracle-test-zero-mode-charm.sh -- driver test for a modal ability
# asked for zero modes (xproof cluster xpt-d2190210).
#
# Bumi, King of Three Trials's ETB is DB$ Charm | MinCharmNum$ 0 | CharmNum$ X
# with X = the Lesson cards in your graveyard. With none, CR 700.2 lets the
# controller choose up to zero modes, and CR 603.3c removes a modal ability
# with no mode chosen from the stack. gorge poses no KModes decision for that
# ask (engine/effects/charm.go returns before the decision when max < 1), so
# Forge's ask has exactly one answer: the empty list. The driver used to
# strict-miss because the offered mode list was not the ask's num.
#
# ZeroModeCharmTest (fork fixtures zero-mode/) replays Bumi with no Lesson in
# the graveyard and requires the empty answer, no harness row, no leftover and
# no AI fallback; it replays the same card with one Lesson and no mode
# decision and requires the num = 1 ask to keep its strict miss. The same
# zero-Lesson request then goes through scripts/forge-oracle-run.sh, which is
# the exporter/runner path production uses.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-zero-mode-charm.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-zero-mode-charm}
mkdir -p "$out"
out=$(cd "$out" && pwd)

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh
fixture=$forge/forge/forge-oracle/fixtures/zero-mode/bumi-zero-mode.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-zero-mode-charm: FAIL: $*" >&2; exit 1; }
[ -s "$forge/cp.txt" ] || fail "no Forge build at $forge: run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || fail "no JDK at $jdk: run make xmage-oracle-setup or set FORGE_ORACLE_JDK"
[ -f "$runner" ] || fail "no driver test runner at $runner"
[ -s "$fixture" ] || fail "no $fixture: the Forge build predates the zero-mode driver fix"

FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk \
  FORGE_ORACLE_TEST_CLASSES=forge.oracle.ZeroModeCharmTest \
  bash "$runner"

"$here/scripts/forge-oracle-run.sh" "$fixture" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-zero-mode-charm: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
if not row.get("strict"):
    fail("not a strict run")
notes = row.get("notes") or []
if not any(n.startswith("modes ") and ": none (0 modes possible" in n for n in notes):
    fail("the zero-mode ask was not answered empty; notes = %r" % (notes,))
if any(n.startswith("loose:") for n in notes):
    fail("the zero-mode ask fell through to the AI; notes = %r" % (notes,))
print("PASS zero-mode charm: Bumi's zero-mode ETB takes the empty list, no harness/strict_miss/leftover")
PY
