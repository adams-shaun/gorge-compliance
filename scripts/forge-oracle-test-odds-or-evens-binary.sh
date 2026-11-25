#!/usr/bin/env bash
# Driver test: Forge's ChooseEvenOddEffect asks chooseBinary(OddsOrEvens),
# true meaning Odd. The driver mapped only TapOrUntap, so gorge's even/odd pick
# was left over: Extinction Event logs a resolution choose_n[odd] "odd" at step
# 6 and Gollum, Riddle Master logs an as-enters choose_n[evenodd] "Odd" at step
# 4 (resume "etb", so the pick kind rather than a resume string is the role to
# match). Both are answered Odd. This replays the forge-export of the two
# xproof finds at FORGE_ORACLE_REF and requires a strict run with no leftover
# and the driver's own odd note. Needs the out-of-tree build
# (make forge-oracle-setup), like the other forge-oracle-test-*.sh that replay
# a game. Usage: forge-oracle-test-odds-or-evens-binary.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-odds-or-evens-binary}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-odds-or-evens-binary: FAIL: $*" >&2; exit 1; }

# The three exported requests, verbatim (the third is cycle 11250151's live
# Gollum s1 forge-export, re-exported at the current gorge pin: same decision
# as the second, card named, id suffixed so the rows stay distinct). Their
# abilities maps name only the intrinsic basic land mana ability, so no Forge
# card script is embedded.
cat > "$req" <<'JSON'
{"id":"xs:XP:extinction-event:cast:s1","scenario_sha":"cf51a2dc8b2c7a0978abf915a848a3da67fb87fec2722dc6c74c420d41e98be4","item":{"id":"xs:XP:extinction-event:cast:s1","card":"","template":"xproof-start","payment":["","","","","BBBB","","","","","","","","","","","","","",""],"name":"xproof start Extinction Event cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp","Swamp","Swamp"],"hand":["Extinction Event"],"library_top":["Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#4","ability_index":0},{"op":"cast","seat":0,"card":"p0:Extinction Event"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["odd"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":6,"seat":0,"kind":"choose_n","options":2,"picks":["odd"],"pick_idx":[0],"pick_refs":["odd"],"pick_refs_inexact":[false],"pick_kinds":["odd"],"resume":"chooseevenodd","via":"answer","gorge_kind":"choose","first":"odd","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana","3":"intrinsic: basic land mana"}}
{"id":"xs:XP:gollum-riddle-master:cast:s1","scenario_sha":"9afffc932934dca46f98617cc668f6584f4dc9bea81e8a499ce4937c066dcddf","item":{"id":"xs:XP:gollum-riddle-master:cast:s1","card":"","template":"xproof-start","payment":["","","BB","","","","","","","","","","","","","",""],"name":"xproof start Gollum, Riddle Master cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp"],"hand":["Gollum, Riddle Master"],"library_top":["Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Gollum, Riddle Master"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["Odd"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"choose_n","options":2,"picks":["Odd"],"pick_idx":[0],"pick_refs":["Odd"],"pick_refs_inexact":[false],"pick_kinds":["evenodd"],"resume":"etb","via":"answer","gorge_kind":"choose","first":"Odd","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana"}}
{"id":"xs:XP:gollum-riddle-master:cast:s1:11250151","scenario_sha":"516b6b4ce906512f926558977ee547eb8afa76e0709e7d031e2042ab17c0dd63","item":{"id":"xs:XP:gollum-riddle-master:cast:s1:11250151","card":"Gollum, Riddle Master","template":"xproof-start","payment":["","","BB","","","","","","","","","","","","","",""],"name":"xproof start Gollum, Riddle Master cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp"],"hand":["Gollum, Riddle Master"],"library_top":["Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Gollum, Riddle Master"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0,"answers":[{"kind":"choose","pick":["Odd"]}]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"choose_n","options":2,"picks":["Odd"],"pick_idx":[0],"pick_refs":["Odd"],"pick_refs_inexact":[false],"pick_kinds":["evenodd"],"resume":"etb","via":"answer","gorge_kind":"choose","first":"Odd","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-odds-or-evens-binary: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 3:
    fail("want 3 result rows, got %d" % len(rows))
want = {
    "xs:XP:extinction-event:cast:s1": "binary step 6 (OddsOrEvens): odd [odd]",
    "xs:XP:gollum-riddle-master:cast:s1": "binary step 4 (OddsOrEvens): odd [Odd]",
    "xs:XP:gollum-riddle-master:cast:s1:11250151": "binary step 4 (OddsOrEvens): odd [Odd]",
}
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
    if want[name] not in (row.get("notes") or []):
        fail("%s has no note %r; notes = %r" % (name, want[name], row.get("notes")))
print("PASS OddsOrEvens: Extinction Event odd and Gollum Odd (fixture and live 11250151 export) answered, no leftover")
PY
