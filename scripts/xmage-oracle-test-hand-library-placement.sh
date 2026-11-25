#!/usr/bin/env bash
# Driver test: a forced Hand -> Library put-back consumes gorge's CR 401.4 placement order in the XMage oracle driver.
#
# Sawtooth Loon (xproof cluster xpt-83d468df): "draw two cards, then put two
# cards from your hand on the bottom of your library", cast with an empty hand,
# so the two drawn cards are the only two eligible. gorge has no selection to
# ask and logs only the owner's placement order (an arrange of pick kind
# library_placement); the generator queues it as choices "Island", "Plains".
# XMage's effect calls chooseTarget ONCE for a 2..2 TargetCardInHand, while the
# other Loon trigger's target answer is queued ahead of the choices: the driver
# took ONE forced card, left the other in hand and the choices unused ("(Choices
# of PlayerA) Count are not equal (found [Island, Plains])", p0.hand gorge []
# vs XMage [Plains]).
#
# Compiles ScenarioReplayHandPlacementTest (the helper's accept/refuse cases)
# and replays the exported xproof item xs:XP:sawtooth-loon:cast:s1 through
# scripts/xmage-oracle-run.sh, requiring: no harness row, no leftover, and p0's
# hand empty with 35 library cards after step 8. Requires the read-only XMage
# build, like the driver's other javac checks. Usage:
# xmage-oracle-test-hand-library-placement.sh [OUTDIR]
set -euo pipefail
root=$(realpath -m "${XMAGE_ORACLE_DIR:-$(dirname "$0")/../.oracle-build/xmage}")
out=$(realpath -m "${1:-.coverage/xmage-driver/xmage-oracle-test-hand-library-placement}")
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayHandPlacementTest.java"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayHandPlacementTest

cat > "$out/input.jsonl" <<'JSON'
{"id": "xs:XP:sawtooth-loon:cast:s1", "card": "Sawtooth Loon", "template": "xproof-start", "xmage_answers": [null, null, null, null, null, null, [{"seat": 0, "kind": "choice", "value": "When {this} enters, return a white or blue creature you control to its owner's hand."}], null, [{"seat": 0, "kind": "choice", "value": "Island"}, {"seat": 0, "kind": "choice", "value": "Plains"}], null, [{"seat": 0, "kind": "target", "value": "Sawtooth Loon"}], null], "xmage_ability": ["{T}: Add {W}.", "{T}: Add {W}.", "{T}: Add {W}.", "{T}: Add {U}.", "", "", "", "", "", "", "", ""], "xmage_pos": [null, null, null, null, null, {"turn": 1, "step": "main1", "active": 0}, {"turn": 1, "step": "main1", "active": 0, "resolves": true}, {"turn": 1, "step": "main1", "active": 0}, {"turn": 1, "step": "main1", "active": 0, "resolves": true}, {"turn": 1, "step": "main1", "active": 0}, {"turn": 1, "step": "main1", "active": 0, "resolves": true}, {"turn": 1, "step": "main1", "active": 0}], "payment": ["", "", "", "", "WWWU", "", "", "", "", "", "", ""], "name": "xproof start Sawtooth Loon cast seed 1", "turn": 1, "cr": [], "why": "A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.", "setup": {"p0": {"battlefield": ["Plains", "Plains", "Plains", "Island"], "hand": ["Sawtooth Loon"], "library_top": ["Island", "Plains", "Island", "Plains", "Island", "Plains", "Island", "Plains", "Plains", "Plains", "Plains", "Island", "Island", "Island", "Plains", "Island", "Plains", "Plains", "Plains", "Plains", "Plains", "Island", "Plains", "Island", "Island", "Island", "Island", "Island", "Island", "Plains", "Plains", "Plains", "Island", "Plains", "Island"], "life": 20}, "p1": {"library_top": ["Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains"], "life": 20}}, "steps": [{"op": "activate", "seat": 0, "card": "p0:Plains", "ability_index": 0}, {"op": "activate", "seat": 0, "card": "p0:Plains#2", "ability_index": 0}, {"op": "activate", "seat": 0, "card": "p0:Plains#3", "ability_index": 0}, {"op": "activate", "seat": 0, "card": "p0:Island", "ability_index": 0}, {"op": "cast", "seat": 0, "card": "p0:Sawtooth Loon"}, {"op": "pass", "seat": 0}, {"op": "pass", "seat": 1}, {"op": "pass", "seat": 0, "answers": [{"kind": "trigger_order", "pick": ["p0:Sawtooth Loon", "p0:Sawtooth Loon"]}]}, {"op": "pass", "seat": 1}, {"op": "pass", "seat": 0, "answers": [{"kind": "arrange", "pick": ["p0:Island#2", "p0:Plains#4"]}]}, {"op": "pass", "seat": 1}, {"op": "pass", "seat": 0, "answers": [{"kind": "choose", "pick": ["p0:Sawtooth Loon"]}]}]}
JSON
"$here/scripts/xmage-oracle-run.sh" "$out/input.jsonl" "$out/rows.jsonl" \
  || { echo "xmage-oracle-test-hand-library-placement: FAIL: xmage-oracle-run.sh failed" >&2; exit 1; }

python3 -I - "$out/rows.jsonl" <<'PY'
import json, sys

def fail(msg):
    sys.exit("xmage-oracle-test-hand-library-placement: FAIL: " + msg)

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if len(rows) != 1:
    fail("want 1 result row, got %d" % len(rows))
row = rows[0]
for k in ("harness", "strict_miss", "leftover"):
    if row.get(k):
        fail("%s: %s" % (k, row[k]))
snaps = row.get("snapshots") or []
if len(snaps) < 10:
    fail("want a snapshot after step 8, got %d snapshots" % len(snaps))
p0 = [p for p in snaps[9]["players"] if p["seat"] == 0][0]
if p0["hand"] or p0["library_count"] != 35:
    fail("after step 8 p0 hand %r library_count %r, want [] and 35" % (p0["hand"], p0["library_count"]))
print("PASS hand->library placement: the forced two-card put-back moves both cards from the queued placement order; no leftover")
PY
