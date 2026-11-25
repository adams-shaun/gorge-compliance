#!/usr/bin/env bash
# Driver test: a checkpoint at a pass into declare attackers offers nothing
# while the declaration is pending.
#
# The active player declares attackers as a turn-based action before anyone
# gets priority (CR 508.1, 117.3a), so when the active seat has a legal
# attacker gorge's checkpoint there is the pending declaration and offers
# nothing. Forge hands that seat priority once its attackers callback returns
# without a declaration, and the driver's offered listed the seat's mana
# abilities (xproof a-realm-reborn step 3 "p0:Memnite|activate|", muldrotha,
# icetill-explorer, mm-menon-the-right-hand).
#
# Two requests, both four passes (p0, p1, p0, p1) from the first main phase, so
# step 3 enters declare attackers. With Memnite and a Forest, p0 has a legal
# attacker: step 3's offered must be empty (while step 1, a live priority in
# beginning of combat, still lists the Forest, so the empty set is the
# declaration, not a seat with nothing). With the Forest alone no attacker is
# legal, gorge resolves the forced empty declaration inside the transition and
# its checkpoint is a live priority: step 3 offers the Forest. The blockers
# twin: p0 attacks with Memnite (step 2, after two passes) and both seats pass into declare
# blockers (steps 3, 4). When p1 has a Memnite able to block, step 4 is p1's
# pending declaration and offers nothing; with no creature of p1's there is no
# legal blocker, gorge records the forced empty declaration and step 4 is a live
# priority that still offers the holder's options.
#
# An explicit empty declaration (`block` with no pairs, `attack` with no
# attackers) is answered: its own checkpoint is a live priority and still offers
# the holder's options, though the game state is the same as the pending one.
#
# Needs the out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-declare-attackers-offered.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-declare-attackers-offered}
mkdir -p "$out"
out=$(cd "$out" && pwd)

fail() { echo "forge-oracle-test-declare-attackers-offered: FAIL: $*" >&2; exit 1; }

# The six requests are static one-line fixtures, each with its own item's
# scenario_sha (TestForgeScriptFixturesCarryTheirItemSHA checks every
# forge-oracle-test-*.sh line that names a request).
cat > "$out/request-attacker.jsonl" <<'JSON'
{"id":"t:offered-declare-attackers","scenario_sha":"927bad3132e1e3d7661dd92b59adf9e058280e38c3b3baa874c2f6831102f4b2","item":{"id":"t:offered-declare-attackers","card":"Memnite","template":"xproof-start","compare":["offered"],"name":"t:offered-declare-attackers","turn":1,"cr":[],"why":"declare-attackers offered pin","setup":{"p0":{"battlefield":["Memnite","Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{}}
JSON
cat > "$out/request-no-attacker.jsonl" <<'JSON'
{"id":"t:offered-declare-attackers-none","scenario_sha":"bd61b1e6618e696e372ae058e2dca15fbb2ba22db30935a86bb5315822be241d","item":{"id":"t:offered-declare-attackers-none","card":"Memnite","template":"xproof-start","compare":["offered"],"name":"t:offered-declare-attackers-none","turn":1,"cr":[],"why":"declare-attackers offered pin","setup":{"p0":{"battlefield":["Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{}}
JSON
cat > "$out/request-blocker.jsonl" <<'JSON'
{"id":"t:offered-declare-blockers","scenario_sha":"ec8b6f78785a678eb2b60cd944c233bde6515171be293aa85ac8c3df3250d522","item":{"id":"t:offered-declare-blockers","card":"Memnite","template":"xproof-start","compare":["offered"],"name":"t:offered-declare-blockers","turn":1,"cr":[],"why":"declare-attackers offered pin","setup":{"p0":{"battlefield":["Memnite","Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"battlefield":["Memnite"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"attack","seat":0,"attackers":["p0:Memnite"],"defender":"p1"},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{}}
JSON
cat > "$out/request-no-blocker.jsonl" <<'JSON'
{"id":"t:offered-declare-blockers-none","scenario_sha":"478649109801cb50e374b26a6e4785ff35df33c8b57bf25f205ca16d46279880","item":{"id":"t:offered-declare-blockers-none","card":"Memnite","template":"xproof-start","compare":["offered"],"name":"t:offered-declare-blockers-none","turn":1,"cr":[],"why":"declare-attackers offered pin","setup":{"p0":{"battlefield":["Memnite","Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"attack","seat":0,"attackers":["p0:Memnite"],"defender":"p1"},{"op":"pass","seat":0},{"op":"pass","seat":1}]},"gorge_decisions":[],"abilities":{}}
JSON
cat > "$out/request-empty-block.jsonl" <<'JSON'
{"id":"t:offered-declare-blockers-empty","scenario_sha":"9fca882d4d68b835f1e7e6088aa489540f62828b6bcf772237d793280ec6e78d","item":{"id":"t:offered-declare-blockers-empty","card":"Memnite","template":"xproof-start","compare":["offered"],"name":"t:offered-declare-blockers-empty","turn":1,"cr":[],"why":"declare-attackers offered pin","setup":{"p0":{"battlefield":["Memnite","Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"battlefield":["Memnite"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"attack","seat":0,"attackers":["p0:Memnite"],"defender":"p1"},{"op":"pass_to","decision":"blockers"},{"op":"block","seat":1,"blocks":[]},{"op":"pass_to","step":"main2"}]},"gorge_decisions":[],"abilities":{}}
JSON
cat > "$out/request-empty-attack.jsonl" <<'JSON'
{"id":"t:offered-declare-attackers-empty","scenario_sha":"fd16ed3d53cf883c6c45ee0cd8cb24d4631f9da6290f77b2872e5988acf7967a","item":{"id":"t:offered-declare-attackers-empty","card":"Memnite","template":"xproof-start","compare":["offered"],"name":"t:offered-declare-attackers-empty","turn":1,"cr":[],"why":"declare-attackers offered pin","setup":{"p0":{"battlefield":["Memnite","Forest"],"library_top":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},"steps":[{"op":"pass_to","decision":"attackers"},{"op":"attack","seat":0,"attackers":[],"defender":"p1"},{"op":"pass_to","step":"main2"}]},"gorge_decisions":[],"abilities":{}}
JSON

for k in attacker no-attacker blocker no-blocker empty-block empty-attack; do
  "$here/scripts/forge-oracle-run.sh" "$out/request-$k.jsonl" "$out/rows-$k.jsonl" \
    || fail "forge-oracle-run.sh failed on $k (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"
done

python3 -I - "$out" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-declare-attackers-offered: FAIL: " + msg)

def load(k):
    rows = [json.loads(l) for l in open("%s/rows-%s.jsonl" % (sys.argv[1], k)) if l.strip()]
    if len(rows) != 1:
        fail("%s: want 1 result row, got %d" % (k, len(rows)))
    if rows[0].get("harness"):
        fail("%s: harness row: %s" % (k, rows[0]["harness"]))
    return {s.get("checkpoint"): s for s in rows[0].get("snapshots") or []}

def offered(snap):
    if "offered" not in snap:
        fail("snapshot %r has no offered field" % (snap.get("checkpoint"),))
    return sorted("%s|%s" % (o["source"], o["kind"]) for o in snap["offered"])

with_attacker = load("attacker")
setup = with_attacker.get("setup")
if setup is None or offered(setup) != ["p0:Forest|activate"]:
    fail("setup should offer p0's Forest alone: %r" % (setup,))
s1 = with_attacker.get("step 1 (pass)")
if s1 is None or offered(s1) != ["p0:Forest|activate"]:
    fail("step 1 (a live priority in beginning of combat) should list p0's Forest: %r" % (s1,))
s3 = with_attacker.get("step 3 (pass)")
if s3 is None:
    fail("no step 3 snapshot; checkpoints = %r" % (list(with_attacker),))
if s3.get("step") != "declare-attackers":
    fail("step 3 should be at declare-attackers, is %r" % (s3.get("step"),))
if offered(s3) != []:
    fail("a pass into declare attackers with a legal attacker offers nothing, not %r" % (offered(s3),))

none = load("no-attacker").get("step 3 (pass)")
if none is None:
    fail("no-attacker run has no step 3 snapshot")
if offered(none) != ["p0:Forest|activate"]:
    fail("with no legal attacker step 3 (step %r) is a live priority and offers p0's Forest, not %r"
         % (none.get("step"), offered(none)))
blk = load("blocker")
s2 = blk.get("step 2 (attack)")
if s2 is None or offered(s2) != ["p0:Forest|activate"]:
    fail("step 2 (the priority after the attack declaration) should list p0's Forest: %r" % (s2,))
s4 = blk.get("step 4 (pass)")
if s4 is None:
    fail("no step 4 snapshot in the blocker run; checkpoints = %r" % (list(blk),))
if s4.get("step") != "declare-blockers":
    fail("step 4 should be at declare-blockers, is %r" % (s4.get("step"),))
if offered(s4) != []:
    fail("a pass into declare blockers with a legal blocker offers nothing, not %r" % (offered(s4),))
nb = load("no-blocker").get("step 4 (pass)")
if nb is None or not offered(nb):
    fail("with no legal blocker step 4 is a live priority and offers the holder's options, not %r" % (nb,))
# An explicit EMPTY declaration is answered: the priority that follows it is a
# live one (CR 117.3a) and still lists the holder's mana ability, though the
# game state (step, attackers/blockers, a legal option) matches the pending one.
eb = load("empty-block").get("step 2 (block)")
if eb is None or eb.get("step") != "declare-blockers" or offered(eb) != ["p0:Forest|activate"]:
    fail("after an explicit empty block declaration the live priority lists p0's Forest: %r" % (eb,))
ea = load("empty-attack").get("step 1 (attack)")
if ea is None or ea.get("step") != "declare-attackers" or offered(ea) != ["p0:Forest|activate"]:
    fail("after an explicit empty attack declaration the live priority lists p0's Forest: %r" % (ea,))
print("PASS declare attackers: a pending declaration offers nothing; no legal attacker keeps the holder's options; likewise for blockers; an explicit empty declaration is answered and offers the holder's options")
PY
