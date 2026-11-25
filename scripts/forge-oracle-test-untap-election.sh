#!/usr/bin/env bash
# Driver test: the Whip Vine untap cut (CR 502.3) — finds
# xs:XP:whip-vine:f0a0:s1 and :s2 (xpt-5133dae0). A pass ends turn 1 with Whip
# Vine's optional untap choice pending in turn 2's untap step, so gorge's cut
# position is (2, untap). Forge's ChooseOptionalUntapEffect asks a binary
# chooseBinary(UntapOrLeaveTapped) that the driver's binary* switch left
# unbound (strict_miss class decision_shape, field step: the unbound default
# answered untap and the row completed one step later, at (2, upkeep), while
# gorge holds the cut at (2, untap)). The unbound branch answers leave-tapped
# (gorge left the choice pending, the permanent still tapped), takes the
# pending pass checkpoint at the ask so the snapshot lands on the untap step,
# and names the receiving seat (CR 117.3d) in its snapshot: the untap step
# grants no priority (CR 500.4), so Forge reports the passer while gorge's
# checkpoint names the seat that receives the pass's priority. Needs the
# out-of-tree build (make forge-oracle-setup), like the other
# forge-oracle-test-*.sh that replay a game. Usage:
# forge-oracle-test-untap-election.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-untap-election}
mkdir -p "$out"
out=$(cd "$out" && pwd)
rows=$out/rows.jsonl
# The fixture is the two finds' gorge-played scenarios (the items, not the cut
# harness rows): setup, the block and the Whip Vine activation. It names cards
# only; no Forge card script is embedded — forge-export reads Whip Vine's
# targeted-tap ability from the gitignored .cards link when the test runs, so
# the committed rows never carry its script text. forge-export refuses to
# write inside the repository; the request (and the prep refusals beside it)
# live in a temp dir and are discarded, while the driver's rows may stay under
# the gitignored .coverage output dir.
fixture=$here/cmd/oraclediff/testdata/forge/whip-vine-untap-election.jsonl
tmp=$(mktemp -d "${FORGE_ORACLE_TMP:-${TMPDIR:-/tmp}}/forge-untap-election.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
req=$tmp/request.jsonl

fail() { echo "forge-oracle-test-untap-election: FAIL: $*" >&2; exit 1; }

cd "$here"
go run ./cmd/oraclediff forge-export -cards .cards -scenarios "$fixture" -out "$req" \
  || fail "forge-export failed (is the Forge pin's cardsfolder present?)"

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$req" "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-untap-election: FAIL: " + msg)

reqs = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
rows = [json.loads(l) for l in open(sys.argv[2]) if l.strip()]
if len(reqs) != 2 or len(rows) != 2:
    fail("want 2 requests and rows, got %d and %d" % (len(reqs), len(rows)))
note = "binary step 21 (UntapOrLeaveTapped): unbound optional untap -> leave tapped (checkpoint taken at the ask)"
for row in rows:
    name = row.get("id")
    if name not in ("xs:XP:whip-vine:f0a0:s1", "xs:XP:whip-vine:f0a0:s2"):
        fail("unexpected row id %r" % (name,))
    for key in ("harness", "strict_miss", "leftover"):
        if row.get(key):
            fail("%s %s: %s" % (name, key, row[key]))
    if not row.get("strict"):
        fail("%s is not a strict run" % name)
    if note not in (row.get("notes") or []):
        fail("%s has no note %r; notes = %r" % (name, note, row.get("notes")))
    snaps = row.get("snapshots") or []
    if len(snaps) != 23:
        fail("%s: want 23 snapshots, got %d" % (name, len(snaps)))
    cut = snaps[-1]
    for key, want in (("checkpoint", "step 21 (pass)"), ("step", "untap")):
        if cut.get(key) != want:
            fail("%s: cut %s = %r, want %r" % (name, key, cut.get(key), want))
    for key, want in (("turn", 2), ("active", 1), ("priority", 0)):
        if cut.get(key) != want:
            fail("%s: cut %s = %r, want %r" % (name, key, cut.get(key), want))
    whip = [p for p in cut.get("permanents", []) if p.get("name") == "Whip Vine"]
    if len(whip) != 1 or not whip[0].get("tapped"):
        fail("%s: Whip Vine must stay tapped at the pending choice (CR 502.3)" % name)
# Defense in depth: the exported request must bind Whip Vine's targeted-tap
# ability for the activate step from the cardsfolder, never a committed or
# absent line.
for row in reqs:
    line = (row.get("abilities") or {}).get("9")
    if line is None:
        fail("%s has no ability line for step 9: %r" % (row.get("id"), row.get("abilities")))
    if "AB$ Tap" not in line or "CARDNAME" not in line:
        fail("%s step 9 is not Whip Vine's targeted-tap script: %r" % (row.get("id"), line))
print("PASS Whip Vine untap cut (s1, s2): unbound binary answered leave-tapped, cut at turn 2 untap (priority 0), Whip Vine tapped, no leftover")
PY
