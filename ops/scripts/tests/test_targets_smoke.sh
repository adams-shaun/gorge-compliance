#!/usr/bin/env bash
# test_targets_smoke.sh — prove scripts/test-targets.sh one validates, preflights
# and classifies as documented, with stub go and systemd-run on PATH (no real
# test run, no systemd, no corpus).
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

# go: prints the events in STUB_EVENTS (one per line) and exits STUB_RC; logs its args.
cat > "$BIN/go" <<'STUB'
#!/usr/bin/env bash
echo "go $*" >> "$STUB_LOG"
[ -n "${STUB_EVENTS:-}" ] && printf '%b\n' "$STUB_EVENTS"
exit "${STUB_RC:-0}"
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
chmod +x "$BIN"/*

export PATH="$BIN:$PATH" STUB_LOG=$TMP/calls.log
export TEST_TARGETS_CARDS=$TMP/cards
unset TEST_TARGETS_SCOPED PKG RUN SKIP V TIMEOUT TAGS STUB_EVENTS STUB_RC STUB_NOSYSTEMD

RUNEV='{"Action":"run","Package":"p","Test":"TestA"}'
PASSEV='{"Action":"pass","Package":"p","Test":"TestA"}'

# t NAME WANT_RC WANT_SUBSTR [VAR=val ...] — run the script, check exit, stdout's last line and stderr.
t() {
	local name=$1 want=$2 sub=$3
	shift 3
	: > "$STUB_LOG"
	local o e rc
	o=$(cd "$TMP/work" && env "$@" bash "$SCRIPT" one 2>"$TMP/err"); rc=$?
	e=$(cat "$TMP/err")
	check "$name exits $want" $((rc == want ? 0 : 1)) "rc=$rc out=$o err=$e"
	check "  ...last stdout line is the verdict / names $sub" $([[ $(tail -n 1 <<<"$o") == test-one:* && "$o$e" == *"$sub"* ]] && echo 0 || echo 1) "out=$o err=$e"
	LAST=$o
}

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
check "  ...go test -json -failfast, no -count" $(/usr/bin/grep -q '^go test -json -failfast -timeout 2m -run TestA ./p$' "$STUB_LOG" && ! /usr/bin/grep -q -- '-count' "$STUB_LOG" && echo 0 || echo 1) "$(/usr/bin/grep '^go' "$STUB_LOG")"
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

[ "$fails" = 0 ] && echo "test_targets_smoke: all passed" || echo "test_targets_smoke: $fails FAILED"
exit $((fails != 0))
