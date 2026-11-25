#!/usr/bin/env bash
# Driver test: a forced Hand -> Library put-back consumes gorge's CR 401.4 placement order in the Forge oracle driver.
#
# Sawtooth Loon (xproof cluster xpt-83d468df): "draw two cards, then put two
# cards from your hand on the bottom of your library" (ChangeZone Origin$ Hand
# Destination$ Library LibraryPosition$ -1 ChangeNum$ 2 Mandatory$ True), cast
# with an empty hand, so the two drawn cards are the only two eligible. gorge
# has no selection to ask and poses only the owner's placement order, an
# "order" decision of pick kind library_placement (first pick nearest the top,
# CR 401.4); Forge asks the same forced selection one card at a time ("2
# options, 1..1 to pick", twice) and places the picks in order, so its pick
# order is the bottom order. The driver left the order unconsumed and missed:
# "Missing zone change answer at step 8: 2 options, 1..1 to pick".
#
# This replays the exported xproof request xs:XP:sawtooth-loon:cast:s1 through
# scripts/forge-oracle-run.sh at FORGE_ORACLE_REF and requires: no harness row,
# no strict miss, no leftover decision, the two cards out of p0's hand and back
# in its library (35 cards) after step 8, and the driver note that the order
# was consumed as [Island, Plains]. The same request with gorge's order
# swapped (id suffix :swapped) must note [Plains, Island]: the answer order, not
# a default, is what the per-card asks follow.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-hand-library-placement.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-hand-library-placement}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-hand-library-placement: FAIL: $*" >&2; exit 1; }

cat > "$out/base.jsonl" <<'JSON'
{"id":"xs:XP:sawtooth-loon:cast:s1","scenario_sha":"c87cdb7ec9bd5df156fa8633f961dae479a6e4c651be1c067d9a868b420dcd49","item":{"id":"xs:XP:sawtooth-loon:cast:s1","card":"Sawtooth Loon","template":"xproof-start","payment":["","","","","WWWU","","","","","","","","","","","","","","","","","",""],"name":"xproof start Sawtooth Loon cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Plains","Plains","Plains","Island"],"hand":["Sawtooth Loon"],"library_top":["Island","Plains","Island","Island","Plains","Island","Plains","Plains","Plains","Plains","Island","Plains","Island","Plains","Plains","Plains","Plains","Island","Island","Island","Plains","Plains","Island","Island","Island","Island","Plains","Island","Plains","Plains","Plains","Island","Island","Island","Plains"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Plains","ability_index":0},{"op":"activate","seat":0,"card":"p0:Plains#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Plains#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island","ability_index":0},{"op":"cast","seat":0,"card":"p0:Sawtooth Loon"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"trigger_order","pick":["p0:Sawtooth Loon","p0:Sawtooth Loon"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"arrange","pick":["p0:Island#2","p0:Plains#4"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["p0:Sawtooth Loon"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":6,"seat":0,"kind":"order","options":2,"picks":["Sawtooth Loon: When CARDNAME enters, return a white or blue creature you control to its owner's hand.","Sawtooth Loon: When CARDNAME enters, draw two cards, then put two cards from your hand on the bottom of your library."],"pick_idx":[0,1],"pick_refs":["p0:Sawtooth Loon","p0:Sawtooth Loon"],"pick_refs_inexact":[true,true],"option_refs":["p0:Sawtooth Loon","p0:Sawtooth Loon"],"object_picks":["p0:Sawtooth Loon","p0:Sawtooth Loon"],"pick_kinds":["trigger","trigger"],"via":"answer","gorge_kind":"trigger_order","first":"Sawtooth Loon: When CARDNAME enters, return a white or blue creature you control to its owner's hand.","min":2,"max":2},{"step":8,"seat":0,"kind":"order","options":2,"picks":["Island","Plains"],"pick_idx":[0,1],"pick_refs":["p0:Island#2","p0:Plains#4"],"pick_refs_inexact":[true,true],"option_refs":["p0:Island#2","p0:Plains#4"],"arrange_labels":["Island","Plains"],"arrange_kind":"library_placement","object_picks":["p0:Island#2","p0:Plains#4"],"pick_kinds":["library_placement","library_placement"],"via":"answer","gorge_kind":"arrange","first":"Island","min":2,"max":2},{"step":10,"seat":0,"kind":"choose_n","options":1,"picks":["Sawtooth Loon"],"pick_idx":[0],"pick_refs":["p0:Sawtooth Loon"],"pick_refs_inexact":[true],"option_refs":["p0:Sawtooth Loon"],"object_picks":["p0:Sawtooth Loon"],"pick_kinds":["hidden_pick"],"resume":"hidden_pick","via":"answer","gorge_kind":"choose","first":"Sawtooth Loon","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana","3":"intrinsic: basic land mana"}}
JSON

python3 -I - "$out/base.jsonl" "$req" <<'PY' || exit 1
import json, sys

base = json.loads(open(sys.argv[1]).readline())
swapped = json.loads(json.dumps(base))
swapped["id"] = base["id"] + ":swapped"
swapped["item"]["id"] = swapped["id"]
for d in swapped["gorge_decisions"]:
    if d.get("arrange_kind") == "library_placement":
        for k in ("picks", "pick_idx", "pick_refs", "pick_refs_inexact", "object_picks", "pick_kinds"):
            if k in d:
                d[k] = list(reversed(d[k]))
with open(sys.argv[2], "w") as f:
    for r in (base, swapped):
        f.write(json.dumps(r, separators=(",", ":")) + "\n")
PY

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-hand-library-placement: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 2:
    fail("want 2 result rows, got %d" % len(rows))
want = {
    "xs:XP:sawtooth-loon:cast:s1": "[Island, Plains]",
    "xs:XP:sawtooth-loon:cast:s1:swapped": "[Plains, Island]",
}
for row in rows:
    rid = row.get("id")
    if rid not in want:
        fail("unexpected row id %r" % rid)
    for k in ("harness", "strict_miss", "leftover"):
        if row.get(k):
            fail("%s: %s: %s" % (rid, k, row[k]))
    notes = row.get("notes") or []
    note = "order step 8: hand to library placement, one card per ask " + want[rid]
    if note not in notes:
        fail("%s: no note %r in %r" % (rid, note, notes))
    snaps = row.get("snapshots") or []
    if len(snaps) < 10:
        fail("%s: want a snapshot after step 8, got %d snapshots" % (rid, len(snaps)))
    p0 = [p for p in snaps[9]["players"] if p["seat"] == 0][0]
    if p0["hand"] or p0["library_count"] != 35:
        fail("%s: after step 8 p0 hand %r library_count %r, want [] and 35" % (rid, p0["hand"], p0["library_count"]))
print("PASS hand->library placement: the forced two-card put-back answers Forge's per-card asks from gorge's library_placement order; no strict miss, no leftover")
PY
