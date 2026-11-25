#!/usr/bin/env bash
# Driver test: DigMultiple's one-card-per-category choice replays in the Forge oracle driver.
#
# In the Presence of Ages (xproof cluster xpt-cd204b22): SP$ DigMultiple |
# DigNum$ 4 | ChangeValid$ Creature,Land | DestinationZone2$ Graveyard |
# Optional$ True. Forge asks chooseCardsForEffectMultiple (at most one card per
# ChangeValid$ category); gorge logs one choose_n[digmultiple] over the dug
# cards. The driver missed "cards (multiple)" unconditionally and left gorge's
# pick over. This replays the exported xproof request
# xs:XP:in-the-presence-of-ages:cast:s1 (four Forests on top, gorge takes one)
# through scripts/forge-oracle-run.sh at FORGE_ORACLE_REF and requires: no
# harness row, no strict miss, no leftover, and after step 5 one Forest in
# hand and three Forests plus the spell in the graveyard.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-digmultiple-pick.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-digmultiple-pick}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-digmultiple-pick: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:in-the-presence-of-ages:cast:s1","scenario_sha":"0da4e599a4ef9fadf596f70d830f75dad4c5822dbb34e7d4078f74707b66ea37","item":{"id":"xs:XP:in-the-presence-of-ages:cast:s1","card":"","template":"xproof-start","payment":["","","","GGG","","","","","","","","","","","","","","",""],"name":"xproof start In the Presence of Ages cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Forest","Forest","Forest"],"hand":["In the Presence of Ages"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Forest","ability_index":0},{"op":"activate","seat":0,"card":"p0:Forest#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Forest#3","ability_index":0},{"op":"cast","seat":0,"card":"p0:In the Presence of Ages"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"play","seat":0,"card":"p0:Forest#4","answers":[{"kind":"choose","pick":["p0:Forest#4"]}]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":5,"seat":0,"kind":"choose_n","options":4,"picks":["Forest"],"pick_idx":[0],"pick_refs":["p0:Forest#4"],"pick_refs_inexact":[true],"option_refs":["p0:Forest#4","p0:Forest#5","p0:Forest#6","p0:Forest#7"],"object_picks":["p0:Forest#4"],"pick_kinds":["digmultiple"],"resume":"digmultiple","via":"answer","gorge_kind":"choose","first":"Forest","min":0,"max":2},{"step":5,"seat":0,"kind":"order","options":4,"picks":["Forest","Forest","Forest","In the Presence of Ages"],"pick_idx":null,"pick_refs":["p0:Forest","p0:Forest","p0:Forest","p0:In the Presence of Ages"],"via":"","gorge_kind":"graveyard_order","min":4,"max":4}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-digmultiple-pick: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
for k in ("harness", "strict_miss", "leftover"):
    if row.get(k):
        fail("%s: %s" % (k, row[k]))
snaps = row.get("snapshots") or []
if len(snaps) < 7:
    fail("want a snapshot after step 5, got %d snapshots" % len(snaps))
p0 = [p for p in snaps[6]["players"] if p["seat"] == 0][0]
if p0["hand"] != ["Forest"]:
    fail("want one Forest in hand after step 5, got %r" % p0["hand"])
gy = p0["graveyard"]
if gy.count("Forest") != 3 or "In the Presence of Ages" not in gy:
    fail("want three Forests and the spell in the graveyard, got %r" % gy)
print("PASS digmultiple: gorge's Forest pick goes to hand, the other three Forests to the graveyard; no strict miss")
PY
