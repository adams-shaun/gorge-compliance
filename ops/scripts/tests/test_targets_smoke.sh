#!/usr/bin/env bash
# test_targets_smoke.sh — prove scripts/test-targets.sh one|targeted|smart|list
# validates, preflights and classifies as documented, with stub go, git,
# systemd-run, make and gh on PATH (no real test run, no systemd, no corpus, no
# network).
#
#   ops/scripts/tests/test_targets_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT=$ROOT/scripts/test-targets.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/test-targets-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

BIN=$TMP/bin
mkdir -p "$BIN" "$TMP/cards/cardsfolder" "$TMP/empty/cardsfolder" "$TMP/work"
touch "$TMP/cards/cardsfolder/a.txt"

# go: `run` prints the selector's output (STUB_SELECT); `vet` prints STUB_VET;
# every other call prints STUB_EVENTS (a go test -json stream) and exits STUB_RC.
cat > "$BIN/go" <<'STUB'
#!/usr/bin/env bash
echo "go $*" >> "$STUB_LOG"
case "${1:-}" in
run)
	[ -n "${STUB_SELECT+x}" ] && printf '%s\n' "$STUB_SELECT"
	exit "${STUB_SELECT_RC:-0}"
	;;
vet)
	[ -n "${STUB_VET:-}" ] && printf '%s\n' "$STUB_VET"
	exit "${STUB_VET_RC:-0}"
	;;
list)
	printf '%s\n' "${STUB_LIST:-}"
	exit 0
	;;
esac
[ -n "${STUB_EVENTS:-}" ] && printf '%b\n' "$STUB_EVENTS"
exit "${STUB_RC:-0}"
STUB
# git: enough of rev-parse/status/merge-base/diff/ls-files for the selector and
# the gofmt steps. Handles a leading -C DIR. STUB_DIFF_ALL is the no-pathspec
# diff (a non-Go path here, so the gofmt step sees nothing).
cat > "$BIN/git" <<'STUB'
#!/usr/bin/env bash
echo "git $*" >> "$STUB_LOG"
[ "${1:-}" = "-C" ] && shift 2
case "${1:-}" in
rev-parse)
	if [[ "$*" == *"--short"* ]]; then echo "${STUB_SHA:-abc1234567}"; exit 0; fi
	if [[ "$*" == *"--verify"* ]]; then exit "${STUB_BASE_RC:-0}"; fi
	echo "${STUB_SHA:-abc1234567}"
	exit 0
	;;
status)
	[ -n "${STUB_DIRTY:-}" ] && echo " M file.go"
	exit 0
	;;
merge-base)
	[[ "$*" == *"--is-ancestor"* ]] && exit 0
	echo "${STUB_MB:-deadbeef}"
	exit 0
	;;
diff)
	if [[ "$*" == *"*.go"* ]]; then
		[ -n "${STUB_DIFF_GO:-}" ] && printf '%s\n' "$STUB_DIFF_GO"
	elif [[ "$*" == *"*.sh"* ]]; then
		[ -n "${STUB_DIFF_SH:-}" ] && printf '%s\n' "$STUB_DIFF_SH"
	else
		[ -n "${STUB_DIFF_ALL:-}" ] && printf '%s\n' "$STUB_DIFF_ALL"
	fi
	exit 0
	;;
ls-files | show-ref)
	exit 0
	;;
*)
	exit 0
	;;
esac
STUB
# systemd-run: succeeds for the probe; otherwise drops its flags up to `env` and runs the rest.
cat > "$BIN/systemd-run" <<'STUB'
#!/usr/bin/env bash
echo "systemd-run $*" >> "$STUB_LOG"
[ "${STUB_NOSYSTEMD:-}" = 1 ] && exit 1
[ "${@: -1}" = true ] && exit 0
while [ "$1" != env ]; do shift; done
exec "$@"
STUB
# make: test-smart's cheap steps; nothing is really built here.
cat > "$BIN/make" <<'STUB'
#!/usr/bin/env bash
echo "make $*" >> "$STUB_LOG"
exit "${STUB_MAKE_RC:-0}"
STUB
# gh: answer the fork-pin API with the commit the URL names.
cat > "$BIN/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh $*" >> "$STUB_LOG"
[ "${1:-}" = api ] && { printf '%s\n' "${2##*/}"; exit 0; }
exit 1
STUB
chmod +x "$BIN"/*

export PATH="$BIN:$PATH" STUB_LOG=$TMP/calls.log
export TEST_TARGETS_CARDS=$TMP/cards
unset TEST_TARGETS_SCOPED PKG RUN SKIP V TIMEOUT TAGS STUB_EVENTS STUB_RC STUB_NOSYSTEMD
unset STUB_SHA STUB_MB STUB_DIRTY STUB_DIFF_ALL STUB_DIFF_GO STUB_DIFF_SH STUB_SELECT STUB_SELECT_RC
unset STUB_VET STUB_VET_RC STUB_LIST STUB_MAKE_RC STUB_BASE_RC BASE FORGE_ORACLE_REF XMAGE_REF

RUNEV='{"Action":"run","Package":"p","Test":"TestA"}'
PASSEV='{"Action":"pass","Package":"p","Test":"TestA"}'

# tm MODE NAME WANT_RC WANT_SUBSTR [VAR=val ...] — run the script, check exit,
# stdout's last line and stderr.
tm() {
	local mode=$1 name=$2 want=$3 sub=$4
	shift 4
	: > "$STUB_LOG"
	local o e rc
	o=$(cd "$TMP/work" && env "$@" bash "$SCRIPT" "$mode" 2>"$TMP/err"); rc=$?
	e=$(cat "$TMP/err")
	check "$name exits $want" $((rc == want ? 0 : 1)) "rc=$rc out=$o err=$e"
	check "  ...last stdout line is the verdict / names $sub" $([[ $(tail -n 1 <<<"$o") == test-$mode:* && "$o$e" == *"$sub"* ]] && echo 0 || echo 1) "out=$o err=$e"
	LAST=$o
	ERR=$e
	RC=$rc
}
t() { tm one "$@"; }

t "missing PKG" 2 PKG RUN=x
t "PKG=./..." 2 PKG PKG=./... RUN=x
t "PKG with a ... suffix" 2 PKG PKG=./a/... RUN=x
t "PKG not relative" 2 PKG PKG=/etc RUN=x
t "missing RUN" 2 RUN PKG=./p
t "TIMEOUT=9m" 2 TIMEOUT PKG=./p RUN=x TIMEOUT=9m
t "TIMEOUT=0s" 2 TIMEOUT PKG=./p RUN=x TIMEOUT=0s
t "TIMEOUT=2h" 2 TIMEOUT PKG=./p RUN=x TIMEOUT=2h
t "bad TAGS" 2 TAGS PKG=./p RUN=x 'TAGS=a b'
check "usage errors never reached go" $([ ! -s "$STUB_LOG" ] || ! /usr/bin/grep -q '^go ' "$STUB_LOG" && echo 0 || echo 1)

t "empty cards dir" 3 "make cards" PKG=./p RUN=x TEST_TARGETS_CARDS="$TMP/empty"
t "missing cards dir" 3 "make cards" PKG=./p RUN=x TEST_TARGETS_CARDS="$TMP/nope"
check "ENV never reached go" $(! /usr/bin/grep -q '^go ' "$STUB_LOG" && echo 0 || echo 1)

t "pass" 0 "test-one: PASS" PKG=./p RUN=TestA STUB_EVENTS="$RUNEV\n$PASSEV"
check "  ...verdict names the package" $([[ $LAST == *" ./p "* ]] && echo 0 || echo 1) "$LAST"
check "  ...scoped under systemd-run with the cap" $(/usr/bin/grep -q 'systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env TEST_TARGETS_SCOPED=1 GOMAXPROCS=4 GOMEMLIMIT=3GiB' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...go test -json -failfast, no -count" $(/usr/bin/grep -q '^go test -json -failfast -timeout 2m -run TestA ./p$' "$STUB_LOG" && ! /usr/bin/grep '^go ' "$STUB_LOG" | /usr/bin/grep -q -- '-count' && echo 0 || echo 1) "$(/usr/bin/grep '^go' "$STUB_LOG")"
check "  ...stream saved under .oracle-build/test" $(ls "$TMP"/work/.oracle-build/test/one-*.json >/dev/null 2>&1 && echo 0 || echo 1)

t "flags pass through" 0 "test-one: PASS" PKG=./p RUN=TestA SKIP=Slow TAGS=a,b V=1 TIMEOUT=90s STUB_EVENTS="$RUNEV\n$PASSEV"
check "  ...-skip -tags -v -timeout" $(/usr/bin/grep -q '^go test -json -failfast -timeout 90s -run TestA -skip Slow -tags a,b -v ./p$' "$STUB_LOG" && echo 0 || echo 1) "$(/usr/bin/grep '^go' "$STUB_LOG")"

t "no run event" 4 "test-one: EMPTY" PKG=./p RUN=TestNone STUB_EVENTS='{"Action":"start","Package":"p"}\n{"Action":"run","Package":"p"}\n{"Action":"pass","Package":"p"}'

t "go exits 1" 1 "test-one: FAIL" PKG=./p RUN=TestA STUB_RC=1 \
	STUB_EVENTS="$RUNEV\n"'{"Action":"output","Package":"p","Test":"TestA","Output":"    a_test.go:3: boom\\n"}\n{"Action":"fail","Package":"p","Test":"TestA"}'
check "  ...prints the failing test's output" $([[ $LAST == *"a_test.go:3: boom"* ]] && echo 0 || echo 1) "$LAST"

t "timeout panic" 5 "test-one: BUDGET" PKG=./p RUN=TestA STUB_RC=1 \
	STUB_EVENTS='{"Action":"output","Package":"p","Output":"panic: test timed out after 2m0s\\n"}'
t "killed (143)" 5 "test-one: BUDGET" PKG=./p RUN=TestA STUB_RC=143

t "no user systemd runs uncapped" 0 "no user systemd; running uncapped" PKG=./p RUN=TestA STUB_NOSYSTEMD=1 STUB_EVENTS="$RUNEV\n$PASSEV"
check "  ...and still ran go" $(/usr/bin/grep -q '^go test' "$STUB_LOG" && echo 0 || echo 1)

# ---- targeted ----------------------------------------------------------------
tm targeted "targeted pass" 0 "test-targeted: PASS" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p STUB_EVENTS="$RUNEV\n$PASSEV"
check "  ...selects through cmd/testselect" $(/usr/bin/grep -q '^go run ./cmd/testselect -root . -files -$' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...BASE defaults to develop" $(/usr/bin/grep -q 'merge-base develop HEAD' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...runs the selected package" $(/usr/bin/grep -q '^go test -json -failfast -timeout 2m ./p$' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...verdict names the package" $([[ $LAST == *" ./p "* ]] && echo 0 || echo 1) "$LAST"

tm targeted "targeted empty selection" 0 "test-targeted: PASS" STUB_DIFF_ALL=
check "  ...names no packages" $([[ $LAST == *" - "* ]] && echo 0 || echo 1) "$LAST"

tm targeted "targeted selection failure" 3 "selection failed" \
	STUB_DIFF_ALL=README.md STUB_SELECT_RC=2

tm targeted "targeted fail" 1 "test-targeted: FAIL" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p STUB_RC=1 \
	STUB_EVENTS="$RUNEV\n"'{"Action":"output","Package":"p","Test":"TestA","Output":"    a_test.go:3: boom\\n"}\n{"Action":"fail","Package":"p","Test":"TestA"}'
check "  ...prints the failing test's output" $([[ $LAST == *"a_test.go:3: boom"* ]] && echo 0 || echo 1) "$LAST"

tm targeted "targeted no tests" 4 "test-targeted: EMPTY" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p \
	STUB_EVENTS='{"Action":"start","Package":"p"}\n{"Action":"pass","Package":"p"}'

tm targeted "targeted killed (143)" 5 "test-targeted: BUDGET" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p STUB_RC=143

# ---- smart -------------------------------------------------------------------
tm smart "smart dirty tree" 3 "commit first" STUB_DIRTY=1
check "  ...never reached make" $(! /usr/bin/grep -q '^make ' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"

tm smart "smart pass" 0 "test-smart: PASS" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p STUB_EVENTS="$RUNEV\n$PASSEV"
check "  ...checks the fork pins" $(/usr/bin/grep -q '^gh api repos/adams-shaun/forge/commits/' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...runs the licence guards" $(/usr/bin/grep -q '^make -C .* boundary$' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...runs the script smokes" $(/usr/bin/grep -q '^make -C .* smoke$' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"
check "  ...runs the affected package" $(/usr/bin/grep -q '^go test -json -failfast -timeout 2m ./p$' "$STUB_LOG" && echo 0 || echo 1) "$(cat "$STUB_LOG")"

tm smart "smart boundary failure" 1 "boundary" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p STUB_MAKE_RC=1

tm smart "smart test failure" 1 "test-smart: FAIL" \
	STUB_DIFF_ALL=README.md STUB_SELECT=./p STUB_RC=1 \
	STUB_EVENTS="$RUNEV\n"'{"Action":"output","Package":"p","Test":"TestA","Output":"    a_test.go:3: boom\\n"}\n{"Action":"fail","Package":"p","Test":"TestA"}'

# ---- list --------------------------------------------------------------------
: > "$STUB_LOG"
o=$(cd "$TMP/work" && env STUB_DIFF_ALL=README.md STUB_SELECT=./p bash "$SCRIPT" list 2>"$TMP/err"); rc=$?
e=$(cat "$TMP/err")
check "list exits 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc out=$o err=$e"
check "  ...prints the selection" $([[ "$o" == "./p" ]] && echo 0 || echo 1) "$o"

[ "$fails" = 0 ] && echo "test_targets_smoke: all passed" || echo "test_targets_smoke: $fails FAILED"
exit $((fails != 0))
