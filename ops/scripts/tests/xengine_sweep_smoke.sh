#!/usr/bin/env bash
# xengine_sweep_smoke.sh — prove scripts/xengine-sweep.sh schedules, resumes
# and caps follower JVMs, without Java, systemd, a corpus or the network.
#
# go (play, diff, cov), the two follower runners, oraclediff and pgrep are
# stubs on the script's override hooks; only `xengine sweep-plan` is the real
# thing (it is the resume logic). The assertions:
#   - a cold 2 sets x 2 chunks run does every stage and marks every chunk done;
#   - a rerun does nothing;
#   - deleting one .done redoes exactly that chunk, the old directory moved aside;
#   - 2 running JVMs make the follower stage wait, then time out with a message;
#   - a root inside the repository is refused;
#   - a failing follower leaves no .done.
#
#   ops/scripts/tests/xengine_sweep_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SWEEP=$ROOT/scripts/xengine-sweep.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/xengine-sweep-smoke.XXXXXX")
trap 'rm -rf "$TMP" "$ROOT/.xengine-smoke-root"' EXIT
fails=0
# check NAME CONDITION [DETAIL]: CONDITION is shell text, eval'd here.
check() { if eval "$2"; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }
count() { /usr/bin/grep -c "^$1" "$LOG" || true; }

REALGO=$(command -v go) || { echo "xengine_sweep_smoke: no go on PATH" >&2; exit 2; }
BIN=$TMP/bin
mkdir -p "$BIN"
LOG=$TMP/calls.log
: > "$LOG"
export LOG REALGO

# go stub: `go run -trimpath ./cmd/xengine SUB ARGS` -> a canned chunk; sweep-plan
# (the resume logic under test) goes to the real go untouched.
cat > "$BIN/go" <<'STUB'
#!/usr/bin/env bash
[ "$1" = run ] || exit 0
if [ "$4" = sweep-plan ]; then exec "$REALGO" "$@"; fi
shift 3
sub=$1; shift
declare -A o
w=0
while [ $# -gt 0 ]; do
	case "$1" in
	-windows) w=1; shift ;;
	-missing) shift ;;
	-ledger) o[ledger]="${o[ledger]:-} $2"; shift 2 ;;
	-*) o[${1#-}]=${2:-}; shift 2 ;;
	*) shift ;;
	esac
done
case "$sub" in
play)
	echo "play ${o[set]} ${o[chunk]} windows=$w ledger=${o[ledger]:-none}" >> "$LOG"
	mkdir -p "${o[out]}"
	printf '{"id":"xe:%s:%s:0:tape"}\n' "${o[set]}" "${o[chunk]}" > "${o[out]}/items.jsonl"
	printf 'card\tkey\texercised\tcompared\toffered\nA\tk\t1\t0\t0\n' > "${o[out]}/exercised.tsv"
	printf '{"id":"i","status":"lower_ok"}\n{"id":"j","status":"refused"}\n' > "${o[out]}/lower.jsonl"
	;;
diff)
	echo "diff ${o[engine]}" >> "$LOG"
	mkdir -p "${o[out]}"
	printf '{"id":"i","class":"agree"}\n' > "${o[out]}/verdicts.jsonl"
	printf 'card\tkey\texercised\tcompared\toffered\nA\tk\t0\t1\t0\n' > "${o[out]}/compared.tsv"
	;;
cov)
	echo "cov" >> "$LOG"
	[ -z "${o[write]:-}" ] || printf 'card\tkey\texercised\tcompared\toffered\nA\tk\t1\t1\t0\n' > "${o[write]}"
	echo "cov report"
	;;
esac
STUB
cat > "$BIN/xmage" <<'STUB'
#!/usr/bin/env bash
echo "xmage" >> "$LOG"
[ "${FAIL_FOLLOWER:-}" != xmage ] || exit 1
cat "$1" > "$2"
STUB
cat > "$BIN/forge" <<'STUB'
#!/usr/bin/env bash
echo "forge" >> "$LOG"
[ "${FAIL_FOLLOWER:-}" != forge ] || exit 1
cat "$1" > "$2"
STUB
cat > "$BIN/oraclediff" <<'STUB'
#!/usr/bin/env bash
[ "$1" = forge-export ] || exit 1
echo "export" >> "$LOG"
cat "$3" > "$5"
STUB
cat > "$BIN/pgrep" <<'STUB'
#!/usr/bin/env bash
echo "${SMOKE_JVMS:-0}"
[ "${SMOKE_JVMS:-0}" != 0 ]
STUB
chmod +x "$BIN"/*

unset HEAVY_LOCK GORGE_BROKER_CLASS
sweep() { # root args...
	local root=$1
	shift
	XENGINE_ROOT=$root XENGINE_GO=$BIN/go XENGINE_SCOPE=none XENGINE_XMAGE_RUNNER=$BIN/xmage \
		XENGINE_FORGE_RUNNER=$BIN/forge XENGINE_ORACLEDIFF=$BIN/oraclediff XENGINE_PGREP=$BIN/pgrep \
		XENGINE_JVM_WAIT=2 XENGINE_JVM_POLL=1 "$SWEEP" --sets 'FDN BLB' --chunks 2 --games 3 "$@" 2>&1
}

R=$TMP/run1
# 1. Cold run: 2 sets x 2 chunks.
out=$(sweep "$R"); rc=$?
check "cold sweep exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "cold: 4 play stages" '[ "$(count play)" = 4 ]' "$(count play)"
check "cold: 4 xmage replays" '[ "$(count xmage)" = 4 ]'
check "cold: 4 forge replays (4 exports)" '[ "$(count forge)" = 4 ] && [ "$(count export)" = 4 ]'
check "cold: 8 diffs" '[ "$(count diff)" = 8 ]'
check "cold: 4 ledger merges" '[ "$(count cov)" -ge 4 ]'
n=0
for s in FDN BLB; do for c in 0 1; do [ -e "$R/$s/$c/.done" ] && n=$((n + 1)); done; done
check "cold: every chunk has .done" '[ "$n" = 4 ]' "done=$n"
check "cold: play got -windows" '/usr/bin/grep -q "windows=1" "$LOG"'
check "cold: first chunk plays without a ledger" '/usr/bin/grep -q "^play FDN 0 windows=1 ledger=none" "$LOG"' "$(cat "$LOG")"
check "cold: second chunk reads the ledger" '/usr/bin/grep -q "^play FDN 1 .*ledger-in.tsv" "$LOG"'
check "cold: ledger.tsv written, no temp left" '[ -s "$R/ledger.tsv" ] && [ -z "$(find "$R" -name "ledger.tsv.tmp*")" ]'
check "cold: SUMMARY.md has both sets and engines" \
	'/usr/bin/grep -q "^## BLB" "$R/SUMMARY.md" && /usr/bin/grep -q "xmage: class histogram" "$R/SUMMARY.md" && /usr/bin/grep -q "^agree 2" "$R/SUMMARY.md"' "$(cat "$R/SUMMARY.md" 2>/dev/null)"

# 2. Rerun: nothing to do.
: > "$LOG"
out=$(sweep "$R"); rc=$?
check "rerun exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "rerun does nothing" '[ ! -s "$LOG" ]' "$(cat "$LOG")"
check "rerun reports 0 chunks" 'has "$out" "0 chunk(s) to do"' "$out"

# 3. One .done removed: exactly that chunk is redone.
rm "$R/FDN/1/.done"
: > "$LOG"
out=$(sweep "$R"); rc=$?
check "redo exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "redo: exactly one play, of FDN chunk 1" '[ "$(count play)" = 1 ] && /usr/bin/grep -q "^play FDN 1 " "$LOG"' "$(cat "$LOG")"
check "redo: one xmage and one forge replay" '[ "$(count xmage)" = 1 ] && [ "$(count forge)" = 1 ]'
check "redo: stale directory kept, .done back" '[ -d "$R/FDN/1.stale.1" ] && [ -e "$R/FDN/1/.done" ]'

# 4. Two JVMs running: the follower stage waits, then times out.
R2=$TMP/run2
: > "$LOG"
out=$(SMOKE_JVMS=2 sweep "$R2" --sets FDN --chunks 1); rc=$?
check "2 JVMs: sweep exits non-zero" '[ "$rc" != 0 ]' "rc=$rc"
check "2 JVMs: waits, then says so" 'has "$out" "waiting for a slot" && has "$out" "timed out"' "$out"
check "2 JVMs: no follower started" '[ "$(count xmage)" = 0 ] && [ "$(count forge)" = 0 ]'
check "2 JVMs: no .done" '[ ! -e "$R2/FDN/0/.done" ]'

# 5. A root inside the repository is refused.
out=$(sweep "$ROOT/.xengine-smoke-root"); rc=$?
check "repo root refused" '[ "$rc" != 0 ] && has "$out" "inside the repository"' "rc=$rc $out"
check "repo root not created" '[ ! -e "$ROOT/.xengine-smoke-root" ]'

# 6. A failing follower leaves no .done.
R3=$TMP/run3
out=$(FAIL_FOLLOWER=forge sweep "$R3" --sets FDN --chunks 2); rc=$?
check "failing follower: sweep exits non-zero" '[ "$rc" != 0 ]' "rc=$rc"
check "failing follower: no .done, later chunk skipped" '[ ! -e "$R3/FDN/0/.done" ] && [ ! -d "$R3/FDN/1" ]' "$out"
check "failing follower: says which stage" 'has "$out" "FDN/0: forge replay FAILED"' "$out"

[ "$fails" = 0 ] && echo "xengine_sweep_smoke: all passed" || echo "xengine_sweep_smoke: $fails FAILED"
exit $((fails != 0))
