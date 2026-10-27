#!/usr/bin/env bash
# xengine-sweep.sh [--run NAME] [--sets 'FDN BLB'] [--chunks N] [--games K] [--seed S] [--force]
#
# A chunked, resumable xengine sweep outside go test (DESIGN xengine section 8).
# Per set and chunk c (K games each):
#   1. xengine play  -- gorge plays K games, lowers them to <chunk>/items.jsonl
#                       (every go stage -- play, diff, cov, sweep-plan -- runs
#                       inside a systemd scope: 4G, 400% CPU, GOMAXPROCS=4)
#   2. each follower in XENGINE_FOLLOWERS replays items.jsonl under the heavy
#      lock (scripts/heavy-lock.sh), after waiting for a free JVM slot
#   3. xengine diff  -- classify the follower rows into <chunk>/diff-<f>
#   4. the coverage ledger <root>/ledger.tsv absorbs the chunk (atomically),
#      SUMMARY.md is regenerated, and <chunk>/.done is touched. A chunk is done
#      only when every stage exited 0.
#
# Resumable: xengine sweep-plan lists the chunks without .done; an unfinished
# chunk directory is moved to <dir>.stale.<n> and redone from stage 1.
# Chunks of a set run in order and each plays against the ledger as it stood
# when the previous chunk finished (kept as <chunk>/ledger-in.tsv). With
# XENGINE_GO_JOBS > 1 the plan is grouped into rounds by chunk index: every
# set's chunk c plays from ONE ledger snapshot taken at the start of the round
# (up to XENGINE_GO_JOBS plays at once), and only when all of them have played
# are the followers, diff and ledger merge run, chunk by chunk in set order. A crash
# between the ledger merge and .done re-merges that chunk's counts on the
# redo: counters inflate, "never compared" stays exact. --force redoes chunks
# that already have .done and re-merges them the same way: the ledger is never
# rolled back.
# A failed chunk stops its set (later chunks would read a stale ledger); other
# sets go on and the sweep exits 1.
#
# One follower JVM at a time, ever: before each follower stage the script
# counts running JVMs with `pgrep -c -x java` and waits (poll XENGINE_JVM_POLL,
# max XENGINE_JVM_WAIT seconds) while 2 or more run.
#
# Environment:
#   XENGINE_ROOT          output root (default /mnt/sata/gorge-training/xengine/<run>);
#                         refused inside the repository
#   XENGINE_FOLLOWERS     'forge xmage' (default)
#   XENGINE_GO_JOBS       concurrent play stages, a positive integer (default 1:
#                         strictly serial; see the ledger merge below)
#   XENGINE_WINDOWS       1 (default) passes -windows to play
#   XENGINE_JVM_WAIT      max seconds to wait for a JVM slot (default 21600)
#   XENGINE_JVM_POLL      poll interval in seconds (default 30)
#   XENGINE_FORGE_WAIT / XENGINE_FORGE_MEM, XENGINE_XMAGE_WAIT / XENGINE_XMAGE_MEM
#                         heavy-lock --wait / --mem per follower (20000 s / 12G)
#   Overrides for the smoke: XENGINE_GO (go command), XENGINE_XMAGE_RUNNER,
#   XENGINE_FORGE_RUNNER, XENGINE_ORACLEDIFF, XENGINE_PGREP, XENGINE_SCOPE=none
#   (no systemd-run).
#
# Ledger merge. play writes exercised.tsv = (its input snapshot L) + (this
# chunk's exercised counts E), and cov -ledger adds counters. A chunk is merged
# as a delta, in one call: cov -ledger <root>/ledger.tsv -ledger exercised.tsv
# -sub ledger-in.tsv -ledger compared.tsv..., i.e. current + (L + E) - L + C =
# current + E + C. current already holds the deltas of the chunks finished
# earlier in the round, so none of them is lost and L is not counted twice.
# Serially current == L and the result equals exercised + compared. A chunk
# that played before any ledger existed has no L: no -sub.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

run="" sets="FDN BLB" chunks=4 games=50 seed=1 force=0
while [ $# -gt 0 ]; do
	case "$1" in
	--run) run=${2:?--run needs a name}; shift 2 ;;
	--sets) sets=${2:?--sets needs a list}; shift 2 ;;
	--chunks) chunks=${2:?--chunks needs a number}; shift 2 ;;
	--games) games=${2:?--games needs a number}; shift 2 ;;
	--seed) seed=${2:?--seed needs a number}; shift 2 ;;
	--force) force=1; shift ;;
	*) echo "xengine-sweep: unknown argument $1" >&2; exit 2 ;;
	esac
done
[ -n "$run" ] || run=seed$seed
sets=${sets//,/ }

GO=${XENGINE_GO:-go}
PGREP=${XENGINE_PGREP:-/usr/bin/pgrep}
followers=${XENGINE_FOLLOWERS:-forge xmage}
jvm_wait=${XENGINE_JVM_WAIT:-21600}
jvm_poll=${XENGINE_JVM_POLL:-30}
root=${XENGINE_ROOT:-/mnt/sata/gorge-training/xengine/$run}

jobs=${XENGINE_GO_JOBS:-1}
case "$jobs" in
"" | *[!0-9]* | 0*) echo "xengine-sweep: XENGINE_GO_JOBS=$jobs: want a positive integer (no leading 0)" >&2; exit 2 ;;
esac
case "$chunks$games$seed" in *[!0-9]* | "") echo "xengine-sweep: --chunks, --games and --seed are non-negative integers" >&2; exit 2 ;; esac
command -v jq >/dev/null || { echo "xengine-sweep: jq is needed for SUMMARY.md" >&2; exit 2; }
for f in $followers; do
	case "$f" in forge | xmage) ;; *) echo "xengine-sweep: unknown follower $f (want forge or xmage)" >&2; exit 2 ;; esac
done

# Follower output stays off-repo.
absroot=$(realpath -m "$root")
case "$absroot/" in
"$repo"/*) echo "xengine-sweep: refusing XENGINE_ROOT $root: it is inside the repository $repo" >&2; exit 2 ;;
esac
mkdir -p "$absroot"
root=$absroot
ledger=$root/ledger.tsv

# capped CMD...: the box rule (4G, 400% CPU, GOMAXPROCS=4) for every go stage.
capped() {
	if [ "${XENGINE_SCOPE:-systemd}" = none ]; then
		env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	else
		systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	fi
}
xe() { capped "$GO" run -trimpath ./cmd/xengine "$@"; }

# Stages run in the background so the pid is ours to stop; the trap stops only
# pids this script started.
pids=()
stop_children() {
	local p
	for p in ${pids[@]+"${pids[@]}"}; do kill "$p" 2>/dev/null || true; done
}
trap 'stop_children; exit 130' INT TERM
run_stage() {
	"$@" &
	pids+=("$!")
	local rc=0
	wait "$!" || rc=$?
	return "$rc"
}

# exported: the locked command re-runs it (slot_exec) once the heavy lock is held.
jvm_slot() {
	local waited=0 n
	while :; do
		n=$("$PGREP" -c -x java || true)
		[ "${n:-0}" -lt 2 ] && return 0
		if [ "$waited" -ge "$jvm_wait" ]; then
			echo "xengine-sweep: timed out after ${waited}s waiting for a free JVM slot ($n java processes running, cap 2; XENGINE_JVM_WAIT=$jvm_wait)" >&2
			return 1
		fi
		echo "xengine-sweep: $n JVMs running; waiting for a slot (${waited}s of ${jvm_wait}s)"
		sleep "$jvm_poll"
		waited=$((waited + jvm_poll))
	done
}

export -f jvm_slot
export PGREP jvm_wait jvm_poll
# The lock wait can be hours, so the count taken before it is stale: the
# locked command (bash -c 'jvm_slot && exec RUNNER ...') counts again first.

oracledif() {
	if [ -n "${XENGINE_ORACLEDIFF:-}" ]; then
		"$XENGINE_ORACLEDIFF" "$@"
	else
		mkdir -p "$root/bin"
		"$GO" build -trimpath -o "$root/bin/oraclediff" ./cmd/oraclediff
		"$root/bin/oraclediff" "$@"
	fi
}

follower_rows() { # follower dir -> rows file
	local f=$1 d=$2 runner
	case "$f" in
	xmage)
		runner=${XENGINE_XMAGE_RUNNER:-$repo/scripts/xmage-oracle-run.sh}
		jvm_slot || return 1
		run_stage "$repo/scripts/heavy-lock.sh" --wait "${XENGINE_XMAGE_WAIT:-20000}" --mem "${XENGINE_XMAGE_MEM:-12G}" \
			--name xengine-xmage -- bash -c 'jvm_slot && exec "$@"' _ "$runner" "$d/items.jsonl" "$d/xmage.jsonl"
		;;
	forge)
		runner=${XENGINE_FORGE_RUNNER:-$repo/scripts/forge-oracle-run.sh}
		oracledif forge-export -scenarios "$d/items.jsonl" -out "$d/forge-req.jsonl" >"$d/forge-export.log" 2>&1 || return 1
		jvm_slot || return 1
		run_stage "$repo/scripts/heavy-lock.sh" --wait "${XENGINE_FORGE_WAIT:-20000}" --mem "${XENGINE_FORGE_MEM:-12G}" \
			--name xengine-forge -- bash -c 'jvm_slot && exec "$@"' _ "$runner" "$d/forge-req.jsonl" "$d/forge.jsonl"
		;;
	esac
}

snapshot_into() { # dir [snapshot-file]: the chunk's input ledger, if there is one
	local d=$1 src=${2:-$ledger}
	rm -f "$d/ledger-in.tsv"
	[ ! -s "$src" ] || cp "$src" "$d/ledger-in.tsv"
}

play_stage() { # set chunk dir (reads <dir>/ledger-in.tsv when snapshot_into made one)
	local s=$1 c=$2 d=$3 args
	args=(play -set "$s" -chunk "$c" -games "$games" -seed "$seed" -out "$d")
	[ "${XENGINE_WINDOWS:-1}" = 0 ] || args+=(-windows)
	[ ! -e "$d/ledger-in.tsv" ] || args+=(-ledger "$d/ledger-in.tsv")
	run_stage xe "${args[@]}"
}

# finish_chunk set chunk: followers, diff, the ledger merge and .done of a
# chunk that has played. Run serially: it reads and replaces <root>/ledger.tsv.
finish_chunk() {
	local s=$1 c=$2 d=$root/$1/$2 f
	local cargs=()
	[ ! -s "$ledger" ] || cargs+=(-ledger "$ledger")
	cargs+=(-ledger "$d/exercised.tsv")
	# -sub once from the merged total (see the header), not per -ledger.
	[ ! -s "$ledger" ] || [ ! -e "$d/ledger-in.tsv" ] || cargs+=(-sub "$d/ledger-in.tsv")
	for f in $followers; do
		echo "== $s chunk $c: $f"
		follower_rows "$f" "$d" >"$d/$f.log" 2>&1 || { echo "$s/$c: $f replay FAILED"; tail -3 "$d/$f.log"; return 1; }
		xe diff -play "$d" -engine "$f" -rows "$d/$f.jsonl" -out "$d/diff-$f" >"$d/diff-$f.log" 2>&1 \
			|| { echo "$s/$c: $f diff FAILED"; tail -3 "$d/diff-$f.log"; return 1; }
		cargs+=(-ledger "$d/diff-$f/compared.tsv")
	done
	xe cov "${cargs[@]}" -write "$d/ledger-out.tsv" >"$d/cov.log" 2>&1 || { echo "$s/$c: ledger merge FAILED"; tail -3 "$d/cov.log"; return 1; }
	# finish_chunk runs under `if`, where set -e is off: every step is checked by hand.
	{ cp "$d/ledger-out.tsv" "$ledger.tmp.$$" && mv "$ledger.tmp.$$" "$ledger"; } \
		|| { echo "$s/$c: ledger publish FAILED"; rm -f "$ledger.tmp.$$"; return 1; }
	: >"$d/.done" || { echo "$s/$c: writing .done FAILED"; return 1; }
}

play_failed() { # set chunk: report a failed play
	echo "$1/$2: play FAILED"
	tail -3 "$root/$1/$2/play.log"
}

chunk() { # set chunk (serial: play, then finish, against the ledger as it is now)
	local s=$1 c=$2 d=$root/$1/$2
	mkdir -p "$d"
	snapshot_into "$d"
	echo "== $s chunk $c: play"
	play_stage "$s" "$c" "$d" >"$d/play.log" 2>&1 || { play_failed "$s" "$c"; return 1; }
	finish_chunk "$s" "$c"
}

histogram() { # file jq-field dir... -> "value count" lines
	local file=$1 field=$2 d
	shift 2
	for d in "$@"; do [ ! -f "$d/$file" ] || jq -r "$field" "$d/$file"; done | LC_ALL=C sort | uniq -c | sed 's/^ *\([0-9]*\) \(.*\)/\2 \1/'
}

summary() {
	local tmp=$root/SUMMARY.md.tmp.$$ s f d
	{
		echo "# xengine sweep $run"
		echo
		echo "sets: $sets; followers: $followers; chunks per set: $chunks; games per chunk: $games; seed: $seed"
		for s in $sets; do
			echo
			echo "## $s"
			local done_dirs=()
			for d in "$root/$s"/*/; do
				d=${d%/}
				[[ $d =~ /[0-9]+$ ]] && [ -e "$d/.done" ] && done_dirs+=("$d")
			done
			echo
			echo "chunks done: ${#done_dirs[@]}"
			[ ${#done_dirs[@]} -gt 0 ] || continue
			echo
			echo "### lowering (windows lowered vs refused, by reason)"
			echo '```'
			histogram lower.jsonl .status "${done_dirs[@]}"
			echo '```'
			for f in $followers; do
				echo
				echo "### $f: class histogram"
				echo '```'
				histogram "diff-$f/verdicts.jsonl" .class "${done_dirs[@]}"
				echo '```'
			done
			if [ -s "$ledger" ]; then
				echo
				echo "### coverage"
				echo '```'
				xe cov -ledger "$ledger" -sets "$s" -missing 2>&1 || true
				echo '```'
			fi
		done
	} >"$tmp"
	mv "$tmp" "$root/SUMMARY.md"
}

planargs=(sweep-plan -root "$root" -sets "$sets" -chunks "$chunks" -stale)
[ "$force" = 0 ] || planargs+=(-force)
plan=$(xe "${planargs[@]}")
echo "== xengine sweep $run: root $root, $(printf '%s\n' "$plan" | /usr/bin/grep -c . || true) chunk(s) to do"

rc=0
failed=" "
if [ "$jobs" = 1 ]; then
	for sc in $plan; do
		s=${sc%/*} c=${sc#*/}
		case "$failed" in *" $s "*) echo "$s/$c: skipped (an earlier chunk of $s failed)"; continue ;; esac
		if chunk "$s" "$c"; then
			summary
			echo "$s/$c: done"
		else
			failed="$failed$s "
			rc=1
		fi
	done
	exit "$rc"
fi

# jobs > 1: one round per chunk index, in ascending order; within a round the
# sets keep plan order. All plays of a round read one snapshot of the ledger and
# finish before any follower starts; the chunks are then finished serially.
for c in $(printf '%s\n' $plan | sed 's|.*/||' | LC_ALL=C sort -un); do
	members=()
	for sc in $plan; do
		[ "${sc#*/}" = "$c" ] || continue
		s=${sc%/*}
		case "$failed" in *" $s "*) echo "$s/$c: skipped (an earlier chunk of $s failed)"; continue ;; esac
		members+=("$s")
	done
	[ ${#members[@]} -gt 0 ] || continue
	snap=$root/ledger-snap.$c.$$.tsv
	if [ -s "$ledger" ]; then cp "$ledger" "$snap" || { echo "round $c: ledger snapshot FAILED"; exit 1; }; else rm -f "$snap"; fi
	queue=() # pids in launch order, oldest first
	declare -A play_pid=() play_rc=()
	for s in "${members[@]}"; do
		d=$root/$s/$c
		mkdir -p "$d"
		snapshot_into "$d" "$snap"
		while [ ${#queue[@]} -ge "$jobs" ]; do
			wait "${queue[0]}" || play_rc[${queue[0]}]=$?
			queue=("${queue[@]:1}")
		done
		echo "== $s chunk $c: play"
		(
			pids=()
			trap 'stop_children; exit 130' INT TERM
			play_stage "$s" "$c" "$d" >"$d/play.log" 2>&1
		) &
		play_pid[$s]=$!
		pids+=("$!")
		queue+=("$!")
	done
	for p in ${queue[@]+"${queue[@]}"}; do wait "$p" || play_rc[$p]=$?; done
	rm -f "$snap"
	for s in "${members[@]}"; do
		if [ "${play_rc[${play_pid[$s]}]:-0}" != 0 ]; then
			play_failed "$s" "$c"
			failed="$failed$s "
			rc=1
		elif finish_chunk "$s" "$c"; then
			summary
			echo "$s/$c: done"
		else
			failed="$failed$s "
			rc=1
		fi
	done
	unset play_pid play_rc
done
exit "$rc"
