#!/usr/bin/env bash
# forge_driver_follow_smoke.sh — scripts/forge-driver-follow.sh against a local fixture "fork" (real git merges and
# pushes; gh is a stub answering compare from the fixture's ancestry, contents with a fixed gorge FORGE_REF).
# Cases: driver holds every ref (no push); driver merges gorge main's FORGE_REF and fast-forwards oracle-driver;
# pin with the ref held (no change); pin merges, checks trees, pushes oracle/pin-<ref>, amends FORGE_ORACLE_REF;
# pin refuses a driver whose card scripts differ (tree mismatch) and a conflicting merge, leaving the Makefile alone.
#
#   ops/scripts/tests/forge_driver_follow_smoke.sh
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT=$ROOT/scripts/forge-driver-follow.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/forge-driver-follow-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() { if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }
g() { git -C "$1" -c user.name=t -c user.email=t@t -c commit.gpgsign=false "${@:2}"; }

# The fork: FR1 (gorge's old FORGE_REF) -> D1 (driver code) on oracle-driver; FR1 -> FR2 (a card script fix).
F=$TMP/fork; W=$TMP/w
git init -q -b main "$W"
mkdir -p "$W/forge-gui/res/cardsfolder/a" "$W/forge-gui/res/tokenscripts" "$W/forge-oracle"
echo v1 >"$W/forge-gui/res/cardsfolder/a/a.txt"; echo t >"$W/forge-gui/res/tokenscripts/t.txt"; echo p >"$W/pom.xml"
g "$W" add -A; g "$W" commit -q -m FR1; FR1=$(g "$W" rev-parse HEAD)
echo d1 >"$W/forge-oracle/Driver.java"; g "$W" add -A; g "$W" commit -q -m D1; D1=$(g "$W" rev-parse HEAD)
g "$W" branch -q oracle-driver "$D1"
g "$W" checkout -q -b fr2 "$FR1"; echo v2 >"$W/forge-gui/res/cardsfolder/a/a.txt"; g "$W" commit -q -am FR2; FR2=$(g "$W" rev-parse HEAD)
# DX: a driver that also edits a card script (tree mismatch); DC: one that edits a.txt (conflict)
g "$W" checkout -q -b dx "$D1"; echo x >"$W/forge-gui/res/cardsfolder/a/b.txt"; g "$W" add -A; g "$W" commit -q -m DX; DX=$(g "$W" rev-parse HEAD)
g "$W" checkout -q -b dc "$D1"; echo conflict >"$W/forge-gui/res/cardsfolder/a/a.txt"; g "$W" commit -q -am DC; DC=$(g "$W" rev-parse HEAD)
# FR3: a FORGE_REF that also brings Forge code (an upstream merge): pin must leave it to a person
g "$W" checkout -q -b fr3 "$FR1"; mkdir -p "$W/forge-game"; echo code >"$W/forge-game/Game.java"; echo v3 >"$W/forge-gui/res/cardsfolder/a/a.txt"; g "$W" add -A; g "$W" commit -q -m FR3; FR3=$(g "$W" rev-parse HEAD)
git clone -q --bare "$W" "$F"

BIN=$TMP/bin; mkdir -p "$BIN"; LOG=$TMP/gh.log
cat >"$BIN/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh $*" >>"$GH_LOG"
case "$2" in
repos/*/compare/*)
	r=${2#*compare/}; a=$(git -C "$FIX" rev-parse "${r%%...*}"); b=$(git -C "$FIX" rev-parse "${r##*...}")
	if [ "$a" = "$b" ]; then echo identical
	elif git -C "$FIX" merge-base --is-ancestor "$b" "$a"; then echo behind
	elif git -C "$FIX" merge-base --is-ancestor "$a" "$b"; then echo ahead
	else echo diverged; fi ;;
repos/*/contents/Makefile*) printf 'FORGE_REF  ?= %s\n' "$GH_FREF" | base64 ;;
repos/*/pulls*) printf '%s\n' ${GH_PRS:-} ;;
*) exit 2 ;;
esac
STUB
chmod +x "$BIN/gh"

# The compliance tree the script runs in.
C=$TMP/c; git init -q -b develop "$C"; mkdir -p "$C/scripts"; cp "$SCRIPT" "$C/scripts/"
mkc() { printf '# pin\nFORGE_ORACLE_REF ?= %s\nX ?= 1\n' "$1" >"$C/Makefile"; mkdir -p "$C/cmd/oraclediff"; printf 'package x\n\n// note\nconst forgeOracleDieRollRef = "%s"\n' "$1" >"$C/cmd/oraclediff/forge_dieroll_test.go"; echo 1111111111111111111111111111111111111111 >"$C/GORGE_PIN"; g "$C" add -A; g "$C" commit -q --allow-empty -m "gorge pin bump"; }
run() { (cd "$C" && env PATH="$BIN:$PATH" GH_LOG="$LOG" FIX="$F" FORGE_FORK_URL="$F" WORK="$TMP/work" "$@" bash scripts/forge-driver-follow.sh "${ARGS[@]}" 2>&1); }

# driver
mkc "$D1"
ARGS=(driver "$FR1"); out=$(run); rc=$?
check "driver: oracle-driver already holds FR1 (exit 0, no push)" $([ "$rc" = 0 ] && has "$out" "holds every FORGE_REF" && [ "$(git -C "$F" rev-parse oracle-driver)" = "$D1" ] && echo 0 || echo 1) "rc=$rc $out"
ARGS=(driver); out=$(run GH_FREF="$FR2" GH_PRS="$FR1"); rc=$?
tip=$(git -C "$F" rev-parse oracle-driver)
check "driver: merges gorge main's FORGE_REF into oracle-driver" $([ "$rc" = 0 ] && git -C "$F" merge-base --is-ancestor "$FR2" "$tip" && git -C "$F" merge-base --is-ancestor "$D1" "$tip" && echo 0 || echo 1) "rc=$rc $out"
check "  ...read gorge main and the open PRs' Makefiles" $(/usr/bin/grep -q "repos/adams-shaun/gorge/contents/Makefile?ref=main" "$LOG" && /usr/bin/grep -q "repos/adams-shaun/gorge/pulls" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

# pin
mkc "$D1"; head0=$(g "$C" rev-parse HEAD)
ARGS=(pin); out=$(run FORGE_REF="$FR1"); rc=$?
check "pin: FORGE_ORACLE_REF already holds FORGE_REF (no change)" $([ "$rc" = 0 ] && has "$out" "holds FORGE_REF" && [ "$(g "$C" rev-parse HEAD)" = "$head0" ] && echo 0 || echo 1) "rc=$rc $out"
out=$(run FORGE_REF="$FR2"); rc=$?
new=$(awk '$1 == "FORGE_ORACLE_REF" {print $3}' "$C/Makefile")
check "pin: merges FR2 into the driver and sets FORGE_ORACLE_REF" $([ "$rc" = 0 ] && [ "$new" != "$D1" ] && git -C "$F" merge-base --is-ancestor "$FR2" "$new" && git -C "$F" merge-base --is-ancestor "$D1" "$new" && echo 0 || echo 1) "rc=$rc $out"
check "  ...pushed oracle/pin-<FR2 12>" $([ "$(git -C "$F" rev-parse "oracle/pin-${FR2:0:12}" 2>/dev/null)" = "$new" ] && echo 0 || echo 1)
check "  ...trees equal FORGE_REF's" $([ "$(git -C "$F" rev-parse "$new:forge-gui/res/cardsfolder")" = "$(git -C "$F" rev-parse "$FR2:forge-gui/res/cardsfolder")" ] && echo 0 || echo 1)
check "  ...forgeOracleDieRollRef follows (card scripts only)" $(/usr/bin/grep -q "^const forgeOracleDieRollRef = \"$new\"" "$C/cmd/oraclediff/forge_dieroll_test.go" && echo 0 || echo 1) "$(cat "$C/cmd/oraclediff/forge_dieroll_test.go")"
check "  ...amended the bump commit (one commit, Makefile clean)" $([ "$(g "$C" rev-list --count HEAD)" = 2 ] && [ "$(g "$C" log -1 --format=%s)" = "gorge pin bump" ] && g "$C" diff --quiet && /usr/bin/grep -q "merged with gorge FORGE_REF ${FR2:0:10}" "$C/Makefile" && echo 0 || echo 1) "$(g "$C" log --oneline)"

mkc "$DX"; before=$(cat "$C/Makefile")
out=$(run FORGE_REF="$FR2"); rc=$?
check "pin: a driver whose card scripts differ is refused (exit 1, Makefile untouched)" $([ "$rc" = 1 ] && has "$out" "cardsfolder differs" && [ "$(cat "$C/Makefile")" = "$before" ] && echo 0 || echo 1) "rc=$rc $out"
mkc "$DC"; before=$(cat "$C/Makefile")
out=$(run FORGE_REF="$FR2"); rc=$?
check "pin: a conflicting merge is refused (exit 1, Makefile untouched)" $([ "$rc" = 1 ] && has "$out" "conflicts" && [ "$(cat "$C/Makefile")" = "$before" ] && echo 0 || echo 1) "rc=$rc $out"

mkc "$D1"; before=$(cat "$C/Makefile")
out=$(run FORGE_REF="$FR3"); rc=$?
check "pin: a FORGE_REF that brings Forge code is left to a person (exit 1, nothing pushed)" $([ "$rc" = 1 ] && has "$out" "changes Forge code" && [ "$(cat "$C/Makefile")" = "$before" ] && ! git -C "$F" rev-parse -q --verify "oracle/pin-${FR3:0:12}" >/dev/null && echo 0 || echo 1) "rc=$rc $out"

ARGS=(bogus); out=$(run); rc=$?
check "usage: exit 2" $([ "$rc" = 2 ] && echo 0 || echo 1) "rc=$rc $out"

# mkpin reads its whole input: an awk that exits at the first match closes the pipe, and once gorge's Makefile
# outgrew the pipe buffer `base64 -d` died of SIGPIPE under pipefail (gorge-pin-follow red from 2026-11-27).
for f in scripts/forge-driver-follow.sh scripts/check-forge-ref-in-driver.sh; do
	fn=$(/usr/bin/grep -m1 '^mkpin() ' "$ROOT/$f")
	out=$(bash -c "set -euo pipefail; $fn"$'\n''{ echo "FORGE_REF ?= abc123 # pin"; head -c 400000 /dev/zero | tr "\0" "x" | fold -w 80; } | base64 | base64 -d | mkpin FORGE_REF' 2>&1); rc=$?
	check "mkpin in $f survives a large Makefile under pipefail" $([ "$rc" = 0 ] && [ "$out" = abc123 ] && echo 0 || echo 1) "rc=$rc $out"
done

[ "$fails" = 0 ] && echo "forge_driver_follow_smoke: PASS" || { echo "forge_driver_follow_smoke: $fails FAIL"; exit 1; }
