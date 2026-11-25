#!/usr/bin/env bash
# Driver test: cast_mode "suspend" replays in the Forge oracle driver.
#
# Suspend from hand is a special action (CR 702.62a, 116.2f): it uses no stack
# and exiles the card with its time counters. gorge records it as a cast step
# with cast_mode "suspend". The driver used to pick the ordinary cast and then
# fail the item with "was not put on the stack". This replays the xproof shape
# (Lotus Bloom, Suspend 3--{0}, in p0's hand: step 0 the suspend, then passes)
# through scripts/forge-oracle-run.sh at FORGE_ORACLE_REF and requires: no
# harness row, no "was not put on the stack", Lotus Bloom in p0's exile with
# an empty stack, and the driver's own note that it holds 3 time counters (the
# snapshot lists exiled names only).
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-suspend.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-suspend}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-suspend: FAIL: $*" >&2; exit 1; }

printf '%s\n' '{"id":"xs:XP:lotus-bloom:cast:s1","scenario_sha":"7fe585f18388404b07431eee14fd4f3aab8f775fb85eafc3e70bef60a173a890","item":{"id":"xs:XP:lotus-bloom:cast:s1","card":"","template":"xproof-start","name":"xproof start Lotus Bloom cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1'"'"'s first main phase.","setup":{"p0":{"hand":["Lotus Bloom"],"life":20},"p1":{"life":20}},"steps":[{"op":"cast","seat":0,"card":"p0:Lotus Bloom","cast_mode":"suspend"},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{}}' > "$req"

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-suspend: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if "was not put on the stack" in json.dumps(row):
    fail("the suspend was treated as a spell cast")
snaps = row.get("snapshots") or []
if len(snaps) < 2:
    fail("want a snapshot after the suspend step, got %d snapshots" % len(snaps))
after = snaps[1]
p0 = after["players"][0]
if p0["exile"] != ["Lotus Bloom"]:
    fail("p0 exile = %r, want ['Lotus Bloom']" % (p0["exile"],))
if "Lotus Bloom" in p0["hand"]:
    fail("Lotus Bloom is still in hand")
if after["stack"]:
    fail("suspend used the stack: %r" % (after["stack"],))
want = "suspend step 0: p0:Lotus Bloom exiled with 3 time counters"
if want not in row.get("notes", []):
    fail("no note %r; notes = %r" % (want, row.get("notes")))
print("PASS cast_mode suspend: Lotus Bloom is in exile with 3 time counters, stack empty, no harness error")
PY
