#!/usr/bin/env bash
# Driver test: cast_mode "foretell" replays in the Forge oracle driver.
#
# Foretell (CR 702.143a, 116.2h) is a special action: pay {2}, exile the card
# from hand face down; it uses no stack. gorge records it as a cast step with
# cast_mode "foretell". StepMachine.spellForMode had no arm for it, so the
# driver took the ordinary spell and checkActed failed the item with "was not
# put on the stack". This replays the exported xproof request
# xs:XP:sozin-s-comet:cast:s1 (p0: five Mountains, Sozin's Comet in hand;
# steps 0-1 tap two Mountains, step 2 foretells it for RR, then passes)
# through scripts/forge-oracle-run.sh at FORGE_ORACLE_REF and requires: no
# harness row, Sozin's Comet in p0's exile and out of the hand with an empty
# stack after step 2, and the driver's note that it was foretold.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-foretell-cast-mode.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-foretell-cast-mode}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-foretell-cast-mode: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:sozin-s-comet:cast:s1","scenario_sha":"253ffc3cedaa88f5d8be614b7bfc7a06b76e9eb26281d7e5552b3869f2e2bd37","item":{"id":"xs:XP:sozin-s-comet:cast:s1","card":"Sozin's Comet","template":"xproof-start","payment":["","","RR","","","","","","","","","","","",""],"name":"xproof start Sozin's Comet cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Mountain","Mountain","Mountain","Mountain","Mountain"],"hand":["Sozin's Comet"],"library_top":["Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"activate","seat":0,"card":"p0:Mountain#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Sozin's Comet","cast_mode":"foretell"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-foretell-cast-mode: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if "was not put on the stack" in json.dumps(row):
    fail("the foretell was treated as a spell cast")
snaps = row.get("snapshots") or []
if len(snaps) < 4:
    fail("want a snapshot after the foretell step (step 2), got %d snapshots" % len(snaps))
after = snaps[3]
p0 = after["players"][0]
if p0["exile"] != ["Sozin's Comet"]:
    fail("p0 exile = %r, want Sozin's Comet alone" % (p0["exile"],))
if "Sozin's Comet" in p0["hand"]:
    fail("Sozin's Comet is still in hand")
if after["stack"]:
    fail("foretell used the stack: %r" % (after["stack"],))
want = "foretell step 2: p0:Sozin's Comet exiled face down, foretold"
if want not in row.get("notes", []):
    fail("no note %r; notes = %r" % (want, row.get("notes")))
print("PASS cast_mode foretell: Sozin's Comet is foretold in exile after step 2, stack empty, no harness error")
PY
