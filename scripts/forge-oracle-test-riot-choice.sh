#!/usr/bin/env bash
# Driver test: Riot's counter-or-haste choice replays in the Forge oracle driver.
#
# Riot (CR 702.136a): the permanent enters with its controller's choice of a
# +1/+1 counter or haste. Forge builds it as an ETB replacement whose DB$
# Animate grants haste UnlessCost$ AddCounter<1/P1P1>, asked through
# payCostToPreventEffect; gorge logs one choose_n[riot] ("Enter with a +1/+1
# counter" or "Gain haste"). The driver read only unless_pay answers there,
# so the item strict-missed "Missing unless-pay answer ... for Riot" and left
# the riot pick unconsumed. This replays the exported xproof request
# xs:XP:spider-punk:cast:s1 twice, with gorge's counter pick and (edited, id
# suffix :haste) the haste pick, through scripts/forge-oracle-run.sh at
# FORGE_ORACLE_REF and requires for each: no harness row, no strict miss, no
# leftover, and Spider-Punk on the battlefield after step 4 with one +1/+1
# counter and no haste, or haste and no counter.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-riot-choice.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-riot-choice}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-riot-choice: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:spider-punk:cast:s1","scenario_sha":"343151cd14ef0e34db57ecb1cacdd0138ef6da8ebf35d9fdc509a2ee8b1b7b05","item":{"id":"xs:XP:spider-punk:cast:s1","card":"Spider-Punk","template":"xproof-start","payment":["","","RR","","","","","","","","","","","","","","","","","","",""],"name":"xproof start Spider-Punk cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Mountain","Mountain","Spinneret and Spiderling"],"hand":["Spider-Punk"],"library_top":["Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"activate","seat":0,"card":"p0:Mountain#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Spider-Punk"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["Enter with a +1/+1 counter"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass_to","seat":0,"decision":"attackers"},{"op":"attack","seat":0,"attackers":["p0:Spinneret and Spiderling"],"defender":"p1"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"choose_n","options":2,"picks":["Enter with a +1/+1 counter"],"pick_idx":[0],"pick_refs":["p0:Spider-Punk"],"pick_refs_inexact":[true],"option_refs":["p0:Spider-Punk","p0:Spider-Punk"],"object_picks":["p0:Spider-Punk"],"pick_kinds":["riot"],"resume":"etb","via":"answer","gorge_kind":"choose","first":"Enter with a +1/+1 counter","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana"}}
{"id":"xs:XP:spider-punk:cast:s1:haste","scenario_sha":"343151cd14ef0e34db57ecb1cacdd0138ef6da8ebf35d9fdc509a2ee8b1b7b05","item":{"id":"xs:XP:spider-punk:cast:s1:haste","card":"Spider-Punk","template":"xproof-start","payment":["","","RR","","","","","","","","","","","","","","","","","","",""],"name":"xproof start Spider-Punk cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Mountain","Mountain","Spinneret and Spiderling"],"hand":["Spider-Punk"],"library_top":["Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"activate","seat":0,"card":"p0:Mountain#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Spider-Punk"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["Gain haste"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass_to","seat":0,"decision":"attackers"},{"op":"attack","seat":0,"attackers":["p0:Spinneret and Spiderling"],"defender":"p1"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"choose_n","options":2,"picks":["Gain haste"],"pick_idx":[1],"pick_refs":["p0:Spider-Punk"],"pick_refs_inexact":[true],"option_refs":["p0:Spider-Punk","p0:Spider-Punk"],"object_picks":["p0:Spider-Punk"],"pick_kinds":["riot"],"resume":"etb","via":"answer","gorge_kind":"choose","first":"Gain haste","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-riot-choice: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 2:
    fail("want 2 result rows, got %d" % len(rows))
want = {"xs:XP:spider-punk:cast:s1": (1, False), "xs:XP:spider-punk:cast:s1:haste": (0, True)}
for row in rows:
    rid = row.get("id")
    if rid not in want:
        fail("unexpected row id %r" % rid)
    for k in ("harness", "strict_miss", "leftover"):
        if row.get(k):
            fail("%s: %s: %s" % (rid, k, row[k]))
    snaps = row.get("snapshots") or []
    if len(snaps) < 6:
        fail("%s: want a snapshot after step 4, got %d snapshots" % (rid, len(snaps)))
    sp = [o for o in snaps[5]["permanents"] if o["name"] == "Spider-Punk"]
    if len(sp) != 1:
        fail("%s: Spider-Punk is not on the battlefield after step 4: %r" % (rid, snaps[5]["permanents"]))
    counters = (sp[0].get("counters") or {}).get("P1P1", 0)
    haste = "Haste" in sp[0].get("keywords", [])
    if (counters, haste) != want[rid]:
        fail("%s: Spider-Punk has %d +1/+1 counters, haste=%s; want %r" % (rid, counters, haste, want[rid]))
print("PASS riot: the counter pick enters Spider-Punk with a +1/+1 counter, the haste pick with haste; no strict miss")
PY
