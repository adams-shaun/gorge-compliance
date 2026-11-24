#!/usr/bin/env bash
# phase_sbv2_smoke.sh — prove scripts/phase-sbv2.sh checks the pin before it builds, builds only the
# phase-seat binary with `--locked --profile follower`, passes the card data, catalog and revision
# through, and that --selftest fails loudly on an engine that does not answer hello. cargo is a stub
# that writes a stub engine binary; no Rust, no network, no systemd.
#
#   ops/scripts/tests/phase_sbv2_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
RUN=$ROOT/scripts/phase-sbv2.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/phase-sbv2-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() { if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }

SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
OTHER=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
BODY='good card data'
H=$(printf '%s' "$BODY" | sha256sum | cut -d' ' -f1)
H16=${H:0:16}
BIN=$TMP/bin
LOG=$TMP/calls.log
F=$TMP/follower
O=$TMP/oracle
mkdir -p "$BIN" "$F/phase-follower" "$F/phase-seat" "$O"

mkpin() { # REV_IN_FOLLOWER_TOML REV_IN_SEAT_TOML
	echo "$SHA $H16 $H" > "$F/PHASE_PIN"
	for c in phase-follower:$1 phase-seat:$2; do
		cat > "$F/${c%%:*}/Cargo.toml" <<TOML
[package]
name = "${c%%:*}"

[dependencies]
engine = { package = "phase-engine", git = "https://github.com/phase-rs/phase", rev = "${c##*:}" }
TOML
	done
	cat > "$F/Cargo.lock" <<LOCK
[[package]]
name = "phase-engine"
version = "0.1.0"
source = "git+https://github.com/phase-rs/phase?rev=$SHA#$SHA"
LOCK
}
printf '%s' "$BODY" > "$O/card-data-$H16.json"
: > "$O/card-data-$H16.json.ok"

# The script's default-catalog branch reads the committed pool and the pinned card data; give it a
# card-data stub shaped like phase's export (only the names, core types and mana cost are read).
write_valid_card_data() {
	python3 - "$ROOT/oracle/xengine/pools/fdn-commons.txt" "$1" <<'PY'
import json, sys
names = [line.split("#", 1)[0].strip() for line in open(sys.argv[1], encoding="utf-8")]
cards = {n.lower(): {"card_type": {"core_types": ["Creature"]}, "mana_cost": {}} for n in names if n}
json.dump(cards, open(sys.argv[2], "w", encoding="utf-8"), sort_keys=True)
PY
}

# A stub phase-seat: answers hello with a hello_ok naming phase, any other line with malformed_json,
# and records its argv and PHASE_REF. STUB_MODE=mute answers nothing.
cat > "$BIN/cargo" <<'STUB'
#!/usr/bin/env bash
echo "cargo $* [CARGO_HOME=$CARGO_HOME CARGO_TARGET_DIR=$CARGO_TARGET_DIR cwd=$PWD]" >> "$STUB_LOG"
mkdir -p "$CARGO_TARGET_DIR/follower"
cat > "$CARGO_TARGET_DIR/follower/phase-seat" <<'ENGINE'
#!/usr/bin/env bash
echo "phase-seat $* [PHASE_REF=$PHASE_REF]" >> "$STUB_LOG"
[ "${STUB_MODE:-}" = mute ] && exit 0
while IFS= read -r line; do
	case "$line" in
		*'"request_type":"hello"'*) echo '{"engine":{"name":"phase"},"response_type":"hello_ok"}' ;;
		*) echo '{"error":{"code":"malformed_json"},"response_type":"error"}' ;;
	esac
done
ENGINE
chmod +x "$CARGO_TARGET_DIR/follower/phase-seat"
STUB
chmod +x "$BIN"/*

export PATH="$BIN:$PATH" STUB_LOG=$LOG
export PHASE_FOLLOWER_DIR=$F PHASE_ORACLE_DIR=$O FOLLOWERS_CACHE=$TMP/cache CARGO=$BIN/cargo XENGINE_SCOPE=none
unset PHASE_SBV2_DECKS
called() { /usr/bin/grep -q "^$1 " "$LOG" 2>/dev/null; }

# 1. A phase-seat manifest whose rev differs from PHASE_PIN: exit 2 before cargo.
mkpin "$SHA" "$OTHER"; : > "$LOG"
out=$("$RUN" --compile 2>&1); rc=$?
check "a phase-seat rev that differs from PHASE_PIN exits 2" $([ "$rc" = 2 ] && has "$out" "phase-seat/Cargo.toml" && echo 0 || echo 1) "rc=$rc $out"
check "  ...before cargo" $(! called cargo && echo 0 || echo 1)

# 2. Missing card data is refused by the pin check too.
mkpin "$SHA" "$SHA"; : > "$LOG"; mv "$O/card-data-$H16.json.ok" "$TMP/ok.saved"
out=$("$RUN" --compile 2>&1); rc=$?
check "unverified card data exits 2" $([ "$rc" = 2 ] && ! called cargo && echo 0 || echo 1) "rc=$rc $out"
mv "$TMP/ok.saved" "$O/card-data-$H16.json.ok"

# 3. --compile builds only phase-seat, locked, in the follower profile, in the follower dir.
: > "$LOG"
out=$("$RUN" --compile 2>"$TMP/err"); rc=$?
check "--compile exits 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $(cat "$TMP/err")"
check "  ...printing the binary and the pin" $(has "$out" "phase-seat=$O/target/follower/phase-seat ref=${SHA:0:12}+$H16" && echo 0 || echo 1) "$out"
check "  ...cargo build --locked --profile follower -p phase-seat" $(/usr/bin/grep -q '^cargo build --locked --profile follower -p phase-seat \[' "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"
check "  ...in the follower dir with the cache CARGO_HOME and target dir" \
	$(/usr/bin/grep -qF "CARGO_HOME=$TMP/cache/cargo-home CARGO_TARGET_DIR=$O/target cwd=$F]" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"
check "  ...and never runs the engine" $(! called phase-seat && echo 0 || echo 1)

# 4. --selftest passes against an engine that answers.
: > "$LOG"
"$RUN" --selftest >/dev/null 2>"$TMP/err"; rc=$?
check "--selftest exits 0 against a hello_ok" $([ "$rc" = 0 ] && has "$(cat "$TMP/err")" "selftest ok" && echo 0 || echo 1) "rc=$rc $(cat "$TMP/err")"
check "  ...passing --card-data and the PHASE_REF pin" $(/usr/bin/grep -qF "phase-seat --card-data $O/card-data-$H16.json [PHASE_REF=${SHA:0:12}+$H16]" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

# 5. --selftest fails (exit 1) on an engine that says nothing.
: > "$LOG"
STUB_MODE=mute "$RUN" --selftest >/dev/null 2>"$TMP/err"; rc=$?
check "--selftest exits 1 against a mute engine" $([ "$rc" = 1 ] && has "$(cat "$TMP/err")" "no hello_ok" && echo 0 || echo 1) "rc=$rc $(cat "$TMP/err")"

# 6. Serving passes -decks and -source-revision through as the engine's flags, and relays stdio.
: > "$LOG"
out=$(printf '%s\n' '{"request_type":"hello","protocol":"spellbench/v2","request_id":"h","protocol_minor":0}' | "$RUN" -decks "$TMP/decks" -source-revision r1 2>/dev/null); rc=$?
check "serving relays the engine's lines on stdout" $([ "$rc" = 0 ] && has "$out" '"response_type":"hello_ok"' && echo 0 || echo 1) "rc=$rc $out"
check "  ...with --decks and --source-revision" $(/usr/bin/grep -qF -- "--decks $TMP/decks --source-revision r1 [" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"
: > "$LOG"
PHASE_SBV2_DECKS=$TMP/envdecks "$RUN" </dev/null >/dev/null 2>&1
check "PHASE_SBV2_DECKS is the default catalog" $(/usr/bin/grep -qF -- "--decks $TMP/envdecks [" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"

# 6b. The generated default catalog: valid card data lets the generator run, the directory is keyed by
# the pin AND the pool+generator recipe, and a failed generation serves no catalog and leaves no tmp.
write_valid_card_data "$O/card-data-$H16.json"
rm -rf "$O"/decks-$H16-*
: > "$LOG"
"$RUN" --selftest >/dev/null 2>"$TMP/err"; rc=$?
catalog_key=$( { cat "$ROOT/oracle/xengine/pools/fdn-commons.txt" "$ROOT/scripts/phase-sbv2-decks.py"; } | sha256sum | cut -c1-16 )
DECKS=$O/decks-$H16-$catalog_key
check "the default catalog is generated under a pin+recipe key" \
	$([ "$rc" = 0 ] && [ -d "$DECKS" ] && [ "$(ls "$DECKS"/*.json 2>/dev/null | wc -l)" = 10 ] && /usr/bin/grep -qF -- "--decks $DECKS [" "$LOG" ] && echo 0 || echo 1) \
	"rc=$rc decks=$DECKS $(cat "$TMP/err")"
check "  ...and the key is the pool+generator hash, not just the pin" \
	$([ "$catalog_key" != "$H16" ] && echo 0 || echo 1) "$catalog_key"

rm -rf "$DECKS"
printf 'not json' > "$O/card-data-$H16.json"
: > "$LOG"
out=$("$RUN" --selftest 2>&1); rc=$?
check "a failing generator falls back to no catalog without a tmp left behind" \
	$([ "$rc" = 0 ] && has "$out" "no default catalog" && ! /usr/bin/grep -q -- '--decks ' "$LOG" && [ -z "$(ls -d "$O"/decks-*.tmp.* 2>/dev/null)" ] && echo 0 || echo 1) \
	"rc=$rc $out"
write_valid_card_data "$O/card-data-$H16.json"

# 7. Usage errors exit 2 before anything runs.
: > "$LOG"
out=$("$RUN" --bogus 2>&1); rc=$?
check "an unknown flag exits 2" $([ "$rc" = 2 ] && has "$out" usage && ! called cargo && echo 0 || echo 1) "rc=$rc $out"
out=$("$RUN" -decks 2>&1); rc=$?
check "-decks without a value exits 2" $([ "$rc" = 2 ] && echo 0 || echo 1) "rc=$rc $out"

[ "$fails" = 0 ] && echo "phase_sbv2_smoke: all passed" || echo "phase_sbv2_smoke: $fails FAILED"
exit $((fails != 0))
