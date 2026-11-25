#!/usr/bin/env bash
# forge-oracle-test-mdfc-spell-ability.sh -- driver test for a modal DFC played
# as its land back face whose front face is a spell sharing printed ability
# index 0 with the back land's mana ability (CR 712.12).
#
# Shatterskull Smashing // Shatterskull, the Hammer Pass is played as its land
# at step 2, then the land's {T}: Add {R} is activated at step 13 (seed 1) or
# 9 (seed 2). The scenario has no setup back_face entry, so resolving the
# activate step from the setup face names the front's SP$ DealDamage spell: the
# Forge driver then reports `ability identity at step N ... DealDamage` because
# the card on the battlefield is the land and its only candidate is the mana
# ability. Glasspool Mimic and Akoum Warrior do not expose this because their
# front faces have no ability at that index; the exporter's snapshot-first face
# rule is the class fix, and this test drives it through forge-export so a
# regression in that rule fails here with the driver's harness row.
#
# The fixture is the xproof start's gorge-played scenario (the full item, not
# the cut harness row): setup, play and the later mana activation. It names
# cards only; no Forge card script is embedded.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-mdfc-spell-ability.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-mdfc-spell-ability}
mkdir -p "$out"
out=$(cd "$out" && pwd)
fixture=$here/cmd/oraclediff/testdata/forge/mdfc-spell-ability.jsonl
rows=$out/rows.jsonl
# forge-export refuses to write inside the repository; the driver's rows may
# stay under the gitignored .coverage output dir. The request (and the prep
# refusals beside it) live in a temp dir and are discarded.
tmp=$(mktemp -d "${FORGE_ORACLE_TMP:-${TMPDIR:-/tmp}}/forge-mdfc.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
req=$tmp/request.jsonl

fail() { echo "forge-oracle-test-mdfc-spell-ability: FAIL: $*" >&2; exit 1; }

cd "$here"
go run ./cmd/oraclediff forge-export -cards .cards -scenarios "$fixture" -out "$req" \
  || fail "forge-export failed (is the Forge pin's cardsfolder present?)"

# Run the driver first: on the pre-fix exporter the request names the front
# spell and this row carries the exact `ability identity ... DealDamage` error
# the test exists to catch. Only a clean row is worth checking against the
# request.
"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$req" "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-mdfc-spell-ability: FAIL: " + msg)

# Step index whose ABILITY the item's play turned onto the land face, and the
# two faces the item's land play must not confuse.
want = {
    "xs:XP:shatterskull-smashing-shatterskull-the-hammer-pass:cast:s1": "13",
    "xs:XP:shatterskull-smashing-shatterskull-the-hammer-pass:cast:s2": "9",
}
reqs = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
rows = [json.loads(l) for l in open(sys.argv[2]) if l.strip()]
if len(reqs) != len(want) or len(rows) != len(want):
    fail("want %d requests and rows, got %d and %d" % (len(want), len(reqs), len(rows)))
for row in rows:
    name = row.get("id")
    if name not in want:
        fail("unexpected row id %r" % (name,))
    if row.get("harness"):
        fail("%s harness row: %s" % (name, row["harness"]))
    if row.get("strict_miss"):
        fail("%s strict_miss: %s" % (name, row["strict_miss"]))
    if row.get("leftover"):
        fail("%s leftover: %s" % (name, row["leftover"]))
    if not row.get("strict"):
        fail("%s is not a strict run" % name)
    notes = row.get("notes") or []
    if "play: Shatterskull Smashing as its back-face land Shatterskull, the Hammer Pass" not in notes:
        fail("%s did not play the back-face land; notes = %r" % (name, notes))
    want_note = " at step %s" % want[name]
    if not any(note.startswith("identity: ") and note.endswith(want_note) for note in notes):
        fail("%s has no ability-identity note for step %s; notes = %r" % (name, want[name], notes))
# Defense in depth: the request must name the land face's mana ability for the
# activate step, never the front spell's DealDamage line.
for row in reqs:
    name = row.get("id")
    if name not in want:
        fail("unexpected request id %r" % (name,))
    line = (row.get("abilities") or {}).get(want[name])
    if line is None:
        fail("%s has no ability line for step %s: %r" % (name, want[name], row.get("abilities")))
    if "DealDamage" in line:
        fail("%s step %s exported the front spell's DealDamage line, not the land mana ability: %r"
             % (name, want[name], line))
    if "AB$ Mana" not in line:
        fail("%s step %s is not the land's mana ability: %r" % (name, want[name], line))
print("PASS Shatterskull Smashing: land back face, mana-ability identity, no harness/leftover")
PY
