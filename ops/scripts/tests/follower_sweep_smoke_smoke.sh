#!/usr/bin/env bash
# follower_sweep_smoke_smoke.sh — prove scripts/follower-sweep-smoke.sh wires the
# preflight filter, the sweep and the chunk checks, without Java, systemd, a corpus
# or the network. go (the registry) is a stub, the sweep is a stub on
# FOLLOWER_SWEEP_SCRIPT that writes a canned chunk directory, the runner is a stub.
#   - an unknown engine and --games 11 exit 2;
#   - pool-filter.txt holds exactly the names the preflight refused, and the sweep
#     got XENGINE_POOL / XENGINE_POOL_FILTER / --games / --chunks 1;
#   - a failing preflight exits 1;
#   - a chunk whose row count differs from the request count exits 1;
#   - a good chunk exits 0 and prints the histogram, not_sent and tape/window counts;
#   - --max-class harness=10 over a chunk with 50% harness exits 1; harness=50 exits 0.
#
#   ops/scripts/tests/follower_sweep_smoke_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT=$ROOT/scripts/follower-sweep-smoke.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/follower-sweep-smoke-smoke.XXXXXX")
trap 'rm -rf "$TMP" "$ROOT/.follower-sweep-smoke-out"' EXIT
fails=0
check() { if eval "$2"; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }

BIN=$TMP/bin
mkdir -p "$BIN"
export LOG=$TMP/calls.log
: >"$LOG"

cat >"$BIN/go" <<'STUB'
#!/usr/bin/env bash
[ "$1" = run ] && [ "$4" = followers ] || exit 0
printf '#name\trunner\tprep\tjvm\trunner_env\tmem_env\twait_env\n'
printf 'xmage\tscripts/xmage.sh\txprep\ttrue\tXENGINE_XMAGE_RUNNER\tXENGINE_XMAGE_MEM\tXENGINE_XMAGE_WAIT\n'
printf 'phase\tscripts/phase.sh\tfollower-export\tfalse\tXENGINE_PHASE_RUNNER\tXENGINE_PHASE_MEM\tXENGINE_PHASE_WAIT\n'
STUB
# runner: --preflight FILE refuses Bolt and Zap (a reason after the tab).
cat >"$BIN/runner" <<'STUB'
#!/usr/bin/env bash
[ "$1" = --preflight ] || exit 3
echo "preflight $(tr '\n' ',' <"$2")" >>"$LOG"
[ -z "${SMOKE_PREFAIL:-}" ] || { echo "boom" >&2; exit 4; }
while IFS= read -r n; do
	case "$n" in Bolt | Zap) printf '%s\tunsupported effect\n' "$n" ;; esac
done <"$2"
STUB
# sweep stub: writes <root>/FDN/0 with 4 requests and SMOKE_ROWS rows; verdicts:
# 4 sent (2 harness, 2 agree) and 1 not_sent; ids hold 3 windows and 2 tapes.
cat >"$BIN/sweep" <<'STUB'
#!/usr/bin/env bash
echo "sweep $* pool=$XENGINE_POOL filter=$XENGINE_POOL_FILTER followers=$XENGINE_FOLLOWERS root=$XENGINE_ROOT" >>"$LOG"
d=$XENGINE_ROOT/FDN/0
mkdir -p "$d/diff-phase"
for i in 1 2 3 4; do echo "{\"id\":$i}"; done >"$d/phase-req.jsonl"
n=${SMOKE_ROWS:-4}
for i in $(seq 1 "$n"); do echo "{\"id\":$i}"; done >"$d/phase.jsonl"
echo '{"id":"xe:FDN:0:0:w1"}' >"$d/phase-prep.jsonl"
{
	echo '{"id":"xe:FDN:0:0:w1","class":"harness"}'
	echo '{"id":"xe:FDN:0:0:w2","class":"harness"}'
	echo '{"id":"xe:FDN:0:0:tape","class":"agree"}'
	echo '{"id":"xe:FDN:0:1:tape","class":"agree"}'
	echo '{"id":"xe:FDN:0:1:w1","class":"not_sent"}'
} >"$d/diff-phase/verdicts.jsonl"
[ -n "${SMOKE_NODONE:-}" ] || : >"$d/.done"
exit "${SMOKE_SWEEPRC:-0}"
STUB
chmod +x "$BIN"/*

printf '# pool\nBolt\n\nZap # inline\nGrizzly Bears\nBolt\n' >"$TMP/pool.txt"

ss() { # args...: one run
	XENGINE_GO=$BIN/go XENGINE_SCOPE=none XENGINE_PHASE_RUNNER=$BIN/runner FOLLOWER_SWEEP_SCRIPT=$BIN/sweep \
		XENGINE_ROOT=${R:-$TMP/root} "$SCRIPT" "$@" 2>&1
}

out=$(ss nosuch --pool "$TMP/pool.txt"); rc=$?
check "unknown engine exits 2" '[ "$rc" = 2 ] && has "$out" "unknown follower nosuch (want one of: xmage phase)"' "rc=$rc $out"
out=$(ss phase --games 11 --pool "$TMP/pool.txt"); rc=$?
check "--games 11 exits 2" '[ "$rc" = 2 ] && has "$out" "orchestrator"' "rc=$rc $out"
out=$(R=$ROOT/.follower-sweep-smoke-out ss phase --pool "$TMP/pool.txt"); rc=$?
check "in-repo root exits 2" '[ "$rc" = 2 ] && has "$out" "not under .oracle-build/" && [ ! -e "$ROOT/.follower-sweep-smoke-out" ]' "rc=$rc $out"
out=$(ss phase --pool "$TMP/pool.txt" --max-class harness); rc=$?
check "bad --max-class exits 2" '[ "$rc" = 2 ]' "rc=$rc $out"

# Good chunk.
R=$TMP/r1; : >"$LOG"
out=$(R=$R ss phase --games 3 --seed 7 --pool "$TMP/pool.txt"); rc=$?
check "good chunk exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "pool-names.txt: sorted unique, no comments" '[ "$(cat "$R/pool-names.txt")" = "$(printf "Bolt\nGrizzly Bears\nZap")" ]' "$(cat "$R/pool-names.txt")"
check "pool-filter.txt holds exactly the refused names" '[ "$(cat "$R/pool-filter.txt")" = "$(printf "Bolt\nZap")" ]' "$(cat "$R/pool-filter.txt")"
check "refused K of P line" 'has "$out" "refused 2 of 3 pool cards"' "$out"
check "sweep got pool, filter, games, chunks, seed, run" '/usr/bin/grep -q -- "sweep --run smoke-phase --sets FDN --chunks 1 --games 3 --seed 7 --force pool=$TMP/pool.txt filter=$R/pool-filter.txt followers=phase root=$R" "$LOG"' "$(cat "$LOG")"
check "class histogram printed" 'has "$out" "harness 2" && has "$out" "agree 2" && has "$out" "not_sent 1"' "$out"
check "not_sent and tape/window counts printed" 'has "$out" "not_sent (prep refusals): 1" && has "$out" "items: 2 tape, 3 window"' "$out"

# Failing preflight.
out=$(SMOKE_PREFAIL=1 R=$TMP/r2 ss phase --pool "$TMP/pool.txt"); rc=$?
check "failing preflight exits 1" '[ "$rc" = 1 ] && has "$out" "preflight failed"' "rc=$rc $out"

# Row count differs from request count.
out=$(SMOKE_ROWS=3 R=$TMP/r3 ss phase --pool "$TMP/pool.txt"); rc=$?
check "row/request mismatch exits 1" '[ "$rc" = 1 ] && has "$out" "3 rows for 4 requests"' "rc=$rc $out"
out=$(SMOKE_NODONE=1 R=$TMP/r4 ss phase --pool "$TMP/pool.txt"); rc=$?
check "missing .done exits 1" '[ "$rc" = 1 ] && has "$out" ".done is missing"' "rc=$rc $out"
out=$(SMOKE_SWEEPRC=1 R=$TMP/r5 ss phase --pool "$TMP/pool.txt"); rc=$?
check "failed sweep exits 1" '[ "$rc" = 1 ]' "rc=$rc $out"

# --max-class: harness is 2 of 4 sent = 50%.
out=$(R=$TMP/r6 ss phase --pool "$TMP/pool.txt" --max-class harness=10); rc=$?
check "max-class below the rate exits 1" '[ "$rc" = 1 ] && has "$out" "class harness is 2 of 4"' "rc=$rc $out"
out=$(R=$TMP/r6 ss phase --pool "$TMP/pool.txt" --max-class harness=50 --max-class decision_shape=0); rc=$?
check "max-class at the rate exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"

if [ "$fails" != 0 ]; then echo "follower_sweep_smoke_smoke: $fails check(s) FAILED" >&2; exit 1; fi
echo "follower_sweep_smoke_smoke: ok"
