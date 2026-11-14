#!/usr/bin/env bash
# follower-sweep-smoke.sh ENGINE|'ENGINE ENGINE...' [--games 2] [--pool oracle/xengine/pools/fdn-commons.txt]
#                         [--run NAME] [--seed 1] [--max-class CLASS=PCT]...
#
# One small xengine chunk with one follower (or several, as one quoted
# space-separated word: 'phase manabrew'), the deck pool narrowed by the
# followers' own preflights, and the plumbing checked. The acceptance command for
# a new follower: the real test is the xengine sweep, but a seat may only run a
# smoke-sized one.
#
#   1. Each ENGINE must be a row of `xengine followers` (else exit 2).
#   2. The pool's names (comments and blanks removed, sorted, unique) go to
#      <root>/pool-names.txt; `RUNNER --preflight <root>/pool-names.txt` lists the
#      cards the follower refuses (the field before the tab of each line), written to
#      <root>/pool-filter.txt. RUNNER is $XENGINE_<F>_RUNNER or the registry's
#      runner. A non-zero preflight exit fails the script (exit 1). Without the
#      filter every item of a game holding a refused card comes back not_sent.
#      With several engines each runs its own preflight (preflight-<e>.out/.err)
#      and the filter is the sorted-unique union of their refusals: every
#      follower must accept every card of every deck.
#   3. scripts/xengine-sweep.sh runs one chunk (--sets FDN names the run; the
#      decks come from XENGINE_POOL) with XENGINE_POOL_FILTER set.
#   4. Checks on <root>/FDN/0: .done exists; per engine <ENGINE>.jsonl has as many
#      lines as <ENGINE>-req.jsonl; diff-<ENGINE>/verdicts.jsonl exists (exit 1 if
#      not). Printed per engine: the class histogram, the not_sent count of <ENGINE>-prep.jsonl,
#      and the tape vs window item counts. A class such as harness or
#      decision_shape is reported, never failed: a young follower has many.
#   5. --max-class CLASS=PCT (repeatable): exit 1 when CLASS is above PCT percent
#      (per engine) of the sent items (verdict rows whose class is not not_sent).
#
# --games above 10 is refused (exit 2): bigger sweeps are the orchestrator's, run
# after merge. The root (default .oracle-build/xengine/smoke-ENGINE, engines joined by -, or
# $XENGINE_ROOT) must be under .oracle-build/ or outside the repository.
#
# Exit: 0 ok; 1 a check, the preflight, the sweep or a --max-class failed; 2 usage.
# Overrides for the smoke: XENGINE_GO, XENGINE_SCOPE=none (as in xengine-sweep.sh),
# FOLLOWER_SWEEP_SCRIPT (a stand-in for scripts/xengine-sweep.sh).
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

die() { echo "follower-sweep-smoke: $*" >&2; exit 2; }

engine="" games=2 pool=oracle/xengine/pools/fdn-commons.txt run="" seed=1
maxc=()
while [ $# -gt 0 ]; do
	case "$1" in
	--games) games=${2:?--games needs a number}; shift 2 ;;
	--pool) pool=${2:?--pool needs a file}; shift 2 ;;
	--run) run=${2:?--run needs a name}; shift 2 ;;
	--seed) seed=${2:?--seed needs a number}; shift 2 ;;
	--max-class) maxc+=("${2:?--max-class needs CLASS=PCT}"); shift 2 ;;
	-*) die "unknown argument $1" ;;
	*) [ -z "$engine" ] || die "unexpected argument $1"; engine=$1; shift ;;
	esac
done
[ -n "$engine" ] || die "usage: follower-sweep-smoke.sh ENGINE|'ENGINE ENGINE...' [--games 2] [--pool FILE] [--run NAME] [--seed 1] [--max-class CLASS=PCT]..."
case "$games" in "" | *[!0-9]*) die "--games wants a positive integer" ;; esac
case "$seed" in "" | *[!0-9]*) die "--seed wants a non-negative integer" ;; esac
[ "$games" -ge 1 ] || die "--games wants a positive integer"
[ "$games" -le 10 ] || die "--games $games: a smoke is at most 10 games; bigger sweeps are the orchestrator's, run after merge"
for m in ${maxc[@]+"${maxc[@]}"}; do
	case "$m" in
	?*=*) ;;
	*) die "--max-class $m: want CLASS=PCT" ;;
	esac
	case "${m#*=}" in "" | *[!0-9.]* | .) die "--max-class $m: PCT is a number" ;; esac
done
[ -f "$pool" ] || die "--pool $pool: no such file"
command -v jq >/dev/null || die "jq is needed"
read -r -a engs <<<"$engine"
[ ${#engs[@]} -gt 0 ] || die "usage: follower-sweep-smoke.sh ENGINE|'ENGINE ENGINE...' (no engine given)"
engines="${engs[*]}"
for e in "${engs[@]}"; do
	case "$e" in */* | .*) die "ENGINE $e is not a follower name" ;; esac
done
joined=$(IFS=-; echo "${engs[*]}")
[ -n "$run" ] || run=smoke-$joined

GO=${XENGINE_GO:-go}
capped() {
	if [ "${XENGINE_SCOPE:-systemd}" = none ]; then
		env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	else
		systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	fi
}
xe() { capped "$GO" run -trimpath ./cmd/xengine "$@"; }

# 1. the follower registry; see cmd/xengine/followers.go for the columns.
registry=$(xe followers) || die "xengine followers FAILED"
names=""
while IFS=$'\t' read -r name _; do
	case "$name" in "" | "#"*) continue ;; esac
	names+=" $name"
done <<<"$registry"
runners=() # parallel to engs
for e in "${engs[@]}"; do
	found=0
	while IFS=$'\t' read -r name f_runner _ _ f_runner_env _; do
		case "$name" in "" | "#"*) continue ;; esac
		if [ "$name" = "$e" ]; then found=1; runner_rel=$f_runner runner_env=$f_runner_env; fi
	done <<<"$registry"
	[ "$found" = 1 ] || die "unknown follower $e (want one of:$names)"
	runners+=("${!runner_env:-$repo/$runner_rel}")
done

# Follower output (Forge's is GPL-3.0) can never be one `git add` from a commit.
root=${XENGINE_ROOT:-$repo/.oracle-build/xengine/smoke-$joined}
absroot=$(realpath -m "$root")
case "$absroot/" in
"$repo"/.oracle-build/*) ;;
"$repo"/*) die "refusing root $root: it is inside the repository $repo and not under .oracle-build/" ;;
esac
root=$absroot
mkdir -p "$root"

# 2. the pool narrowed by the follower's preflight.
sed -e 's/#.*//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' "$pool" | sed -e '/^$/d' | LC_ALL=C sort -u >"$root/pool-names.txt"
pres=()
for i in "${!engs[@]}"; do
	e=${engs[$i]}
	if ! "${runners[$i]}" --preflight "$root/pool-names.txt" >"$root/preflight-$e.out" 2>"$root/preflight-$e.err"; then
		cat "$root/preflight-$e.err" >&2
		echo "follower-sweep-smoke: $e --preflight failed" >&2
		exit 1
	fi
	pres+=("$root/preflight-$e.out")
done
cat "${pres[@]}" | cut -f1 | sed -e '/^$/d' | LC_ALL=C sort -u >"$root/pool-filter.txt"
echo "refused $(wc -l <"$root/pool-filter.txt" | tr -d ' ') of $(wc -l <"$root/pool-names.txt" | tr -d ' ') pool cards"

# 3. one chunk. XENGINE_POOL names the decks; --sets FDN only names the run.
sweep=${FOLLOWER_SWEEP_SCRIPT:-$repo/scripts/xengine-sweep.sh}
rc=0
XENGINE_FOLLOWERS="$engines" XENGINE_POOL=$pool XENGINE_POOL_FILTER=$root/pool-filter.txt XENGINE_ROOT=$root \
	"$sweep" --run "$run" --sets FDN --chunks 1 --games "$games" --seed "$seed" --force || rc=$?
if [ "$rc" != 0 ]; then echo "follower-sweep-smoke: the sweep exited $rc" >&2; exit 1; fi

# 4. checks.
d=$root/FDN/0
lines() { if [ -e "$1" ]; then wc -l <"$1" | tr -d ' '; else echo 0; fi; }
rc=0
fail() { echo "follower-sweep-smoke: $*" >&2; rc=1; }
[ -e "$d/.done" ] || fail "$d/.done is missing: the chunk did not finish"
for engine in "${engs[@]}"; do
	if [ ! -e "$d/$engine.jsonl" ]; then
		fail "$d/$engine.jsonl is missing"
	elif [ ! -e "$d/$engine-req.jsonl" ]; then
		fail "$d/$engine-req.jsonl is missing"
	else
		nrows=$(lines "$d/$engine.jsonl") nreq=$(lines "$d/$engine-req.jsonl")
		[ "$nrows" = "$nreq" ] || fail "$engine.jsonl has $nrows rows for $nreq requests"
	fi
	verdicts=$d/diff-$engine/verdicts.jsonl
	if [ ! -e "$verdicts" ]; then
		fail "$verdicts is missing"
		exit 1
	fi

	echo "class histogram ($engine):"
	jq -r .class "$verdicts" | LC_ALL=C sort | uniq -c | sort -k1,1nr -k2,2 | awk '{printf "  %s %s\n", $2, $1}'
	echo "not_sent (prep refusals): $(lines "$d/$engine-prep.jsonl")"
	tapes=$(jq -r '.id | select(contains(":tape"))' "$verdicts" | wc -l | tr -d ' ')
	wins=$(jq -r '.id | select(contains(":w"))' "$verdicts" | wc -l | tr -d ' ')
	echo "items: $tapes tape, $wins window"

	# 5. thresholds, over the sent items.
	sent=$(jq -r 'select(.class != "not_sent") | 1' "$verdicts" | wc -l | tr -d ' ')
	for m in ${maxc[@]+"${maxc[@]}"}; do
		cls=${m%%=*} pct=${m#*=}
		n=$(jq -r --arg c "$cls" 'select(.class == $c) | 1' "$verdicts" | wc -l | tr -d ' ')
		if awk -v n="$n" -v s="$sent" -v p="$pct" 'BEGIN { exit !(n * 100 > p * s) }'; then
			fail "$engine: class $cls is $n of $sent sent items, above --max-class $m"
		fi
	done
done
exit "$rc"
