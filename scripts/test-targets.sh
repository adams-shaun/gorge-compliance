#!/usr/bin/env bash
# test-targets.sh <one|targeted|smart|list>: capped, corpus-checked test runs
# with one verdict line. A port of gorge's modes onto this repository's tools
# (cmd/testselect is the single selector):
#
#   make test-one PKG=./path RUN='^TestX$' [SKIP=re] [V=1] [TIMEOUT=2m] [TAGS=a,b]
#   make test-targeted [BASE=develop]  the packages the diff touches, including
#                                      uncommitted work: the inner loop
#   make test-smart [BASE=develop]     the precheck on a committed tree: gofmt,
#                                      vet, shell syntax, fork pins, licence
#                                      guards, script smokes, then the affected
#                                      packages -- fail-fast
#   make precheck                      an alias for test-smart
#   make test-list [BASE=develop]      print the targeted selection, one package
#                                      per line
#
# The LAST stdout line is `test-<mode>: <STATUS> <sha10>[+dirty] <pkgs> <seconds>`;
# nothing else on stdout starts with `test-`. Exit codes:
#   PASS 0   FAIL 1   USAGE 2   ENV 3   EMPTY 4   BUDGET 5
#
# test-targeted and test-smart use the tracked selector in cmd/testselect (the
# same one scripts/test-select.sh and the pre-merge gate use), so the role rules
# -- scenarios/ selects ./audit, a verdict producer selects the ratchet packages
# -- cannot drift into a second list here.
#
# Overridable for the smoke: TEST_TARGETS_GO (go), TEST_TARGETS_SYSTEMD_RUN
# (systemd-run), TEST_TARGETS_CARDS (.cards).
set -uo pipefail

mode=${1:-}
case $mode in
one | targeted | smart | list) ;;
*)
	echo "test-targets.sh: usage: test-targets.sh one|targeted|smart|list" >&2
	exit 2
	;;
esac

SELF=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")
ROOT=$(cd "$(dirname "$SELF")/.." && pwd)
GO=${TEST_TARGETS_GO:-go}
SD=${TEST_TARGETS_SYSTEMD_RUN:-systemd-run}
PKG=${PKG:-}
RUN=${RUN:-}
SKIP=${SKIP:-}
V=${V:-}
TIMEOUT=${TIMEOUT:-2m}
TAGS=${TAGS:-}
BASE=${BASE:-develop}

t0=$SECONDS
sha=nogit
if s=$(git rev-parse --short=10 HEAD 2>/dev/null); then
	sha=$s
	[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ] && sha=$sha+dirty
fi

SELECTED=()
pkgs_label=

# verdict STATUS [EXIT|MSG] — print the one verdict line and exit with STATUS's
# code. A numeric second argument is the legacy explicit exit code; anything
# else is a message for stderr.
verdict() {
	local st=$1 msg=${2:-} rc
	case $st in
	PASS) rc=0 ;;
	FAIL) rc=1 ;;
	USAGE) rc=2 ;;
	ENV) rc=3 ;;
	EMPTY) rc=4 ;;
	BUDGET) rc=5 ;;
	*) rc=1 ;;
	esac
	case $msg in
	'' | *[!0-9]*) [ -n "$msg" ] && echo "test-targets: $mode: $msg" >&2 ;;
	esac
	printf 'test-%s: %s %s %s %ss\n' "$mode" "$st" "$sha" "${pkgs_label:--}" "$((SECONDS - t0))"
	exit "$rc"
}
usage() {
	echo "test-targets: $mode: $*" >&2
	case $mode in
	one) echo "test-targets: make test-one PKG=./path RUN='^TestX\$' [SKIP=re] [V=1] [TIMEOUT=2m] [TAGS=a,b]" >&2 ;;
	targeted) echo "test-targets: make test-targeted [BASE=develop]" >&2 ;;
	smart) echo "test-targets: make test-smart [BASE=develop]" >&2 ;;
	list) echo "test-targets: make test-list [BASE=develop]" >&2 ;;
	esac
	verdict USAGE 2
}

# Re-exec under a cgroup cap unless already scoped.
if [ -z "${TEST_TARGETS_SCOPED:-}" ]; then
	if "$SD" --user --scope -q true >/dev/null 2>&1; then
		exec "$SD" --user --scope -q -p MemoryMax=4G -p CPUQuota=400% \
			env TEST_TARGETS_SCOPED=1 GOMAXPROCS=4 GOMEMLIMIT=3GiB bash "$SELF" "$mode"
	fi
	echo "test-targets: note: no user systemd; running uncapped" >&2
fi
export GOFLAGS=-trimpath GORGE_TEST_WRAPPER=1

# ---- validation (usage, exit 2) ----
if [ "$mode" = one ]; then
	[[ $PKG =~ ^\./[A-Za-z0-9_./-]+$ ]] && [[ $PKG != *...* ]] ||
		usage "PKG must be one package path like ./oracle/xengine/follower (got '${PKG}'; no '...' patterns)"
	[ -n "$RUN" ] || usage "RUN is required (a go test -run regexp)"
	pkgs_label=$PKG
fi
if [ "$mode" != list ]; then
	[[ $TIMEOUT =~ ^([0-9]+)([sm])$ ]] || usage "TIMEOUT must look like 90s or 2m (got '$TIMEOUT')"
	mult=1; [ "${BASH_REMATCH[2]}" = m ] && mult=60
	tsecs=$((10#${BASH_REMATCH[1]} * mult))
	{ [ "$tsecs" -ge 1 ] && [ "$tsecs" -le 300 ]; } || usage "TIMEOUT must be between 1s and 5m (got '$TIMEOUT')"
	[[ -z $TAGS || $TAGS =~ ^[A-Za-z0-9_,]+$ ]] || usage "TAGS must match [A-Za-z0-9_,]+ (got '$TAGS')"
fi

# ---- preflight (ENV, exit 3) ----
if [ "$mode" != list ]; then
	# Every corpus test skips without the corpus, so a green run would be a false green.
	cards=${TEST_TARGETS_CARDS:-$ROOT/.cards}/cardsfolder
	if [ ! -d "$cards" ] || [ -z "$(ls -A "$cards" 2>/dev/null | head -n 1)" ]; then
		echo "test-targets: corpus missing or empty at $cards: run 'make cards' (corpus tests skip without it, so a green run would be a false green)" >&2
		verdict ENV 3
	fi
fi

mb=
if [ "$mode" != one ]; then
	git -C "$ROOT" rev-parse --verify -q "$BASE^{commit}" >/dev/null ||
		verdict ENV "base $BASE does not resolve in this worktree"
	mb=$(git -C "$ROOT" merge-base "$BASE" HEAD) ||
		verdict ENV "no merge base for $BASE..HEAD"
	git -C "$ROOT" merge-base --is-ancestor "$BASE" HEAD ||
		echo "test-targets: note: HEAD is not on top of $BASE; the landing rebases, so a conflict with it is not covered" >&2
fi

# select_pkgs COMMITTED — fill SELECTED with the packages the diff touches.
# COMMITTED=1 diffs BASE..HEAD (test-smart checks a commit); 0 also reads the
# working tree and untracked files (test-targeted's inner loop).
select_pkgs() {
	local committed=$1 files out rc p
	if [ "$committed" = 1 ]; then
		files=$(git -C "$ROOT" diff --name-only "$mb" HEAD)
	else
		files=$({ git -C "$ROOT" diff --name-only "$mb"; git -C "$ROOT" ls-files -o --exclude-standard; } | sort -u)
	fi
	SELECTED=()
	pkgs_label=-
	[ -n "$files" ] || return 0
	# Keep stderr out of the package list: a go warning on stderr would
	# otherwise be read as a package path.
	local errf
	errf=$(mktemp "${TMPDIR:-/tmp}/test-targets-select.XXXXXX") || verdict ENV "mktemp failed"
	out=$(printf '%s\n' "$files" | (cd "$ROOT" && GOWORK=off "$GO" run ./cmd/testselect -root . -files -) 2>"$errf")
	rc=$?
	if [ "$rc" != 0 ]; then
		cat "$errf" >&2
		rm -f "$errf"
		verdict ENV "package selection failed for $BASE..HEAD"
	fi
	rm -f "$errf"
	while IFS= read -r p; do [ -n "$p" ] && SELECTED+=("$p"); done <<<"$out"
	pkgs_label=$(IFS=,; printf '%s' "${SELECTED[*]-}")
	[ -n "$pkgs_label" ] || pkgs_label=-
}

if [ "$mode" = list ]; then
	select_pkgs 0
	[ "${#SELECTED[@]}" = 0 ] || printf '%s\n' "${SELECTED[@]}"
	exit 0
fi

# check_go_files INCLUDE_UNTRACKED — gofmt and vet the Go files the diff touches.
check_go_files() {
	local include=$1 files dirs unfmt vo
	if [ "$include" = 1 ]; then
		files=$({ git -C "$ROOT" diff --name-only --diff-filter=d "$mb"; git -C "$ROOT" ls-files -o --exclude-standard; } | /usr/bin/grep -E '\.go$' || true)
	else
		files=$(git -C "$ROOT" diff --name-only --diff-filter=d "$mb" HEAD -- '*.go')
	fi
	[ -n "$files" ] || return 0
	unfmt=$(cd "$ROOT" && gofmt -l $files) || verdict ENV "gofmt could not run"
	[ -z "$unfmt" ] || { echo "gofmt: $unfmt" >&2; verdict FAIL "gofmt"; }
	dirs=$(for f in $files; do echo "./$(dirname "$f")"; done | sort -u | sed 's|^\./\.$|.|')
	vo=$(cd "$ROOT" && "$GO" vet $dirs 2>&1) || {
		printf '%s\n' "$vo" | tail -15
		verdict FAIL "go vet"
	}
}

# run_pkgs — run every selected package whole, appended to $out; stop at the
# first failure. No -count: the shared test cache stays on.
run_pkgs() {
	local p args
	: > "$out"
	rc=0
	for p in "${SELECTED[@]}"; do
		args=(test -json -failfast -timeout "$TIMEOUT")
		[ -n "$TAGS" ] && args+=(-tags "$TAGS")
		"$GO" "${args[@]}" "$p" >> "$out"
		rc=$?
		[ "$rc" = 0 ] || break
	done
}

# finish — classify the go test -json stream in $out and print the verdict.
finish() {
	if [ "$rc" = 137 ] || [ "$rc" = 143 ] || /usr/bin/grep -q 'panic: test timed out' "$out"; then
		echo "test-targets: over budget (timeout $TIMEOUT or killed); stream in $out" >&2
		verdict BUDGET 5
	fi
	if [ "$rc" = 0 ]; then
		n=$(jq -Rn '[inputs | fromjson? | select(type == "object" and .Action == "run" and has("Test"))] | length' "$out" 2>/dev/null || echo 0)
		if [ "${n:-0}" = 0 ]; then
			if [ "$mode" = one ]; then
				echo "test-targets: RUN='$RUN' matched no test in $PKG" >&2
			else
				echo "test-targets: the selected packages ran no test" >&2
			fi
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
}

# ---- test-one -----------------------------------------------------------------
if [ "$mode" = one ]; then
	out=.oracle-build/test/one-${sha%+dirty}.json
	mkdir -p "$(dirname "$out")"
	args=(test -json -failfast -timeout "$TIMEOUT" -run "$RUN")
	[ -n "$SKIP" ] && args+=(-skip "$SKIP")
	[ -n "$TAGS" ] && args+=(-tags "$TAGS")
	[ -n "$V" ] && [ "$V" != 0 ] && args+=(-v)
	args+=("$PKG")
	"$GO" "${args[@]}" > "$out"
	rc=$?
	finish
fi

# ---- test-targeted ------------------------------------------------------------
if [ "$mode" = targeted ]; then
	select_pkgs 0
	check_go_files 1
	if [ "${#SELECTED[@]}" = 0 ]; then
		echo "test-targets: no packages selected for $BASE..HEAD" >&2
		verdict PASS 0
	fi
	out=.oracle-build/test/targeted-${sha%+dirty}.json
	mkdir -p "$(dirname "$out")"
	run_pkgs
	finish
fi

# ---- test-smart ---------------------------------------------------------------
# The steps a seat can act on, fail-fast, on a committed tree: the cheap checks
# first (gofmt/vet, shell syntax, fork pins, licence guards, script smokes),
# then the affected packages. precheck is an alias for this mode.
if [ "$mode" = smart ]; then
	[ -z "$(git -C "$ROOT" status --porcelain --untracked-files=no)" ] ||
		verdict ENV "commit first: test-smart checks a commit, not a dirty tree"
	select_pkgs 1
	check_go_files 0

	shfiles=$(git -C "$ROOT" diff --name-only --diff-filter=d "$mb" HEAD -- '*.sh')
	for f in $shfiles; do
		bash -n "$ROOT/$f" || verdict FAIL "shell syntax: $f"
	done

	logdir=.oracle-build/test
	mkdir -p "$logdir"
	pins_log=$logdir/smart-${sha%+dirty}.pins.log
	boundary_log=$logdir/smart-${sha%+dirty}.boundary.log
	smoke_log=$logdir/smart-${sha%+dirty}.smoke.log

	bash "$ROOT/scripts/check-fork-pins.sh" >"$pins_log" 2>&1
	rc=$?
	if [ "$rc" != 0 ]; then
		tail -20 "$pins_log" >&2
		if [ "$rc" = 2 ]; then verdict ENV "fork pin check could not run"; else verdict FAIL "fork pins"; fi
	fi
	make -C "$ROOT" boundary >"$boundary_log" 2>&1
	rc=$?
	if [ "$rc" != 0 ]; then
		tail -20 "$boundary_log" >&2
		verdict FAIL "boundary"
	fi
	make -C "$ROOT" smoke >"$smoke_log" 2>&1
	rc=$?
	if [ "$rc" != 0 ]; then
		tail -20 "$smoke_log" >&2
		verdict FAIL "smoke"
	fi

	if [ "${#SELECTED[@]}" = 0 ]; then
		echo "test-targets: no packages selected for $BASE..HEAD" >&2
		verdict PASS 0
	fi
	out=$logdir/smart-${sha%+dirty}.json
	run_pkgs
	finish
fi
