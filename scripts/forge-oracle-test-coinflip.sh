#!/usr/bin/env bash
# Driver test: the recorded coin flip decides Goblin Kaboomist's upkeep flip.
#
# xproof rng find xs:XP:goblin-kaboomist:f0t0:s2 (gorge~forge): Kaboomist's
# upkeep trigger is DB$ Token (a Land Mine) then DB$ FlipCoin | LoseSubAbility$
# DmgSelf, 2 damage to the 1/2 itself. gorge lost the flip on step 37 (the
# seat-1 pass that resolves the trigger, after two mana activations at steps
# 34-35 and the seat-0 pass), so Kaboomist is in p0's graveyard. The Land Mine
# token's art pick draws an unbounded nextInt() per item of an edition Set, and
# a ForcedDieRandom that polled the armed flips from nextInt() spent the
# recorded result there; Forge then flipped from its own seeded stream and won,
# leaving p0's graveyard empty.
#
# This replays the forge-export of that find at FORGE_ORACLE_REF twice, the
# SAME id both times (so the driver's seeded stream, CRC32 of the id, is
# identical) and only the recorded flip differing:
#   - win:false: Kaboomist must be in p0's graveyard;
#   - win:true:  Kaboomist must be on the battlefield, graveyard empty.
# A driver that does not force the flip produces one and the same game on both
# runs, so it fails exactly one of them whatever its seed. Each run must be
# strict (no harness row, no strict_miss). The fork's ForcedCoinFlipTest pins
# the same pair at the unit level. Needs the out-of-tree build
# (make forge-oracle-setup). Usage: forge-oracle-test-coinflip.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-coinflip}
mkdir -p "$out"
out=$(cd "$out" && pwd)

fail() { echo "forge-oracle-test-coinflip: FAIL: $*" >&2; exit 1; }

# The exported request, verbatim (cmd/oraclediff forge-export of
# cmd/oraclediff/testdata/forge/goblin-kaboomist-upkeep-flip.jsonl). Its
# abilities map names only the intrinsic basic land mana abilities, so no Forge
# card script is embedded.
cat > "$out/lose.request.jsonl" <<'JSON'
{"id":"xs:XP:goblin-kaboomist:f0t0:s2","scenario_sha":"57d0710960ab6b3d5390133378fca1ba5f690847442394c1f8d891cee51e9561","item":{"id":"xs:XP:goblin-kaboomist:f0t0:s2","card":"Goblin Kaboomist","template":"xproof-start","payment":["","","RR","","","","","","","","","","","","","","","","","","","","","","","","","","","","","","","","","","",""],"coin_flips":[{"step":37,"seat":0,"win":false,"draws":1}],"name":"xproof start Goblin Kaboomist f0/t0 seed 2","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Mountain","Mountain"],"hand":["Goblin Kaboomist"],"library_top":["Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain","Mountain"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"activate","seat":0,"card":"p0:Mountain#2","ability_index":0},{"op":"cast","seat":0,"card":"p0:Goblin Kaboomist"},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"play","seat":1,"card":"p1:Plains"},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"activate","seat":0,"card":"p0:Mountain#2","ability_index":0},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{"0":"intrinsic: basic land mana","1":"intrinsic: basic land mana","34":"intrinsic: basic land mana","35":"intrinsic: basic land mana"}}
JSON

# The same request with only the recorded flip changed to a win.
python3 -I - "$out/lose.request.jsonl" "$out/win.request.jsonl" <<'PY' || exit 1
import json, sys
rq = json.loads(open(sys.argv[1]).readline())
flips = rq["item"]["coin_flips"]
if len(flips) != 1 or flips[0]["step"] != 37 or flips[0]["win"]:
    sys.exit("forge-oracle-test-coinflip: FAIL: fixture flip is %r, want one lost flip on step 37" % (flips,))
flips[0]["win"] = True
open(sys.argv[2], "w").write(json.dumps(rq, separators=(",", ":")) + "\n")
PY

for v in lose win; do
  "$here/scripts/forge-oracle-run.sh" "$out/$v.request.jsonl" "$out/$v.rows.jsonl" \
    || fail "forge-oracle-run.sh failed on the $v request (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"
done

python3 -I - "$out/lose.rows.jsonl" "$out/win.rows.jsonl" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-coinflip: FAIL: " + msg)

def last_p0(path, label):
    rows = [json.loads(l) for l in open(path) if l.strip()]
    if len(rows) != 1:
        fail("%s: want 1 result row, got %d" % (label, len(rows)))
    row = rows[0]
    if row.get("harness"):
        fail("%s: harness row: %s" % (label, row["harness"]))
    if row.get("strict_miss"):
        fail("%s: strict_miss: %s" % (label, row["strict_miss"]))
    if not row.get("strict"):
        fail("%s: not a strict run" % label)
    snaps = row.get("snapshots") or []
    if len(snaps) < 38:
        fail("%s: want a snapshot after step 37, got %d snapshots" % (label, len(snaps)))
    for pl in snaps[-1]["players"]:
        if pl["seat"] == 0:
            return pl
    fail("%s: no seat 0 in the last snapshot" % label)

lose = last_p0(sys.argv[1], "lost flip")
if lose["graveyard"] != ["Goblin Kaboomist"]:
    fail("lost flip: p0 graveyard = %r, want ['Goblin Kaboomist'] (the recorded flip did not decide the upkeep flip)" % (lose["graveyard"],))
win = last_p0(sys.argv[2], "won flip")
if win["graveyard"]:
    fail("won flip: p0 graveyard = %r, want [] (the recorded flip did not decide the upkeep flip)" % (win["graveyard"],))
print("PASS coin flip: Goblin Kaboomist dies on the recorded lost flip and survives the recorded won flip (same id, same seed)")
PY
