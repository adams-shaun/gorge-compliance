#!/usr/bin/env bash
# manabrew-oracle-setup.sh -- build Manabrew out of tree, and its card archive from gorge's Forge scripts.
#
# Idempotent. Everything lands under $MANABREW_ORACLE_DIR (default
# ${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance/manabrew), never inside this
# repository: Manabrew is AGPL-3.0 and the Forge scripts are GPL-3.0, so their
# source, build output and the archive are never committed.
#
#   scripts/manabrew-oracle-setup.sh              # clone/fetch, build tree, cargo build, archive, .pin
#   scripts/manabrew-oracle-setup.sh --check-pin  # fast, no network, no build: exit 2 naming what to rerun
#
# Layout under the directory:
#   manabrew/            dev clone of MANABREW_REPO (later tickets commit on its branch `oracle`;
#                        this script never checks out, resets or cleans it)
#   build-<ref12>/       detached worktree of the dev clone at MANABREW_ORACLE_REF
#   forge-res/           blobless sparse clone of FORGE_REPO (never checked out)
#   res-<forge12>/       forge-gui/res/{cardsfolder,tokenscripts,editions,blockdata,lists} at FORGE_REF
#   target/              CARGO_TARGET_DIR
#   cardset-<forge12>.rkyv, cardset-<forge12>.pin   the archive and its three-line pin
#
# Why the archive is built here: Manabrew's own Forge submodule is ~1,000 commits
# behind FORGE_REF, and a script difference would read as an engine disagreement.
# The archive is built from gorge's corpus pin (FORGE_REF, FORGE_REPO), read from
# gorge's Makefile in the module go resolves.
#
# Env: CARGO (default $HOME/.cargo/bin/cargo), XENGINE_SCOPE=none disables the
# systemd scope, FORGE_REF / FORGE_REPO / MANABREW_REPO / MANABREW_ORACLE_REF overrides.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
gorge_mk() {
	local g
	g=$(cd "$here" && go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge)
	sed -n "s/^$1 *?= *//p" "$g/Makefile"
}
die() { echo "manabrew-oracle-setup: $*" >&2; exit 2; }

check=0
case "${1:-}" in
--check-pin) check=1; [ $# -eq 1 ] || die "usage: [--check-pin]" ;;
"") ;;
*) die "usage: [--check-pin]" ;;
esac

forge_ref=${FORGE_REF:-$(gorge_mk FORGE_REF)}
forge_repo=${FORGE_REPO:-$(gorge_mk FORGE_REPO)}
repo=${MANABREW_REPO:-$(mk MANABREW_REPO)}
ref=${MANABREW_ORACLE_REF:-$(mk MANABREW_ORACLE_REF)}
cache=${FOLLOWERS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance}
dir=${MANABREW_ORACLE_DIR:-$cache/manabrew}
[ -n "$forge_ref" ] && [ -n "$forge_repo" ] && [ -n "$repo" ] && [ -n "$ref" ] && [ -n "$dir" ] ||
	die "FORGE_REF/FORGE_REPO/MANABREW_REPO/MANABREW_ORACLE_REF/MANABREW_ORACLE_DIR unset"
dir=$(realpath -m "$dir")
real_here=$(realpath -m "$here")
case "$dir" in
"$real_here" | "$real_here"/*) die "$dir is inside the repository; Manabrew source, build output and the archive are AGPL/GPL material and are never committed" ;;
esac

clone=$dir/manabrew
fres=$dir/forge-res
fref12=${forge_ref:0:12}
res=$dir/res-$fref12
archive=$dir/cardset-$fref12.rkyv
pinfile=$dir/cardset-$fref12.pin
scripts_paths=(forge-gui/res/cardsfolder forge-gui/res/tokenscripts forge-gui/res/editions forge-gui/res/blockdata forge-gui/res/lists)

resolve_ref() {
	oref=$(git -C "$clone" rev-parse --verify -q "$ref^{commit}") || return 1
	oref12=${oref:0:12}
	build=$dir/build-$oref12
}

if [ "$check" = 1 ]; then
	hint="rerun make manabrew-oracle-setup"
	[ -e "$clone/.git" ] || die "no dev clone at $clone; $hint"
	resolve_ref || die "MANABREW_ORACLE_REF $ref does not resolve in $clone; $hint"
	[ -e "$build/.git" ] || die "no build tree $build at $oref12; $hint"
	[ "$(git -C "$build" rev-parse HEAD)" = "$oref" ] || die "build tree $build is not at $oref; $hint"
	[ -s "$archive" ] || die "no archive $archive for FORGE_REF $fref12; $hint"
	[ -s "$pinfile" ] || die "no $pinfile for FORGE_REF $fref12; $hint"
	[ -e "$fres/.git" ] || die "no $fres to check the archive's trees against; $hint"
	ct=$(git -C "$fres" rev-parse "$forge_ref:forge-gui/res/cardsfolder") &&
		tt=$(git -C "$fres" rev-parse "$forge_ref:forge-gui/res/tokenscripts") ||
		die "FORGE_REF $forge_ref or its script trees are not in $fres; $hint"
	want=$(printf '%s\n%s\n%s\n' "$forge_ref" "$ct" "$tt")
	got=$(cat "$pinfile")
	[ "$got" = "$want" ] || die "$pinfile does not match gorge's FORGE_REF $forge_ref (cardsfolder/tokenscripts trees); the archive is stale; $hint"
	echo "manabrew-oracle-setup: pin holds: archive $archive, ref $oref12, FORGE_REF $fref12"
	exit 0
fi

case "$dir" in /tmp/*) die "$dir is under /tmp, which is often RAM-backed; the build needs a directory on disk" ;; esac
mkdir -p "$dir"

# Dev clone: fetched, never moved.
if [ ! -e "$clone/.git" ]; then
	git clone -q --filter=blob:none "$repo" "$clone"
fi
git -C "$clone" fetch -q origin
resolve_ref || die "MANABREW_ORACLE_REF $ref is not in $clone after fetch"

# Build tree: one detached worktree per ref.
if [ ! -e "$build/.git" ]; then
	git -C "$clone" worktree add --detach "$build" "$oref"
fi
[ "$(git -C "$build" rev-parse HEAD)" = "$oref" ] || die "$build is not at $oref"

# Forge res at gorge's FORGE_REF.
if [ ! -e "$fres/.git" ]; then
	git clone -q --filter=blob:none --no-checkout --sparse "$forge_repo" "$fres"
	git -C "$fres" sparse-checkout set "${scripts_paths[@]}"
fi
git -C "$fres" fetch -q origin
git -C "$fres" rev-parse --verify -q "$forge_ref^{commit}" >/dev/null || die "FORGE_REF $forge_ref is not in $fres after fetch"
if [ ! -e "$res/.complete" ]; then
	rm -rf "$res"
	mkdir -p "$res"
	git -C "$fres" archive "$forge_ref" "${scripts_paths[@]}" | tar -x -C "$res"
	: > "$res/.complete"
fi
fr=$res/forge-gui/res
[ -f "$fr/lists/TypeLists.txt" ] || die "no $fr/lists/TypeLists.txt; build-cardset-archive needs it"

# Rust: stable, locked.
cargo=${CARGO:-$HOME/.cargo/bin/cargo}
PATH=$(dirname "$cargo"):$PATH # cargo shells out to rustc, which sits beside it
export CARGO_HOME=$cache/cargo-home CARGO_TARGET_DIR=$dir/target
mkdir -p "$CARGO_HOME"
scope=()
if [ "${XENGINE_SCOPE:-}" != none ] && command -v systemd-run >/dev/null 2>&1; then
	scope=(systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=400%)
fi
heavy() { "$here/scripts/heavy-lock.sh" --wait 20000 --mem 12G --name manabrew-oracle-setup -- "${scope[@]}" "$@"; }

cd "$build/manabrew-rs"
heavy "$cargo" build --locked --release -p forge-cardset-archive --features build --bin build-cardset-archive
heavy "$cargo" build --locked --release -p forge-foundation -p forge-carddb -p manabrew-engine

# Archive, then its pin.
rm -f "$archive.part"
heavy "$dir/target/release/build-cardset-archive" "$fr/cardsfolder" "$fr/tokenscripts" "$fr/editions" "$fr/blockdata" "$fr/lists/TypeLists.txt" "$archive.part"
[ -s "$archive.part" ] || die "build-cardset-archive wrote nothing to $archive.part"
mv "$archive.part" "$archive"
{
	echo "$forge_ref"
	git -C "$fres" rev-parse "$forge_ref:forge-gui/res/cardsfolder"
	git -C "$fres" rev-parse "$forge_ref:forge-gui/res/tokenscripts"
} > "$pinfile"
echo "manabrew-oracle-setup: archive $archive ($(wc -c < "$archive") bytes), ref $oref12, FORGE_REF $fref12"
