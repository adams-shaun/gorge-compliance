#!/usr/bin/env bash
# test_select_smoke.sh — prove scripts/test-select.sh computes the merge base,
# forwards it to cmd/testselect and exits 2 (fails closed) on usage, a missing
# git/go, a failed merge-base and a failed go run. Stub git/go on PATH; no real
# repository, build or test run.
#
#   ops/scripts/tests/test_select_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/test-select-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

REPO=$TMP/repo
mkdir -p "$REPO/scripts" "$REPO/cmd/testselect" "$REPO/internal/testselect" "$TMP/bin"
cp "$ROOT/scripts/test-select.sh" "$REPO/scripts/test-select.sh"

cat > "$TMP/bin/git" <<'STUB'
#!/usr/bin/env bash
echo "git GOWORK=${GOWORK:-} $*" >> "$STUB_LOG"
if [ "$1" = "merge-base" ]; then
	[ "${STUB_MB_RC:-0}" = 0 ] || exit "${STUB_MB_RC}"
	echo "${STUB_MB:-deadbeef}"
	exit 0
fi
exit 1
STUB
cat > "$TMP/bin/go" <<'STUB'
#!/usr/bin/env bash
echo "go GOWORK=${GOWORK:-} $*" >> "$STUB_LOG"
[ "${STUB_GO_RC:-0}" = 0 ] || exit "${STUB_GO_RC}"
printf '%s\n' ./oracle/compliance/adopt ./oracle/compliance/gate ./oracle/compliance/oraclegen
STUB
chmod +x "$TMP/bin/git" "$TMP/bin/go"

LOG=$TMP/calls.log
RC=0
OUT=
ERR=

# run ARGS... — run the wrapper in the fixture repo with the stubs on PATH.
# RUN_PATH overrides PATH; STUB_MB, STUB_MB_RC and STUB_GO_RC reach the stubs.
run() {
	: > "$LOG"
	( cd "$REPO" && env \
		PATH="${RUN_PATH:-$TMP/bin:$PATH}" \
		STUB_LOG="$LOG" \
		STUB_MB="${STUB_MB:-deadbeef}" \
		STUB_MB_RC="${STUB_MB_RC:-0}" \
		STUB_GO_RC="${STUB_GO_RC:-0}" \
		bash "$REPO/scripts/test-select.sh" "$@" ) >"$TMP/out" 2>"$TMP/err"
	RC=$?
	OUT=$(cat "$TMP/out")
	ERR=$(cat "$TMP/err")
}

run
check "no BASE exits 2" $([ "$RC" = 2 ] && echo 0 || echo 1) "rc=$RC out=$OUT err=$ERR"
check "  ...names the usage" $([[ "$ERR" == *"usage: scripts/test-select.sh BASE"* ]] && echo 0 || echo 1) "$ERR"
check "  ...never reached git or go" $([ ! -s "$LOG" ] && echo 0 || echo 1) "$(cat "$LOG")"

run develop
check "BASE..HEAD exits 0" $([ "$RC" = 0 ] && echo 0 || echo 1) "rc=$RC out=$OUT err=$ERR"
check "  ...prints the selected packages" $([[ "$OUT" == $'./oracle/compliance/adopt\n./oracle/compliance/gate\n./oracle/compliance/oraclegen' ]] && echo 0 || echo 1) "$OUT"
check "  ...computes the merge base against HEAD" $(/usr/bin/grep -qx 'git GOWORK= merge-base -- develop HEAD' "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"
check "  ...passes it to the CLI with -head" $(/usr/bin/grep -qx 'go GOWORK=off run ./cmd/testselect -root . -base deadbeef -head HEAD' "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

run develop feature
check "explicit HEAD is forwarded" $(/usr/bin/grep -qx 'git GOWORK= merge-base -- develop feature' "$LOG" &&
	/usr/bin/grep -qx 'go GOWORK=off run ./cmd/testselect -root . -base deadbeef -head feature' "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

STUB_MB_RC=1
run develop
STUB_MB_RC=0
check "failed merge-base exits 2" $([ "$RC" = 2 ] && echo 0 || echo 1) "rc=$RC out=$OUT err=$ERR"
check "  ...says there is no merge base" $([[ "$ERR" == *"no merge base"* ]] && echo 0 || echo 1) "$ERR"

STUB_GO_RC=1
run develop
STUB_GO_RC=0
check "failed go run exits 2" $([ "$RC" = 2 ] && echo 0 || echo 1) "rc=$RC out=$OUT err=$ERR"
check "  ...still printed nothing" $([ -z "$OUT" ] && echo 0 || echo 1) "$OUT"

# /usr/local/go, the seat shim and ~/go are off this PATH; /usr/bin has the
# builtins the wrapper needs, so only go should be missing.
if PATH="/usr/bin:/bin" command -v go >/dev/null 2>&1; then
	printf 'skip missing go: the host has go outside the seat PATH\n'
else
	RUN_PATH="/usr/bin:/bin"
	run develop
	RUN_PATH=
	check "missing go exits 2" $([ "$RC" = 2 ] && echo 0 || echo 1) "rc=$RC out=$OUT err=$ERR"
	check "  ...names the missing tool" $([[ "$ERR" == *"go not found on PATH"* ]] && echo 0 || echo 1) "$ERR"
fi

[ "$fails" = 0 ] && echo "test_select_smoke: all passed" || echo "test_select_smoke: $fails FAILED"
exit $((fails != 0))
