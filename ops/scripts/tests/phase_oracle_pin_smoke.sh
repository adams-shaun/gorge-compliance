#!/usr/bin/env bash
# phase_oracle_pin_smoke.sh — prove scripts/phase-oracle-setup.sh refuses an inconsistent pin,
# a wrong card-data hash and a card-data directory inside the repository, and that a good run
# builds with `--profile follower --locked`, without the network, cargo, rust or systemd.
# curl and cargo are stubs on PATH; PHASE_FOLLOWER_DIR and PHASE_ORACLE_DIR are temp dirs.
#
#   ops/scripts/tests/phase_oracle_pin_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SETUP=$ROOT/scripts/phase-oracle-setup.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/phase-oracle-smoke.XXXXXX")
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
mkdir -p "$BIN" "$F/phase-follower"

mkpin() { # SHA_FOR_TOML SHA_FOR_LOCK
	echo "$SHA $H16 $H" > "$F/PHASE_PIN"
	cat > "$F/phase-follower/Cargo.toml" <<TOML
[package]
name = "phase-follower"

[dependencies]
engine = { package = "phase-engine", git = "https://github.com/phase-rs/phase", rev = "$1" }
serde_json = "1"
TOML
	cat > "$F/Cargo.lock" <<LOCK
[[package]]
name = "other"
version = "1"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "phase-engine"
version = "0.1.0"
source = "git+https://github.com/phase-rs/phase?rev=$2#$2"
LOCK
}

cat > "$BIN/curl" <<'STUB'
#!/usr/bin/env bash
echo "curl $*" >> "$STUB_LOG"
out=
while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
printf '%s' "$STUB_BODY" > "$out"
STUB
cat > "$BIN/cargo" <<'STUB'
#!/usr/bin/env bash
echo "cargo $* [CARGO_HOME=$CARGO_HOME CARGO_TARGET_DIR=$CARGO_TARGET_DIR cwd=$PWD]" >> "$STUB_LOG"
mkdir -p "$CARGO_TARGET_DIR/follower"
printf '#!/usr/bin/env bash\necho 42\n' > "$CARGO_TARGET_DIR/follower/phase-follower"
chmod +x "$CARGO_TARGET_DIR/follower/phase-follower"
STUB
chmod +x "$BIN"/*

export PATH="$BIN:$PATH" STUB_LOG=$LOG STUB_BODY=$BODY
export PHASE_FOLLOWER_DIR=$F PHASE_ORACLE_DIR=$TMP/oracle FOLLOWERS_CACHE=$TMP/cache CARGO=$BIN/cargo XENGINE_SCOPE=none
unset HEAVY_LOCK
called() { /usr/bin/grep -q "^$1 " "$LOG" 2>/dev/null; }

# 1. Cargo.toml rev differs from PHASE_PIN: exit 2 naming both, before curl or cargo.
mkpin "$OTHER" "$SHA"; : > "$LOG"
out=$("$SETUP" 2>&1); rc=$?
check "a Cargo.toml rev that differs from PHASE_PIN exits 2" $([ "$rc" = 2 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...naming both values" $(has "$out" "$OTHER" && has "$out" "$SHA" && echo 0 || echo 1) "$out"
check "  ...before curl or cargo" $(! called curl && ! called cargo && echo 0 || echo 1)
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "  ...and --check-pin refuses it too" $([ "$rc" = 2 ] && echo 0 || echo 1) "rc=$rc"

# 2. Cargo.lock source differs.
mkpin "$SHA" "$OTHER"; : > "$LOG"
out=$("$SETUP" 2>&1); rc=$?
check "a Cargo.lock phase-engine source that differs exits 2" $([ "$rc" = 2 ] && has "$out" "$OTHER" && has "$out" "$SHA" && echo 0 || echo 1) "rc=$rc $out"
check "  ...before curl or cargo" $(! called curl && ! called cargo && echo 0 || echo 1)

# 3. Wrong sha256 from the download: exit 2, no file, no .ok, no build.
mkpin "$SHA" "$SHA"; : > "$LOG"; rm -rf "$TMP/oracle"
out=$(STUB_BODY='tampered' "$SETUP" 2>&1); rc=$?
check "a downloaded file with the wrong sha256 exits 2" $([ "$rc" = 2 ] && has "$out" sha256 && echo 0 || echo 1) "rc=$rc $out"
check "  ...leaving no card data, .part or .ok" $([ -z "$(ls "$TMP/oracle" 2>/dev/null)" ] && echo 0 || echo 1) "$(ls "$TMP/oracle")"
check "  ...and cargo was not called" $(! called cargo && echo 0 || echo 1)
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin refuses missing card data" $([ "$rc" = 2 ] && echo 0 || echo 1) "rc=$rc"

# 4. A card-data directory inside the repository.
: > "$LOG"
out=$(PHASE_ORACLE_DIR=$ROOT/.phase-smoke-oracle "$SETUP" 2>&1); rc=$?
check "a PHASE_ORACLE_DIR inside the repository exits 2" $([ "$rc" = 2 ] && has "$out" "inside the repository" && echo 0 || echo 1) "rc=$rc $out"
check "  ...touching nothing" $([ ! -e "$ROOT/.phase-smoke-oracle" ] && ! called curl && ! called cargo && echo 0 || echo 1)

# 5. A good run.
: > "$LOG"
out=$("$SETUP" 2>&1); rc=$?
check "a good run exits 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...printing the face count and pin" $(has "$out" "phase-oracle-setup: 42 faces, pin ${SHA:0:12}+$H16" && echo 0 || echo 1) "$out"
check "  ...downloading card-data-$H16.json" $(/usr/bin/grep -q "card-data-$H16.json.part" "$LOG" && echo 0 || echo 1)
check "  ...cargo with --locked and --profile follower" $(/usr/bin/grep -q '^cargo build --locked --profile follower -p phase-follower' "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"
check "  ...in the follower dir, with the cache CARGO_HOME and target dir" \
	$(/usr/bin/grep -qF "CARGO_HOME=$TMP/cache/cargo-home CARGO_TARGET_DIR=$TMP/oracle/target cwd=$F]" "$LOG" && echo 0 || echo 1) "$(cat "$LOG")"
check "  ...leaving the card data and its .ok" $([ -s "$TMP/oracle/card-data-$H16.json" ] && [ -e "$TMP/oracle/card-data-$H16.json.ok" ] && [ ! -e "$TMP/oracle/card-data-$H16.json.part" ] && echo 0 || echo 1)
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin then passes" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"

# 6. A second run does not download again; an existing file without .ok is re-hashed.
: > "$LOG"; "$SETUP" >/dev/null 2>&1
check "a second run does not download again" $(! called curl && echo 0 || echo 1)
rm -f "$TMP/oracle/card-data-$H16.json.ok"; : > "$LOG"
"$SETUP" >/dev/null 2>&1
check "an existing file without .ok is re-hashed, not downloaded" $(! called curl && [ -e "$TMP/oracle/card-data-$H16.json.ok" ] && echo 0 || echo 1)
rm -f "$TMP/oracle/card-data-$H16.json.ok"; printf 'corrupt' > "$TMP/oracle/card-data-$H16.json"
out=$("$SETUP" 2>&1); rc=$?
check "a corrupt existing file is deleted and exits 2" $([ "$rc" = 2 ] && [ ! -e "$TMP/oracle/card-data-$H16.json" ] && echo 0 || echo 1) "rc=$rc $out"

[ "$fails" = 0 ] && echo "phase_oracle_pin_smoke: all passed" || echo "phase_oracle_pin_smoke: $fails FAILED"
exit $((fails != 0))
