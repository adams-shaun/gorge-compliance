#!/usr/bin/env bash
# Driver test: cast_mode "alternative" replays in the Forge oracle driver.
#
# Submerge's S:Mode$ AlternativeCost static lets p0 cast it for {0} when an
# opponent controls a Forest and p0 controls an Island (CR 118.9). gorge names
# the cast cast_mode "alternative" (the first AlternativeCost-static option);
# the driver used to map only plot, suspend, adventure_alt, omen_alt,
# modal_spell and warped, so spellFor fell through to the plain {4}{U} spell,
# nothing paid it, and Forge refused the cast ("was not put on the stack").
# This replays the exported xproof request xs:XP:submerge:cast:s1 (the find
# 11250136 b00 diff-forge shape: six Islands and Submerge in hand, Shield
# Sphere and a Forest for p1, step 0 the alternative cast at Shield Sphere)
# at FORGE_ORACLE_REF and requires Submerge on the stack at step 0 and no
# harness row. The request is the exported one, verbatim; its abilities map
# names only the intrinsic basic-land mana abilities of the later activations,
# so no Forge card script is embedded.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-alternative-cast-mode.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-alternative-cast-mode}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-alternative-cast-mode: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:submerge:cast:s1","scenario_sha":"883f0f4ad3e8ee13950cc5b2a96628043a4405f71e80aa74530e392d9326c631","item":{"id":"xs:XP:submerge:cast:s1","card":"Submerge","template":"xproof-start","name":"xproof start Submerge cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Island","Island","Island","Island","Island","Island"],"hand":["Submerge"],"library_top":["Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island","Island"],"life":20},"p1":{"battlefield":["Shield Sphere","Forest"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"cast","seat":0,"card":"p0:Submerge","targets":["p1:Shield Sphere"],"cast_mode":"alternative"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"activate","seat":0,"card":"p0:Island","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#5","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#2","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#6","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#3","ability_index":0},{"op":"activate","seat":0,"card":"p0:Island#4","ability_index":0},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[{"step":0,"seat":0,"kind":"target","options":1,"picks":["Shield Sphere (b)"],"pick_idx":[0],"pick_refs":["p1:Shield Sphere"],"pick_refs_inexact":[false],"option_refs":["p1:Shield Sphere"],"object_picks":["p1:Shield Sphere"],"pick_kinds":["permanent"],"via":"target","gorge_kind":"target","first":"Shield Sphere (b)","min":1,"max":1}],"abilities":{"13":"intrinsic: basic land mana","14":"intrinsic: basic land mana","15":"intrinsic: basic land mana","16":"intrinsic: basic land mana","17":"intrinsic: basic land mana","18":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-alternative-cast-mode: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if "was not put on the stack" in json.dumps(row):
    fail("the alternative cast was treated as the plain cast")
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
if not row.get("strict"):
    fail("not a strict run")
notes = row.get("notes") or []
if not any(n.startswith("cast: cast_mode alternative took") for n in notes):
    fail("the driver did not take the alternative cast; notes = %r" % (notes,))
snaps = row.get("snapshots") or []
step0 = [s for s in snaps if (s.get("checkpoint") or "").startswith("step 0")]
if not step0:
    fail("no snapshot after step 0; checkpoints = %r" % ([s.get("checkpoint") for s in snaps],))
after = step0[0]
stack = after.get("stack") or []
if not any(o.get("kind") == "spell" and o.get("source") == "Submerge"
           and o.get("controller") == 0 for o in stack):
    fail("Submerge is not on the stack at step 0: %r" % (stack,))
p0 = next((p for p in after.get("players") or [] if p.get("seat") == 0), None)
if p0 is None:
    fail("no seat 0 in the step 0 snapshot")
if "Submerge" in (p0.get("hand") or []):
    fail("Submerge is still in hand at step 0")
print("PASS cast_mode alternative: Submerge is on the stack at step 0, no harness row")
PY
