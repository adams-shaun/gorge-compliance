#!/usr/bin/env bash
# forge_sbv2_smoke.sh — prove scripts/forge-sbv2.sh's launcher behaviour without
# Java, git history, systemd or a Forge checkout: it refuses before any javac or
# JVM when the pin or the engine source is wrong, compiles once per source sha,
# serves under a MemoryMax scope with stdio passed through, and its --selftest
# passes an engine that answers the hello exchange and fails one that does not.
#
# git, java, javac and systemd-run are stubs on PATH; the "engine" is a bash
# loop that answers the two selftest lines. What this does NOT show: that the
# real forge.oracle.sbv2 engine speaks the protocol. That is the Java tests'
# and `forge-sbv2.sh --selftest`'s job against a forge-oracle tree that has it.
#
#   ops/scripts/tests/forge_sbv2_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SBV2=$ROOT/scripts/forge-sbv2.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/forge-sbv2-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

FREF=1111111111111111111111111111111111111111
OREF=2222222222222222222222222222222222222222
BIN=$TMP/bin
LOG=$TMP/calls.log
SRC=$TMP/clone/forge-oracle/src/main/java/forge/oracle
mkdir -p "$BIN" "$TMP/root" "$TMP/clone/.git" "$TMP/clone/forge-gui/res" "$SRC" "$TMP/jdk/bin"
echo 'class A {}' > "$SRC/A.java"
echo '/stub/forge-core.jar' > "$TMP/root/cp.txt"

cat > "$BIN/git" <<'STUB'
#!/usr/bin/env bash
echo "git $*" >> "$STUB_LOG"
args=("$@"); [ "${args[0]}" = -C ] && args=("${args[@]:2}")
case "${args[0]}" in
merge-base) exit "${STUB_ANCESTOR:-0}" ;;
diff) exit "${STUB_DIFF:-0}" ;;
ls-files) printf '%s' "${STUB_UNTRACKED:-}" ;;
rev-parse)
	last=${args[${#args[@]}-1]}
	case "$last" in
	--show-toplevel) echo "$STUB_TOP" ;;
	HEAD) echo "$STUB_HEAD" ;;
	*'^{commit}') last=${last%^\{commit\}}; echo "$last" ;;
	*:*) echo "tree-${last#*:}" ;;
	*) exit 2 ;;
	esac ;;
*) exit 2 ;;
esac
STUB
# javac makes the class the launcher looks for in its -d directory; java is the
# engine: STUB_ENGINE=good answers hello_ok and malformed_json, else it is mute.
cat > "$TMP/jdk/bin/javac" <<'STUB'
#!/usr/bin/env bash
echo "javac $*" >> "$STUB_LOG"
[ -z "${STUB_JAVAC_SLEEP:-}" ] || sleep "$STUB_JAVAC_SLEEP"
while [ $# -gt 0 ]; do [ "$1" = -d ] && out=$2; shift; done
mkdir -p "$out/forge/oracle/sbv2" && : > "$out/forge/oracle/sbv2/Sbv2Engine.class"
STUB
cat > "$TMP/jdk/bin/java" <<'STUB'
#!/usr/bin/env bash
echo "java res=$FORGE_RES ref=$FORGE_ORACLE_REF cwd=$PWD args=$*" >> "$STUB_LOG"
case "${STUB_ENGINE:-good}" in
good)
	n=0
	while IFS= read -r line; do
		n=$((n + 1))
		if [ "$n" = 1 ]; then
			echo '{"engine":{},"protocol":"spellbench/v2","request_id":"selftest-hello","response_type":"hello_ok"}'
		else
			echo '{"error":{"code":"malformed_json","message":"dup"},"protocol":"spellbench/v2","request_id":"","response_type":"error"}'
		fi
	done ;;
wrongcode)
	while IFS= read -r line; do
		echo '{"error":{"code":"malformed_request","message":"x"},"protocol":"spellbench/v2","request_id":"selftest-hello","response_type":"error"}'
	done ;;
crash) exit 3 ;;
esac
STUB
cat > "$BIN/systemd-run" <<'STUB'
#!/usr/bin/env bash
echo "systemd-run $*" >> "$STUB_LOG"
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
STUB
chmod +x "$BIN"/* "$TMP"/jdk/bin/*

export PATH="$BIN:$PATH" STUB_LOG=$LOG STUB_TOP=$TMP/clone STUB_HEAD=$OREF
export FORGE_REF=$FREF FORGE_ORACLE_REF=$OREF FORGE_ORACLE_DIR=$TMP/root FORGE_ORACLE_CLONE=$TMP/clone FORGE_ORACLE_JDK=$TMP/jdk
export FORGE_REPO=file:///nonexistent XMAGE_ORACLE_DIR=$TMP/none
unset FORGE_ORACLE_SRC FORGE_ORACLE_CLASSES FORGE_ORACLE_TMP FORGE_SBV2_MAIN FORGE_SBV2_MEM FORGE_SBV2_HEAP FORGE_SBV2_ARGS XENGINE_SCOPE
unset STUB_ANCESTOR STUB_DIFF STUB_UNTRACKED STUB_ENGINE

has() { [[ $1 == *"$2"* ]]; }
reached() { /usr/bin/grep -qE "^(java|javac|systemd-run) " "$LOG" 2>/dev/null; }
count() { /usr/bin/grep -c "^$1 " "$LOG" 2>/dev/null || true; }

# 1. no engine source in the forge-oracle tree: refused before javac, naming why.
: > "$LOG"
out=$("$SBV2" --compile 2>&1); rc=$?
check "refuses a tree without forge/oracle/sbv2 (exit 2)" $([ "$rc" = 2 ] && has "$out" "no SpellBench v2 engine" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
mkdir -p "$SRC/sbv2"; echo 'class Sbv2Engine {}' > "$SRC/sbv2/Sbv2Engine.java"

# 2. pin refusals fire before javac.
: > "$LOG"
out=$(STUB_ANCESTOR=1 "$SBV2" --compile 2>&1); rc=$?
check "refuses a non-ancestor pin (exit 2)" $([ "$rc" = 2 ] && has "$out" "not an ancestor" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_HEAD=3333333333333333333333333333333333333333 "$SBV2" --compile 2>&1); rc=$?
check "refuses a clone that is not at FORGE_ORACLE_REF" $([ "$rc" = 2 ] && has "$out" "is not at FORGE_ORACLE_REF" && echo 0 || echo 1) "rc=$rc $out"
out=$(STUB_DIFF=1 "$SBV2" --compile 2>&1); rc=$?
check "refuses edited scripts under forge-gui/res" $([ "$rc" = 2 ] && has "$out" "uncommitted changes" && echo 0 || echo 1) "rc=$rc $out"
out=$(STUB_UNTRACKED=forge-gui/res/tokenscripts/x.txt "$SBV2" --compile 2>&1); rc=$?
check "refuses untracked runtime scripts" $([ "$rc" = 2 ] && has "$out" "untracked scripts" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$("$SBV2" --nonsense 2>&1); rc=$?
check "refuses an unknown flag (exit 2)" $([ "$rc" = 2 ] && has "$out" usage && echo 0 || echo 1) "rc=$rc $out"

# 3. compile once per source sha, same key as forge-oracle-run.sh.
: > "$LOG"
out=$("$SBV2" --compile 2>/dev/null); rc=$?
check "--compile on a good pin succeeds, printing the sha" $([ "$rc" = 0 ] && [[ $out == "driver_sha="* ]] && echo 0 || echo 1) "rc=$rc $out"
check "  ...compiled once, in a MemoryMax scope" $([ "$(count javac)" = 1 ] && /usr/bin/grep -q '^systemd-run .*MemoryMax=4G' "$LOG" && echo 0 || echo 1)
"$SBV2" --compile >/dev/null 2>&1
check "unchanged source reuses the class dir (no second javac)" $([ "$(count javac)" = 1 ] && echo 0 || echo 1) "javac calls: $(count javac)"
echo 'class B {}' > "$SRC/B.java"
"$SBV2" --compile >/dev/null 2>&1
check "a changed source recompiles" $([ "$(count javac)" = 2 ] && echo 0 || echo 1) "javac calls: $(count javac)"
old=$(ls "$TMP/root/driver" | /usr/bin/grep -vc '^\.') 
check "  ...and the earlier class dir is kept for a running engine" $([ "$old" = 2 ] && echo 0 || echo 1) "dirs: $old"
check "javac scope honours XENGINE_SCOPE=none" $(
	XENGINE_SCOPE=none FORGE_ORACLE_CLASSES=$TMP/c2 "$SBV2" --compile >/dev/null 2>&1
	/usr/bin/grep -c '^systemd-run ' "$LOG" | { read -r n; [ "$n" = 2 ] && echo 0 || echo 1; }) "systemd-run calls changed"

# two launches racing on one fresh source sha (xclient -workers) compile once:
# the flock serialises them and the second finds the finished directory.
: > "$LOG"
export FORGE_ORACLE_CLASSES=$TMP/race
STUB_JAVAC_SLEEP=1 "$SBV2" --compile >/dev/null 2>&1 &
p1=$!
STUB_JAVAC_SLEEP=1 "$SBV2" --compile >/dev/null 2>&1 &
p2=$!
wait "$p1"; r1=$?; wait "$p2"; r2=$?
unset FORGE_ORACLE_CLASSES
check "two concurrent launches on one sha both succeed with a single javac" $([ "$r1" = 0 ] && [ "$r2" = 0 ] && [ "$(count javac)" = 1 ] && echo 0 || echo 1) "rc=$r1,$r2 javac calls: $(count javac)"

# 4. serve: stdio passes through the scope to the engine, pinned env, own main class.
: > "$LOG"
out=$(printf 'a\nb\n' | "$SBV2" 2>/dev/null); rc=$?
check "serve passes stdin through and returns the engine's stdout" $([ "$rc" = 0 ] && [ "$(printf '%s\n' "$out" | wc -l)" = 2 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...under MemoryMax with the heap flag, on the engine main class" \
	$(/usr/bin/grep -q '^systemd-run .*MemoryMax=4G' "$LOG" && /usr/bin/grep -q -- '^java .*-Xmx2048m .*forge.oracle.sbv2.Sbv2Engine$' "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep -E '^(java|systemd-run)' "$LOG" | tail -2)"
check "  ...with the pinned res and ref" $(/usr/bin/grep -qF "java res=$TMP/clone/forge-gui/res ref=$OREF" "$LOG" && echo 0 || echo 1)
FORGE_SBV2_MEM=2G FORGE_SBV2_HEAP=900m FORGE_SBV2_ARGS='-Dforge.sbv2.x=1' "$SBV2" </dev/null >/dev/null 2>&1
check "FORGE_SBV2_MEM, _HEAP and _ARGS reach the command line" \
	$(/usr/bin/grep -q 'MemoryMax=2G' "$LOG" && /usr/bin/grep -q -- '-Xmx900m .*-Dforge.sbv2.x=1' "$LOG" && echo 0 || echo 1)
STUB_ENGINE=crash "$SBV2" </dev/null >/dev/null 2>&1; rc=$?
check "the engine's exit status is the launcher's" $([ "$rc" = 3 ] && echo 0 || echo 1) "rc=$rc"

# 5. --selftest passes a good engine and fails a bad or crashing one.
out=$("$SBV2" --selftest 2>&1); rc=$?
check "--selftest passes an engine that answers hello and malformed_json" $([ "$rc" = 0 ] && has "$out" "selftest: ok" && echo 0 || echo 1) "rc=$rc $out"
out=$(STUB_ENGINE=wrongcode "$SBV2" --selftest 2>&1); rc=$?
check "--selftest fails an engine that gets the exchange wrong (exit 1)" $([ "$rc" = 1 ] && has "$out" "hello answer lacks" && echo 0 || echo 1) "rc=$rc $out"
out=$(STUB_ENGINE=crash "$SBV2" --selftest 2>&1); rc=$?
check "--selftest fails a crashing engine (exit 1)" $([ "$rc" = 1 ] && has "$out" "engine exited 3" && echo 0 || echo 1) "rc=$rc $out"

[ "$fails" = 0 ] && echo "forge_sbv2_smoke: all passed" || echo "forge_sbv2_smoke: $fails FAILED"
exit $((fails != 0))
