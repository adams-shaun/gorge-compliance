#!/usr/bin/env bash
# Driver test: cast_mode "room_alt" casts a Room's second door in the Forge
# oracle driver.
#
# A Room is a split card cast as one of its doors (CR 709.3, 709.3a). gorge
# records the cast of Dazzling Theater // Prop Room's second door as cast_mode
# "room_alt"; the front door is {3}{W} and the three Plains pay {2}{W} for Prop
# Room. StepMachine.spellForMode had no case for it, so spellFor returned the
# card's first spell (the front door), the mana did not cover it and Forge
# refused the cast ("was not put on the stack"). This replays the xproof shape
# xs:XP:dazzling-theater-prop-room:cast:s1 (p0: three Plains, Dazzling Theater
# in hand; steps 0-2 tap the Plains, step 3 casts it room_alt, step 4 resolves)
# through scripts/forge-oracle-run.sh at FORGE_ORACLE_REF and requires: the
# driver's note names Prop Room, the step-3 stack holds Prop Room (not Dazzling
# Theater), and the resolved Room permanent is named Prop Room (CR 709.5: the
# unlocked door's name), with no harness row.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-room-alt-cast-mode.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-room-alt-cast-mode}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-room-alt-cast-mode: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:dazzling-theater-prop-room:cast:s1","scenario_sha":"f59a5b56dc6b2a2bf713369f1337cec21702758d74c6eb3cb20debfd817b07ad","item":{"id":"xs:XP:dazzling-theater-prop-room:cast:s1","card":"Dazzling Theater","template":"xproof-start","payment":["","","","WWW",""],"name":"xproof start Dazzling Theater // Prop Room cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, cut after the room_alt cast and its resolution.","setup":{"p0":{"battlefield":["Plains","Plains","Plains"],"hand":["Dazzling Theater"],"library_top":["Plains","Plains","Plains","Plains","Plains"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Plains","ability_index":0},{"op":"activate","seat":0,"card":"p0:Plains#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Plains#3","ability_index":0},{"op":"cast","seat":0,"card":"p0:Dazzling Theater","cast_mode":"room_alt"},{"op":"resolve","seat":0}]},"gorge_decisions":[],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-room-alt-cast-mode: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if "was not put on the stack" in json.dumps(row):
    fail("the room_alt cast took the front door and was refused")
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
if not row.get("strict"):
    fail("not a strict run")
notes = row.get("notes") or []
if not any(n.startswith("cast: cast_mode room_alt took ") and "Prop Room" in n for n in notes):
    fail("the driver did not select the Room's second door; notes = %r" % (notes,))
snaps = row.get("snapshots") or []
step3 = [s for s in snaps if (s.get("checkpoint") or "").startswith("step 3")]
if not step3:
    fail("no snapshot after step 3; checkpoints = %r" % ([s.get("checkpoint") for s in snaps],))
stack = step3[0].get("stack") or []
if not any(o.get("kind") == "spell" and o.get("source") == "Prop Room"
           and o.get("controller") == 0 for o in stack):
    fail("Prop Room is not on the stack at step 3: %r" % (stack,))
if any(o.get("source") == "Dazzling Theater" for o in stack):
    fail("the front door Dazzling Theater was cast instead: %r" % (stack,))
last = snaps[-1]
if last.get("stack"):
    fail("the Room did not resolve; final stack = %r" % (last.get("stack"),))
if not any(o.get("name") == "Prop Room" and o.get("controller") == 0
           for o in last.get("permanents") or []):
    fail("the resolved Room is not named for its unlocked door Prop Room: %r" % (last.get("permanents"),))
p0 = next((p for p in last.get("players") or [] if p.get("seat") == 0), None)
if p0 is None:
    fail("no seat 0 in the final snapshot")
if "Dazzling Theater" in (p0.get("hand") or []):
    fail("Dazzling Theater is still in hand after the cast")
print("PASS cast_mode room_alt: Prop Room is on the stack at step 3 for {2}{W} and resolves unlocked, no harness error")
PY
