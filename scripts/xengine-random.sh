#!/usr/bin/env bash
# xengine-random.sh [--run NAME] [--sets 'FDN BLB ...'] [--forge-only FILE] [--games K] [--seed S] [--max-chunks N]
#
# Plays seeded games from randomly drawn cards, one xengine-sweep chunk after
# another, until stopped. Every chunk is a sweep chunk of the pseudo-set RAND:
# even games draw from all the sets' supported cards together, odd games from one
# set's cards its dirac table plays, so dirac answers beside gorge in those (no
# table holds cards of two sets). --sets defaults to the Standard sets
# declared in oracle/compliance/declared.json. --forge-only names a file of card names,
# one per line, added to the cross-set draw: a set XMage has no class for, whose
# items Forge alone replays (xprep's xmage_lacks_card). scripts/set-census.sh in gorge
# shows how to read an edition's names at a Forge commit.
#
# Meant for a systemd user unit that caps the whole tree (see docs/oracles.md): the
# stages start no scopes of their own (XENGINE_SCOPE=none), and stopping the unit
# stops everything in it. A stopped run resumes at its first unfinished chunk, so
# it is the same games at the same pin, --seed and --games. Only the rows are kept:
# once a chunk is done its tapes, follower rows and repro directories go, except
# the repros of items that diverged. xengine-sweep's --force on the chunk rebuilds
# them.
#
# A chunk that fails is retried after XENGINE_RANDOM_BACKOFF seconds (default 300).
# After 3 failures it is marked .failed and .done, so that one bad game does not end
# a night's run, and listed at the top of SUMMARY.md.
#
# Output is under XENGINE_ROOT (default .oracle-build/xengine/<run>).
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

run=random seed=1 games=20 max=0 sets=""
while [ $# -gt 0 ]; do
	case "$1" in
	--run) run=${2:?--run needs a name}; shift 2 ;;
	--sets) sets=${2:?--sets needs a list}; shift 2 ;;
	--forge-only) export XENGINE_FORGE_ONLY=$(realpath "${2:?--forge-only needs a file}"); shift 2 ;;
	--games) games=${2:?--games needs a number}; shift 2 ;;
	--seed) seed=${2:?--seed needs a number}; shift 2 ;;
	--max-chunks) max=${2:?--max-chunks needs a number}; shift 2 ;;
	*) echo "xengine-random: unknown argument $1" >&2; exit 2 ;;
	esac
done
[ -n "$sets" ] || sets=$(jq -r 'keys | join(" ")' oracle/compliance/declared.json)

export XENGINE_ROOT=${XENGINE_ROOT:-$repo/.oracle-build/xengine/$run}
export XENGINE_RANDOM_SETS=$sets
export XENGINE_DIRAC=${XENGINE_DIRAC:-1}
export XENGINE_SCOPE=${XENGINE_SCOPE:-none}
export XMAGE_ORACLE_HEAP=${XMAGE_ORACLE_HEAP:-5g}
sweep=${XENGINE_SWEEP:-$repo/scripts/xengine-sweep.sh}
backoff=${XENGINE_RANDOM_BACKOFF:-300}
root=$XENGINE_ROOT/RAND
mkdir -p "$root"

child=""
stopping=0
trap 'stopping=1; [ -z "$child" ] || kill "$child" 2>/dev/null || true' INT TERM

# prune DIR: a done chunk down to its rows, the verdicts and the repros that matter.
prune() {
	local d=$1 f
	rm -rf "$d/tapes"
	rm -f "$d"/{gorge,items,xitems,forge-req,forge,xmage}.jsonl "$d"/*.jsonl.log
	for f in "$d"/diff-*/finds/*/; do
		[ "$(basename "$f")" = diverge ] || rm -rf "$f"
	done
}

failed_note() {
	local s=$XENGINE_ROOT/SUMMARY.md f
	[ -f "$s" ] || return 0
	for f in "$root"/*/.failed; do
		[ -e "$f" ] || continue
		printf 'failed chunk: %s\n' "$(dirname "$f")"
	done | cat - "$s" >"$s.tmp" && mv "$s.tmp" "$s"
}

c=0
fails=0
while [ "$stopping" = 0 ] && { [ "$max" = 0 ] || [ "$c" -lt "$max" ]; }; do
	d=$root/$c
	if [ -e "$d/.done" ]; then
		c=$((c + 1))
		continue
	fi
	rc=0
	"$sweep" --run "$run" --sets RAND --chunks $((c + 1)) --games "$games" --seed "$seed" &
	child=$!
	wait "$child" || rc=$?
	child=""
	[ "$stopping" = 0 ] || break
	if [ "$rc" = 0 ] && [ -e "$d/.done" ]; then
		prune "$d"
		rm -rf "$root"/*.stale.*
		failed_note
		fails=0
		c=$((c + 1))
		continue
	fi
	fails=$((fails + 1))
	echo "xengine-random: chunk $c failed ($fails of 3)" >&2
	if [ "$fails" -ge 3 ]; then
		mkdir -p "$d"
		: >"$d/.failed"
		: >"$d/.done"
		fails=0
		c=$((c + 1))
		continue
	fi
	sleep "$backoff" &
	child=$!
	wait "$child" || true
	child=""
done
echo "xengine-random: stopped at chunk $c"
