#!/usr/bin/env bash
# Driver test: AlternativeDecider's "second from the top or the bottom" replays in the Forge oracle driver.
#
# Trickster's Stratagem and Happy Hogan, Bodyguard (xproof cluster
# xpt-71c21a6c): ChangeZone AlternativeDecider$ TargetedOwner, LibraryPosition$
# 1, LibraryPositionAlternative$ -1 (CR 401.4: the owner chooses). gorge asks
# the owner a choose_n[changezone_alternative] whose options are kind "primary"
# ("second from top") and "bottom"; Forge asks confirmAction
# ChangeZoneToAltDestination with ["Top (2nd)", "Bottom"]. The driver matched
# gorge's label as a prefix of Forge's and strict-missed "Missing yesno
# answer". This replays the exported xproof requests
# xs:XP:trickster-s-stratagem:cast:s1 (and, edited, id suffix :bottom) and
# xs:XP:happy-hogan-bodyguard:cast:s1 through scripts/forge-oracle-run.sh at
# FORGE_ORACLE_REF and requires for each: no harness row, no strict miss, no
# leftover, and the moved card second from the top of p1's library after the
# decision step (out of its top five for the bottom pick).
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-alt-destination-second-from-top.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-alt-destination-second-from-top}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-alt-destination-second-from-top: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:trickster-s-stratagem:cast:s1","scenario_sha":"46d60cd1686e2a08c888d39c57b68c8ff841f2e34a95e704040aaf48cb97bc7e","item":{"id":"xs:XP:trickster-s-stratagem:cast:s1","card":"Trickster's Stratagem","template":"xproof-start","payment":["","","","","UUUU","","","","","","","","","","","","","",""],"name":"xproof start Trickster's Stratagem cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Island","Island","Island","Island"],"hand":["Trickster's Stratagem"],"library_top":["Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island"],"life":20},"p1":{"battlefield":["Memnite"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Island","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#4","ability_index":0},{"op":"cast","seat":0,"card":"p0:Trickster's Stratagem","targets":["p1:Memnite"]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["second from top"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"target","options":1,"picks":["Memnite (b)"],"pick_idx":[0],"pick_refs":["p1:Memnite"],"pick_refs_inexact":[false],"option_refs":["p1:Memnite"],"object_picks":["p1:Memnite"],"pick_kinds":["permanent"],"via":"target","gorge_kind":"target","first":"Memnite (b)","min":1,"max":1},{"step":6,"seat":1,"kind":"choose_n","options":2,"picks":["second from top"],"pick_idx":[0],"pick_refs":["second from top"],"pick_refs_inexact":[false],"pick_kinds":["primary"],"resume":"changezone_alternative","via":"answer","gorge_kind":"choose","first":"second from top","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana","3":"intrinsic: basic land mana"}}
{"id":"xs:XP:trickster-s-stratagem:cast:s1:bottom","scenario_sha":"2e5212333efea643addb0f3feef7340ce460c9cc6251e9599c4f668cfd7def76","item":{"id":"xs:XP:trickster-s-stratagem:cast:s1:bottom","card":"Trickster's Stratagem","template":"xproof-start","payment":["","","","","UUUU","","","","","","","","","","","","","",""],"name":"xproof start Trickster's Stratagem cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Island","Island","Island","Island"],"hand":["Trickster's Stratagem"],"library_top":["Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island"],"life":20},"p1":{"battlefield":["Memnite"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Island","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#4","ability_index":0},{"op":"cast","seat":0,"card":"p0:Trickster's Stratagem","targets":["p1:Memnite"]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["bottom"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"target","options":1,"picks":["Memnite (b)"],"pick_idx":[0],"pick_refs":["p1:Memnite"],"pick_refs_inexact":[false],"option_refs":["p1:Memnite"],"object_picks":["p1:Memnite"],"pick_kinds":["permanent"],"via":"target","gorge_kind":"target","first":"Memnite (b)","min":1,"max":1},{"step":6,"seat":1,"kind":"choose_n","options":2,"picks":["bottom"],"pick_idx":[1],"pick_refs":["bottom"],"pick_refs_inexact":[false],"pick_kinds":["bottom"],"resume":"changezone_alternative","via":"answer","gorge_kind":"choose","first":"bottom","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana","3":"intrinsic: basic land mana"}}
{"id":"xs:XP:happy-hogan-bodyguard:cast:s1","scenario_sha":"c9e23b4ec07e8fbbd7901974bfc0499e97e94003b37c64e25d96e5620a2a0059","item":{"id":"xs:XP:happy-hogan-bodyguard:cast:s1","card":"","template":"xproof-start","payment":["","","","","","","UUUUUU","","","","","","","","","","","","","",""],"name":"xproof start Happy Hogan, Bodyguard cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Island","Island","Island","Island","Island","Island"],"hand":["Happy Hogan, Bodyguard"],"library_top":["Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island"],"life":20},"p1":{"battlefield":["Shield Sphere"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Island","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#4","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#5","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#6","ability_index":0},{"op":"cast","seat":0,"card":"p0:Happy Hogan, Bodyguard"},{"op":"pass","seat":0},{"op":"resolve","seat":0,"answers":[{"kind":"target","pick":["p1:Shield Sphere"]},{"kind":"choose","pick":["second from top"]}]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":8,"seat":0,"kind":"target","options":1,"picks":["Shield Sphere (b)"],"pick_idx":[0],"pick_refs":["p1:Shield Sphere"],"pick_refs_inexact":[false],"option_refs":["p1:Shield Sphere"],"object_picks":["p1:Shield Sphere"],"pick_kinds":["permanent"],"via":"answer","gorge_kind":"target","first":"Shield Sphere (b)","min":1,"max":1},{"step":8,"seat":1,"kind":"choose_n","options":2,"picks":["second from top"],"pick_idx":[0],"pick_refs":["second from top"],"pick_refs_inexact":[false],"pick_kinds":["primary"],"resume":"changezone_alternative","via":"answer","gorge_kind":"choose","first":"second from top","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana","3":"intrinsic: basic land mana","4":"intrinsic: basic land mana","5":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-alt-destination-second-from-top: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 3:
    fail("want 3 result rows, got %d" % len(rows))
# id -> (decision step, moved card, want it second from the top)
want = {
    "xs:XP:trickster-s-stratagem:cast:s1": (6, "Memnite", True),
    "xs:XP:trickster-s-stratagem:cast:s1:bottom": (6, "Memnite", False),
    "xs:XP:happy-hogan-bodyguard:cast:s1": (8, "Shield Sphere", True),
}
for row in rows:
    rid = row.get("id")
    if rid not in want:
        fail("unexpected row id %r" % rid)
    for k in ("harness", "strict_miss", "leftover"):
        if row.get(k):
            fail("%s: %s: %s" % (rid, k, row[k]))
    step, card, second = want[rid]
    snaps = row.get("snapshots") or []
    if len(snaps) < step + 2:
        fail("%s: want a snapshot after step %d, got %d snapshots" % (rid, step, len(snaps)))
    p1 = [p for p in snaps[step + 1]["players"] if p["seat"] == 1][0]
    top = p1["library_top"]
    if second and (len(top) < 2 or top[1] != card or top[0] == card):
        fail("%s: %s is not second from the top of p1's library: %r" % (rid, card, top))
    if not second and card in top:
        fail("%s: %s should be on the bottom, but is in p1's library top %r" % (rid, card, top))
print("PASS alt destination: the owner's second-from-top pick puts the card second from the top, the bottom pick on the bottom; no strict miss")
PY
