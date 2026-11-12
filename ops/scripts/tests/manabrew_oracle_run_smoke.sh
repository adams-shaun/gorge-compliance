#!/usr/bin/env bash
# manabrew_oracle_run_smoke.sh — prove scripts/manabrew-oracle-run.sh restarts the driver after
# exit 3 on the lines not yet in OUT, fails (exit 2) on any other non-zero status, and passes the
# --preflight protocol through on stdout, without Rust, network or a Manabrew checkout.
#
# The setup script, git, cargo and the driver binary are stubs.
#
#   ops/scripts/tests/manabrew_oracle_run_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

# A repository copy of just what the runner reads: itself, the Makefile, and a stub setup script.
REPO=$TMP/repo
mkdir -p "$REPO/scripts"
cp "$ROOT/scripts/manabrew-oracle-run.sh" "$REPO/scripts/"
cp "$ROOT/Makefile" "$REPO/Makefile"
cat > "$REPO/scripts/manabrew-oracle-setup.sh" <<'STUB'
#!/usr/bin/env bash
echo "setup $*" >> "$STUB_LOG"
echo "manabrew-oracle-setup: pin holds (stub)"
[ -z "${STUB_PIN_FAIL:-}" ] || exit 2
STUB
chmod +x "$REPO/scripts/manabrew-oracle-setup.sh"

BIN=$TMP/bin
DIR=$TMP/mb
LOG=$TMP/calls.log
MREF=2222222222222222222222222222222222222222
FREF=1111111111111111111111111111111111111111
mkdir -p "$BIN" "$DIR/build-${MREF:0:12}/manabrew-rs" "$DIR/target/oracle"

cat > "$BIN/git" <<'STUB'
#!/usr/bin/env bash
last=${@: -1}
case "$last" in *'^{commit}') last=${last%^\{commit\}}; echo "$last" ;; *) exit 2 ;; esac
STUB
cat > "$BIN/cargo" <<'STUB'
#!/usr/bin/env bash
echo "cargo $* [cwd=$PWD CARGO_HOME=$CARGO_HOME CARGO_TARGET_DIR=$CARGO_TARGET_DIR]" >> "$STUB_LOG"
STUB
# The driver: STUB_MODE=panic exits 3 once, after writing one row (state in $STUB_STATE);
# STUB_MODE=fail exits 1; STUB_MODE=stuck exits 3 without a row; else exits 0.
cat > "$DIR/target/oracle/manabrew-oracle" <<'STUB'
#!/usr/bin/env bash
echo "driver $* [MANABREW_REF=$MANABREW_REF FORGE_CARD_TRACE=${FORGE_CARD_TRACE-unset}]" >> "$STUB_LOG"
if [ "$1" = --preflight ]; then
	printf 'No Such Card\tnot in archive\n'
	exit 0
fi
in=$2 out=$3
case "${STUB_MODE:-ok}" in
fail) echo "driver: boom" >&2; exit 1 ;;
stuck) exit 3 ;;
esac
first=1
while IFS= read -r line; do
	printf '{"row":%s}\n' "$line" >> "$out"
	if [ "${STUB_MODE:-ok}" = panic ] && [ ! -e "$STUB_STATE" ]; then
		: > "$STUB_STATE"
		echo "driver: panicked" >&2
		exit 3
	fi
done < "$in"
exit 0
STUB
chmod +x "$BIN/git" "$BIN/cargo" "$DIR/target/oracle/manabrew-oracle"
touch "$DIR/cardset-${FREF:0:12}.rkyv"

run() { # run MODE args... ; stdout in $TMP/stdout, rc in $rc
	local mode=$1; shift
	: > "$LOG"; rm -f "$TMP/state"
	PATH=$BIN:$PATH STUB_LOG=$LOG STUB_STATE=$TMP/state STUB_MODE=$mode STUB_PIN_FAIL=${PIN_FAIL:-} FORGE_CARD_TRACE=1 \
		XENGINE_SCOPE=none MANABREW_ORACLE_DIR=$DIR MANABREW_ORACLE_REF=$MREF FORGE_REF=$FREF CARGO=$BIN/cargo \
		"$REPO/scripts/manabrew-oracle-run.sh" "$@" > "$TMP/stdout" 2> "$TMP/stderr"
	rc=$?
}

printf '1\n2\n3\n4\n' > "$TMP/in.jsonl"

run panic "$TMP/in.jsonl" "$TMP/sub/out.jsonl"
check "restart: exit 0" "$rc" "(rc=$rc)"
want=$(printf '{"row":1}\n{"row":2}\n{"row":3}\n{"row":4}\n')
[ "$(cat "$TMP/sub/out.jsonl")" = "$want" ]; check "restart: one row per input line, in order" $? "$(cat "$TMP/sub/out.jsonl")"
[ "$(/usr/bin/grep -c '^driver scenarios' "$LOG")" = 2 ]; check "restart: driver started twice" $?
/usr/bin/grep -q 'cargo build --locked --profile oracle -p manabrew-oracle \[cwd=.*/build-222222222222/manabrew-rs ' "$LOG"; check "cargo build in the build tree" $?
/usr/bin/grep -q 'MANABREW_REF=2222222222222222222222222222222222222222 FORGE_CARD_TRACE=unset' "$LOG"; check "MANABREW_REF set, trace variables unset" $?
/usr/bin/grep -q -- "--archive $DIR/cardset-111111111111.rkyv" "$LOG"; check "archive argument" $?
/usr/bin/grep -q 'panicked' "$TMP/sub/out.jsonl.log"; check "stderr goes to OUT.log" $?
[ ! -e "$TMP/sub"/manabrew-oracle-run.* ]; check "no temp files left beside OUT" $?
[ "$(sed -n 1p "$LOG")" = "setup --check-pin" ]; check "pin checked first" $?

run ok "$TMP/in.jsonl" "$TMP/out2.jsonl"
[ "$rc" = 0 ] && [ "$(grep -c '' "$TMP/out2.jsonl")" = 4 ] && [ "$(/usr/bin/grep -c '^driver scenarios' "$LOG")" = 1 ]; check "clean run: one start, four rows" $?

run fail "$TMP/in.jsonl" "$TMP/out3.jsonl"
[ "$rc" = 2 ]; check "exit 1 from the driver fails the run with 2" $? "(rc=$rc)"

run stuck "$TMP/in.jsonl" "$TMP/out4.jsonl"
[ "$rc" = 2 ]; check "exit 3 without progress fails the run with 2" $? "(rc=$rc)"

printf 'Grizzly Bears\nNo Such Card\n' > "$TMP/names.txt"
run ok --preflight "$TMP/names.txt"
[ "$rc" = 0 ] && [ "$(cat "$TMP/stdout")" = "$(printf 'No Such Card\tnot in archive')" ]; check "--preflight: stdout is the driver's alone" $? "$(cat "$TMP/stdout")"

PIN_FAIL=1 run ok "$TMP/in.jsonl" "$TMP/out5.jsonl"
[ "$rc" = 2 ] && ! /usr/bin/grep -q '^driver' "$LOG"; check "pin failure exits 2 before the driver" $? "(rc=$rc)"

[ "$fails" = 0 ] || { echo "$fails failure(s)"; exit 1; }
echo "manabrew_oracle_run_smoke: all ok"
