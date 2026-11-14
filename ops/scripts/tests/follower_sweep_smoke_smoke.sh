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
printf 'manabrew\tscripts/manabrew.sh\tfollower-export\tfalse\tXENGINE_MANABREW_RUNNER\tXENGINE_MANABREW_MEM\tXENGINE_MANABREW_WAIT\n'
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
for e in $XENGINE_FOLLOWERS; do
	mkdir -p "$d/diff-$e"
	for i in 1 2 3 4; do echo "{\"id\":$i}"; done >"$d/$e-req.jsonl"
	n=${SMOKE_ROWS:-4}
	[ "$e" != manabrew ] || n=${SMOKE_ROWS_manabrew:-$n}
	for i in $(seq 1 "$n"); do echo "{\"id\":$i}"; done >"$d/$e.jsonl"
	echo '{"id":"xe:FDN:0:0:w1"}' >"$d/$e-prep.jsonl"
	{
		echo '{"id":"xe:FDN:0:0:w1","class":"harness"}'
		echo '{"id":"xe:FDN:0:0:w2","class":"harness"}'
		echo '{"id":"xe:FDN:0:0:tape","class":"agree"}'
		echo '{"id":"xe:FDN:0:1:tape","class":"agree"}'
		echo '{"id":"xe:FDN:0:1:w1","class":"not_sent"}'
	} >"$d/diff-$e/verdicts.jsonl"
done
[ -n "${SMOKE_NODONE:-}" ] || : >"$d/.done"
exit "${SMOKE_SWEEPRC:-0}"
STUB
# runner2 (manabrew): refuses Zap and Fireball; SMOKE_PREFAIL_manabrew fails it.
cat >"$BIN/runner2" <<'STUB'
#!/usr/bin/env bash
[ "$1" = --preflight ] || exit 3
echo "preflight2 $(tr '\n' ',' <"$2")" >>"$LOG"
[ -z "${SMOKE_PREFAIL_manabrew:-}" ] || { echo "boom2" >&2; exit 4; }
while IFS= read -r n; do
	case "$n" in Zap | Fireball) printf '%s\tno such card\n' "$n" ;; esac
done <"$2"
STUB
chmod +x "$BIN"/*

printf '# pool\nBolt\n\nZap # inline\nGrizzly Bears\nBolt\n' >"$TMP/pool.txt"

ss() { # args...: one run
	XENGINE_GO=$BIN/go XENGINE_SCOPE=none XENGINE_PHASE_RUNNER=$BIN/runner XENGINE_MANABREW_RUNNER=$BIN/runner2 FOLLOWER_SWEEP_SCRIPT=$BIN/sweep \
		XENGINE_ROOT=${R:-$TMP/root} "$SCRIPT" "$@" 2>&1
}

out=$(ss nosuch --pool "$TMP/pool.txt"); rc=$?
check "unknown engine exits 2" '[ "$rc" = 2 ] && has "$out" "unknown follower nosuch (want one of: xmage phase manabrew)"' "rc=$rc $out"
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

# Two engines: phase refuses Bolt,Zap; manabrew refuses Zap,Fireball.
printf '# pool\nBolt\nZap\nFireball\nGrizzly Bears\n' >"$TMP/pool2.txt"
R=$TMP/r7; : >"$LOG"
out=$(R=$R ss 'phase manabrew' --games 4 --pool "$TMP/pool2.txt"); rc=$?
check "two engines: exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "two engines: pool-filter.txt is the union" '[ "$(cat "$R/pool-filter.txt")" = "$(printf "Bolt\nFireball\nZap")" ]' "$(cat "$R/pool-filter.txt")"
check "two engines: refused over the union" 'has "$out" "refused 3 of 4 pool cards"' "$out"
check "two engines: per-engine preflight files" '[ "$(cut -f1 "$R/preflight-phase.out" | paste -sd,)" = "Bolt,Zap" ] && [ "$(cut -f1 "$R/preflight-manabrew.out" | paste -sd,)" = "Fireball,Zap" ]'
check "two engines: each preflight ran against its own runner" '/usr/bin/grep -q "^preflight " "$LOG" && /usr/bin/grep -q "^preflight2 " "$LOG"' "$(cat "$LOG")"
check "two engines: sweep got both followers, run and root" '/usr/bin/grep -q -- "sweep --run smoke-phase-manabrew --sets FDN --chunks 1 --games 4 --seed 1 --force pool=$TMP/pool2.txt filter=$R/pool-filter.txt followers=phase manabrew root=$R" "$LOG"' "$(cat "$LOG")"
check "two engines: a histogram per engine" 'has "$out" "class histogram (phase):" && has "$out" "class histogram (manabrew):"' "$out"
check "two engines: not_sent and window counts per engine" '[ "$(/usr/bin/grep -c "^not_sent (prep refusals): 1" <<<"$out")" = 2 ] && [ "$(/usr/bin/grep -c "^items: 2 tape, 3 window" <<<"$out")" = 2 ]' "$out"

# Default root/run name joins the engines with -.
out=$(env -u XENGINE_ROOT XENGINE_GO=$BIN/go XENGINE_SCOPE=none XENGINE_PHASE_RUNNER=$BIN/runner XENGINE_MANABREW_RUNNER=$BIN/runner2 \
	FOLLOWER_SWEEP_SCRIPT=$BIN/sweep "$SCRIPT" 'phase manabrew' --pool "$TMP/pool2.txt" 2>&1); rc=$?
check "two engines: default root is smoke-phase-manabrew" '[ "$rc" = 0 ] && /usr/bin/grep -q "followers=phase manabrew root=$ROOT/.oracle-build/xengine/smoke-phase-manabrew$" "$LOG"' "rc=$rc $out"
rm -rf "$ROOT/.oracle-build/xengine/smoke-phase-manabrew"

out=$(SMOKE_PREFAIL_manabrew=1 R=$TMP/r8 ss 'phase manabrew' --pool "$TMP/pool2.txt"); rc=$?
check "second engine's failing preflight exits 1 naming it" '[ "$rc" = 1 ] && has "$out" "manabrew --preflight failed"' "rc=$rc $out"
out=$(SMOKE_ROWS_manabrew=3 R=$TMP/r9 ss 'phase manabrew' --pool "$TMP/pool2.txt"); rc=$?
check "second engine's row mismatch exits 1 naming it" '[ "$rc" = 1 ] && has "$out" "manabrew.jsonl has 3 rows for 4 requests" && ! has "$out" "phase.jsonl has"' "rc=$rc $out"
out=$(R=$TMP/r10 ss 'phase manabrew' --pool "$TMP/pool2.txt" --max-class harness=10); rc=$?
check "max-class failure names the engine" '[ "$rc" = 1 ] && has "$out" "phase: class harness is 2 of 4" && has "$out" "manabrew: class harness is 2 of 4"' "rc=$rc $out"
out=$(R=$TMP/r11 ss 'phase nosuch' --pool "$TMP/pool2.txt"); rc=$?
check "unknown second engine exits 2" '[ "$rc" = 2 ] && has "$out" "unknown follower nosuch"' "rc=$rc $out"

if [ "$fails" != 0 ]; then echo "follower_sweep_smoke_smoke: $fails check(s) FAILED" >&2; exit 1; fi
echo "follower_sweep_smoke_smoke: ok"
