#!/usr/bin/env bash
# forge-oracle-test-cast-ability-target.sh -- driver test for a SPELL aimed at a stack ability
# (xproof xpt-77f82e86, xs:XP:stifle:cast:s1): p1 activates Dark Sphere ({T}, Sacrifice: a
# non-mana ability, so it uses the stack, CR 113.3b and 602.2) and p0 casts Stifle ("Counter
# target activated or triggered ability") with targets ["p1:ability:Dark Sphere"]. A driver
# whose target-ref resolver knows only a player or a card sets no target, so the cast is
# refused ("was not put on the stack"). The ability-target ref (pN:ability:Name, the resolver
# the activated-ability side added for Azorius Guildmage) must serve a CAST's target too:
# this replays the forge-export of the fixture, requires a strict run with no harness row,
# the driver's own "target step 4: p1:ability:Dark Sphere" note, Stifle paid for, and an
# empty stack at the last checkpoint (Dark Sphere's ability was countered).
#
# The fixture is shared with the XMage twin (scripts/xmage-oracle-test-cast-ability-target.sh)
# and names cards only; no Forge card script is embedded: the request is exported at run time.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-cast-ability-target.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-cast-ability-target}
mkdir -p "$out"
out=$(cd "$out" && pwd)
fixture=$here/oracle/xmageoracle/test/org/mage/test/oracle/fixtures/cast-ability-target-stifle.jsonl
rows=$out/rows.jsonl
# forge-export refuses to write inside the repository: the request lives in a temp dir.
tmp=$(mktemp -d "${FORGE_ORACLE_TMP:-${TMPDIR:-/tmp}}/forge-cast-ability.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
req=$tmp/request.jsonl

fail() { echo "forge-oracle-test-cast-ability-target: FAIL: $*" >&2; exit 1; }

cd "$here"
go run ./cmd/oraclediff forge-export -cards .cards -scenarios "$fixture" -out "$req" \
  || fail "forge-export failed (is the Forge pin's cardsfolder present?)"
"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-cast-ability-target: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
name = row.get("id")
if name != "xs:XP:stifle:cast:s1":
    fail("unexpected row id %r" % (name,))
for key in ("harness", "error", "strict_miss", "leftover"):
    if row.get(key):
        fail("%s %s: %s" % (name, key, row[key]))
if not row.get("strict"):
    fail("%s is not a strict run" % name)
notes = row.get("notes") or []
if not any(n.startswith("target step 4: p1:ability:Dark Sphere -> ") for n in notes):
    fail("no 'target step 4: p1:ability:Dark Sphere' note; notes = %r" % (notes,))
if "pay mana: pool 1 -> 0, paid=true for Stifle" not in notes:
    fail("Stifle was not paid for; notes = %r" % (notes,))
last = row["snapshots"][-1]
if last.get("checkpoint") != "step 8 (pass)":
    fail("last checkpoint %r, want 'step 8 (pass)'" % (last.get("checkpoint"),))
if last.get("stack") != []:
    fail("stack after Stifle resolves = %r, want [] (Dark Sphere's ability left means it was not countered)" % (last.get("stack"),))
print("PASS Stifle countered Dark Sphere's activated ability (Forge target ref on a cast)")
PY
