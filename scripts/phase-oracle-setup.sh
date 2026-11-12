#!/usr/bin/env bash
# phase-oracle-setup.sh -- build the phase.rs follower out of tree, at the pinned engine commit,
# and fetch the pinned card data.
#
#   scripts/phase-oracle-setup.sh              # pin check, card data, build, count faces
#   scripts/phase-oracle-setup.sh --check-pin  # pin consistency + card data present with .ok
#                                              # (fast: no network, no build; the runner calls it)
#
# followers/phase/PHASE_PIN holds one line: <engine sha> <hash16> <sha256 of the card data>.
# phase.rs is MIT OR Apache-2.0 and is a Cargo git dependency at that commit (never vendored). The
# card data is 100 MB derived from Oracle text: it lives under $PHASE_ORACLE_DIR (outside the
# repository) and is never committed.
#
# Environment: PHASE_ORACLE_DIR (default $FOLLOWERS_CACHE/phase), FOLLOWERS_CACHE (default
# ${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance), PHASE_FOLLOWER_DIR (default followers/phase),
# CARGO (default $HOME/.cargo/bin/cargo, the rustup proxy), XENGINE_SCOPE=none (no systemd scope).
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
die() { echo "phase-oracle-setup: $*" >&2; exit 2; }

fcache=${FOLLOWERS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance}
follower=$(realpath -m "${PHASE_FOLLOWER_DIR:-$here/followers/phase}")
root=$(realpath -m "${PHASE_ORACLE_DIR:-$fcache/phase}")
cargo=${CARGO:-$HOME/.cargo/bin/cargo}
url_base=${PHASE_DATA_URL:-https://data.phase-rs.dev}

mode=setup
case "${1:-}" in
	"") ;;
	--check-pin) mode=check ;;
	*) die "usage: phase-oracle-setup.sh [--check-pin]" ;;
esac

# The card data is never committed: refuse a directory inside the repository.
repo=$(realpath -m "$here")
case "$root/" in "$repo/"*) die "PHASE_ORACLE_DIR $root is inside the repository $repo; the card data is never committed" ;; esac

[ -r "$follower/PHASE_PIN" ] || die "no $follower/PHASE_PIN"
read -r sha hash16 sha256 extra < "$follower/PHASE_PIN" || true
[ -z "${extra:-}" ] && [[ $sha =~ ^[0-9a-f]{40}$ ]] && [[ $hash16 =~ ^[0-9a-f]{16}$ ]] && [[ $sha256 =~ ^[0-9a-f]{64}$ ]] \
	|| die "$follower/PHASE_PIN must be one line: <40-hex engine sha> <16-hex> <64-hex sha256>"
[ "${sha256:0:16}" = "$hash16" ] || die "PHASE_PIN: hash16 $hash16 is not the first 16 hex of sha256 $sha256"
tag=${sha:0:12}+$hash16

# Pin consistency: Cargo.toml's rev and Cargo.lock's source both name PHASE_PIN's engine sha.
toml_rev=$(sed -n 's/^engine *=.*[ {,]rev *= *"\([^"]*\)".*/\1/p' "$follower/phase-follower/Cargo.toml")
[ "$toml_rev" = "$sha" ] || die "pin mismatch: phase-follower/Cargo.toml rev '$toml_rev' != PHASE_PIN engine sha '$sha'"
want_src="source = \"git+https://github.com/phase-rs/phase?rev=$sha#$sha\""
lock_src=$(awk '/^name = "phase-engine"$/ {f=1; next} f && /^source = / {print; exit} /^\[\[package\]\]/ {f=0}' "$follower/Cargo.lock" 2>/dev/null || true)
[ "$lock_src" = "$want_src" ] || die "pin mismatch: Cargo.lock phase-engine ${lock_src:-has no source line}, want $want_src (PHASE_PIN engine sha $sha)"

data=$root/card-data-$hash16.json

if [ "$mode" = check ]; then
	[ -s "$data" ] && [ -e "$data.ok" ] || die "card data $data is missing or unverified (no .ok); run make phase-oracle-setup"
	echo "phase-oracle-setup: pin $tag holds"
	exit 0
fi

# Card data: download beside, verify, rename; .ok marks a verified file.
mkdir -p "$root"
sha_ok() { # FILE: full sha256 equals the pin's (the first 16 hex then equal hash16)
	local got; got=$(sha256sum "$1" | cut -d' ' -f1)
	[ "$got" = "$sha256" ]
}
if [ -s "$data" ] && [ ! -e "$data.ok" ]; then
	if sha_ok "$data"; then : > "$data.ok"; else rm -f "$data"; die "existing $data does not match PHASE_PIN sha256 $sha256; deleted"; fi
fi
if [ ! -s "$data" ]; then
	rm -f "$data.ok" "$data.part"
	curl -fsSL -o "$data.part" "$url_base/card-data-$hash16.json" || { rm -f "$data.part"; die "download of card-data-$hash16.json failed"; }
	if ! sha_ok "$data.part"; then rm -f "$data.part"; die "downloaded card data does not match PHASE_PIN sha256 $sha256"; fi
	mv "$data.part" "$data"
	: > "$data.ok"
fi

# Build, serialised and capped (a cold build is minutes and several GB; one-off per pin).
# RUSTC names the rustup proxy beside cargo: with CARGO_HOME moved to the cache, the proxy's cargo
# no longer finds a rustc on PATH ("could not execute process `rustc -vV`").
build=(env CARGO_HOME="$fcache/cargo-home" CARGO_TARGET_DIR="$root/target" RUSTC="$(dirname "$cargo")/rustc" "$cargo" build --locked --profile follower -p phase-follower)
scope=()
if [ "${XENGINE_SCOPE:-}" != none ]; then
	scope=(systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=400%)
fi
cd "$follower"
"$here/scripts/heavy-lock.sh" --wait 20000 --mem 12G --name phase-oracle-setup -- "${scope[@]}" "${build[@]}"

bin=$root/target/follower/phase-follower
[ -x "$bin" ] || die "build produced no $bin"
n=$(RUST_MIN_STACK=16777216 "$bin" --card-data "$data" --count)
echo "phase-oracle-setup: $n faces, pin $tag"
