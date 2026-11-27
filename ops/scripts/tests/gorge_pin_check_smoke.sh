#!/usr/bin/env bash
# gorge_pin_check_smoke.sh — prove scripts/gorge-pin.sh check passes a tree
# whose GORGE_PIN, go.mod require and go.sum agree (pseudo-version
# v0.0.0-<14-digit timestamp>-<12 hex of the SHA>, require line or block,
# direct or indirect) and fails closed on every way the pin invariant can
# break: a non-SHA GORGE_PIN, a missing require, a non-pseudo-version, a
# malformed (13-digit) timestamp, a well-formed timestamp go.sum does not back,
# a 12-hex tail that is not the pinned commit's, any replace for the module
# (line or block form), and a missing module or go.mod hash in go.sum. It also
# checks the real tree and the usage
# exit. Fixtures live under mktemp with their own git init; no network, no
# writes to the repo.
#
#   ops/scripts/tests/gorge_pin_check_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT=$ROOT/scripts/gorge-pin.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/gorge-pin-check-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() { # name got want
	if [ "$2" = "$3" ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s: exit %s, want %s\n' "$1" "$2" "$3"; fails=$((fails + 1)); fi
}

SHA=1a3c73f9eb5fde59d7da378bbf4c07a947e0b056
V=v0.0.0-20261127212030-1a3c73f9eb5f
MOD=github.com/adams-shaun/gorge

new_fixture() { # dir — a valid pinned tree; the caller mutates one file per case
	rm -rf "$1"
	mkdir -p "$1"
	git -C "$1" init -q
	echo "$SHA" >"$1/GORGE_PIN"
	printf 'module github.com/adams-shaun/gorge-compliance\n\ngo 1.24.0\n\nrequire %s %s\n' "$MOD" "$V" >"$1/go.mod"
	printf '%s %s h1:AAAA==\n%s %s/go.mod h1:BBBB==\n' "$MOD" "$V" "$MOD" "$V" >"$1/go.sum"
}
run_check() { # dir — exit code of `check` in that tree
	(cd "$1" && bash "$SCRIPT" check) >/dev/null 2>&1
	echo $?
}

F=$TMP/fix

new_fixture "$F"
check valid "$(run_check "$F")" 0

new_fixture "$F"
printf 'NotAFullSha\n' >"$F/GORGE_PIN"
check bad-gorge-pin "$(run_check "$F")" 1

new_fixture "$F"
printf 'module github.com/adams-shaun/gorge-compliance\n\ngo 1.24.0\n' >"$F/go.mod"
check missing-require "$(run_check "$F")" 1

new_fixture "$F"
sed -i "s|$V|v1.2.3|" "$F/go.mod" "$F/go.sum"
check not-a-pseudo-version "$(run_check "$F")" 1

new_fixture "$F"
sed -i "s|$V|v0.0.0-2026112721203-1a3c73f9eb5f|" "$F/go.mod" "$F/go.sum"
check malformed-timestamp "$(run_check "$F")" 1

# A well-formed but wrong 14-digit timestamp is indistinguishable from the
# real commit time offline; the go.sum hashes for the version go.mod names are
# what catch it (go.sum is written by `go mod tidy` from the resolved module).
new_fixture "$F"
sed -i "s|$V|v0.0.0-20991231235959-1a3c73f9eb5f|" "$F/go.mod"
check wrong-timestamp-unbacked "$(run_check "$F")" 1

new_fixture "$F"
sed -i "s|-${SHA:0:12}|-000000000000|" "$F/go.mod" "$F/go.sum"
check wrong-commit-suffix "$(run_check "$F")" 1

new_fixture "$F"
printf '\nreplace %s => ../gorge\n' "$MOD" >>"$F/go.mod"
check replace-line "$(run_check "$F")" 1

new_fixture "$F"
printf '\nreplace (\n\t%s => ../gorge\n)\n' "$MOD" >>"$F/go.mod"
check replace-block "$(run_check "$F")" 1

new_fixture "$F"
sed -i "\|^$MOD $V h1:|d" "$F/go.sum"
check missing-module-hash "$(run_check "$F")" 1

new_fixture "$F"
sed -i "\|^$MOD $V/go.mod h1:|d" "$F/go.sum"
check missing-gomod-hash "$(run_check "$F")" 1

new_fixture "$F"
printf 'module github.com/adams-shaun/gorge-compliance\n\ngo 1.24.0\n\nrequire (\n\t%s %s // indirect\n)\n' "$MOD" "$V" >"$F/go.mod"
check require-block-indirect "$(run_check "$F")" 0

new_fixture "$F"
(cd "$F" && bash "$SCRIPT" check extra) >/dev/null 2>&1
check usage-extra-arg "$?" 2

bash "$SCRIPT" check >/dev/null 2>&1
check real-tree "$?" 0

if [ "$fails" = 0 ]; then
	echo "gorge_pin_check_smoke: all passed"
else
	echo "gorge_pin_check_smoke: $fails failed" >&2
fi
exit "$((fails > 0))"
