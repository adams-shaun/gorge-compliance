#!/usr/bin/env bash
# check_fork_pins_smoke.sh — prove scripts/check-fork-pins.sh fails closed,
# honours a command-line pin override, and reads the real Makefile's pins,
# without gh, the network or a forge checkout.
#
# gh is a stub on PATH; the script under test is copied next to a fixture
# Makefile so the missing-pin case can be exercised. Cases: every pin in the
# fork network (0), a miss (1 and the exact push-it-first line), a candidate
# env override, the real Makefile pins (hit and miss; `make precheck` is the
# full test-smart gate now, so its pin step calls this script with the same
# env overrides), the precheck alias, no gh (2), a missing Makefile pin (2) and
# a non-40-hex pin (2).
#
#   ops/scripts/tests/check_fork_pins_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT=$ROOT/scripts/check-fork-pins.sh
BASH_BIN=$(command -v bash)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/check-fork-pins-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

FORGE=1111111111111111111111111111111111111111
XMAGE=2222222222222222222222222222222222222222
MISS=0000000000000000000000000000000000000001

BIN=$TMP/bin
LOG=$TMP/gh.log
mkdir -p "$BIN" "$TMP/repo/scripts" "$TMP/nogh"
cp "$SCRIPT" "$TMP/repo/scripts/check-fork-pins.sh"
chmod +x "$TMP/repo/scripts/check-fork-pins.sh"

# gh: answer the requested sha, or 422 for GH_MISS. It records the exact
# `api repos/<fork>/commits/<sha> --jq .sha` invocation the script must make.
cat > "$BIN/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh $*" >> "$GH_LOG"
[ "$1" = api ] || exit 2
sha=${2##*/}
if [ -n "${GH_MISS:-}" ] && [ "$sha" = "$GH_MISS" ]; then
	echo "gh: No commit found for SHA: $sha (HTTP 422)" >&2
	exit 1
fi
printf '%s\n' "$sha"
STUB
chmod +x "$BIN/gh"

write_makefile() {
	cat > "$TMP/repo/Makefile" <<EOF
FORGE_ORACLE_REF ?= $FORGE
XMAGE_REF ?= $XMAGE
EOF
}

has() { [[ $1 == *"$2"* ]]; }

# 1. Both pins resolve in their fork networks.
write_makefile
: > "$LOG"
out=$(cd "$TMP/repo" && GH_LOG=$LOG PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "hit: both pins in their fork networks (exit 0)" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...names FORGE_ORACLE_REF and its fork" $(has "$out" "fork pin FORGE_ORACLE_REF $FORGE is in the adams-shaun/forge fork network" && echo 0 || echo 1) "$out"
check "  ...names XMAGE_REF and its fork" $(has "$out" "fork pin XMAGE_REF $XMAGE is in the adams-shaun/mage fork network" && echo 0 || echo 1) "$out"
check "  ...used the documented gh api call" $(/usr/bin/grep -qx "gh api repos/adams-shaun/forge/commits/$FORGE --jq .sha" "$LOG" && /usr/bin/grep -qx "gh api repos/adams-shaun/mage/commits/$XMAGE --jq .sha" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

# 2. A miss exits 1, names the sha with the exact required line, and still
#    checks the later pin rather than stopping at the first failure.
: > "$LOG"
out=$(cd "$TMP/repo" && GH_LOG=$LOG GH_MISS=$MISS FORGE_ORACLE_REF=$MISS PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "miss: exit 1" $([ "$rc" = 1 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...exact push-it-first line names the sha" $(has "$out" "fork pin FORGE_ORACLE_REF $MISS is not on adams-shaun/forge: push it first" && echo 0 || echo 1) "$out"
check "  ...kept checking XMAGE_REF after the failure" $(has "$out" "fork pin XMAGE_REF $XMAGE is in the adams-shaun/mage fork network" && echo 0 || echo 1) "$out"

# 3. An environment value wins over the Makefile, so a candidate can be checked
#    before the bump is committed.
: > "$LOG"
CAND=3333333333333333333333333333333333333333
out=$(cd "$TMP/repo" && GH_LOG=$LOG FORGE_ORACLE_REF=$CAND PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "env override: the candidate is checked, not the Makefile pin" $(has "$out" "fork pin FORGE_ORACLE_REF $CAND is in the adams-shaun/forge fork network" && ! has "$out" "$FORGE is" && echo 0 || echo 1) "rc=$rc $out"

# 4. The real tree's Makefile is what a seat's check reads. Command-line
#    overrides arrive as environment values (make exports command-line
#    variables), so the same script call covers the landing gate's entry point.
: > "$LOG"
out=$(cd "$ROOT" && GH_LOG=$LOG FORGE_ORACLE_REF="$FORGE" XMAGE_REF="$XMAGE" PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "real Makefile: honours both pin overrides (exit 0)" $([ "$rc" = 0 ] && has "$out" "$FORGE" && has "$out" "$XMAGE" && echo 0 || echo 1) "rc=$rc $out"

: > "$LOG"
out=$(cd "$ROOT" && GH_LOG=$LOG GH_MISS=$MISS FORGE_ORACLE_REF="$MISS" XMAGE_REF="$XMAGE" PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "real Makefile: an unpushed pin fails the check" $([ "$rc" != 0 ] && has "$out" "fork pin FORGE_ORACLE_REF $MISS is not on adams-shaun/forge: push it first" && echo 0 || echo 1) "rc=$rc $out"

# 4b. `make precheck` is still one recipe: the test-smart alias. `make -n`
#     prints it without running the gate.
out=$(cd "$ROOT" && make -n precheck 2>&1); rc=$?
check "make precheck: aliases test-smart" $([ "$rc" = 0 ] && has "$out" "bash scripts/test-targets.sh smart" && echo 0 || echo 1) "rc=$rc $out"

# 5. No gh: usage error, not a skip.
ln -s "$(command -v dirname)" "$TMP/nogh/dirname"
ln -s "$(command -v awk)" "$TMP/nogh/awk"
out=$(cd "$TMP/repo" && PATH="$TMP/nogh" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "no gh: exit 2" $([ "$rc" = 2 ] && has "$out" "check-fork-pins: gh is needed" && echo 0 || echo 1) "rc=$rc $out"

# 6. A Makefile that does not define a pin: usage error, not a skip.
cat > "$TMP/repo/Makefile" <<EOF
XMAGE_REF ?= $XMAGE
EOF
out=$(cd "$TMP/repo" && env -u FORGE_ORACLE_REF -u XMAGE_REF GH_LOG=$LOG PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "missing pin: exit 2 and names the variable" $([ "$rc" = 2 ] && has "$out" "check-fork-pins: Makefile defines no FORGE_ORACLE_REF" && echo 0 || echo 1) "rc=$rc $out"

# 7. A malformed pin: usage error before any API call.
write_makefile
: > "$LOG"
out=$(cd "$TMP/repo" && GH_LOG=$LOG FORGE_ORACLE_REF=deadbeef PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "non-40-hex pin: exit 2 and names the value" $([ "$rc" = 2 ] && has "$out" "is not a full commit sha: 'deadbeef'" && echo 0 || echo 1) "rc=$rc $out"
check "  ...and made no API call" $([ ! -s "$LOG" ] && echo 0 || echo 1) "$(cat "$LOG")"

[ "$fails" = 0 ] && echo "check_fork_pins_smoke: all passed" || echo "check_fork_pins_smoke: $fails FAILED"
exit $((fails != 0))
