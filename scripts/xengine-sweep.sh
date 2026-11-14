#!/usr/bin/env bash
# xengine-sweep.sh [--run NAME] [--sets 'FDN BLB'] [--chunks N] [--games K] [--seed S] [--force]
#
# A chunked, resumable xengine sweep outside go test.
# Per set and chunk c (K games each):
#   1. xengine play  -- gorge plays K games, lowers them to <chunk>/items.jsonl
#                       (every go stage -- play, diff, cov, sweep-plan, followers -- runs
#                       inside a systemd scope: 4G, 400% CPU, GOMAXPROCS=4)
#   2. each follower in XENGINE_FOLLOWERS (a name `xengine followers` lists:
#      the registry, oracle/xengine/follower) has its items prepared by its
#      prep stage, then its runner replays them under the heavy lock
#      (scripts/heavy-lock.sh) into <chunk>/<f>.jsonl; a JVM follower first
#      waits for a free JVM slot. The prep stages:
#        xprep            (xmage) `xengine xprep` fills the driver's answers and
#                         ability prefixes into <chunk>/xitems.jsonl (the
#                         refused items, with their reasons, go to
#                         <chunk>/xprep.jsonl and have no row), and play was
#                         given -xmage so no deck holds a card XMage spells
#                         differently
#        forge-export     (forge) `oraclediff forge-export` writes
#                         <chunk>/<f>-req.jsonl (log <chunk>/<f>-export.log)
#        follower-export  `oraclediff follower-export -engine <f>` writes
#                         <chunk>/<f>-req.jsonl and its refusals to
#                         <chunk>/<f>-prep.jsonl (log <chunk>/<f>-export.log)
#   3. xengine diff  -- classify the follower rows into <chunk>/diff-<f>
#      with both followers, xengine adjudicate -- the three-way pattern of each
#                       item either diverged on (<chunk>/adjudicate/, ticket drafts
#                       under tickets/; nothing is ruled or committed); with phase
#                       among the followers too, the four-way table beside it
#                       (<chunk>/adjudicate/fourway.jsonl, patterns4.tsv)
#                       With XENGINE_DIRAC=1 play first plays each game of a set
#                       whose decks a dirac table holds in lockstep with gorge
#                       (<chunk>/dirac.jsonl), and adjudicate writes dirac's answer
#                       beside the three-way outcome of each of its items
#                       (<chunk>/adjudicate/dirac.jsonl)
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
# One follower JVM at a time, ever: before each JVM follower's stage (jvm
# true in `xengine followers`) the script counts running JVMs with
# `pgrep -c -x java` and waits (poll XENGINE_JVM_POLL, max XENGINE_JVM_WAIT
# seconds) while 2 or more run. A follower without a JVM takes no slot: it
# runs under the heavy lock alone, with its own XENGINE_<F>_MEM.
#
# Environment:
#   XENGINE_ROOT          output root (default .oracle-build/xengine/<run>); any
#                         other directory inside the repository is refused, because
#                         follower output must never be committed
#   XENGINE_FOLLOWERS     'forge xmage' (default): followers, in run order; each
#                         must be a name `xengine followers` lists
#   XENGINE_GO_JOBS       concurrent play stages, a positive integer (default 1:
#                         strictly serial; see the ledger merge below)
#   XENGINE_WINDOWS       1 (default) passes -windows to play
#   XENGINE_TAPE_TURNS    comma-separated turn counts passed to play as -tape-turns
#                         (default none): the prefixes `xengine calib` reads
#   XENGINE_XMAGE_TAPES   1 passes -tapes to xprep, so tape items go to XMage too;
#                         0 (default) keeps XMage window-only. The
#                         calibration re-runs use 1.
#   XENGINE_DIRAC         1 passes -dirac to play: dirac plays beside gorge, and the
#                         decks hold only cards its table plays. It needs dirac's
#                         tables for the pin; play's error names the command.
#   XENGINE_RANDOM_SETS   set codes passed to play as -random: the decks are drawn
#                         at random across them, ignoring the ledger, and --sets
#                         only names the run (scripts/xengine-random.sh)
#   XENGINE_DIRAC_EVERY   with XENGINE_RANDOM_SETS and XENGINE_DIRAC, passed to play as
#                         -dirac-every: every K-th game is one a dirac table plays (default 2;
#                         1 gives dirac a lockstep game on every deck)
#   XENGINE_FORGE_ONLY    with XENGINE_RANDOM_SETS, a file of card names, one per line,
#                         added to the cross-set deal: a set XMage has no class for.
#                         An item holding one is not sent to XMage (xmage_lacks_card)
#   XENGINE_COMMANDER_DECKS  a directory of Commander deck files passed to play as -format commander
#                         -commander-decks: each game is a 1v1 Commander match between two of them
#                         (gorge's testkit/testutil/decks holds 15), --sets only names the run. Not
#                         with XENGINE_RANDOM_SETS, XENGINE_FORGE_ONLY, XENGINE_POOL,
#                         XENGINE_POOL_FILTER (play refuses). XMage refuses every
#                         Commander item (xmage_commander): run with XENGINE_FOLLOWERS=forge.
#                         With XENGINE_DIRAC=1 each game is offered to dirac's cmd5 Commander table
#                         (a pair it holds gets a lockstep row, any other reads not_applicable);
#                         XENGINE_DIRAC_EVERY does not apply.
#   XENGINE_SEATS         with XENGINE_COMMANDER_DECKS, passed to play as -seats (2..4, default 2): each
#                         game deals that many distinct decks. More than two seats are played by gorge
#                         alone: every window and tape lowers to `refused` (seats:<n>), so no follower
#                         sees it. play refuses it without XENGINE_COMMANDER_DECKS.
#   XENGINE_POOL          a card-name file passed to play as -pool: the decks are drawn
#                         from it alone (oracle/xengine/pools/), against the ledger as a
#                         set's are, and --sets only names the run. Not with
#                         XENGINE_RANDOM_SETS, XENGINE_FORGE_ONLY, XENGINE_COMMANDER_DECKS or
#                         XENGINE_DIRAC (play refuses)
#   XENGINE_POOL_FILTER   with XENGINE_POOL, a file of names left out of the pool
#                         (play -pool-filter); <chunk>/pool.json records both and their sha256
#   XENGINE_DECKS         a directory of gorge deck files (*.json) passed to play as -decks:
#                         every game plays two different fixed decks of it (main deck
#                         only; real 60-card lists), and --sets only names the run. Not
#                         with XENGINE_POOL, XENGINE_RANDOM_SETS, XENGINE_FORGE_ONLY or
#                         XENGINE_DIRAC (play refuses); <chunk>/decks.json records each
#                         deck, its sha256 and whether it was playable or why not
#   XENGINE_JVM_WAIT      max seconds to wait for a JVM slot (default 21600)
#   XENGINE_JVM_POLL      poll interval in seconds (default 30)
#   XENGINE_<F>_WAIT / XENGINE_<F>_MEM
#                         heavy-lock --wait / --mem for follower <f> (20000 s / 12G),
#                         <F> its name upper-cased: XENGINE_FORGE_WAIT,
#                         XENGINE_XMAGE_MEM, ... (`xengine followers` names them)
#   XENGINE_<F>_RUNNER    the runner for follower <f> instead of the registry's
#                         script (XENGINE_XMAGE_RUNNER, XENGINE_FORGE_RUNNER)
#   Overrides for the smoke: XENGINE_GO (go command), XENGINE_<F>_RUNNER,
#   XENGINE_ORACLEDIFF, XENGINE_PGREP. XENGINE_SCOPE=none
#   starts no scope, for the go stages or the JVMs, so that all of them stay in
#   the cgroup of a unit that caps the whole run.
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
root=${XENGINE_ROOT:-$repo/.oracle-build/xengine/$run}

jobs=${XENGINE_GO_JOBS:-1}
case "$jobs" in
"" | *[!0-9]* | 0*) echo "xengine-sweep: XENGINE_GO_JOBS=$jobs: want a positive integer (no leading 0)" >&2; exit 2 ;;
esac
case "$chunks$games$seed" in *[!0-9]* | "") echo "xengine-sweep: --chunks, --games and --seed are non-negative integers" >&2; exit 2 ;; esac
command -v jq >/dev/null || { echo "xengine-sweep: jq is needed for SUMMARY.md" >&2; exit 2; }

# capped CMD...: the box rule (4G, 400% CPU, GOMAXPROCS=4) for every go stage.
capped() {
	if [ "${XENGINE_SCOPE:-systemd}" = none ]; then
		env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	else
		systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	fi
}
xe() { capped "$GO" run -trimpath ./cmd/xengine "$@"; }

# The follower registry (oracle/xengine/follower), one row per follower; see
# cmd/xengine/followers.go for the columns.
f_names=()
declare -A f_runner=() f_prep=() f_jvm=() f_runner_env=() f_mem_env=() f_wait_env=()
registry=$(xe followers) || { echo "xengine-sweep: xengine followers FAILED" >&2; exit 2; }
while IFS=$'\t' read -r name runner prep jvm runner_env mem_env wait_env _; do
	case "$name" in "" | "#"*) continue ;; esac
	f_names+=("$name")
	f_runner[$name]=$runner f_prep[$name]=$prep f_jvm[$name]=$jvm
	f_runner_env[$name]=$runner_env f_mem_env[$name]=$mem_env f_wait_env[$name]=$wait_env
done <<<"$registry"
for f in $followers; do
	[ -n "${f_prep[$f]+set}" ] || { echo "xengine-sweep: unknown follower $f (want one of: ${f_names[*]})" >&2; exit 2; }
done

# Follower output (Forge's is GPL-3.0) can never be one `git add` from a commit.
absroot=$(realpath -m "$root")
case "$absroot/" in
"$repo"/.oracle-build/*) ;;
"$repo"/*) echo "xengine-sweep: refusing XENGINE_ROOT $root: it is inside the repository $repo and not under .oracle-build/" >&2; exit 2 ;;
esac
mkdir -p "$absroot"
root=$absroot
ledger=$root/ledger.tsv

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

# follower_rows follower dir: the follower's prep stage, then its runner,
# writing <dir>/<follower>.jsonl. The registry row says which prep, which
# runner (XENGINE_<F>_RUNNER overrides it), and whether it holds a JVM slot.
follower_rows() {
	local f=$1 d=$2 in
	local runner_env=${f_runner_env[$f]} mem_env=${f_mem_env[$f]} wait_env=${f_wait_env[$f]}
	local runner=${!runner_env:-$repo/${f_runner[$f]}}
	local lock=("$repo/scripts/heavy-lock.sh" --wait "${!wait_env:-20000}" --mem "${!mem_env:-12G}" --name "xengine-$f" --)
	case "${f_prep[$f]}" in
	xprep)
		# xprep writes the fixed xitems.jsonl / xprep.jsonl that xengine diff
		# reads: one xprep follower (xmage) per chunk.
		local xargs=()
		[ "${XENGINE_XMAGE_TAPES:-0}" != 1 ] || xargs+=(-tapes)
		xe xprep ${xargs[@]+"${xargs[@]}"} -play "$d" -out "$d/xitems.jsonl" || return 1
		in=$d/xitems.jsonl
		;;
	forge-export)
		oracledif forge-export -scenarios "$d/items.jsonl" -out "$d/$f-req.jsonl" >"$d/$f-export.log" 2>&1 || return 1
		in=$d/$f-req.jsonl
		;;
	follower-export)
		# H3: oraclediff follower-export also writes the follower's refusals to
		# <dir>/<f>-prep.jsonl, which xengine diff reads as not_sent. Its
		# --preflight goes to the same runner as the replay (XENGINE_<F>_RUNNER
		# when set), not the registry's.
		oracledif follower-export -engine "$f" -scenarios "$d/items.jsonl" -out "$d/$f-req.jsonl" -runner "$runner" >"$d/$f-export.log" 2>&1 || return 1
		in=$d/$f-req.jsonl
		;;
	*)
		echo "xengine-sweep: follower $f: unknown prep ${f_prep[$f]}" >&2
		return 1
		;;
	esac
	# A prep that sent the follower nothing (every RAND tape item is
	# xmage_window_only, so XMage gets none) leaves no replay to run: an empty
	# rows file is what the runner writes for zero items, without its JVM
	# start-up, JVM slot or (XMAGE_ORACLE_PRIVATE_DB) ~300 MB card-DB copy.
	if [ ! -s "$in" ]; then
		echo "xengine-sweep: $f: no items to replay; runner skipped"
		: >"$d/$f.jsonl"
		return 0
	fi
	if [ "${f_jvm[$f]}" = true ]; then
		jvm_slot || return 1
		run_stage "${lock[@]}" bash -c 'jvm_slot && exec "$@"' _ "$runner" "$in" "$d/$f.jsonl"
	else
		# No JVM slot. TODO: a CPU quota for non-JVM followers; heavy-lock.sh
		# takes none.
		run_stage "${lock[@]}" "$runner" "$in" "$d/$f.jsonl"
	fi
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
	[ -z "${XENGINE_TAPE_TURNS:-}" ] || args+=(-tape-turns "$XENGINE_TAPE_TURNS")
	case " $followers " in *" xmage "*) args+=(-xmage) ;; esac
	[ "${XENGINE_DIRAC:-0}" != 1 ] || args+=(-dirac)
	[ -z "${XENGINE_RANDOM_SETS:-}" ] || args+=(-random "$XENGINE_RANDOM_SETS")
	[ -z "${XENGINE_DIRAC_EVERY:-}" ] || args+=(-dirac-every "$XENGINE_DIRAC_EVERY")
	[ -z "${XENGINE_FORGE_ONLY:-}" ] || args+=(-forge-only "$XENGINE_FORGE_ONLY")
	[ -z "${XENGINE_COMMANDER_DECKS:-}" ] || args+=(-format commander -commander-decks "$XENGINE_COMMANDER_DECKS")
	[ -z "${XENGINE_SEATS:-}" ] || args+=(-seats "$XENGINE_SEATS")
	[ -z "${XENGINE_POOL:-}" ] || args+=(-pool "$XENGINE_POOL")
	[ -z "${XENGINE_POOL_FILTER:-}" ] || args+=(-pool-filter "$XENGINE_POOL_FILTER")
	[ -z "${XENGINE_DECKS:-}" ] || args+=(-decks "$XENGINE_DECKS")
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
	# Both followers' diffs are needed (the patterns compare them with each other).
	# phase.rs, when it followed too, is the fourth column (adjudicate/fourway.jsonl).
	if [[ " $followers " == *" forge "* && " $followers " == *" xmage "* ]]; then
		local pargs=()
		[[ " $followers " != *" phase "* ]] || pargs=(-phase "$d/diff-phase")
		xe adjudicate -play "$d" -xmage "$d/diff-xmage" -forge "$d/diff-forge" ${pargs[@]+"${pargs[@]}"} -out "$d/adjudicate" >"$d/adjudicate.log" 2>&1 \
			|| { echo "$s/$c: adjudicate FAILED"; tail -3 "$d/adjudicate.log"; return 1; }
	fi
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

joint_tuples() { # dir... -> one "class1 class2 ..." line per item present in every follower's verdicts (ids are chunk-scoped, so joined per chunk)
	local d f ok
	for d in "$@"; do
		ok=1
		for f in $followers; do [ -f "$d/diff-$f/verdicts.jsonl" ] || ok=0; done
		[ "$ok" = 1 ] || continue
		for f in $followers; do jq -c --arg f "$f" '{f: $f, id: .id, c: .class}' "$d/diff-$f/verdicts.jsonl"; done |
			jq -rs --args '. as $r
				| (reduce $ARGS.positional[] as $f ([]; . + [$r | map(select(.f == $f) | {(.id): .c}) | add // {}])) as $m
				| ($m[0] | keys[]) as $id
				| select([$m[] | has($id)] | all)
				| [$m[] | .[$id]] | join(" ")' $followers
	done
}

summary() {
	local tmp=$root/SUMMARY.md.tmp.$$ s f d
	{
		echo "# xengine sweep $run"
		echo
		echo "sets: $sets; followers: $followers; chunks per set: $chunks; games per chunk: $games; seed: $seed"
		[ -z "${XENGINE_RANDOM_SETS:-}" ] || echo "decks drawn across: $XENGINE_RANDOM_SETS"
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
			if [ "$(wc -w <<<"$followers")" -ge 2 ]; then
				echo
				echo "### joint: class tuple per item ($followers)"
				echo '```'
				joint_tuples "${done_dirs[@]}" | LC_ALL=C sort | uniq -c | sed 's/^ *\([0-9]*\) \(.*\)/\2 \1/'
				echo '```'
			fi
			if [ -n "$(ls "${done_dirs[@]/%//adjudicate/findings.jsonl}" 2>/dev/null)" ]; then
				echo
				echo "### three-way: pattern histogram"
				echo '```'
				histogram adjudicate/findings.jsonl .pattern "${done_dirs[@]}"
				echo '```'
				echo
				echo "### three-way: gorge suspects (FX|G) by card"
				echo '```'
				for d in "${done_dirs[@]}"; do
					[ ! -f "$d/adjudicate/findings.jsonl" ] || jq -r 'select(.pattern == "FX|G") | "\(.card // "?") \(.gx_field)"' "$d/adjudicate/findings.jsonl"
				done | LC_ALL=C sort | uniq -c
				echo '```'
			fi
			if [ "${XENGINE_DIRAC:-0}" = 1 ]; then
				echo
				echo "### dirac: games"
				echo '```'
				histogram dirac.jsonl .status "${done_dirs[@]}"
				echo '```'
				echo
				echo "### four-way: three-way pattern and dirac's answer"
				echo '```'
				histogram adjudicate/dirac.jsonl .four "${done_dirs[@]}"
				echo '```'
				echo
				echo "### dirac: games it stopped or differed in"
				echo '```'
				for d in "${done_dirs[@]}"; do
					[ ! -f "$d/dirac.jsonl" ] || jq -r 'select(.status | IN("diverged", "unmodelled", "hang")) | "\(.game) \(.status) turn \(.from_turn // "?") \(.what // "") \(.field // .reason // "") \(.implicates // [] | join(", "))"' "$d/dirac.jsonl"
				done
				echo '```'
			fi
			if [ -s "$ledger" ] && [ -z "${XENGINE_RANDOM_SETS:-}" ]; then
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
