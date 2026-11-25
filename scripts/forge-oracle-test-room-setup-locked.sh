#!/usr/bin/env bash
# Driver test: a Room placed by setup enters locked and has no name.
#
# A permanent with a shared type line gets an unlocked designation only when
# one of its halves was cast as a spell; a Room placed by setup was not cast,
# so it enters with neither designation and no name (CR 709.5d, 709.5). gorge
# (engine/rules/oracle_run.go fieldName) and the XMage driver place it locked
# (ScenarioReplay's placeSetup... path); the Forge driver used to unlock the
# door the setup name named, so the setup permanent read "Greenhouse" where
# gorge read the empty string (xproof cluster xpt-e4b207b7, Greenhouse //
# Rickety Gazebo). The driver must not unlock a setup door: no scenario step
# drives the CR 709.5e unlock special action, and the setup name only binds
# the card.
#
# This replays a Greenhouse // Rickety Gazebo xproof item with the Room on
# p0's battlefield and requires the setup snapshot's Room permanent to be
# named "" (not "Greenhouse") and typed Enchantment Room.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-room-setup-locked.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-room-setup-locked}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-room-setup-locked: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:greenhouse-rickety-gazebo:static0.0:s1","scenario_sha":"699f0799f9c0673a81e49f647cc4b59115548dd85731affcba0fa6ce1373ceb6","item":{"id":"xs:XP:greenhouse-rickety-gazebo:static0.0:s1","card":"Greenhouse","template":"xproof-start","compare":["offered"],"name":"xproof start Greenhouse // Rickety Gazebo static#0.0 seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Greenhouse","Forest"],"tapped_after_untap":["Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"pass","seat":0}]}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-room-setup-locked: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if not row.get("strict"):
    fail("not a strict run")
snaps = row.get("snapshots") or []
setup = next((s for s in snaps if s.get("checkpoint") == "setup"), None)
if setup is None:
    fail("no setup snapshot; checkpoints = %r" % ([s.get("checkpoint") for s in snaps],))
rooms = [p for p in setup.get("permanents") or []
         if p.get("controller") == 0 and "Room" in (p.get("types") or [])]
if len(rooms) != 1:
    fail("want one p0 Room permanent at setup, got %r" % (rooms,))
room = rooms[0]
if "Enchantment" not in (room.get("types") or []):
    fail("the Room permanent is not an enchantment: %r" % (room,))
got = room.get("name")
if got != "":
    fail("a setup Room is named %r at setup; CR 709.5d gives a Room that was not cast as a door neither unlocked designation and no name" % (got,))
print("PASS setup Room is locked with an empty name (CR 709.5d, 709.5)")
PY
