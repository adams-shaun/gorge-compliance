#!/usr/bin/env bash
# phase-oracle-run.sh IN.jsonl OUT.jsonl
# phase-oracle-run.sh --preflight NAMES.txt
#
# The phase.rs follower's runner, the interface every follower runner has:
#   IN OUT        replay each request line (one JSON object) and write one result row per line to
#                 OUT, stderr to OUT.log. A per-row failure is a harness row, never a non-zero exit.
#   --preflight   print NAME<TAB>REASON on stdout for each name in NAMES.txt phase cannot replay
#                 (stderr passes through).
# Both first check the pin (scripts/phase-oracle-setup.sh --check-pin) and bring the follower build
# up to date (a no-op when fresh). The sweep's heavy lock is the caller's, not this script's.
#
# Exit: 0 ok; 1 the follower failed outright; 2 setup errors (pin, build, usage).
#
# Environment (defaults as in the Makefile and scripts/phase-oracle-setup.sh; these override):
#   FOLLOWERS_CACHE   default ${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance
#   PHASE_ORACLE_DIR  default $FOLLOWERS_CACHE/phase (target/ and card-data-<hash16>.json live here)
#   PHASE_FOLLOWER_DIR default followers/phase (PHASE_PIN, Cargo.lock)
#   CARGO             default $HOME/.cargo/bin/cargo
#   PHASE_ORACLE_MEM  scenarios scope MemoryMax (default 8G)
#   XENGINE_SCOPE=none  no systemd scope (the sweep's own cap may already hold)
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
die() { echo "phase-oracle-run: $*" >&2; exit 2; }
usage="usage: phase-oracle-run.sh IN.jsonl OUT.jsonl | --preflight NAMES.txt"

mode=run
case "${1:-}" in
	--preflight) mode=preflight; names=${2:?$usage}; [ $# -eq 2 ] || die "$usage" ;;
	"" | -*) die "$usage" ;;
	*) in=$1; out=${2:?$usage}; [ $# -eq 2 ] || die "$usage" ;;
esac

fcache=${FOLLOWERS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance}
follower=$(realpath -m "${PHASE_FOLLOWER_DIR:-$here/followers/phase}")
root=$(realpath -m "${PHASE_ORACLE_DIR:-$fcache/phase}")
cargo=${CARGO:-$HOME/.cargo/bin/cargo}

# The pin check prints its verdict on stdout; the preflight protocol owns ours.
"$here/scripts/phase-oracle-setup.sh" --check-pin >&2 || exit 2

read -r sha hash16 _ < "$follower/PHASE_PIN" || true
data=$root/card-data-$hash16.json
bin=$root/target/follower/phase-follower

echo "phase-oracle-run: bringing the follower build up to date (a no-op when fresh; a stale build is minutes)" >&2
build=(env CARGO_HOME="$fcache/cargo-home" CARGO_TARGET_DIR="$root/target" RUSTC="$(dirname "$cargo")/rustc" "$cargo" build --locked --profile follower -p phase-follower)
bscope=()
[ "${XENGINE_SCOPE:-systemd}" = none ] || bscope=(systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=400%)
(cd "$follower" && "${bscope[@]}" "${build[@]}") >&2 || die "cargo build failed"
[ -x "$bin" ] || die "build produced no $bin"

export RUST_MIN_STACK=16777216
export PHASE_REF=${sha:0:12}+$hash16

if [ "$mode" = preflight ]; then
	[ -r "$names" ] || die "cannot read $names"
	exec "$bin" --preflight "$names" --card-data "$data"
fi

[ -r "$in" ] || die "cannot read $in"
out=$(realpath -m "$out")
in=$(realpath "$in")
mkdir -p "$(dirname "$out")"
scope=(systemd-run --user --scope -q -p MemoryMax="${PHASE_ORACLE_MEM:-8G}" -p CPUQuota=400%)
[ "${XENGINE_SCOPE:-systemd}" != none ] || scope=()
: > "$out"
"${scope[@]}" "$bin" scenarios "$in" "$out" --card-data "$data" 2> "$out.log" || {
	rc=$?
	cat "$out.log" >&2
	echo "phase-oracle-run: phase-follower exited $rc" >&2
	exit 1
}
