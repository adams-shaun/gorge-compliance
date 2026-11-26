#!/usr/bin/env bash
# check_forge_ref_in_driver_smoke.sh — prove scripts/check-forge-ref-in-driver.sh
# passes a FORGE_REF the driver contains and refuses one it does not, offline.
#
# gh is a stub on PATH answering the compare and contents APIs. Cases: behind
# (0), identical pins (0, no API call), diverged (1, names both short SHAs and
# the merge-first fix; the 2026-11-25 xproof outage), ahead (1), FORGE_REF read
# from gorge's Makefile at GORGE_PIN through the contents API (1 on diverged),
# GORGE_DIR wins over GORGE_PIN, a 422 compare (1), gh failing otherwise (2),
# a malformed pin (2).
#
#   ops/scripts/tests/check_forge_ref_in_driver_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT=$ROOT/scripts/check-forge-ref-in-driver.sh
BASH_BIN=$(command -v bash)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/check-forge-ref-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}
has() { [[ $1 == *"$2"* ]]; }

ORACLE=4385467a9eb5f1953a34498e07be47e24ae4b66c
FREF=5bc5851a54a0fbbbf7f1a38b48d8ff9b7714ddb2
GPIN=d6d98cb720257b19a78b89a9865cc5558f2341ee

BIN=$TMP/bin
LOG=$TMP/gh.log
mkdir -p "$BIN" "$TMP/repo/scripts" "$TMP/gorge"
cp "$SCRIPT" "$TMP/repo/scripts/"
printf 'FORGE_ORACLE_REF ?= %s\n' "$ORACLE" >"$TMP/repo/Makefile"
printf '%s\n' "$GPIN" >"$TMP/repo/GORGE_PIN"
printf 'FORGE_REF  ?= %s\n' "$FREF" >"$TMP/gorge/Makefile"

cat >"$BIN/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh $*" >>"$GH_LOG"
[ "$1" = api ] || exit 2
case "$2" in
repos/*/compare/*)
	case ${GH_COMPARE:-behind} in
	422) echo "gh: No commit found (HTTP 422)" >&2; exit 1 ;;
	err) echo "gh: rate limited (HTTP 403)" >&2; exit 1 ;;
	*) echo "${GH_COMPARE:-behind}" ;;
	esac
	;;
repos/*/contents/Makefile*) printf 'FORGE_REF  ?= %s\n' "$GH_FREF" | base64 ;;
*) exit 2 ;;
esac
STUB
chmod +x "$BIN/gh"

run() { (cd "$TMP/repo" && env GH_LOG="$LOG" PATH="$BIN:$PATH" "$@" "$BASH_BIN" scripts/check-forge-ref-in-driver.sh 2>&1); }

: >"$LOG"
out=$(run FORGE_REF=$FREF GH_COMPARE=behind); rc=$?
check "behind: contained (exit 0)" $([ "$rc" = 0 ] && has "$out" "is contained in FORGE_ORACLE_REF 4385467a9e" && echo 0 || echo 1) "rc=$rc $out"
check "  ...compares driver...FORGE_REF on the fork" $(/usr/bin/grep -qx "gh api repos/adams-shaun/forge/compare/$ORACLE...$FREF --jq .status" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

: >"$LOG"
out=$(run FORGE_REF=$ORACLE); rc=$?
check "identical pins: exit 0, no API call" $([ "$rc" = 0 ] && [ ! -s "$LOG" ] && echo 0 || echo 1) "rc=$rc $out"

out=$(run FORGE_REF=$FREF GH_COMPARE=diverged); rc=$?
check "diverged: exit 1" $([ "$rc" = 1 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...names both SHAs and the merge-first fix" $(has "$out" "FORGE_REF 5bc5851a54" && has "$out" "not contained in FORGE_ORACLE_REF 4385467a9e" && has "$out" "Merge 5bc5851a54 into the driver" && echo 0 || echo 1) "$out"

out=$(run FORGE_REF=$FREF GH_COMPARE=ahead); rc=$?
check "ahead (driver is an ancestor of FORGE_REF): exit 1" $([ "$rc" = 1 ] && echo 0 || echo 1) "rc=$rc $out"

: >"$LOG"
out=$(run GH_FREF=$FREF GH_COMPARE=diverged); rc=$?
check "GORGE_PIN path: reads gorge's Makefile at the pin and fails" $([ "$rc" = 1 ] && has "$out" "at GORGE_PIN d6d98cb720" && /usr/bin/grep -q "repos/adams-shaun/gorge/contents/Makefile?ref=$GPIN" "$LOG" && echo 0 || echo 1) "rc=$rc $out $(cat "$LOG")"

: >"$LOG"
out=$(run GORGE_DIR="$TMP/gorge" GH_COMPARE=behind); rc=$?
check "GORGE_DIR wins over GORGE_PIN" $([ "$rc" = 0 ] && has "$out" "gorge Makefile in $TMP/gorge" && ! /usr/bin/grep -q contents "$LOG" && echo 0 || echo 1) "rc=$rc $out"

out=$(run FORGE_REF=$FREF GH_COMPARE=422); rc=$?
check "unknown commit: exit 1" $([ "$rc" = 1 ] && has "$out" "is not in the adams-shaun/forge network" && echo 0 || echo 1) "rc=$rc $out"

out=$(run FORGE_REF=$FREF GH_COMPARE=err); rc=$?
check "API failure: exit 2, never a pass" $([ "$rc" = 2 ] && echo 0 || echo 1) "rc=$rc $out"

out=$(run FORGE_REF=deadbeef); rc=$?
check "malformed FORGE_REF: exit 2" $([ "$rc" = 2 ] && has "$out" "'deadbeef'" && echo 0 || echo 1) "rc=$rc $out"

[ "$fails" = 0 ] && echo "check_forge_ref_in_driver_smoke: PASS" || { echo "check_forge_ref_in_driver_smoke: $fails FAIL"; exit 1; }
