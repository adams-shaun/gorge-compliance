#!/usr/bin/env bash
# manabrew_oracle_pin_smoke.sh — prove scripts/manabrew-oracle-setup.sh refuses an
# in-repo directory and a stale archive pin, and that a good run builds with
# --locked and never moves the dev clone, without Rust, network or a Manabrew checkout.
#
# git, cargo and tar are stubs on PATH.
#
#   ops/scripts/tests/manabrew_oracle_pin_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SETUP=$ROOT/scripts/manabrew-oracle-setup.sh
# Not /tmp: the setup refuses RAM-backed build directories.
CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance
mkdir -p "$CACHE"
TMP=$(mktemp -d "$CACHE/manabrew-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

FREF=1111111111111111111111111111111111111111
MREF=2222222222222222222222222222222222222222
F12=${FREF:0:12}
M12=${MREF:0:12}
BIN=$TMP/bin
LOG=$TMP/calls.log
DIR=$TMP/mb
mkdir -p "$BIN"

cat > "$BIN/git" <<'STUB'
#!/usr/bin/env bash
echo "git $*" >> "$STUB_LOG"
args=("$@"); C=""
if [ "${args[0]}" = -C ]; then C=${args[1]}; args=("${args[@]:2}"); fi
case "${args[0]}" in
clone) mkdir -p "${args[${#args[@]}-1]}/.git" ;;
fetch | sparse-checkout) ;;
worktree) mkdir -p "${args[3]}/manabrew-rs"; echo gitdir > "${args[3]}/.git" ;;
archive) ;;
rev-parse)
	last=${args[${#args[@]}-1]}
	case "$last" in
	HEAD) echo "$STUB_HEAD" ;;
	*'^{commit}') last=${last%^\{commit\}}; echo "$last" ;;
	*:*) echo "tree-${last#*:}" ;;
	*) exit 2 ;;
	esac ;;
*) exit 2 ;;
esac
STUB
cat > "$BIN/cargo" <<'STUB'
#!/usr/bin/env bash
echo "cargo $* [cwd=$PWD CARGO_HOME=$CARGO_HOME CARGO_TARGET_DIR=$CARGO_TARGET_DIR]" >> "$STUB_LOG"
case " $* " in
*" --bin build-cardset-archive "*)
	mkdir -p "$CARGO_TARGET_DIR/release"
	printf '#!/usr/bin/env bash\necho "build-cardset-archive $*" >> "$STUB_LOG"\nprintf rkyv > "${@: -1}"\n' > "$CARGO_TARGET_DIR/release/build-cardset-archive"
	chmod +x "$CARGO_TARGET_DIR/release/build-cardset-archive" ;;
esac
STUB
# tar -x -C DIR: fake the extraction of the Forge res tree.
cat > "$BIN/tar" <<'STUB'
#!/usr/bin/env bash
echo "tar $*" >> "$STUB_LOG"
cat > /dev/null
d=$3
mkdir -p "$d/forge-gui/res/lists" "$d/forge-gui/res/cardsfolder"
[ -n "${STUB_NO_TYPELISTS:-}" ] || echo x > "$d/forge-gui/res/lists/TypeLists.txt"
STUB
chmod +x "$BIN"/*

export PATH="$BIN:$PATH" STUB_LOG=$LOG STUB_HEAD=$MREF
export FORGE_REF=$FREF FORGE_REPO=file:///nonexistent MANABREW_REPO=file:///nonexistent MANABREW_ORACLE_REF=$MREF
export MANABREW_ORACLE_DIR=$DIR CARGO=$BIN/cargo XENGINE_SCOPE=none FOLLOWERS_CACHE=$TMP/cache
unset HEAVY_LOCK STUB_NO_TYPELISTS

moved() { /usr/bin/grep -qE "^git (-C [^ ]+ )?(checkout|reset|clean|switch|stash|merge|rebase)" "$LOG" 2>/dev/null; }

# 1. A directory inside the repository is refused before anything runs.
: > "$LOG"
out=$(MANABREW_ORACLE_DIR=$ROOT/.oracle-build/manabrew "$SETUP" 2>&1); rc=$?
check "setup refuses a MANABREW_ORACLE_DIR inside the repository (exit 2)" $([ "$rc" = 2 ] && [[ $out == *"inside the repository"* ]] && echo 0 || echo 1) "rc=$rc $out"
check "  ...before any git or cargo" $([ ! -s "$LOG" ] && echo 0 || echo 1)
out=$(MANABREW_ORACLE_DIR=$ROOT/.oracle-build/manabrew "$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin refuses it too" $((rc == 2 ? 0 : 1)) "rc=$rc"

# 2. A good run: --locked cargo, the pinned archive, a three-line pin, dev clone untouched.
: > "$LOG"
out=$("$SETUP" 2>&1); rc=$?
check "setup on a good stub run succeeds" $((rc == 0 ? 0 : 1)) "rc=$rc $out"
check "  ...printing the archive line" $([[ $out == *"archive $DIR/cardset-$F12.rkyv (4 bytes), ref $M12, FORGE_REF $F12"* ]] && echo 0 || echo 1) "$out"
check "  ...cargo ran with --locked, release, in the build tree" \
	$(/usr/bin/grep -c "^cargo build --locked --release .*cwd=$DIR/build-$M12/manabrew-rs CARGO_HOME=$TMP/cache/cargo-home CARGO_TARGET_DIR=$DIR/target" "$LOG" | /usr/bin/grep -qx 2 && echo 0 || echo 1) "$(/usr/bin/grep '^cargo' "$LOG")"
check "  ...every cargo call is --locked" $([ "$(/usr/bin/grep -c '^cargo' "$LOG")" = "$(/usr/bin/grep -c '^cargo build --locked' "$LOG")" ] && echo 0 || echo 1)
check "  ...archive built from the five res inputs" \
	$(/usr/bin/grep -qF "build-cardset-archive $DIR/res-$F12/forge-gui/res/cardsfolder $DIR/res-$F12/forge-gui/res/tokenscripts $DIR/res-$F12/forge-gui/res/editions $DIR/res-$F12/forge-gui/res/blockdata $DIR/res-$F12/forge-gui/res/lists/TypeLists.txt" "$LOG" && echo 0 || echo 1)
check "  ...three-line pin" $([ "$(cat "$DIR/cardset-$F12.pin")" = "$(printf '%s\ntree-forge-gui/res/cardsfolder\ntree-forge-gui/res/tokenscripts' "$FREF")" ] && echo 0 || echo 1) "$(cat "$DIR/cardset-$F12.pin" 2>&1)"
moved; check "  ...never checkout/reset/clean on a clone" $((! $? ))
check "  ...the build tree is a detached worktree add" $(/usr/bin/grep -q "^git -C $DIR/manabrew worktree add --detach $DIR/build-$M12 $MREF" "$LOG" && echo 0 || echo 1)

# 3. --check-pin: good, then each stale pin.
: > "$LOG"
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin accepts the good build" $((rc == 0 ? 0 : 1)) "rc=$rc $out"
check "  ...with no cargo, clone or network" $(! /usr/bin/grep -qE '^(cargo|tar|git clone|git fetch)' "$LOG" && echo 0 || echo 1)

out=$(FORGE_REF=3333333333333333333333333333333333333333 "$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin: archive missing for a different gorge FORGE_REF exits 2" $([ "$rc" = 2 ] && [[ $out == *"manabrew-oracle-setup"* ]] && echo 0 || echo 1) "rc=$rc $out"

cp "$DIR/cardset-$F12.pin" "$TMP/pin.good"
printf '%s\ntree-forge-gui/res/cardsfolder\ntree-forge-gui/res/tokenscripts\n' 4444444444444444444444444444444444444444 > "$DIR/cardset-$F12.pin"
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin: a .pin whose FORGE_REF differs exits 2" $([ "$rc" = 2 ] && [[ $out == *"stale"* ]] && echo 0 || echo 1) "rc=$rc $out"
printf '%s\ntree-moved\ntree-forge-gui/res/tokenscripts\n' "$FREF" > "$DIR/cardset-$F12.pin"
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin: a .pin whose cardsfolder tree differs exits 2" $([ "$rc" = 2 ] && [[ $out == *"stale"* ]] && echo 0 || echo 1) "rc=$rc $out"
printf '%s\ntree-forge-gui/res/cardsfolder\ntree-moved\n' "$FREF" > "$DIR/cardset-$F12.pin"
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin: a .pin whose tokenscripts tree differs exits 2" $([ "$rc" = 2 ] && [[ $out == *"stale"* ]] && echo 0 || echo 1) "rc=$rc $out"
cp "$TMP/pin.good" "$DIR/cardset-$F12.pin"
out=$(STUB_HEAD=5555555555555555555555555555555555555555 "$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin: a build tree not at the ref exits 2" $((rc == 2 ? 0 : 1)) "rc=$rc $out"
rm "$DIR/cardset-$F12.rkyv"
out=$("$SETUP" --check-pin 2>&1); rc=$?
check "--check-pin: a missing archive exits 2" $([ "$rc" = 2 ] && [[ $out == *"rerun make manabrew-oracle-setup"* ]] && echo 0 || echo 1) "rc=$rc $out"

# 4. A res tree without TypeLists.txt is a hard error.
rm -rf "$DIR/res-$F12"
out=$(STUB_NO_TYPELISTS=1 "$SETUP" 2>&1); rc=$?
check "setup: missing TypeLists.txt is a hard error (exit 2)" $([ "$rc" = 2 ] && [[ $out == *TypeLists.txt* ]] && echo 0 || echo 1) "rc=$rc $out"
moved; check "  ...and still no checkout/reset/clean" $((! $? ))

[ "$fails" = 0 ] && echo "manabrew_oracle_pin_smoke: all passed" || echo "manabrew_oracle_pin_smoke: $fails FAILED"
exit $((fails != 0))
