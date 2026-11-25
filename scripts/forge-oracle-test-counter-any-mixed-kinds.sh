#!/usr/bin/env bash
# Driver test: a remove-any-type ask counts every counter-kind pick at its step.
#
# Mabel, Bitter Recluse's ETB removes "up to three counters from another target
# creature or planeswalker". gorge answers with a target (p1's Ajani Resolute,
# carrying two loyalty and one +1/+1) and then one counter-kind pick per
# counter removed: LOYALTY, LOYALTY, P1P1 (CR 122.1). Ajani at 0 loyalty goes
# to the graveyard (CR 704.5i).
#
# Forge's CountersRemoveEffect.removeAnyType asks chooseCounterType (the kind)
# and then chooseNumber (that kind's amount). StepMachine.takeCounterKind used
# to TAKE the first kind pick, so the amount ask counted only the one remaining
# LOYALTY and returned 1; P1P1 was the lone kind and removed its one. Forge
# removed one loyalty and one +1/+1, Ajani stayed at 1 loyalty, and every pick
# was consumed so no strict miss fired. At this pin chooseCounterType asks
# takeCounterKind before the lone-option shortcut, so a lone kind reaches it too
# (the Done sibling consumes an early stop there). A mixed-kind target is still
# the shape whose amount run spans two kinds, so this request pins that count.
#
# This replays the exported xproof request xs:XP:mabel-bitter-recluse:cast:s1
# (the find 11250151 b00 shape) at FORGE_ORACLE_REF and requires p1.graveyard
# [Ajani Resolute] at step 3 with no harness or strict-miss row. The request is
# the exported one, verbatim; its abilities map names only the intrinsic basic
# land mana ability, so no Forge card script is embedded.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-counter-any-mixed-kinds.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-counter-any-mixed-kinds}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-counter-any-mixed-kinds: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:mabel-bitter-recluse:cast:s1","scenario_sha":"8bde910d56c635c581ae0a8ca0beb7259d201b5fad0add4fef2a3296edd378ce","item":{"id":"xs:XP:mabel-bitter-recluse:cast:s1","card":"Mabel, Bitter Recluse","template":"xproof-start","payment":["","B","",""],"name":"xproof start Mabel, Bitter Recluse cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp"],"hand":["Mabel, Bitter Recluse"],"library_top":["Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp"],"life":20},"p1":{"battlefield":["Ajani Resolute"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"counters":{"Ajani Resolute":{"P1P1":1}},"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"cast","seat":0,"card":"p0:Mabel, Bitter Recluse"},{"op":"pass","seat":0},{"op":"resolve","seat":0,"answers":[{"kind":"target","pick":["p1:Ajani Resolute"]},{"kind":"choose","pick":["LOYALTY"]},{"kind":"choose","pick":["LOYALTY"]},{"kind":"choose","pick":["P1P1"]}]}]},"gorge_decisions":[{"step":3,"seat":0,"kind":"target","options":1,"picks":["Ajani Resolute (b)"],"pick_idx":[0],"pick_refs":["p1:Ajani Resolute"],"pick_refs_inexact":[false],"option_refs":["p1:Ajani Resolute"],"object_picks":["p1:Ajani Resolute"],"pick_kinds":["permanent"],"via":"answer","gorge_kind":"target","first":"Ajani Resolute (b)","min":1,"max":1},{"step":3,"seat":0,"kind":"choose_n","options":3,"picks":["LOYALTY"],"pick_idx":[0],"pick_refs":["LOYALTY"],"pick_refs_inexact":[false],"pick_kinds":["counter"],"resume":"counter_kind","via":"answer","gorge_kind":"choose","first":"LOYALTY","min":1,"max":1},{"step":3,"seat":0,"kind":"choose_n","options":3,"picks":["LOYALTY"],"pick_idx":[0],"pick_refs":["LOYALTY"],"pick_refs_inexact":[false],"pick_kinds":["counter"],"resume":"counter_kind","via":"answer","gorge_kind":"choose","first":"LOYALTY","min":1,"max":1},{"step":3,"seat":0,"kind":"choose_n","options":2,"picks":["P1P1"],"pick_idx":[0],"pick_refs":["P1P1"],"pick_refs_inexact":[false],"pick_kinds":["counter"],"resume":"counter_kind","via":"answer","gorge_kind":"choose","first":"P1P1","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-counter-any-mixed-kinds: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
if row.get("harness"):
    fail("harness row: %s" % row["harness"])
if row.get("strict_miss"):
    fail("strict_miss: %s" % row["strict_miss"])
if row.get("leftover"):
    fail("leftover: %s" % row["leftover"])
if not row.get("strict"):
    fail("not a strict run")
snaps = row.get("snapshots") or []
step3 = [s for s in snaps if (s.get("checkpoint") or "").startswith("step 3")]
if not step3:
    fail("no snapshot after step 3; checkpoints = %r" % ([s.get("checkpoint") for s in snaps],))
p1 = next((p for p in step3[0].get("players") or [] if p.get("seat") == 1), None)
if p1 is None:
    fail("step 3 snapshot has no p1")
if p1.get("graveyard") != ["Ajani Resolute"]:
    fail("p1.graveyard at step 3 is %r, want ['Ajani Resolute']" % (p1.get("graveyard"),))
notes = row.get("notes") or []
if not any(n.startswith("counter step 3: removed 2 Loyalty") for n in notes):
    fail("Forge did not remove both loyalty counters; notes = %r" % (notes,))
if not any(n.startswith("counter step 3: removed 1 +1/+1") for n in notes):
    fail("Forge did not remove the +1/+1 counter; notes = %r" % (notes,))
print("PASS counter any mixed kinds: Ajani Resolute is in p1's graveyard at step 3 (2 loyalty + 1 +1/+1 removed), no harness row")
PY
