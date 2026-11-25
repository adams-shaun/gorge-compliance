#!/usr/bin/env bash
# Driver test: a dual-typed land's intrinsic mana ability replays by colour.
#
# A land with two basic land types (CR 305.6: Underground Sea, the shock,
# battle and surveil lands) has two intrinsic one-colour mana abilities. gorge's
# choice between them is dropped from the request because the step names the
# ability by index, so the driver met two "Mana T" candidates and failed
# "ability identity ... basic-land 2" (xproof cluster xpt-079c8ffe). The export
# now sends "intrinsic: basic land mana <C>" for such a land and the driver
# filters by that colour. This replays two exported requests for Underground
# Sea, one per ability (U, then B), through scripts/forge-oracle-run.sh at
# FORGE_ORACLE_REF and requires for each: no harness row, no strict miss, no
# leftover, and p0's pool after step 0 holding exactly that colour.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage: forge-oracle-test-dual-land-colour.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-dual-land-colour}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-dual-land-colour: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:underground-sea:f0a0:s1","scenario_sha":"382a0a280d9139d66c98253d2de2bee0ae023d4272df1d85469ce53709b7cad0","item":{"id":"xs:XP:underground-sea:f0a0:s1","card":"Underground Sea","template":"xproof-start","name":"xproof start Underground Sea f0/a0 seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Underground Sea"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Underground Sea","ability_index":0},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{"0":"intrinsic: basic land mana U"}}
{"id":"xs:XP:underground-sea:f0a1:s1","scenario_sha":"98855f8102105c36c0361e76b102eb2a78ed5ed358a1e63328afad982ba5984b","item":{"id":"xs:XP:underground-sea:f0a1:s1","card":"Underground Sea","template":"xproof-start","name":"xproof start Underground Sea f0/a1 seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Underground Sea"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Underground Sea","ability_index":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{"0":"intrinsic: basic land mana B"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-dual-land-colour: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 2:
    fail("want 2 result rows, got %d" % len(rows))
want = {"xs:XP:underground-sea:f0a0:s1": "U", "xs:XP:underground-sea:f0a1:s1": "B"}
seen = set()
for row in rows:
    rid = row.get("id")
    if rid not in want:
        fail("unexpected row id %r" % rid)
    seen.add(rid)
    for k in ("harness", "strict_miss", "leftover"):
        if row.get(k):
            fail("%s: %s: %s" % (rid, k, row[k]))
    snaps = row.get("snapshots") or []
    if len(snaps) < 2:
        fail("%s: want a snapshot after step 0, got %d snapshots" % (rid, len(snaps)))
    pool = snaps[1]["players"][0].get("pool")
    if pool != want[rid]:
        fail("%s: p0's pool after step 0 is %r, want %r" % (rid, pool, want[rid]))
if seen != set(want):
    fail("rows %r, want %r" % (sorted(seen), sorted(want)))
print("PASS dual-land colour: Underground Sea's U and B abilities each replay by the line's colour; no harness row, no strict miss")
PY
