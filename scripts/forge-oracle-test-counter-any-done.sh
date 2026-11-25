#!/usr/bin/env bash
# Driver test: an up-to remove-any-type election stops early on gorge's Done.
#
# Mabel, Bitter Recluse's ETB removes "up to three counters from another target
# creature or planeswalker". gorge answers with p1's Ajani Resolute (two loyalty
# and one +1/+1), removes one LOYALTY, then logs a counter-kind answer of Done
# (pick kind remove_counter_done, engine/effects/removecounter.go's
# removeCounterAnyCarrier) and stops. The permanent keeps one loyalty and its
# +1/+1 (CR 122.1).
#
# Forge's CountersRemoveEffect.removeAnyType asks chooseCounterType (the kind)
# then chooseNumber (the amount), and it offers no Done type. The driver used
# to match only picks naming an offered CounterType, so the Done pick
# strict-missed "counter type" and stayed queued: the amount ask answered for
# the next present kind and Forge removed it. StepMachine.takeCounterKind now
# reads the Done pick by its pick kind, consumes it and answers with a legal
# offered type; that type's chooseNumber then finds no counter pick and answers
# 0 (min is 0 under UpTo), which is how removeAnyType stops early.
#
# This replays the exported xproof request xs:XP:mabel-bitter-recluse:cast:s1
# (the setup scripts/forge-oracle-test-counter-any-mixed-kinds.sh uses) with
# its answers/gorge_decisions changed to remove one LOYALTY and then Done. It
# requires Ajani Resolute still on p1's battlefield at 1 loyalty and one +1/+1,
# with no harness, strict-miss or leftover row. The request's abilities map
# names only the intrinsic basic land mana ability, so no Forge card script is
# embedded.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-counter-any-done.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-counter-any-done}
mkdir -p "$out"
out=$(cd "$out" && pwd)
req=$out/request.jsonl
rows=$out/rows.jsonl

fail() { echo "forge-oracle-test-counter-any-done: FAIL: $*" >&2; exit 1; }

cat > "$req" <<'JSON'
{"id":"xs:XP:mabel-bitter-recluse:cast:s1","scenario_sha":"8600ba91b965e6d270d2cd3dbe74d49a838403656c280f347b1d1c5d7e99f7b6","item":{"id":"xs:XP:mabel-bitter-recluse:cast:s1","card":"Mabel, Bitter Recluse","template":"xproof-start","payment":["","B","",""],"name":"xproof start Mabel, Bitter Recluse cast seed 1","turn":1,"cr":[],"why":"A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.","setup":{"p0":{"battlefield":["Swamp"],"hand":["Mabel, Bitter Recluse"],"library_top":["Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp","Swamp"],"life":20},"p1":{"battlefield":["Ajani Resolute"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"counters":{"Ajani Resolute":{"P1P1":1}},"life":20}},"steps":[{"op":"activate","seat":0,"card":"p0:Swamp","ability_index":0},{"op":"cast","seat":0,"card":"p0:Mabel, Bitter Recluse"},{"op":"pass","seat":0},{"op":"resolve","seat":0,"answers":[{"kind":"target","pick":["p1:Ajani Resolute"]},{"kind":"choose","pick":["LOYALTY"]},{"kind":"choose","pick":["Done"]}]}]},"gorge_decisions":[{"step":3,"seat":0,"kind":"target","options":1,"picks":["Ajani Resolute (b)"],"pick_idx":[0],"pick_refs":["p1:Ajani Resolute"],"pick_refs_inexact":[false],"option_refs":["p1:Ajani Resolute"],"object_picks":["p1:Ajani Resolute"],"pick_kinds":["permanent"],"via":"answer","gorge_kind":"target","first":"Ajani Resolute (b)","min":1,"max":1},{"step":3,"seat":0,"kind":"choose_n","options":3,"picks":["LOYALTY"],"pick_idx":[0],"pick_refs":["LOYALTY"],"pick_refs_inexact":[false],"pick_kinds":["counter"],"resume":"counter_kind","via":"answer","gorge_kind":"choose","first":"LOYALTY","min":1,"max":1},{"step":3,"seat":0,"kind":"choose_n","options":3,"picks":["Done"],"pick_idx":[2],"pick_refs":["Done"],"pick_refs_inexact":[false],"pick_kinds":["remove_counter_done"],"resume":"counter_kind","via":"answer","gorge_kind":"choose","first":"Done","min":1,"max":1}],"abilities":{"0":"intrinsic: basic land mana"}}
JSON

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-counter-any-done: FAIL: " + msg)

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
notes = row.get("notes") or []
if not any(n.startswith("counter kind step 3: Done") for n in notes):
    fail("the Done pick was not consumed at the counter-kind ask; notes = %r" % (notes,))
snaps = row.get("snapshots") or []
step3 = [s for s in snaps if (s.get("checkpoint") or "").startswith("step 3")]
if not step3:
    fail("no snapshot after step 3; checkpoints = %r" % ([s.get("checkpoint") for s in snaps],))
after = step3[0]
ajani = [p for p in after.get("permanents") or [] if p.get("name") == "Ajani Resolute"]
if len(ajani) != 1:
    fail("want one Ajani Resolute on the battlefield at step 3, got %r" % (ajani,))
planeswalker = ajani[0]
if planeswalker.get("controller") != 1:
    fail("Ajani Resolute is controlled by %r, want p1" % (planeswalker.get("controller"),))
counters = planeswalker.get("counters") or {}
if counters.get("LOYALTY") != 1:
    fail("Ajani Resolute has %r loyalty at step 3, want 1" % (counters.get("LOYALTY"),))
if counters.get("P1P1") != 1:
    fail("Ajani Resolute has %r +1/+1 counters at step 3, want 1" % (counters.get("P1P1"),))
p1 = next((p for p in after.get("players") or [] if p.get("seat") == 1), None)
if p1 is None:
    fail("step 3 snapshot has no p1")
if "Ajani Resolute" in (p1.get("graveyard") or []):
    fail("Ajani Resolute is in p1's graveyard at step 3")
print("PASS counter any done: Ajani Resolute is still on p1's battlefield at 1 loyalty and one +1/+1, no harness row")
PY
