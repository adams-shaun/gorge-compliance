#!/usr/bin/env bash
# manabrew-oracle-run.sh IN.jsonl OUT.jsonl
# manabrew-oracle-run.sh --preflight NAMES.txt
#
# The Manabrew follower's runner, the interface every follower runner has:
#   IN OUT        replay each request line (one JSON object) and write one result row per line to
#                 OUT, stderr to OUT.log. A per-row failure is a harness row, never a non-zero exit.
#   --preflight   print NAME<TAB>REASON on stdout for each name in NAMES.txt Manabrew cannot replay
#                 (stderr passes through).
# Both first check the pin (scripts/manabrew-oracle-setup.sh --check-pin) and bring the driver build
# up to date (a no-op when fresh). The sweep's heavy lock is the caller's, not this script's.
#
# Restart after a panic: the engine keeps thread-local depth counters and global registries that a
# panic can leave dirty. The binary turns a panic into that row's harness row and then exits 3; this
# script restarts it on the input lines after the rows already in OUT (appending) until it exits 0.
# Any other non-zero status, or a restart that wrote no row, fails the run (exit 2).
#
# Exit: 0 ok; 2 setup errors (pin, build, usage) or a driver failure.
#
# Environment (defaults as in the Makefile and scripts/manabrew-oracle-setup.sh; these override):
#   FOLLOWERS_CACHE        default ${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance
#   MANABREW_ORACLE_DIR    default $FOLLOWERS_CACHE/manabrew (build-<ref12>/, target/, cardset-<forge12>.rkyv)
#   MANABREW_ORACLE_REF    the driver commit (Makefile)
#   FORGE_REF              the archive's Forge pin (gorge's Makefile)
#   CARGO                  default $HOME/.cargo/bin/cargo
#   MANABREW_ORACLE_MEM    scenarios scope MemoryMax (default 8G)
#   XENGINE_SCOPE=none     no systemd scope (the sweep's own cap may already hold)
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
gorge_mk() {
	local g
	g=$(cd "$here" && go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge)
	sed -n "s/^$1 *?= *//p" "$g/Makefile"
}
die() { echo "manabrew-oracle-run: $*" >&2; exit 2; }
usage="usage: manabrew-oracle-run.sh IN.jsonl OUT.jsonl | --preflight NAMES.txt"

mode=run
case "${1:-}" in
	--preflight) mode=preflight; names=${2:?$usage}; [ $# -eq 2 ] || die "$usage" ;;
	"" | -*) die "$usage" ;;
	*) in=$1; out=${2:?$usage}; [ $# -eq 2 ] || die "$usage" ;;
esac

# The pin check prints its verdict on stdout; the preflight protocol owns ours.
"$here/scripts/manabrew-oracle-setup.sh" --check-pin >&2 || exit 2

fcache=${FOLLOWERS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance}
dir=$(realpath -m "${MANABREW_ORACLE_DIR:-$fcache/manabrew}")
ref=${MANABREW_ORACLE_REF:-$(mk MANABREW_ORACLE_REF)}
forge_ref=${FORGE_REF:-$(gorge_mk FORGE_REF)}
[ -n "$ref" ] && [ -n "$forge_ref" ] || die "MANABREW_ORACLE_REF / FORGE_REF unset"
cargo=${CARGO:-$HOME/.cargo/bin/cargo}

oref=$(git -C "$dir/manabrew" rev-parse --verify -q "$ref^{commit}") || die "MANABREW_ORACLE_REF $ref does not resolve in $dir/manabrew"
build=$dir/build-${oref:0:12}
archive=$dir/cardset-${forge_ref:0:12}.rkyv
bin=$dir/target/oracle/manabrew-oracle

echo "manabrew-oracle-run: bringing the driver build up to date (a no-op when fresh; a stale build is minutes)" >&2
cb=(env CARGO_HOME="$fcache/cargo-home" CARGO_TARGET_DIR="$dir/target" PATH="$(dirname "$cargo"):$PATH" "$cargo" build --locked --profile oracle -p manabrew-oracle)
bscope=()
[ "${XENGINE_SCOPE:-systemd}" = none ] || bscope=(systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=400%)
(cd "$build/manabrew-rs" && "${bscope[@]}" "${cb[@]}") >&2 || die "cargo build failed"
[ -x "$bin" ] || die "build produced no $bin"

# No engine trace may reach stderr (OUT.log would grow without bound).
run_env=(env)
for v in FORGE_BF_TRACE FORGE_CARD_TRACE FORGE_FAIL_TRACE FORGE_PAYMENT_TRACE FORGE_RNG_TRACE FORGE_SORT_TRACE FORGE_STACK_TRACE FORGE_TRIGGER_TRACE FORGE_ZONE_TRACE; do
	run_env+=(-u "$v")
done
run_env+=(MANABREW_REF="$oref")

if [ "$mode" = preflight ]; then
	[ -r "$names" ] || die "cannot read $names"
	exec "${run_env[@]}" "$bin" --preflight "$names" --archive "$archive"
fi

[ -r "$in" ] || die "cannot read $in"
out=$(realpath -m "$out")
in=$(realpath "$in")
mkdir -p "$(dirname "$out")"
scope=(systemd-run --user --scope -q -p MemoryMax="${MANABREW_ORACLE_MEM:-8G}" -p CPUQuota=400%)
[ "${XENGINE_SCOPE:-systemd}" != none ] || scope=()

tmp=$(mktemp -d "$(dirname "$out")/manabrew-oracle-run.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
: > "$out"
: > "$out.log"
total=$(grep -c '' "$in" || true)
done_rows=0
while :; do
	tail -n +$((done_rows + 1)) "$in" > "$tmp/in.jsonl"
	: > "$tmp/out.jsonl"
	rc=0
	"${scope[@]}" "${run_env[@]}" "$bin" scenarios "$tmp/in.jsonl" "$tmp/out.jsonl" --archive "$archive" 2>> "$out.log" || rc=$?
	cat "$tmp/out.jsonl" >> "$out"
	prev=$done_rows
	done_rows=$(grep -c '' "$out" || true)
	case $rc in
	0) break ;;
	3)
		[ "$done_rows" -lt "$total" ] || break
		[ "$done_rows" -gt "$prev" ] || { tail -n 20 "$out.log" >&2; die "manabrew-oracle exited 3 after row $done_rows without writing a row; not restarting"; }
		echo "manabrew-oracle-run: manabrew-oracle exited 3 (panic) after $done_rows of $total rows; restarting" >&2 ;;
	*)
		tail -n 20 "$out.log" >&2
		die "manabrew-oracle exited $rc" ;;
	esac
done
