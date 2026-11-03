#!/usr/bin/env bash
# xengine_random_smoke.sh: prove scripts/xengine-random.sh walks the chunks, keeps
# only the rows, gets past a chunk that keeps failing, and stops cleanly. The sweep is
# a stub on XENGINE_SWEEP: no Java, systemd, corpus or network.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/xengine-random-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() { if eval "$2"; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s\n' "$1"; fails=$((fails + 1)); fi; }

cat >"$TMP/sweep" <<'STUB'
#!/usr/bin/env bash
# args: --run R --sets RAND --chunks N --games K --seed S
c=$(($6 - 1))
d=$XENGINE_ROOT/RAND/$c
echo "$c $XENGINE_RANDOM_SETS $XENGINE_DIRAC $XENGINE_SCOPE ${XENGINE_DIRAC_EVERY:-}" >>"$XENGINE_ROOT/calls"
if [ "$c" = "${FAIL_CHUNK:-}" ]; then exit 1; fi
if [ "$c" = "${HANG_CHUNK:-}" ]; then exec sleep 60; fi
mkdir -p "$d/tapes" "$d/diff-forge/finds/diverge/x" "$d/diff-forge/finds/harness/y" "$d/adjudicate"
echo row >"$d/lower.jsonl"
echo row >"$d/gorge.jsonl"
echo row >"$d/dirac.jsonl"
: >"$d/.done"
echo summary >"$XENGINE_ROOT/SUMMARY.md"
STUB
chmod +x "$TMP/sweep"
export XENGINE_SWEEP=$TMP/sweep XENGINE_RANDOM_BACKOFF=0

r=$TMP/plain
XENGINE_ROOT=$r bash "$ROOT/scripts/xengine-random.sh" --run p --sets 'FDN BLB' --max-chunks 3 >/dev/null
check "three chunks in order" '[ "$(cut -d" " -f1 "$r/calls" | tr -d "\n")" = 012 ]'
check "the sets, dirac and no scopes reach the sweep" '[ "$(head -1 "$r/calls")" = "0 FDN BLB 1 none " ]'
check "rows stay" '[ -e "$r/RAND/0/lower.jsonl" ] && [ -e "$r/RAND/0/dirac.jsonl" ]'
check "tapes and follower rows go" '[ ! -e "$r/RAND/0/tapes" ] && [ ! -e "$r/RAND/0/gorge.jsonl" ]'
check "repros of diverged items stay, others go" '[ -e "$r/RAND/0/diff-forge/finds/diverge/x" ] && [ ! -e "$r/RAND/0/diff-forge/finds/harness" ]'
XENGINE_ROOT=$r bash "$ROOT/scripts/xengine-random.sh" --run p --sets 'FDN BLB' --max-chunks 4 >/dev/null
check "a rerun resumes after the done chunks" '[ "$(cut -d" " -f1 "$r/calls" | tr -d "\n")" = 0123 ]'

r=$TMP/every
XENGINE_ROOT=$r bash "$ROOT/scripts/xengine-random.sh" --run e --sets FDN --dirac-every 1 --max-chunks 1 >/dev/null
check "--dirac-every reaches the sweep" '[ "$(head -1 "$r/calls")" = "0 FDN 1 none 1" ]'

r=$TMP/fail
FAIL_CHUNK=1 XENGINE_ROOT=$r bash "$ROOT/scripts/xengine-random.sh" --run f --sets FDN --max-chunks 3 >/dev/null 2>&1
check "a failing chunk is tried 3 times, then passed" '[ "$(cut -d" " -f1 "$r/calls" | tr -d "\n")" = 01112 ]'
check "it is marked failed" '[ -e "$r/RAND/1/.failed" ]'
check "and named in the summary" '/usr/bin/grep -q "failed chunk: .*/RAND/1" "$r/SUMMARY.md"'

r=$TMP/stop
HANG_CHUNK=1 XENGINE_ROOT=$r bash "$ROOT/scripts/xengine-random.sh" --run s --sets FDN >"$TMP/stop.out" 2>&1 &
pid=$!
for _ in $(seq 100); do [ -e "$r/calls" ] && [ "$(wc -l <"$r/calls")" = 2 ] && break; sleep 0.1; done
sleep 0.2
kill -TERM "$pid"
wait "$pid"
check "TERM ends the loop with status 0" '[ $? = 0 ]'
check "it says where it stopped" '/usr/bin/grep -q "stopped at chunk 1" "$TMP/stop.out"'
check "the chunk it stopped in is not done" '[ ! -e "$r/RAND/1/.done" ]'

[ "$fails" = 0 ] && echo "xengine_random_smoke: all ok" || { echo "xengine_random_smoke: $fails failed"; exit 1; }
