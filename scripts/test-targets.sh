#!/usr/bin/env bash
# test-targets.sh <one|whole|targeted|smart|list>: capped, corpus-checked test runs
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
#   make boundary                      this script's whole mode on the licence
#                                      guards: one package run whole with
#                                      -count=1 (the guard must re-execute, not
#                                      replay the test cache) and no corpus
#                                      preflight (the guards run corpus-less)
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
# Two bounded-run rules keep the seat targets from becoming `go test ./...`:
#   * a pin change (go.mod/go.sum/GORGE_PIN) makes the selector expand to every
#     package. That is the merge gate's full suite, not a seat's; the mode exits
#     ENV 3 with the package count instead of running it.
#   * SLOW_PKGS (default ./oracle/compliance/oraclegen/templates, the one
#     ~3-minute package) runs only SLOW_RUN, a named test, never the whole
#     package. The merge gate still runs the package whole.
# Both rules are re-derived from the selection itself, not a hand-kept copy of
# cmd/testselect's rules.
#
# Overridable for the smoke: TEST_TARGETS_GO (go), TEST_TARGETS_SYSTEMD_RUN
# (systemd-run), TEST_TARGETS_CARDS (.cards), TEST_TARGETS_SLOW_PKGS,
# TEST_TARGETS_SLOW_RUN.
set -uo pipefail

mode=${1:-}
case $mode in
one | whole | targeted | smart | list) ;;
*)
	echo "test-targets.sh: usage: test-targets.sh one|whole|targeted|smart|list" >&2
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
SLOW_PKGS=${TEST_TARGETS_SLOW_PKGS:-./oracle/compliance/oraclegen/templates}
SLOW_RUN=${TEST_TARGETS_SLOW_RUN:-^TestTemplatesAreDistinctAndVersioned$}

t0=$SECONDS
sha=nogit
if s=$(git rev-parse --short=10 HEAD 2>/dev/null); then
	sha=$s
	[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ] && sha=$sha+dirty
fi

SELECTED=()
pkgs_label=
all_selection=0

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
	whole) echo "test-targets: make boundary (scripts/test-targets.sh whole PKG=./path [RUN='re'] [V=1])" >&2 ;;
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
if [ "$mode" = one ] || [ "$mode" = whole ]; then
	[[ $PKG =~ ^\./[A-Za-z0-9_./-]+$ ]] && [[ $PKG != *...* ]] ||
		usage "PKG must be one package path like ./oracle/xengine/follower (got '${PKG}'; no '...' patterns)"
	[ "$mode" = whole ] || [ -n "$RUN" ] || usage "RUN is required (a go test -run regexp)"
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
# whole is exempt: it runs one named package exactly as asked, so that
# package's corpus need is the caller's assertion, not something this script
# can judge. Its only user today is make boundary, whose licence guards are
# designed to run corpus-less (internal/boundary skips its byte-identity rule
# when no corpus is present and still runs its content and path rules) and
# which the README documents as needing no corpus — so requiring .cards here
# would red a link-less worktree on a guard that passes. The modes that fan
# out over a selection keep the check: there, a green run of silently
# skipping tests is a false green.
if [ "$mode" != list ] && [ "$mode" != whole ]; then
	# Every corpus test skips without the corpus, so a green run would be a false green.
	cards=${TEST_TARGETS_CARDS:-$ROOT/.cards}/cardsfolder
	if [ ! -d "$cards" ] || [ -z "$(ls -A "$cards" 2>/dev/null | head -n 1)" ]; then
		echo "test-targets: corpus missing or empty at $cards: run 'make cards' (corpus tests skip without it, so a green run would be a false green)" >&2
		verdict ENV 3
	fi
fi

mb=
if [ "$mode" != one ] && [ "$mode" != whole ]; then
	git -C "$ROOT" rev-parse --verify -q "$BASE^{commit}" >/dev/null ||
		verdict ENV "base $BASE does not resolve in this worktree"
	mb=$(git -C "$ROOT" merge-base "$BASE" HEAD) ||
		verdict ENV "no merge base for $BASE..HEAD"
	git -C "$ROOT" merge-base --is-ancestor "$BASE" HEAD ||
		echo "test-targets: note: HEAD is not on top of $BASE; the landing rebases, so a conflict with it is not covered" >&2
fi

# selection_is_all — true when SELECTED is exactly `go list ./...`, which is
# how cmd/testselect reports its All marker (a pin bump) after expanding it.
# Comparing with go's own list keeps this rule structural: a new pin file in
# the selector cannot leave a full-suite selection looking like a normal one.
selection_is_all() {
	local module all selected imp
	module=$(cd "$ROOT" && GOWORK=off "$GO" list -m -f '{{.Path}}' 2>/dev/null) || return 1
	[ -n "$module" ] || return 1
	all=$(cd "$ROOT" && GOWORK=off "$GO" list ./... 2>/dev/null) || return 1
	[ -n "$all" ] || return 1
	all=$(printf '%s\n' "$all" | while IFS= read -r imp; do
		case $imp in
		"$module") printf '.\n' ;;
		"$module"/*) printf './%s\n' "${imp#"$module"/}" ;;
		esac
	done | sort)
	selected=$(printf '%s\n' "${SELECTED[@]}" | sort)
	[ "$selected" = "$all" ]
}

# select_pkgs COMMITTED [CHECK_ALL] — fill SELECTED with the packages the diff
# touches. COMMITTED=1 diffs BASE..HEAD (test-smart checks a commit); 0 also
# reads the working tree and untracked files (test-targeted's inner loop).
# CHECK_ALL=1 marks a selection that is every package (a pin bump).
#
# --no-renames is load-bearing on every diff: with rename detection on, a
# producer file moved out of its prefix appears only under the new path, so
# the ratchet packages the old path selected would be silently dropped.
select_pkgs() {
	local committed=$1 check_all=${2:-0} files out rc p
	if [ "$committed" = 1 ]; then
		files=$(git -C "$ROOT" diff --name-only --no-renames "$mb" HEAD)
	else
		files=$({ git -C "$ROOT" diff --name-only --no-renames "$mb"; git -C "$ROOT" ls-files -o --exclude-standard; } | sort -u)
	fi
	SELECTED=()
	all_selection=0
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
	if [ "$check_all" = 1 ] && [ "${#SELECTED[@]}" -gt 0 ] && selection_is_all; then
		all_selection=1
		pkgs_label='./...'
	fi
}

if [ "$mode" = list ]; then
	select_pkgs 0
	[ "${#SELECTED[@]}" = 0 ] || printf '%s\n' "${SELECTED[@]}"
	exit 0
fi

# check_go_files INCLUDE_UNTRACKED — gofmt and vet the Go files the diff touches.
check_go_files() {
	local include=$1 files dirs unfmt vo grc
	if [ "$include" = 1 ]; then
		files=$({ git -C "$ROOT" diff --name-only --no-renames --diff-filter=d "$mb"; git -C "$ROOT" ls-files -o --exclude-standard; } | /usr/bin/grep -E '\.go$' || true)
	else
		files=$(git -C "$ROOT" diff --name-only --no-renames --diff-filter=d "$mb" HEAD -- '*.go')
	fi
	[ -n "$files" ] || return 0
	command -v gofmt >/dev/null 2>&1 || verdict ENV "gofmt not found on PATH"
	unfmt=$(cd "$ROOT" && gofmt -l $files)
	grc=$?
	if [ "$grc" != 0 ]; then # a syntax error is the changed code's failure, not a missing tool
		printf '%s\n' "$unfmt" >&2
		verdict FAIL "gofmt"
	fi
	[ -z "$unfmt" ] || { echo "gofmt: $unfmt" >&2; verdict FAIL "gofmt"; }
	dirs=$(for f in $files; do echo "./$(dirname "$f")"; done | sort -u | sed 's|^\./\.$|.|')
	vo=$(cd "$ROOT" && "$GO" vet $dirs 2>&1) || {
		printf '%s\n' "$vo" | tail -15
		verdict FAIL "go vet"
	}
}

# is_slow_pkg PKG — the packages the whole-run inner loop must not run whole.
is_slow_pkg() {
	local p
	for p in $SLOW_PKGS; do [ "$p" = "$1" ] && return 0; done
	return 1
}

# run_pkgs — run the selection, appended to $out; stop at the first failure.
# A slow package runs only its named guard (SLOW_RUN), never whole. No -count:
# the shared test cache stays on.
run_pkgs() {
	local p args
	: > "$out"
	rc=0
	for p in "${SELECTED[@]}"; do
		args=(test -json -failfast -timeout "$TIMEOUT")
		if is_slow_pkg "$p"; then
			echo "test-targets: $p is slow: running only -run '$SLOW_RUN' (the whole package is the merge gate's)" >&2
			args+=(-run "$SLOW_RUN")
		fi
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

# ---- test-whole ----------------------------------------------------------------
# One package run whole, with the shared test cache bypassed (-count=1):
# `make boundary`'s licence guards must re-execute on every call rather than
# replay a cached PASS, which is exactly what makes this mode the script's
# exception to the no--count seat rule. RUN is optional: with none the whole
# package is the request (make boundary), and when set it passes through as
# `-run "$RUN"` exactly like one mode, so the same sanctioned path also
# filters a package (make oracle-audit's composite regexp, make mtgish-xc's
# ledger test). V adds -v the way one mode does; the stream is saved either
# way. Like one mode it takes only a single relative package path and skips
# the BASE merge-base checks (nothing is diffed) and the corpus preflight
# (see the comment there): the caller names a specific package, so that
# package's corpus need is the caller's business.
if [ "$mode" = whole ]; then
	out=.oracle-build/test/whole-${sha%+dirty}.json
	mkdir -p "$(dirname "$out")"
	args=(test -json -failfast -count=1 -timeout "$TIMEOUT")
	[ -n "$RUN" ] && args+=(-run "$RUN")
	[ -n "$TAGS" ] && args+=(-tags "$TAGS")
	[ -n "$V" ] && [ "$V" != 0 ] && args+=(-v)
	args+=("$PKG")
	"$GO" "${args[@]}" > "$out"
	rc=$?
	finish
fi

# ---- test-targeted ------------------------------------------------------------
if [ "$mode" = targeted ]; then
	select_pkgs 0 1
	check_go_files 1
	if [ "$all_selection" = 1 ]; then
		echo "test-targets: $BASE..HEAD changes a pin: the selector expands to every package (${#SELECTED[@]}); the seat target does not run the full suite -- the merge gate does" >&2
		verdict ENV 3
	fi
	if [ "${#SELECTED[@]}" = 0 ]; then
		echo "test-targets: no packages selected for $BASE..HEAD (a non-Go diff; the merge gate still runs the full suite)" >&2
		verdict EMPTY 4
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
	select_pkgs 1 1
	check_go_files 0

	shfiles=$(git -C "$ROOT" diff --name-only --no-renames --diff-filter=d "$mb" HEAD -- '*.sh')
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

	if [ "$all_selection" = 1 ]; then
		echo "test-targets: $BASE..HEAD changes a pin: the selector expands to every package (${#SELECTED[@]}); the seat target does not run the full suite -- the merge gate does" >&2
		verdict ENV 3
	fi
	if [ "${#SELECTED[@]}" = 0 ]; then
		# Unlike test-targeted, smart has already run its checks (shell syntax,
		# fork pins, licence guards, smokes) by here, so a diff with no Go
		# package behind it (shell, docs) is a pass of those checks, not an
		# empty run: EMPTY would be a false red.
		echo "test-targets: no Go package selected for $BASE..HEAD; the shell, pin, licence and smoke checks above passed" >&2
		verdict PASS 0
	fi
	out=$logdir/smart-${sha%+dirty}.json
	run_pkgs
	finish
fi
