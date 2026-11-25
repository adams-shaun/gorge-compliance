#!/usr/bin/env bash
# Driver test: cast_mode "mayplay" casts the MayPlay-bound spell in the Forge
# oracle driver.
#
# Hurl Through Hell exiles p1's Shield Sphere and grants "until the end of your
# next turn, you may cast that card" (STPlay's MayPlay$ True). gorge records
# step 7 as cast with cast_mode "mayplay" (CR 601.2a: the card is cast from
# where it is). The driver had no mayplay arm, so spellFor fell through to the
# card's own spell; that copy carries no MayPlay option and Forge's
# SpellAbilityRestriction.checkZoneRestrictions refuses a spell outside its
# zone, so Forge stopped with "was not put on the stack". The MayPlay-bound copy
# lives in c.getAllPossibleAbilities with getMayPlay() set.
#
# This replays the exported xproof request xs:XP:hurl-through-hell:cast:s1 (the
# find 11250141 b02 shape) at FORGE_ORACLE_REF and requires p1's Shield Sphere
# on the stack (controller p0) at step 7 with no harness row. The request is
# the exported one, verbatim; its abilities map names only the intrinsic basic
# land mana abilities of the four activations, so no Forge card script is
# embedded.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-mayplay-cast.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-mayplay-cast}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-mayplay-cast: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:hurl-through-hell:cast:s1","scenario_sha":"150a06f43246760b45537c9b342c6e3ddcb091a37bd55a70440376dfd13e0a0a","item":{"id":"xs:XP:hurl-through-hell:cast:s1","card":"Hurl Through Hell","template":"xproof-start","payment":["","","","","BBBR","","","","","","","","","","","","","","","","",""],"name":"xproof start Hurl Through Hell cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp","Swamp","Swamp","Mountain"],"hand":["Hurl Through Hell"],"library_top":["Mountain","Swamp","Swamp","Swamp","Swamp","Swamp","Mountain","Mountain","Swamp","Mountain","Swamp","Swamp","Mountain","Mountain","Mountain","Swamp","Swamp","Swamp","Swamp","Mountain","Mountain","Swamp","Swamp","Swamp","Mountain","Mountain","Swamp","Mountain","Mountain","Mountain","Swamp","Swamp","Mountain","Mountain","Mountain"],"life":20},"p1":{"battlefield":["Shield Sphere"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Swamp#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"cast","seat":0,"card":"p0:Hurl Through Hell","targets":["p1:Shield Sphere"]},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"cast","seat":0,"card":"p1:Shield Sphere","cast_mode":"mayplay"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":4,"seat":0,"kind":"target","options":1,"picks":["Shield Sphere (b)"],"pick_idx":[0],"pick_refs":["p1:Shield Sphere"],"pick_refs_inexact":[false],"option_refs":["p1:Shield Sphere"],"object_picks":["p1:Shield Sphere"],"pick_kinds":["permanent"],"via":"target","gorge_kind":"target","first":"Shield Sphere (b)","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","2":"intrinsic: basic land mana","3":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-mayplay-cast: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if "was not put on the stack" in json.dumps(row):
    fail("the mayplay cast was treated as the card's hand spell")
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
if not row.get("strict"):
    fail("not a strict run")
notes = row.get("notes") or []
if not any(n.startswith("cast: cast_mode mayplay took") for n in notes):
    fail("the driver did not take the mayplay cast; notes = %r" % (notes,))
if not any(n.startswith("cast: from Exile by Hurl Through Hell") for n in notes):
    fail("the driver did not take the MayPlay-bound copy; notes = %r" % (notes,))
snaps = row.get("snapshots") or []
step7 = [s for s in snaps if (s.get("checkpoint") or "").startswith("step 7")]
if not step7:
    fail("no snapshot after step 7; checkpoints = %r" % ([s.get("checkpoint") for s in snaps],))
after = step7[0]
stack = after.get("stack") or []
if not any(o.get("kind") == "spell" and o.get("source") == "Shield Sphere"
           and o.get("controller") == 0 for o in stack):
    fail("p1's Shield Sphere is not on the stack under p0 at step 7: %r" % (stack,))
p1 = next((p for p in after.get("players") or [] if p.get("seat") == 1), None)
if p1 is not None and "Shield Sphere" in (p1.get("exile") or []):
    fail("Shield Sphere is still in p1's exile at step 7")
print("PASS cast_mode mayplay: p1's Shield Sphere is on the stack under p0 at step 7, no harness row")
PY
