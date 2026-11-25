#!/usr/bin/env bash
# check_fork_pins_smoke.sh — prove scripts/check-fork-pins.sh fails closed,
# honours a command-line pin override, and reads the real Makefile's pins,
# without gh, the network or a forge checkout.
#
# gh is a stub on PATH; the script under test is copied next to a fixture
# Makefile so the missing-pin case can be exercised. The fixture also carries a
# git ref for the base branch, so the forward check reads a real `git show`
# while the compare API stays an offline fake. Cases: every pin in the fork
# network (0), a miss (1 and the exact push-it-first line), a candidate env
# override, the real Makefile pins (hit and miss; `make precheck` is the full
# test-smart gate now, so its pin step calls this script with the same env
# overrides), the precheck alias, no gh (2), a missing Makefile pin (2), a
# non-40-hex pin (2), a pin that moves backward (1 and both short SHAs), a pin
# that diverges (1), a forward bump (0), a local develop ahead of origin/develop
# (1), a pin new to the base (0), and the contents-API fallback when the base ref
# is not in the checkout (0).
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
git -C "$TMP/repo" init -q

# gh: answer a commit sha, or 422 for GH_MISS; answer the compare API with
# GH_COMPARE_STATUS (identical when both sides are the same commit); answer the
# contents API with GH_CONTENTS for the fallback case. It records every call.
cat > "$BIN/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh $*" >> "$GH_LOG"
[ "$1" = api ] || exit 2
case "$2" in
repos/*/compare/*)
	range=${2#*compare/}
	old=${range%%...*}
	new=${range##*...}
	if [ "$old" = "$new" ]; then
		echo identical
	else
		echo "${GH_COMPARE_STATUS:-ahead}"
	fi
	;;
repos/*/contents/Makefile*)
	printf '%s\n' "${GH_CONTENTS:-}"
	;;
*)
	sha=${2##*/}
	if [ -n "${GH_MISS:-}" ] && [ "$sha" = "$GH_MISS" ]; then
		echo "gh: No commit found for SHA: $sha (HTTP 422)" >&2
		exit 1
	fi
	printf '%s\n' "$sha"
	;;
esac
STUB
chmod +x "$BIN/gh"

write_makefile() {
	local forge=${1:-$FORGE} xmage=${2:-$XMAGE}
	cat > "$TMP/repo/Makefile" <<EOF
FORGE_ORACLE_REF ?= $forge
XMAGE_REF ?= $xmage
EOF
}

# Commit the same content as the base branch's Makefile and point a local
# origin/develop at it, so the forward check exercises its real `git show` path.
set_base_makefile() {
	write_makefile "$@"
	git -C "$TMP/repo" add Makefile
	git -C "$TMP/repo" -c user.name=smoke -c user.email=smoke@example.invalid \
		commit -q --allow-empty -m base
	git -C "$TMP/repo" update-ref refs/remotes/origin/develop HEAD
}

has() { [[ $1 == *"$2"* ]]; }

# 1. Both pins resolve in their fork networks.
set_base_makefile
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
out=$(cd "$ROOT" && GH_LOG=$LOG FORGE_PIN_BASE_REF=HEAD FORGE_ORACLE_REF="$FORGE" XMAGE_REF="$XMAGE" PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "real Makefile: honours both pin overrides (exit 0)" $([ "$rc" = 0 ] && has "$out" "$FORGE" && has "$out" "$XMAGE" && echo 0 || echo 1) "rc=$rc $out"

: > "$LOG"
out=$(cd "$ROOT" && GH_LOG=$LOG FORGE_PIN_BASE_REF=HEAD GH_MISS=$MISS FORGE_ORACLE_REF="$MISS" XMAGE_REF="$XMAGE" PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
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

# 8. A working-tree pin behind the base branch is refused, naming both short
#    SHAs. The compare call still goes base -> working.
BACK_NEW=27b8bfe66159db22002db3b7ae25f3f531b7ef8d
BACK_OLD=1b6d284d103ec532cf0f3d4c22477e03756ff613
set_base_makefile "$FORGE" "$BACK_NEW"
write_makefile "$FORGE" "$BACK_OLD"
: > "$LOG"
out=$(cd "$TMP/repo" && GH_LOG=$LOG GH_COMPARE_STATUS=behind PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "backward pin: exit 1" $([ "$rc" = 1 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...exact moves-backward line names both short SHAs" $(has "$out" "XMAGE_REF moves backward: 27b8bfe661 -> 1b6d284d10" && echo 0 || echo 1) "$out"
check "  ...compares the base pin with the working pin" $(/usr/bin/grep -qx "gh api repos/adams-shaun/mage/compare/$BACK_NEW...$BACK_OLD --jq .status" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

# 9. A diverged pin (neither an ancestor nor a descendant) is refused too.
: > "$LOG"
out=$(cd "$TMP/repo" && GH_LOG=$LOG GH_COMPARE_STATUS=diverged PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "diverged pin: exit 1" $([ "$rc" = 1 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...names the base ref and both short SHAs" $(has "$out" "XMAGE_REF diverged from origin/develop: 27b8bfe661 -> 1b6d284d10" && echo 0 || echo 1) "$out"

# 10. A forward bump (ahead) is allowed.
set_base_makefile "$FORGE" "$BACK_OLD"
write_makefile "$FORGE" "$BACK_NEW"
: > "$LOG"
out=$(cd "$TMP/repo" && GH_LOG=$LOG GH_COMPARE_STATUS=ahead PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "forward bump: exit 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...still checks both pins" $(has "$out" "fork pin FORGE_ORACLE_REF $FORGE" && has "$out" "fork pin XMAGE_REF $BACK_NEW" && echo 0 || echo 1) "$out"

# 10b. The local develop branch is the floor when it has advanced past
#      origin/develop; an unpushed pin bump must still be caught.
set_base_makefile "$FORGE" "$BACK_OLD"
BACK_OLD_SHA=$(git -C "$TMP/repo" rev-parse HEAD)
set_base_makefile "$FORGE" "$BACK_NEW"
git -C "$TMP/repo" update-ref refs/heads/develop HEAD
git -C "$TMP/repo" update-ref refs/remotes/origin/develop "$BACK_OLD_SHA"
write_makefile "$FORGE" "$BACK_OLD"
out=$(cd "$TMP/repo" && GH_LOG=$LOG GH_COMPARE_STATUS=behind PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "local develop ahead: exit 1" $([ "$rc" = 1 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...uses local develop, not the older origin/develop" $(has "$out" "XMAGE_REF moves backward: 27b8bfe661 -> 1b6d284d10" && echo 0 || echo 1) "$out"
git -C "$TMP/repo" update-ref -d refs/heads/develop

# 11. A pin that the base branch does not define is a new row, not a regression.
cat > "$TMP/repo/Makefile" <<EOF
FORGE_ORACLE_REF ?= $FORGE
EOF
git -C "$TMP/repo" add Makefile
git -C "$TMP/repo" -c user.name=smoke -c user.email=smoke@example.invalid \
	commit -q --allow-empty -m base
git -C "$TMP/repo" update-ref refs/remotes/origin/develop HEAD
write_makefile
out=$(cd "$TMP/repo" && GH_LOG=$LOG GH_COMPARE_STATUS=ahead PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "new pin in base-less row: exit 0" $([ "$rc" = 0 ] && has "$out" "fork pin XMAGE_REF $XMAGE is in the adams-shaun/mage fork network" && echo 0 || echo 1) "rc=$rc $out"

# 12. CI's checkout need not have origin/develop: the contents API is the
#     fallback when `git show` cannot read the base Makefile.
NONGIT=$TMP/nongit
mkdir -p "$NONGIT/scripts"
cp "$SCRIPT" "$NONGIT/scripts/check-fork-pins.sh"
chmod +x "$NONGIT/scripts/check-fork-pins.sh"
printf 'FORGE_ORACLE_REF ?= %s\nXMAGE_REF ?= %s\n' "$FORGE" "$XMAGE" > "$NONGIT/Makefile"
GH_CONTENTS=$(printf 'FORGE_ORACLE_REF ?= %s\nXMAGE_REF ?= %s\n' "$FORGE" "$XMAGE" | base64 | tr -d '\n')
: > "$LOG"
out=$(cd "$NONGIT" && GITHUB_REPOSITORY=smoke/repo GH_CONTENTS="$GH_CONTENTS" GH_LOG=$LOG PATH="$BIN:$PATH" "$BASH_BIN" scripts/check-fork-pins.sh 2>&1); rc=$?
check "no base ref: contents API fallback (exit 0)" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...asked for develop's Makefile" $(/usr/bin/grep -qx "gh api repos/smoke/repo/contents/Makefile?ref=develop --jq .content" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

[ "$fails" = 0 ] && echo "check_fork_pins_smoke: all passed" || echo "check_fork_pins_smoke: $fails FAILED"
exit $((fails != 0))
