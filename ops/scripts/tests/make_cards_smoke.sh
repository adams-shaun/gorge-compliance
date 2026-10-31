#!/usr/bin/env bash
# make_cards_smoke.sh: `make cards` finds a sibling gorge checkout or an explicit
# GORGE_ROOT, and fails with a message that says what to do otherwise. It uses a
# scratch copy of the Makefile and the script: no Go, no corpus, no network.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/make-cards-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() { if eval "$2"; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s: %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }

C=$TMP/work/gorge-compliance
mkdir -p "$C/scripts"
cp "$ROOT/Makefile" "$C/"
cp "$ROOT/scripts/link-cards.sh" "$C/scripts/"
cards() { (cd "$C" && make --no-print-directory cards FORGE_REF=want "$@" 2>&1); }
gorge() { # dir: a gorge-shaped checkout with no corpus
	mkdir -p "$1"
	printf 'fetch-cards:\n' >"$1/Makefile"
}

out=$(cards); rc=$?
check "no checkout fails" '[ "$rc" != 0 ]' "rc=$rc"
check "no checkout says what to do" 'has "$out" "GORGE_ROOT=/path/to/gorge"' "$out"
check "no link left behind" '[ ! -e "$C/.cards" ] && [ ! -L "$C/.cards" ]'

gorge "$TMP/work/gorge"
out=$(cards); rc=$?
check "sibling with no corpus fails" '[ "$rc" != 0 ]' "rc=$rc"
check "sibling with no corpus names the fetch" 'has "$out" "make fetch-cards compile-cards"' "$out"

mkdir -p "$TMP/work/gorge/.cards/cardsfolder"
echo '{"ref":"want"}' >"$TMP/work/gorge/.cards/cards.lock"
out=$(cards); rc=$?
check "sibling links" '[ "$rc" = 0 ] && [ "$(readlink "$C/.cards")" = "$TMP/work/gorge/.cards" ]' "rc=$rc $out"
check "matching corpus has no warning" '! has "$out" warning' "$out"

gorge "$TMP/other"
mkdir -p "$TMP/other/.cards/cardsfolder"
echo '{"ref":"elsewhere"}' >"$TMP/other/.cards/cards.lock"
out=$(cards GORGE_ROOT="$TMP/other"); rc=$?
check "explicit GORGE_ROOT wins" '[ "$rc" = 0 ] && [ "$(readlink "$C/.cards")" = "$TMP/other/.cards" ]' "rc=$rc $out"
check "a corpus at another commit warns" 'has "$out" "not at FORGE_REF"' "$out"

out=$(cards GORGE_ROOT="$TMP/nowhere"); rc=$?
check "bad GORGE_ROOT fails" '[ "$rc" != 0 ] && has "$out" "no gorge checkout"' "rc=$rc $out"

mkdir -p "$TMP/notgorge"
out=$(cards GORGE_ROOT="$TMP/notgorge"); rc=$?
check "a directory that is not gorge fails" '[ "$rc" != 0 ] && has "$out" "not a gorge checkout"' "rc=$rc $out"

rm -f "$C/.cards"; mkdir "$C/.cards"
out=$(cards); rc=$?
check "a real .cards directory is left alone" '[ "$rc" != 0 ] && has "$out" "not a symlink"' "rc=$rc $out"

[ "$fails" = 0 ] && echo "make_cards_smoke: all passed" || { echo "make_cards_smoke: $fails failed"; exit 1; }
