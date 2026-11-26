#!/usr/bin/env bash
# forge-oracle-test-ninjutsu-commander-identity.sh -- driver test for a keyword-
# expanded ability whose Cost$ carries a cost part that gorge and Forge spell
# differently (xproof cluster xpt-d77c0bf0, Yuriko, the Tiger's Shadow).
#
# gorge expands "Commander ninjutsu {U}{B}" (CR 702.49d, 903.8) into an
# `AB$ ChangeZone | Defined$ Self | Origin$ Hand ... | Cost$ U B Return<1/Creature.
# YouCtrl+attacking+unblocked> | ActivationZone$ Hand | Keyword$ Ninjutsu` line.
# Forge's K:Ninjutsu:U B:Commander makes two SpellAbilities, one activated from hand
# and one from the command zone, both costing `U B Return<1/Creature.attacking+
# unblocked/unblocked attacker>`. The driver's keyword tier kept only the SAs whose
# Cost.toSimpleString() equalled gorge's, but that string renders the Return part from
# its description text, so gorge's spelling never equalled Forge's, every hit was
# dropped, and the api+cost tier then failed the same way: `ability identity at step
# 9: api+cost 0 (want AB$ ChangeZone | Defined$ Self | Origin$ Hand ...)`. The cost
# filter now falls back to the mana part plus the cost-part classes, and Origin$/
# ActivationZone$ Hand then pick the hand SA over the command-zone one.
#
# The fixture is the xproof find's scenario (Yuriko, seed 1: attack with Grizzly
# Bears, then the commander ninjutsu from hand returning the unblocked Bears). It
# names cards only; no Forge card script is embedded.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-ninjutsu-commander-identity.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-ninjutsu-commander-identity}
mkdir -p "$out"
out=$(cd "$out" && pwd)
fixture=$here/cmd/oraclediff/testdata/forge/ninjutsu-commander-identity.jsonl
rows=$out/rows.jsonl
# forge-export refuses to write inside the repository; the request lives in a
# temp dir and is discarded.
tmp=$(mktemp -d "${FORGE_ORACLE_TMP:-${TMPDIR:-/tmp}}/forge-ninjutsu.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
req=$tmp/request.jsonl

fail() { echo "forge-oracle-test-ninjutsu-commander-identity: FAIL: $*" >&2; exit 1; }

cd "$here"
go run ./cmd/oraclediff forge-export -cards .cards -scenarios "$fixture" -out "$req" \
  || fail "forge-export failed (is the Forge pin's cardsfolder present?)"

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$req" "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-ninjutsu-commander-identity: FAIL: " + msg)

# Step index of the commander ninjutsu activation.
name, step = "xs:XP:yuriko-the-tiger-s-shadow:f0a0:s1", "9"
reqs = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
rows = [json.loads(l) for l in open(sys.argv[2]) if l.strip()]
if len(reqs) != 1 or len(rows) != 1:
    fail("want 1 request and row, got %d and %d" % (len(reqs), len(rows)))
row = rows[0]
if row.get("id") != name:
    fail("unexpected row id %r" % (row.get("id"),))
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
if not row.get("strict"):
    fail("not a strict run")
notes = row.get("notes") or []
if not any(n.startswith("identity: keyword") and n.endswith(" at step " + step) for n in notes):
    fail("did not bind the ninjutsu ability by keyword at step %s; notes = %r" % (step, notes))
# The request must carry the keyword-expanded ninjutsu line (the driver's input).
line = (reqs[0].get("abilities") or {}).get(step)
if line is None:
    fail("no ability line for step %s: %r" % (step, reqs[0].get("abilities")))
if not line.startswith("AB$ ChangeZone | ") or "Keyword$ Ninjutsu" not in line or "Origin$ Hand" not in line:
    fail("step %s is not the keyword-expanded Ninjutsu line: %r" % (step, line))
print("PASS Ninjutsu: the commander-ninjutsu line binds Forge's hand SA by keyword and cost shape, no harness/leftover")
PY
