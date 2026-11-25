#!/usr/bin/env bash
# forge-oracle-test-encore-keyword-identity.sh -- driver test for a keyword-
# expanded activated ability whose gorge API differs from the one Forge derives
# (xproof cluster xpt-1ea6e65f, Elvish Dreadlord and Rakshasa Debaser).
#
# gorge expands "Encore {5}{B}{B}" (CR 702.141a) into an `AB$ Encore | Cost$ 5 B B
# ExileFromGrave<1/CARDNAME> | ActivationZone$ Graveyard | Keyword$ Encore` line;
# Forge derives the same graveyard ability from K:Encore as a CopyPermanent
# SpellAbility with the same keyword and the same cost. The driver's identity
# tiers used to need an equal API both for the "keyword" tier and for "api+cost",
# so it stopped with `ability identity at step N: api+cost 0 (want AB$ Encore ...
# candidates: CopyPermanent 5BB,EXILECARDNAMEFROMYOURGRAVEYARD kw=Encore)`. The
# keyword tier now binds a Keyword$ line to the SA derived from that keyword by
# cost when no SA shares the API.
#
# The fixture is the four xproof finds' scenarios (Elvish Dreadlord seeds 1 and 2,
# Rakshasa Debaser seeds 1 and 2: setup, then the Encore activation from the
# graveyard). It names cards only; no Forge card script is embedded.
#
# Needs the out-of-tree build at FORGE_ORACLE_REF (make forge-oracle-setup).
# Usage: forge-oracle-test-encore-keyword-identity.sh [OUTDIR]
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/xmage-driver/forge-oracle-test-encore-keyword-identity}
mkdir -p "$out"
out=$(cd "$out" && pwd)
fixture=$here/cmd/oraclediff/testdata/forge/encore-keyword-identity.jsonl
rows=$out/rows.jsonl
# forge-export refuses to write inside the repository; the request lives in a
# temp dir and is discarded.
tmp=$(mktemp -d "${FORGE_ORACLE_TMP:-${TMPDIR:-/tmp}}/forge-encore.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
req=$tmp/request.jsonl

fail() { echo "forge-oracle-test-encore-keyword-identity: FAIL: $*" >&2; exit 1; }

cd "$here"
go run ./cmd/oraclediff forge-export -cards .cards -scenarios "$fixture" -out "$req" \
  || fail "forge-export failed (is the Forge pin's cardsfolder present?)"

"$here/scripts/forge-oracle-run.sh" "$req" "$rows" \
  || fail "forge-oracle-run.sh failed (is the Forge build at FORGE_ORACLE_REF present? make forge-oracle-setup)"

python3 -I - "$req" "$rows" <<'PY' || exit 1
import json, sys

def fail(msg):
    sys.exit("forge-oracle-test-encore-keyword-identity: FAIL: " + msg)

# Step index of the Encore activation (Elvish Dreadlord has Encore {5}{B}{B},
# Rakshasa Debaser {6}{B}{B}: one more land is set up before the activation).
want = {
    "xs:XP:elvish-dreadlord:f0a0:s1": ("7", "Cost$ 5 B B "),
    "xs:XP:elvish-dreadlord:f0a0:s2": ("7", "Cost$ 5 B B "),
    "xs:XP:rakshasa-debaser:f0a0:s1": ("8", "Cost$ 6 B B "),
    "xs:XP:rakshasa-debaser:f0a0:s2": ("8", "Cost$ 6 B B "),
}
reqs = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
rows = [json.loads(l) for l in open(sys.argv[2]) if l.strip()]
if len(reqs) != len(want) or len(rows) != len(want):
    fail("want %d requests and rows, got %d and %d" % (len(want), len(reqs), len(rows)))
for row in rows:
    name = row.get("id")
    if name not in want:
        fail("unexpected row id %r" % (name,))
    if row.get("harness"):
        fail("%s harness row: %s" % (name, row["harness"]))
    if row.get("strict_miss"):
        fail("%s strict_miss: %s" % (name, row["strict_miss"]))
    if row.get("leftover"):
        fail("%s leftover: %s" % (name, row["leftover"]))
    if not row.get("strict"):
        fail("%s is not a strict run" % name)
    step = want[name][0]
    notes = row.get("notes") or []
    if "identity: keyword at step " + step not in notes:
        fail("%s did not bind the Encore ability by keyword at step %s; notes = %r" % (name, step, notes))
# The request must carry the keyword-expanded Encore line (the driver's input).
for row in reqs:
    name = row.get("id")
    if name not in want:
        fail("unexpected request id %r" % (name,))
    step, cost = want[name]
    line = (row.get("abilities") or {}).get(step)
    if line is None:
        fail("%s has no ability line for step %s: %r" % (name, step, row.get("abilities")))
    if not line.startswith("AB$ Encore | " + cost) or "Keyword$ Encore" not in line:
        fail("%s step %s is not the keyword-expanded Encore line: %r" % (name, step, line))
print("PASS Encore: the keyword-expanded line binds Forge's CopyPermanent SA by keyword and cost, no harness/leftover")
PY
