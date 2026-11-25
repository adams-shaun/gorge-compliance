#!/usr/bin/env bash
# Driver test: cast_mode "evoked" replays in the Forge oracle driver.
#
# Evoke is an alternative cost (CR 702.74a). gorge records the cast as a cast
# step with cast_mode "evoked". StepMachine.spellForMode had no case for it, so
# the driver took the card's first (hard-cost) spell, the mana gorge paid did
# not cover it and Forge refused the cast ("was not put on the stack"). This
# replays the xproof shape xs:XP:emptiness:f0t0:s1, cut after the cast (p0:
# four Plains, Emptiness in hand; steps 0-1 tap two Plains, step 2 casts
# Emptiness evoked for {W/B}{W/B} with two "Pay W" picks) through
# scripts/forge-oracle-run.sh at FORGE_ORACLE_REF and requires: no harness row,
# Emptiness on the stack after step 2, out of p0's hand, and two of p0's
# Plains still untapped (the evoke cost, not {3}{W/B}{W/B}, was paid).
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-evoked-cast-mode.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-evoked-cast-mode}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-evoked-cast-mode: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:emptiness:f0t0:s1","scenario_sha":"33e067ef3a01fdb88ce9791356d5a3a6c60980209b284af94bae7a69a5c1e17d","item":{"id":"xs:XP:emptiness:f0t0:s1","card":"Emptiness","template":"xproof-start","payment":["","","WW"],"name":"xproof start Emptiness f0/t0 seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, cut after the evoked cast.","setup":{"p0":{"battlefield":["Plains","Plains","Plains","Plains"],"hand":["Emptiness"],"library_top":["Plains","Plains","Plains","Plains"],"life":20},"p1":{"battlefield":["Phyrexian Walker"],"library_top":["Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Plains","ability_index":0},{"op":"activate","seat":0,"card":"p0:Plains#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Emptiness","cast_mode":"evoked","answers":[{"kind":"choose","pick":["Pay W"]},{"kind":"choose","pick":["Pay W"]}]}]},"gorge_decisions":[{"step":2,"seat":0,"kind":"choose_n","options":1,"picks":["Pay W"],"pick_idx":[0],"pick_refs":["Pay W"],"pick_refs_inexact":[false],"pick_kinds":["pay_W"],"via":"answer","gorge_kind":"choose","first":"Pay W","min":1,"max":1},{"step":2,"seat":0,"kind":"choose_n","options":1,"picks":["Pay W"],"pick_idx":[0],"pick_refs":["Pay W"],"pick_refs_inexact":[false],"pick_kinds":["pay_W"],"via":"answer","gorge_kind":"choose","first":"Pay W","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-evoked-cast-mode: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if "was not put on the stack" in json.dumps(row):
    fail("the evoked cast was refused")
snaps = row.get("snapshots") or []
if len(snaps) < 4:
    fail("want a snapshot after the cast step (step 2), got %d snapshots" % len(snaps))
after = snaps[3]
if not any(o.get("source") == "Emptiness" or o.get("name") == "Emptiness" for o in after["stack"]):
    fail("Emptiness is not on the stack after step 2: %r" % (after["stack"],))
p0 = after["players"][0]
if "Emptiness" in p0["hand"]:
    fail("Emptiness is still in hand")
untapped = [o for o in after["permanents"]
            if o["controller"] == 0 and o["name"] == "Plains" and not o.get("tapped")]
if len(untapped) != 2:
    fail("want two of p0's Plains untapped (the evoke cost is {W/B}{W/B}), got %d: %r" % (len(untapped), after["permanents"]))
print("PASS cast_mode evoked: Emptiness is on the stack after step 2 for {W/B}{W/B}, two Plains untapped, no harness error")
PY
