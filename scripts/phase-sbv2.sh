#!/usr/bin/env bash
# phase-sbv2.sh [--compile | --selftest] [-decks DIR] [-source-revision REV] [--house-seat p0|p1 [--house-difficulty NAME]]
#
# Runs phase.rs as a SpellBench v2 *engine*: an NDJSON server on this process's stdin and stdout
# (docs/site/api/spellbench-v2.md, "Engine role", in gorge), so four-yay's
# `cmd/xclient -engine sbv2 -engine-cmd scripts/phase-sbv2.sh` plays phase with any policy.
# Diagnostics and the end-of-run census go to stderr; stdout carries protocol lines only.
#
# The engine is followers/phase/phase-seat (Apache-2.0) over phase.rs at followers/phase/PHASE_PIN.
# This script owns what gorge-compliance owns: the pin check, the build of that one binary, and the
# memory-capped scope. It is scripts/phase-oracle-run.sh's recipe: same pin check, same out-of-tree
# build tree and card data (make phase-oracle-setup fetches them).
#
#   scripts/phase-sbv2.sh              # serve one engine session on stdio
#   scripts/phase-sbv2.sh --compile    # build only; prints "phase-seat=<path> ref=<sha12>+<hash16>"
#   scripts/phase-sbv2.sh --selftest   # build, then a hello / malformed-line exchange
#
# -decks DIR is the catalog (gorge deck JSON, catalog_id = file stem); PHASE_SBV2_DECKS is the
# default. With neither, ten decks are generated from the committed FDN-commons pool and the pinned
# card data under the out-of-tree root (decks-<hash16>-<catalog16>), so a host that would otherwise inject its
# own bundled decks gets only cards phase replays. PHASE_SBV2_DECKS=none serves no catalog.
# -source-revision REV is reported as engine.source_revision.
#
# --house-seat p0|p1 has phase-ai answer that seat in-process (difficulty --house-difficulty, default
# medium; veryeasy, easy, medium, hard, veryhard, cedh), so the client is posed only the other seat.
# It builds phase-seat with its `house-ai` feature (a larger build, kept apart from the default one);
# hello_ok's engine.version ends /house-<seat>-<difficulty>.
#
# Environment (defaults as in scripts/phase-oracle-run.sh; these override):
#   FOLLOWERS_CACHE, PHASE_ORACLE_DIR, PHASE_FOLLOWER_DIR, CARGO
#   PHASE_SBV2_MEM    serving scope MemoryMax (default 8G); the build's is 12G
#   XENGINE_SCOPE=none  no systemd scope
#
# Exit: the engine's own status when serving (0 on clean end of input), 2 for a precondition (pin,
# build, usage), 1 for a failed selftest.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
die() { echo "phase-sbv2: $*" >&2; exit 2; }
usage="usage: phase-sbv2.sh [--compile | --selftest] [-decks DIR] [-source-revision REV] [--house-seat p0|p1 [--house-difficulty NAME]]"

mode=serve
decks=${PHASE_SBV2_DECKS:-}
rev=
house=()
while [ $# -gt 0 ]; do
	case "$1" in
		--compile) mode=compile ;;
		--selftest) mode=selftest ;;
		-decks | --decks) [ $# -ge 2 ] || die "$usage"; decks=$2; shift ;;
		-source-revision | --source-revision) [ $# -ge 2 ] || die "$usage"; rev=$2; shift ;;
		--house-seat | --house-difficulty) [ $# -ge 2 ] || die "$usage"; house+=("$1" "$2"); shift ;;
		*) die "$usage" ;;
	esac
	shift
done

fcache=${FOLLOWERS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance}
follower=$(realpath -m "${PHASE_FOLLOWER_DIR:-$here/followers/phase}")
root=$(realpath -m "${PHASE_ORACLE_DIR:-$fcache/phase}")
cargo=${CARGO:-$HOME/.cargo/bin/cargo}

# The pin check prints its verdict on stdout; the protocol owns ours.
"$here/scripts/phase-oracle-setup.sh" --check-pin >&2 || exit 2

read -r sha hash16 _ < "$follower/PHASE_PIN" || true
data=$root/card-data-$hash16.json
bin=$root/target/follower/phase-seat
ref=${sha:0:12}+$hash16

echo "phase-sbv2: bringing the phase-seat build up to date (a no-op when fresh; a stale build is minutes)" >&2
build=(env CARGO_HOME="$fcache/cargo-home" CARGO_TARGET_DIR="$root/target" RUSTC="$(dirname "$cargo")/rustc" "$cargo" build --locked --profile follower -p phase-seat)
# The house seat is phase-ai in the same binary: only a house run pays for (and rebuilds with) that feature.
[ ${#house[@]} -eq 0 ] || build+=(--features house-ai)
bscope=()
[ "${XENGINE_SCOPE:-systemd}" = none ] || bscope=(systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=400%)
(cd "$follower" && "${bscope[@]}" "${build[@]}") >&2 || die "cargo build failed"
[ -x "$bin" ] || die "build produced no $bin"

if [ "$mode" = compile ]; then
	echo "phase-seat=$bin ref=$ref"
	exit 0
fi

# The catalog a host that injects its own decks would otherwise be given: xclient writes four-yay's
# bundled FDN lists and relaunches with -decks when the hello lists none, and those lists carry cards
# phase does not replay (Tinybones, Bauble Burglar). A generated catalog from the committed pool keeps
# every served card replayable; the generation is deterministic and cached per pin AND per catalog
# recipe (a pool or generator edit must not be served from a stale cache).
if [ "$decks" = none ]; then
	decks=
elif [ -z "$decks" ]; then
	catalog_key=$( { cat "$here/oracle/xengine/pools/fdn-commons.txt" "$here/scripts/phase-sbv2-decks.py"; } 2>/dev/null | sha256sum | cut -c1-16 )
	decks=$root/decks-$hash16-$catalog_key
	if [ ! -d "$decks" ]; then
		pool=$here/oracle/xengine/pools/fdn-commons.txt
		if [ -f "$pool" ] && [ -f "$data" ] && command -v python3 >/dev/null 2>&1; then
			tmp=$decks.tmp.$$
			# A SIGTERM/interrupt must not leave a partial `.tmp.<pid>` under the out-of-tree root.
			trap 'rm -rf "$tmp"' EXIT INT TERM
			if python3 "$here/scripts/phase-sbv2-decks.py" "$pool" "$data" "$tmp" 10 1>&2; then
				# mv -T: a concurrent generator that loses the race leaves the winner's directory alone.
				mv -T "$tmp" "$decks" 2>/dev/null || rm -rf "$tmp"
			else
				rm -rf "$tmp"
				echo "phase-sbv2: no default catalog (phase-sbv2-decks.py failed); serving none" >&2
			fi
			trap - EXIT INT TERM
		fi
		[ -d "$decks" ] || decks=
	fi
fi

export RUST_MIN_STACK=16777216
export PHASE_REF=$ref
args=(--card-data "$data")
[ -z "$decks" ] || args+=(--decks "$decks")
[ -z "$rev" ] || args+=(--source-revision "$rev")
[ ${#house[@]} -eq 0 ] || args+=("${house[@]}")
scope=()
[ "${XENGINE_SCOPE:-systemd}" = none ] || scope=(systemd-run --user --scope -q -p MemoryMax="${PHASE_SBV2_MEM:-8G}" -p CPUQuota=400%)

if [ "$mode" = selftest ]; then
	req='{"request_type":"hello","protocol":"spellbench/v2","request_id":"selftest-1","protocol_minor":0}'
	# A here-string, not a pipe: under `pipefail` an engine that exits before reading
	# the request turns printf's SIGPIPE into a spurious "engine exited non-zero".
	selftest_in=$(printf '%s\nnot json\n' "$req")
	out=$("${scope[@]}" "$bin" "${args[@]}" <<< "$selftest_in") || { echo "phase-sbv2: selftest: the engine exited non-zero" >&2; exit 1; }
	first=$(printf '%s\n' "$out" | sed -n 1p)
	second=$(printf '%s\n' "$out" | sed -n 2p)
	# Canonical JSON sorts keys, so the engine block comes before response_type: test each on its own.
	case "$first" in *'"response_type":"hello_ok"'*) ;; *) echo "phase-sbv2: selftest: no hello_ok: $first" >&2; exit 1 ;; esac
	case "$first" in *'"name":"phase"'*) ;; *) echo "phase-sbv2: selftest: the engine is not phase: $first" >&2; exit 1 ;; esac
	case "$second" in *'"code":"malformed_json"'*) ;; *) echo "phase-sbv2: selftest: no malformed_json for a bad line: $second" >&2; exit 1 ;; esac
	echo "phase-sbv2: selftest ok ($ref)" >&2
	exit 0
fi

exec "${scope[@]}" "$bin" "${args[@]}"
