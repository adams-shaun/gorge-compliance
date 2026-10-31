#!/usr/bin/env bash
# link-cards.sh: point this repository's .cards at a gorge checkout's corpus.
# `make cards` runs it.
#
#   GORGE_ROOT   the gorge checkout holding the corpus (default ../gorge)
#   FORGE_REF    the corpus commit the pinned gorge expects; when set, a corpus
#                at another commit gets a warning
#
# gorge's `make fetch-cards compile-cards` fills the checkout's .cards. Go
# resolves gorge's code from go.mod's pin, usually in the module cache, which
# has no corpus: that is why this takes a checkout and not the module directory.
set -euo pipefail

root=${GORGE_ROOT:-../gorge}
root=${root/#\~/$HOME} # make passes a quoted ~ through unexpanded
die() { echo "make cards: $*" >&2; exit 1; }

[ -d "$root" ] || die "no gorge checkout at $root.
Clone gorge next to this repository, or name a checkout: make cards GORGE_ROOT=/path/to/gorge"
abs=$(cd "$root" && pwd)
[ -f "$abs/Makefile" ] && /usr/bin/grep -q '^fetch-cards' "$abs/Makefile" || die "$abs is not a gorge checkout (its Makefile has no fetch-cards target)"
[ -d "$abs/.cards/cardsfolder" ] || die "$abs has no corpus. Fetch it there: (cd $abs && make fetch-cards compile-cards)"
if [ -e .cards ] && [ ! -L .cards ]; then
	die "$PWD/.cards exists and is not a symlink; move it aside first"
fi

lock=$abs/.cards/cards.lock
if [ -n "${FORGE_REF:-}" ] && [ -f "$lock" ] && ! /usr/bin/grep -q "$FORGE_REF" "$lock"; then
	echo "make cards: warning: the corpus in $abs is not at FORGE_REF $FORGE_REF, the commit the pinned gorge expects." >&2
	echo "Corpus-dependent results will differ from the committed targets. See docs/PINNING.md." >&2
fi
ln -sfn "$abs/.cards" .cards
echo ".cards -> $abs/.cards"
