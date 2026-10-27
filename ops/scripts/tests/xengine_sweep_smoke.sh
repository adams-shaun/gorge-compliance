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
#   - the JVM count is taken again once the heavy lock is held;
#   - a failing follower, or a failing ledger publish, leaves no .done;
#   - XENGINE_GO_JOBS=0 is refused; XENGINE_GO_JOBS=2 plays each chunk index of
#     all sets before any follower starts, merges every chunk as a ledger delta
#     (cov -sub <chunk>/ledger-in.tsv), resumes like the serial run, and a
#     failing set does not stop the other.
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
	if [ -n "${o[write]:-}" ]; then echo "cov-write sub=${o[sub]:-none} ledger=${o[ledger]:-none}" >> "$LOG"; else echo "cov-report" >> "$LOG"; fi
	[ -z "${o[write]:-}" ] || printf 'card\tkey\texercised\tcompared\toffered\nA\tk\t1\t1\t0\n' > "${o[write]}"
	echo "cov report"
	;;
esac
STUB
cat > "$BIN/xmage" <<'STUB'
#!/usr/bin/env bash
echo "xmage" >> "$LOG"
if [ "${FAIL_FOLLOWER:-}" = xmage ] && { [ -z "${FAIL_SET:-}" ] || [[ $1 == */$FAIL_SET/* ]]; }; then exit 1; fi
cat "$1" > "$2"
STUB
cat > "$BIN/forge" <<'STUB'
#!/usr/bin/env bash
echo "forge" >> "$LOG"
if [ "${FAIL_FOLLOWER:-}" = forge ] && { [ -z "${FAIL_SET:-}" ] || [[ $1 == */$FAIL_SET/* ]]; }; then exit 1; fi
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
# SMOKE_PGREP_FREE_FIRST: the first count is 0 (the pre-lock check), later ones SMOKE_JVMS.
n=${SMOKE_JVMS:-0}
if [ -n "${SMOKE_PGREP_FREE_FIRST:-}" ] && [ ! -e "$SMOKE_PGREP_FREE_FIRST" ]; then : > "$SMOKE_PGREP_FREE_FIRST"; n=0; fi
echo "$n"
[ "$n" != 0 ]
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
check "cold: 4 ledger merges" '[ "$(count cov-write)" = 4 ]' "$(count cov-write)"
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

# 4b. The count is taken again once the heavy lock is held: free before the
# lock, 2 JVMs after it -> no follower starts.
R2b=$TMP/run2b
: > "$LOG"
out=$(SMOKE_JVMS=2 SMOKE_PGREP_FREE_FIRST=$TMP/pgrep-first sweep "$R2b" --sets FDN --chunks 1); rc=$?
check "JVM appears during the lock wait: sweep exits non-zero" '[ "$rc" != 0 ]' "rc=$rc"
check "JVM appears during the lock wait: no follower started" '[ "$(count xmage)" = 0 ] && [ "$(count forge)" = 0 ] && has "$out" "timed out"' "$out"

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

# 7. A ledger publish that fails leaves no .done (chunk runs under `if`, where
# set -e is off). A cp that fails only for the ledger temp file stands in.
FAILCP=$TMP/failcp
mkdir -p "$FAILCP"
cat > "$FAILCP/cp" <<'STUB'
#!/usr/bin/env bash
case "${*: -1}" in *ledger.tsv.tmp*) exit 1 ;; esac
exec /usr/bin/cp "$@"
STUB
chmod +x "$FAILCP/cp"
R4=$TMP/run4
out=$(PATH=$FAILCP:$PATH sweep "$R4" --sets FDN --chunks 2); rc=$?
check "ledger publish failure: sweep exits non-zero" '[ "$rc" != 0 ]' "rc=$rc"
check "ledger publish failure: no .done, later chunk skipped" '[ ! -e "$R4/FDN/0/.done" ] && [ ! -d "$R4/FDN/1" ] && [ ! -e "$R4/ledger.tsv" ]' "$out"
check "ledger publish failure: says so" 'has "$out" "FDN/0: ledger publish FAILED"' "$out"

# 8. XENGINE_GO_JOBS: 0 is refused; 2 runs a round (one chunk index of every
# set) of plays, then the chunks one by one, merging deltas.
out=$(XENGINE_GO_JOBS=0 sweep "$TMP/run5"); rc=$?
check "jobs=0: exits 2 with a message" '[ "$rc" = 2 ] && has "$out" "XENGINE_GO_JOBS=0" && [ ! -e "$TMP/run5" ]' "rc=$rc $out"
out=$(XENGINE_GO_JOBS=two sweep "$TMP/run5"); rc=$?
check "jobs=two: exits 2" '[ "$rc" = 2 ]' "rc=$rc $out"

R5=$TMP/run5
: > "$LOG"
out=$(XENGINE_GO_JOBS=2 sweep "$R5"); rc=$?
check "jobs=2 cold: exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "jobs=2 cold: 4 plays, 4 xmage, 4 forge, 8 diffs, 4 merges" \
	'[ "$(count play)" = 4 ] && [ "$(count xmage)" = 4 ] && [ "$(count forge)" = 4 ] && [ "$(count diff)" = 8 ] && [ "$(count cov-write)" = 4 ]' "$(cat "$LOG")"
n=0
for s in FDN BLB; do for c in 0 1; do [ -e "$R5/$s/$c/.done" ] && n=$((n + 1)); done; done
check "jobs=2 cold: every chunk has .done" '[ "$n" = 4 ]' "done=$n"
# Round barrier: both chunk-0 plays precede the first follower line, and both
# chunk-1 plays precede the first follower line after them.
first_follower=$(/usr/bin/grep -n -m1 -E '^(xmage|forge|export)' "$LOG" | cut -d: -f1)
last_play0=$(/usr/bin/grep -n -E '^play [A-Z]+ 0 ' "$LOG" | tail -1 | cut -d: -f1)
play1=$(/usr/bin/grep -n -E '^play [A-Z]+ 1 ' "$LOG" | cut -d: -f1 | head -1)
last_follower0=$(/usr/bin/grep -n -E '^(xmage|forge|export|diff)' "$LOG" | cut -d: -f1 | awk -v p="$play1" '$1 < p' | tail -1)
check "jobs=2: both round-0 plays before the first follower" '[ "$(/usr/bin/grep -c -E "^play [A-Z]+ 0 " "$LOG")" = 2 ] && [ "$last_play0" -lt "$first_follower" ]' "$(cat "$LOG")"
check "jobs=2: round-1 plays come after round 0's followers" '[ -n "$last_follower0" ] && [ "$play1" -gt "$last_follower0" ]' "$(cat "$LOG")"
check "jobs=2: round-1 plays read a ledger snapshot" '[ "$(/usr/bin/grep -c -E "^play [A-Z]+ 1 .*ledger-in.tsv" "$LOG")" = 2 ]' "$(cat "$LOG")"
check "jobs=2: round-0 merges have no -sub" '/usr/bin/grep -E "^cov-write sub=none" "$LOG" | wc -l | /usr/bin/grep -qx 2' "$(cat "$LOG")"
check "jobs=2: delta merge (-sub chunk ledger-in.tsv, -ledger root ledger.tsv)" \
	'/usr/bin/grep -E "^cov-write sub=$R5/[A-Z]+/1/ledger-in.tsv ledger= $R5/ledger.tsv " "$LOG" | wc -l | /usr/bin/grep -qx 2' "$(/usr/bin/grep '^cov-write' "$LOG")"
check "jobs=2: ledger.tsv written, no temp or snapshot left" '[ -s "$R5/ledger.tsv" ] && [ -z "$(find "$R5" -name "ledger.tsv.tmp*" -o -name "ledger-snap*")" ]'
check "jobs=2: SUMMARY.md has both sets" '/usr/bin/grep -q "^## BLB" "$R5/SUMMARY.md" && /usr/bin/grep -q "^## FDN" "$R5/SUMMARY.md"'

: > "$LOG"
out=$(XENGINE_GO_JOBS=2 sweep "$R5"); rc=$?
check "jobs=2 rerun: exits 0, does nothing" '[ "$rc" = 0 ] && [ ! -s "$LOG" ]' "rc=$rc $(cat "$LOG")"
rm "$R5/BLB/0/.done"
: > "$LOG"
out=$(XENGINE_GO_JOBS=2 sweep "$R5"); rc=$?
check "jobs=2 redo: exits 0, exactly BLB chunk 0" '[ "$rc" = 0 ] && [ "$(count play)" = 1 ] && /usr/bin/grep -q "^play BLB 0 " "$LOG" && [ -e "$R5/BLB/0/.done" ]' "rc=$rc $(cat "$LOG")"
check "jobs=2 redo: delta merge against the existing ledger" '/usr/bin/grep -q "^cov-write sub=$R5/BLB/0/ledger-in.tsv ledger= $R5/ledger.tsv " "$LOG"' "$(/usr/bin/grep '^cov-write' "$LOG")"

# A follower that fails for FDN only: FDN/0 gets no .done and FDN/1 is skipped;
# BLB finishes both chunks; exit 1.
R6=$TMP/run6
: > "$LOG"
out=$(FAIL_FOLLOWER=forge FAIL_SET=FDN XENGINE_GO_JOBS=2 sweep "$R6"); rc=$?
check "jobs=2 failing FDN follower: exits 1" '[ "$rc" = 1 ]' "rc=$rc $out"
check "jobs=2 failing FDN follower: FDN/0 no .done, FDN/1 skipped" '[ ! -e "$R6/FDN/0/.done" ] && [ ! -d "$R6/FDN/1" ] && has "$out" "FDN/0: forge replay FAILED" && has "$out" "FDN/1: skipped"' "$out"
check "jobs=2 failing FDN follower: BLB still finishes both chunks" '[ -e "$R6/BLB/0/.done" ] && [ -e "$R6/BLB/1/.done" ]' "$out"

[ "$fails" = 0 ] && echo "xengine_sweep_smoke: all passed" || echo "xengine_sweep_smoke: $fails FAILED"
exit $((fails != 0))
