#!/usr/bin/env bash
# Driver test: an empty hidden-origin search still poses Forge's hand-or-graveyard
# destination confirm, but it is not a choice gorge should have logged.
#
# Dina's Guidance (xproof cluster xpt-a26c982d, find 11250344/b01) searches the
# library for a creature card, reveals it, and puts it into its owner's hand or
# graveyard (ChangeZone | Origin$ Library | Destination$ Hand |
# DestinationAlternative$ Graveyard). With only lands in the library the search
# finds nothing: there is no "it" to put anywhere (CR 609.3), so gorge poses no
# destination choice. Forge's ChangeZoneEffect.changeHiddenOriginResolve still
# calls handleAltDest, which calls confirmAction(ChangeZoneToAltDestination);
# the driver had no decision to spend and strict-missed "Missing yesno answer".
# The empty candidate list is the signal: keep the primary destination with a
# note, no miss. A second request puts a creature on top and answers gorge's
# changezone_dest_alt pick with graveyard, proving a real destination answer is
# still consumed (not defaulted to the primary zone). A third request keeps
# that search pick but drops the destination answer: a card WAS found, so the
# strict miss must remain rather than being swallowed by the empty-search rule.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-empty-hidden-search.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-empty-hidden-search}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-empty-hidden-search: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:dina-s-guidance:cast:s1","scenario_sha":"10f06601807ba6ac512c540b4330767f453376347987f6d57926b89e09f650c9","item":{"id":"xs:XP:dina-s-guidance:cast:s1","card":"Dina's Guidance","template":"xproof-start","payment":["","","","BBG","","","","","","","","","","","","","",""],"ignore":["library_top"],"name":"xproof start Dina's Guidance cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp","Forest"],"hand":["Dina's Guidance"],"library_top":["Forest","Forest","Swamp","Swamp","Forest","Forest","Swamp","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Swamp","Forest","Swamp","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Forest","Swamp","Swamp","Forest","Forest","Swamp","Forest","Swamp","Swamp","Forest","Swamp","Swamp"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Forest","ability_index":0},{"op":"cast","seat":0,"card":"p0:Dina's Guidance"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana"}}
{"id":"xs:XP:dina-s-guidance:cast:s1:creature","scenario_sha":"55b001cd8a1fa7661758d8478de50316ef4ad6af1e270d807b181bf4d7d1d79a","item":{"id":"xs:XP:dina-s-guidance:cast:s1:creature","card":"Dina's Guidance","template":"xproof-start","payment":["","","","BBG","","","","","","","","","","","","","",""],"ignore":["library_top"],"name":"xproof start Dina's Guidance cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp","Forest"],"hand":["Dina's Guidance"],"library_top":["Elvish Mystic","Forest","Swamp","Swamp","Forest","Forest","Swamp","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Swamp","Forest","Swamp","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Forest","Swamp","Swamp","Forest","Forest","Swamp","Forest","Swamp","Swamp","Forest","Swamp","Swamp"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Forest","ability_index":0},{"op":"cast","seat":0,"card":"p0:Dina's Guidance"},{"op":"pass","seat":0},{"op":"pass","seat":1,"answers":[{"kind":"choose","pick":["Elvish Mystic"]},{"kind":"choose","pick":["graveyard"]}]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":5,"seat":0,"kind":"choose_n","options":1,"picks":["Elvish Mystic"],"pick_idx":[0],"pick_refs":["p0:Elvish Mystic"],"pick_refs_inexact":[true],"option_refs":["p0:Elvish Mystic"],"object_picks":["p0:Elvish Mystic"],"pick_kinds":["search"],"resume":"search","via":"answer","gorge_kind":"choose","first":"Elvish Mystic","min":0,"max":1},{"step":5,"seat":0,"kind":"choose_n","options":2,"picks":["graveyard"],"pick_idx":[1],"pick_refs":["graveyard"],"pick_refs_inexact":[false],"pick_kinds":["destination"],"resume":"changezone_dest_alt","via":"answer","gorge_kind":"choose","first":"hand","min":1,"max":1},{"step":5,"seat":0,"kind":"order","options":2,"picks":["Elvish Mystic","Dina's Guidance"],"pick_idx":null,"pick_refs":["p0:Elvish Mystic","p0:Dina's Guidance"],"via":"","gorge_kind":"graveyard_order","min":2,"max":2}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana"}}
{"id":"xs:XP:dina-s-guidance:cast:s1:creature-no-answer","scenario_sha":"55b001cd8a1fa7661758d8478de50316ef4ad6af1e270d807b181bf4d7d1d79a","item":{"id":"xs:XP:dina-s-guidance:cast:s1:creature","card":"Dina's Guidance","template":"xproof-start","payment":["","","","BBG","","","","","","","","","","","","","",""],"ignore":["library_top"],"name":"xproof start Dina's Guidance cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp","Forest"],"hand":["Dina's Guidance"],"library_top":["Elvish Mystic","Forest","Swamp","Swamp","Forest","Forest","Swamp","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Swamp","Forest","Swamp","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Forest","Swamp","Swamp","Forest","Forest","Swamp","Forest","Swamp","Swamp","Forest","Swamp","Swamp"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Forest","ability_index":0},{"op":"cast","seat":0,"card":"p0:Dina's Guidance"},{"op":"pass","seat":0},{"op":"pass","seat":1,"answers":[{"kind":"choose","pick":["Elvish Mystic"]},{"kind":"choose","pick":["graveyard"]}]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":5,"seat":0,"kind":"choose_n","options":1,"picks":["Elvish Mystic"],"pick_idx":[0],"pick_refs":["p0:Elvish Mystic"],"pick_refs_inexact":[true],"option_refs":["p0:Elvish Mystic"],"object_picks":["p0:Elvish Mystic"],"pick_kinds":["search"],"resume":"search","via":"answer","gorge_kind":"choose","first":"Elvish Mystic","min":0,"max":1},{"step":5,"seat":0,"kind":"order","options":2,"picks":["Elvish Mystic","Dina's Guidance"],"pick_idx":null,"pick_refs":["p0:Elvish Mystic","p0:Dina's Guidance"],"via":"","gorge_kind":"graveyard_order","min":2,"max":2}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY'
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-empty-hidden-search: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 3:
    fail("want 3 result rows, got %d" % len(rows))
by_id = {r.get("id"): r for r in rows}
empty_id = "xs:XP:dina-s-guidance:cast:s1"
found_id = empty_id + ":creature"
neg_id = empty_id + ":creature-no-answer"
for rid in (empty_id, found_id, neg_id):
    if rid not in by_id:
        fail("missing result row %s" % rid)
for rid in (empty_id, found_id):
    row = by_id[rid]
    for k in ("harness", "strict_miss", "leftover"):
        if row.get(k):
            fail("%s: %s: %s" % (rid, k, row[k]))

# Empty search: no card moved, and no invented creature in any board zone.
row = by_id[empty_id]
snaps = row.get("snapshots") or []
if not snaps:
    fail("%s: no snapshots" % empty_id)
p0 = [p for p in snaps[-1]["players"] if p["seat"] == 0][0]
for zone in ("hand", "graveyard", "battlefield"):
    if "Elvish Mystic" in (p0.get(zone) or []):
        fail("%s: empty search moved Elvish Mystic to %s: %r" % (empty_id, zone, p0.get(zone)))

# Creature found: the graveyard answer is consumed, not defaulted to hand.
row = by_id[found_id]
snaps = row.get("snapshots") or []
if len(snaps) < 7:
    fail("%s: want a snapshot after step 5, got %d" % (found_id, len(snaps)))
p0 = [p for p in snaps[6]["players"] if p["seat"] == 0][0]
if "Elvish Mystic" not in (p0.get("graveyard") or []):
    fail("%s: Elvish Mystic is not in p0's graveyard after the destination answer: %r"
         % (found_id, p0.get("graveyard")))
if "Elvish Mystic" in (p0.get("hand") or []):
    fail("%s: Elvish Mystic is in p0's hand; the driver defaulted to the primary zone" % found_id)

# Control: a search that DID find a card and no destination answer is still a
# strict miss; the empty-search fallback must not hide it.
row = by_id[neg_id]
if row.get("harness"):
    fail("%s: %s" % (neg_id, row["harness"]))
if row.get("leftover"):
    fail("%s: leftover: %s" % (neg_id, row["leftover"]))
miss = row.get("strict_miss") or ""
if "ChangeZoneToAltDestination" not in miss:
    fail("%s: want the ChangeZoneToAltDestination strict miss, got %r" % (neg_id, miss))

print("PASS empty hidden search: no yesno miss, primary destination kept; a found card still takes gorge's graveyard answer, and a found search with no answer still misses")
PY
