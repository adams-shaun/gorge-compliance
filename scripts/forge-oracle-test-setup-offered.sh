#!/usr/bin/env bash
# Driver test: a checkpoint snapshot carries "offered" when the item compares it.
#
# The driver wrote no "offered" field, so every compare:["offered"] item
# diverged at setup the moment gorge offered anything (xproof cluster
# xpt-a64b83f5: Muldrotha, Adagia, ...): Forge's empty list against gorge's
# non-empty one. With Muldrotha, the Gravetide on the battlefield and a Plains
# in the graveyard, the priority holder may play that Plains (Forge's MayPlay
# copy of the land), so the setup snapshot must list p0:Plains as a "play", and
# after the play the Plains's mana ability as an "activate" (CR 605.3a: mana
# abilities are offered too), the shape gorge's snapshot and the XMage driver use.
#
# This replays the exported xproof request xs:XP:muldrotha-the-gravetide:static0.0:s1
# (find 11251008 b00) at FORGE_ORACLE_REF, verbatim; its abilities map is empty,
# so no Forge card script is embedded. A control run of the same request without
# compare:["offered"] must carry no "offered" at all (the other items' wire bytes
# stay what they were).
#
# The unit half (RefTableTest.refOfNamesAnObjectAsGorgeSnapshotsIt) pins how an
# offered source is named: the smallest setup-bound ref, else gorge's positional
# pN:Name / pN:Name#k.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-setup-offered.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-setup-offered}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
ctl=$out/request-control.jsonl
rows=$out/rows.jsonl
ctlrows=$out/rows-control.jsonl

fail() { echo "forge-oracle-test-setup-offered: FAIL: $*" >&2; exit 1; }

forge=${FORGE_ORACLE_DIR:-$here/$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
xmage=${XMAGE_ORACLE_DIR:-$here/$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
runner=$forge/forge/forge-oracle/run-unit-tests.sh
[ -f "$runner" ] || fail "no driver test runner at $runner (make forge-oracle-setup)"
ulog=$out/unit.log
FORGE_ORACLE_DIR=$forge FORGE_ORACLE_JDK=$jdk FORGE_ORACLE_TEST_CLASSES=forge.oracle.RefTableTest \
  bash "$runner" > "$ulog" 2>&1 || { cat "$ulog" >&2; fail "RefTableTest failed"; }
grep -qx 'PASS RefTableTest\.refOfNamesAnObjectAsGorgeSnapshotsIt' "$ulog" \
  || fail "RefTableTest.refOfNamesAnObjectAsGorgeSnapshotsIt did not pass; the pinned driver has no RefTable.refOf"

cat > "$req" <<'JSON'
{"id":"xs:XP:muldrotha-the-gravetide:static0.0:s1","scenario_sha":"8eaed916e0836c571de09b4e49b6b0b49aca05f827c00f2a241a3de425b9e782","item":{"id":"xs:XP:muldrotha-the-gravetide:static0.0:s1","card":"Muldrotha, the Gravetide","template":"xproof-start","compare":["offered"],"name":"xproof start Muldrotha, the Gravetide static#0.0 seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Muldrotha, the Gravetide"],"graveyard":["Plains"],"library_top":["Swamp","Island","Swamp","Island","Forest","Swamp","Forest","Swamp","Forest","Forest","Swamp","Forest","Island","Swamp","Swamp","Swamp","Island","Swamp","Forest","Swamp","Forest","Swamp","Forest","Island","Island","Island","Island","Forest","Swamp","Island","Forest","Island","Swamp","Forest","Forest","Island","Island","Island"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"play","seat":0,"card":"p0:Plains"}]},"gorge_decisions":[],"abilities":{}}
JSON
python3 -I - "$req" "$ctl" <<'PY'
import json, sys
row = json.loads(open(sys.argv[1]).readline())
row["id"] = row["id"] + ":control"
del row["item"]["compare"]
open(sys.argv[2], "w").write(json.dumps(row, separators=(",", ":")) + "\n")
PY

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"
"$here/scripts/forge-oracle-run.sh" "$ctl" "$ctlrows" \
  || fail "forge-oracle-run.sh failed on the control request"

python3 -I - "$rows" "$ctlrows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-setup-offered: FAIL: " + msg)

def load(path):
    rows = [json.loads(l) for l in open(path) if l.strip()]
    if len(rows) != 1:
        fail("%s: want 1 result row, got %d" % (path, len(rows)))
    if rows[0].get("harness"):
        fail("harness row: %s" % rows[0]["harness"])
    return rows[0].get("snapshots") or []

snaps = load(sys.argv[1])
by = {s.get("checkpoint"): s for s in snaps}
setup = by.get("setup")
if setup is None:
    fail("no setup snapshot; checkpoints = %r" % (list(by),))
offered = setup.get("offered")
if offered is None:
    fail("the setup snapshot has no offered field")
if not any(o.get("source") == "p0:Plains" and o.get("kind") == "play" for o in offered):
    fail("setup does not offer the graveyard Plains as a play: %r" % (offered,))
if any(o.get("kind") != "play" for o in offered if o.get("source") == "p0:Plains"):
    fail("an offer on the unplayed Plains is not a play: %r" % (offered,))
after = next((s for c, s in by.items() if (c or "").startswith("step 0")), None)
if after is None:
    fail("no snapshot after step 0; checkpoints = %r" % (list(by),))
mana = [o for o in after.get("offered") or [] if o.get("source") == "p0:Plains" and o.get("kind") == "activate"]
if not mana:
    fail("after the play the Plains's mana ability is not offered as an activate: %r" % (after.get("offered"),))
for s in load(sys.argv[2]):
    if "offered" in s:
        fail("the control snapshot %r carries offered: %r" % (s.get("checkpoint"), s["offered"]))
print("PASS offered: setup offers p0:Plains as a play, step 0 its mana ability as an activate; no offered without compare")
PY
