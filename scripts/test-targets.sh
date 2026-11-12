#!/usr/bin/env bash
# test-targets.sh — capped, corpus-checked single-package test runs with one
# verdict line. A port of gorge's `make test-one` (its `one` mode only).
#
#   make test-one PKG=./path RUN='^TestX$' [SKIP=re] [V=1] [TIMEOUT=2m] [TAGS=a,b]
#   bash scripts/test-targets.sh one      (same variables in the environment)
#
# The LAST stdout line is `test-one: <STATUS> <sha10>[+dirty] <pkg> <seconds>`;
# nothing else on stdout starts with `test-`. Exit codes:
#   PASS 0   FAIL 1   USAGE 2   ENV 3   EMPTY 4   BUDGET 5
#
# Overridable for the smoke: TEST_TARGETS_GO (go), TEST_TARGETS_SYSTEMD_RUN
# (systemd-run), TEST_TARGETS_CARDS (.cards).
set -uo pipefail

mode=${1:-}
SELF=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")
GO=${TEST_TARGETS_GO:-go}
SD=${TEST_TARGETS_SYSTEMD_RUN:-systemd-run}
PKG=${PKG:-}
RUN=${RUN:-}
SKIP=${SKIP:-}
V=${V:-}
TIMEOUT=${TIMEOUT:-2m}
TAGS=${TAGS:-}

sha=nogit
if s=$(git rev-parse --short=10 HEAD 2>/dev/null); then
	sha=$s
	[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ] && sha=$sha+dirty
fi

# verdict STATUS EXIT — print the one verdict line and exit.
verdict() {
	printf 'test-one: %s %s %s %ss\n' "$1" "$sha" "${PKG:--}" "$SECONDS"
	exit "$2"
}
usage() {
	echo "test-targets: usage: $*" >&2
	echo "test-targets: make test-one PKG=./path RUN='^TestX\$' [SKIP=re] [V=1] [TIMEOUT=2m] [TAGS=a,b]" >&2
	verdict USAGE 2
}

[ "$mode" = one ] || usage "unknown mode '$mode' (only 'one' is supported)"

# Re-exec under a cgroup cap unless already scoped.
if [ -z "${TEST_TARGETS_SCOPED:-}" ]; then
	if "$SD" --user --scope -q true >/dev/null 2>&1; then
		exec "$SD" --user --scope -q -p MemoryMax=4G -p CPUQuota=400% \
			env TEST_TARGETS_SCOPED=1 GOMAXPROCS=4 GOMEMLIMIT=3GiB bash "$SELF" one
	fi
	echo "test-targets: note: no user systemd; running uncapped" >&2
fi
export GOFLAGS=-trimpath

# ---- validation (usage, exit 2) ----
[[ $PKG =~ ^\./[A-Za-z0-9_./-]+$ ]] && [[ $PKG != *...* ]] ||
	usage "PKG must be one package path like ./oracle/xengine/follower (got '${PKG}'; no '...' patterns)"
[ -n "$RUN" ] || usage "RUN is required (a go test -run regexp)"
[[ $TIMEOUT =~ ^([0-9]+)([sm])$ ]] || usage "TIMEOUT must look like 90s or 2m (got '$TIMEOUT')"
mult=1; [ "${BASH_REMATCH[2]}" = m ] && mult=60
tsecs=$((10#${BASH_REMATCH[1]} * mult))
{ [ "$tsecs" -ge 1 ] && [ "$tsecs" -le 300 ]; } || usage "TIMEOUT must be between 1s and 5m (got '$TIMEOUT')"
[[ -z $TAGS || $TAGS =~ ^[A-Za-z0-9_,]+$ ]] || usage "TAGS must match [A-Za-z0-9_,]+ (got '$TAGS')"

# ---- preflight (ENV, exit 3): every corpus test skips without the corpus ----
cards=${TEST_TARGETS_CARDS:-.cards}/cardsfolder
if [ ! -d "$cards" ] || [ -z "$(ls -A "$cards" 2>/dev/null | head -n 1)" ]; then
	echo "test-targets: corpus missing or empty at $cards: run 'make cards' (corpus tests skip without it, so a green run would be a false green)" >&2
	verdict ENV 3
fi

# ---- run ----
# The go shim allows `go test` only from a sanctioned wrapper (as gorge's copy).
export GORGE_TEST_WRAPPER=1
out=.oracle-build/test/one-${sha%+dirty}.json
mkdir -p "$(dirname "$out")"
args=(test -json -failfast -timeout "$TIMEOUT" -run "$RUN")
[ -n "$SKIP" ] && args+=(-skip "$SKIP")
[ -n "$TAGS" ] && args+=(-tags "$TAGS")
[ -n "$V" ] && [ "$V" != 0 ] && args+=(-v)
args+=("$PKG")
"$GO" "${args[@]}" > "$out"
rc=$?

if [ "$rc" = 137 ] || [ "$rc" = 143 ] || /usr/bin/grep -q 'panic: test timed out' "$out"; then
	echo "test-targets: over budget (timeout $TIMEOUT or killed); stream in $out" >&2
	verdict BUDGET 5
fi
if [ "$rc" = 0 ]; then
	n=$(jq -Rn '[inputs | fromjson? | select(type == "object" and .Action == "run" and has("Test"))] | length' "$out" 2>/dev/null || echo 0)
	if [ "${n:-0}" = 0 ]; then
		echo "test-targets: RUN='$RUN' matched no test in $PKG" >&2
		verdict EMPTY 4
	fi
	verdict PASS 0
fi

# FAIL: the output of the failing tests, at most 60 lines.
failed=$(jq -Rrj '
	[inputs | fromjson? | select(type == "object")] as $e
	| ([$e[] | select(.Action == "fail" and .Test != null) | {key: (.Package + " " + .Test), value: true}] | from_entries) as $f
	| $e[] | select(.Action == "output" and .Output != null and $f[(.Package // "") + " " + (.Test // "")] == true)
	| "  " + .Output' -n "$out" 2>/dev/null | head -n 60)
if [ -z "$failed" ]; then # build failure or similar: no failing test, show the tail
	failed=$(jq -Rrj '[inputs | fromjson? | select(type == "object" and .Action == "output" and .Output != null)] | .[-60:][] | "  " + .Output' -n "$out" 2>/dev/null | head -n 60)
fi
[ -n "$failed" ] && printf '%s\n' "$failed"
verdict FAIL 1
