#!/usr/bin/env bash
# xmage_sbv2_smoke.sh — prove scripts/xmage-sbv2.sh's launcher behaviour without Maven, a real
# JDK, the network or SpellBench: it fetches the two pins from local git fixtures, builds once
# (under the heavy-lock and a memory scope) on first use, verifies BUILD-MANIFEST.json's identity
# strings against the pins and against the patch series, refuses a stale, stock or wrong-pin
# build before any engine runs, serves with stdio passed through bash engine.sh under JDK 21, and
# its --selftest passes an engine that answers the hello exchange and fails one that does not.
#
# git is real (file:// fixtures); java, javac, mvn, build.sh, engine.sh and systemd-run are stubs.
# What this does NOT show: that the real build compiles, or that the real engine speaks the
# protocol (make xmage-sbv2-setup and `xmage-sbv2.sh --selftest` do, on a box with Maven and
# network). The identity formulas the stub build.sh mirrors were checked once against upstream's
# real patches/xmage (xpat 6f8a902b..., sets xpat 94cbe27a...); see the commit message.
#
#   ops/scripts/tests/xmage_sbv2_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
LAUNCH=$ROOT/scripts/xmage-sbv2.sh
mkdir -p "$ROOT/.oracle-build"
TMP=$(mktemp -d "$ROOT/.oracle-build/xmage-sbv2-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}
has() { [[ $1 == *"$2"* ]]; }
gitq() { git -c user.name=t -c user.email=t@example.invalid -c commit.gpgsign=false -c init.defaultBranch=main "$@"; }

# --- fixtures: a fake XMage and a fake spellbench, both real git repos fetched by sha --------
UP=$TMP/up
mkdir -p "$UP/mage/Mage.Sets/src" "$UP/sb/engines/xmage/"{scripts,patches/xmage}
echo 'class Card {}' > "$UP/mage/Mage.Sets/src/Card.java"
gitq -C "$UP/mage" init -q && gitq -C "$UP/mage" add -A && gitq -C "$UP/mage" commit -q -m mage
XREF=$(git -C "$UP/mage" rev-parse HEAD)
git -C "$UP/mage" config uploadpack.allowAnySHA1InWant true
printf 'diff --git a/Mage.Sets/src/Card.java b/Mage.Sets/src/Card.java\n+sets change\ndiff --git a/Mage/src/Core.java b/Mage/src/Core.java\n+core change\n' > "$UP/sb/engines/xmage/patches/xmage/0001-x.patch"
printf 'diff --git a/Mage/src/Rng.java b/Mage/src/Rng.java\n+rng\n' > "$UP/sb/engines/xmage/patches/xmage/0002-y.patch"
printf 'XMAGE_UPSTREAM=file://%s\nXMAGE_COMMIT=%s\nCABT_COMMIT=cccccccccccccccccccccccccccccccccccccccc\n' "$UP/mage" "$XREF" > "$UP/sb/engines/xmage/pins.env"

# build.sh stub: mirrors the real identity formulas; STUB_BUILD=stock|oldjdk|badrev corrupts it.
cat > "$UP/sb/engines/xmage/scripts/build.sh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
HERE=$(cd "$(dirname "$0")/.." && pwd)
. "$HERE/pins.env"
echo "build.sh args=$* PATH0=${PATH%%:*} JAVA_HOME=${JAVA_HOME:-} mem=${MAVEN_OPTS:-}" >> "$STUB_LOG"
[ "${STUB_BUILD_FAIL:-}" != 1 ] || { echo "stub build failure" >&2; exit 1; }
while [ $# -gt 0 ]; do case "$1" in --out) OUT=$2; shift 2 ;; --xmage-repo) XR=$2; shift 2 ;; --m2) shift 2 ;; esac; done
mkdir -p "$OUT/lib" "$OUT/src"
sha256() { sha256sum | cut -c1-64; }
printf 'jar-%s' "${STUB_JAR:-1}" > "$OUT/lib/a.jar"; echo a.jar > "$OUT/lib/classpath.txt"
P=$(ls "$HERE"/patches/xmage/*.patch | LC_ALL=C sort)
XPAT=$(cat $P | sha256); SX=$(cat $P | awk '/^diff --git /{keep = ($3 ~ /^a\/Mage\.Sets\//)} keep' | sha256)
series=applied; [ "${STUB_BUILD:-}" != stock ] || { XPAT=none; SX=none; series=none; }
SPELL=$(git -C "$HERE" rev-parse HEAD); [ "${STUB_BUILD:-}" != badrev ] || SPELL=0000000000000000000000000000000000000000
jdk='openjdk version "21.0.13.1" 2026-01-20'; [ "${STUB_BUILD:-}" != oldjdk ] || jdk='openjdk version "17.0.1" 2021-10-19'
{
echo "{"
echo "  \"xmage_commit\": \"$XMAGE_COMMIT\","
echo "  \"cabt_commit\": \"$CABT_COMMIT\","
echo "  \"spellbench_source_revision\": \"$SPELL\","
echo "  \"patch_series\": \"$series\","
echo "  \"rules_snapshot_id\": \"xmage-$XMAGE_COMMIT-xpat-$XPAT\","
echo "  \"card_pool_identity\": \"xmage-sets-$(git -C "$XR" rev-parse "$XMAGE_COMMIT:Mage.Sets")-xpat-$SX\","
echo "  \"jdk\": \"$(printf '%s' "$jdk" | sed 's/"/\\"/g')\","
echo "  \"maven\": \"Apache Maven 3.9.11\","
echo "  \"jars\": {"
echo "    \"a.jar\": \"$(sha256 < "$OUT/lib/a.jar")\""
echo "  },"
echo "  \"lib_digest\": \"$(cd "$OUT/lib" && LC_ALL=C ls *.jar | while read -r j; do printf '%s  %s\n' "$(sha256 < "$j")" "$j"; done | sha256)\""
echo "}"
} > "$OUT/BUILD-MANIFEST.json"
cat "$OUT/BUILD-MANIFEST.json"
STUB
# engine.sh stub: STUB_ENGINE=good answers the hello exchange; crash exits 3; mute reads and says nothing.
cat > "$UP/sb/engines/xmage/scripts/engine.sh" <<'STUB'
#!/usr/bin/env bash
echo "engine.sh args=$* JAVA_HOME=${JAVA_HOME:-} path0=${PATH%%:*} cwd=$PWD db=$(ls "$(dirname "$0")/../../../../" >/dev/null 2>&1; echo ok)" >> "$STUB_LOG"
[ -x "$0" ] && echo "engine.sh was executable" >> "$STUB_LOG"
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
gitq -C "$UP/sb" init -q && gitq -C "$UP/sb" add -A && gitq -C "$UP/sb" commit -q -m sb
SREF=$(git -C "$UP/sb" rev-parse HEAD)
git -C "$UP/sb" config uploadpack.allowAnySHA1InWant true

# --- stubs on PATH --------------------------------------------------------------------------
BIN=$TMP/bin; LOG=$TMP/calls.log; mkdir -p "$BIN" "$TMP/jdk/bin"
cat > "$TMP/jdk/bin/javac" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat > "$TMP/jdk/bin/java" <<'STUB'
#!/usr/bin/env bash
echo "java cwd=$PWD args=$*" >> "$STUB_LOG"
case "$*" in
*-version*) echo "openjdk version \"${STUB_JAVA_VERSION:-21.0.13.1}\" 2026-01-20" >&2 ;;
*--scan-only*) mkdir -p db && echo card > db/cards.h2 && echo "x1: card database built" ;;
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

export PATH="$BIN:$PATH" STUB_LOG=$LOG
export XMAGE_SBV2_REF=$SREF XMAGE_SBV2_XMAGE_REF=$XREF XMAGE_SBV2_REPO=file://$UP/sb XMAGE_SBV2_JDK=$TMP/jdk
unset XMAGE_SBV2_XMX XMAGE_SBV2_MEM XMAGE_SBV2_WORK XENGINE_SCOPE HEAVY_LOCK STUB_BUILD STUB_BUILD_FAIL STUB_ENGINE STUB_JAR STUB_JAVA_VERSION

fresh() { # a new cache root with the Maven stub the setup would otherwise download
	export XMAGE_SBV2_DIR=$TMP/cache.$1
	mkdir -p "$XMAGE_SBV2_DIR/maven/bin" && printf '#!/bin/sh\nexit 0\n' > "$XMAGE_SBV2_DIR/maven/bin/mvn" && chmod +x "$XMAGE_SBV2_DIR/maven/bin/mvn"
	: > "$LOG"
}
count() { /usr/bin/grep -c "^$1" "$LOG" 2>/dev/null || true; }

# 0. argument and pin refusals, before anything is fetched or built.
fresh args
out=$("$LAUNCH" --nonsense 2>&1); rc=$?
check "refuses an unknown flag (exit 2)" $([ "$rc" = 2 ] && has "$out" usage && echo 0 || echo 1) "rc=$rc $out"
out=$(XMAGE_SBV2_REF=abc "$LAUNCH" --verify 2>&1); rc=$?
check "refuses a non-sha spellbench pin (exit 2)" $([ "$rc" = 2 ] && has "$out" "XMAGE_SBV2_REF" && echo 0 || echo 1) "rc=$rc $out"
out=$(XMAGE_SBV2_DIR=/tmp/x "$LAUNCH" --verify 2>&1); rc=$?
check "refuses a cache under /tmp (RAM-backed)" $([ "$rc" = 2 ] && has "$out" "/tmp" && echo 0 || echo 1) "rc=$rc $out"
out=$(env -u XMAGE_SBV2_REF -u XMAGE_SBV2_XMAGE_REF "$LAUNCH" --verify 2>&1); rc=$?
check "the Makefile pins are read when the env sets none (reaches 'no BUILD-MANIFEST')" $([ "$rc" = 2 ] && has "$out" "no $XMAGE_SBV2_DIR/build/BUILD-MANIFEST.json" || { [ "$rc" = 2 ] && has "$out" "not a clean checkout"; } && echo 0 || echo 1) "rc=$rc $out"
check "the Makefile pins are full shas and --setup is wired" \
	$([ -n "$(sed -n 's/^XMAGE_SBV2_REF ?= \([0-9a-f]\{40\}\)$/\1/p' "$ROOT/Makefile")" ] && [ -n "$(sed -n 's/^XMAGE_SBV2_XMAGE_REF ?= \([0-9a-f]\{40\}\)$/\1/p' "$ROOT/Makefile")" ] && /usr/bin/grep -q 'scripts/xmage-sbv2.sh --setup' "$ROOT/Makefile" && echo 0 || echo 1)

# 1. first use builds once, under heavy-lock path and a memory scope, then serves.
fresh first
out=$(printf 'a\nb\n' | "$LAUNCH" 2>"$TMP/err"); rc=$?
check "first use builds, then serves stdio through the engine (exit 0)" $([ "$rc" = 0 ] && [ "$(printf '%s\n' "$out" | wc -l)" = 2 ] && echo 0 || echo 1) "rc=$rc $out $(tail -3 "$TMP/err")"
check "  ...stdout carried protocol lines only (no build chatter)" $([[ $out != *build* && $out != *"card database"* ]] && echo 0 || echo 1) "$out"
check "  ...both pins were fetched into the cache, at the pins" \
	$([ "$(git -C "$XMAGE_SBV2_DIR/spellbench" rev-parse HEAD)" = "$SREF" ] && [ "$(git -C "$XMAGE_SBV2_DIR/xmage-src" rev-parse HEAD)" = "$XREF" ] && echo 0 || echo 1)
check "  ...build.sh ran once, with JDK 21 and Maven first on PATH, under MemoryMax" \
	$([ "$(count build.sh)" = 1 ] && /usr/bin/grep -q "^build.sh args=--out $XMAGE_SBV2_DIR/build.new --xmage-repo $XMAGE_SBV2_DIR/xmage-src --m2 $XMAGE_SBV2_DIR/m2 PATH0=$TMP/jdk/bin" "$LOG" && /usr/bin/grep -q '^systemd-run .*MemoryMax=7G' "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep '^build.sh' "$LOG")"
check "  ...the card database was scanned from lib/* and installed with its stamp" \
	$(/usr/bin/grep -q '^java cwd=.*dbwork args=-Xmx2g -cp .*/build/lib/\* mage.player.spellbench.x1.DeterminismCheck --scan-only' "$LOG" && [ -f "$XMAGE_SBV2_DIR/db/cards.h2" ] && [ -s "$XMAGE_SBV2_DIR/db.done" ] && [ ! -d "$XMAGE_SBV2_DIR/build/src" ] && echo 0 || echo 1)
check "  ...engine.sh ran through bash (it is not executable), JDK 21 first on PATH, --build --db --work --xmx" \
	$(/usr/bin/grep -q "^engine.sh args=--build $XMAGE_SBV2_DIR/build --db $XMAGE_SBV2_DIR/db --work $XMAGE_SBV2_DIR/work --xmx 2g JAVA_HOME=$TMP/jdk path0=$TMP/jdk/bin" "$LOG" && ! /usr/bin/grep -q 'was executable' "$LOG" && /usr/bin/grep -q '^systemd-run .*MemoryMax=4G' "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep '^engine.sh' "$LOG")"

# 2. a verified cache is reused: no fetch, no build; knobs reach the command line.
: > "$LOG"
printf '' | XMAGE_SBV2_XMX=3g XMAGE_SBV2_MEM=6G "$LAUNCH" >/dev/null 2>&1
check "the second launch reuses the verified cache (no build.sh, no scan)" $([ "$(count build.sh)" = 0 ] && [ "$(count java)" = 0 ] && echo 0 || echo 1)
check "XMAGE_SBV2_XMX and _MEM reach the command line" $(/usr/bin/grep -q -- '--xmx 3g' "$LOG" && /usr/bin/grep -q 'MemoryMax=6G' "$LOG" && echo 0 || echo 1)
XENGINE_SCOPE=none "$LAUNCH" </dev/null >/dev/null 2>&1; c=$(count systemd-run)
check "XENGINE_SCOPE=none skips systemd-run" $([ "$c" = 1 ] && echo 0 || echo 1) "systemd-run calls: $c (the one from the previous launch expected)"
STUB_ENGINE=crash "$LAUNCH" </dev/null >/dev/null 2>&1; rc=$?
check "the engine's exit status is the launcher's" $([ "$rc" = 3 ] && echo 0 || echo 1) "rc=$rc"
out=$("$LAUNCH" --verify 2>&1); rc=$?
check "--verify passes the cache, naming the identity" $([ "$rc" = 0 ] && has "$out" "verified" && has "$out" "rules_snapshot_id=xmage-$XREF-xpat-" && echo 0 || echo 1) "rc=$rc $out"
: > "$LOG"; "$LAUNCH" --setup >/dev/null 2>&1; rc=$?
check "--setup on a verified cache is a no-op" $([ "$rc" = 0 ] && [ "$(count build.sh)" = 0 ] && echo 0 || echo 1) "rc=$rc"

# 3. a cache that fails its identity check is refused before any engine runs.
check_refused() { # name sed-edit-of-the-manifest expected-text
	local out rc
	: > "$LOG"
	cp "$XMAGE_SBV2_DIR/build/BUILD-MANIFEST.json" "$TMP/manifest.good"
	sed -i "$2" "$XMAGE_SBV2_DIR/build/BUILD-MANIFEST.json"
	out=$("$LAUNCH" </dev/null 2>&1 >/dev/null); rc=$?
	check "refuses a manifest with $1 (exit 2, naming it)" $([ "$rc" = 2 ] && has "$out" "$3" && echo 0 || echo 1) "rc=$rc $out"
	check "  ...before any engine, build or scan" $([ "$(count engine.sh)" = 0 ] && [ "$(count build.sh)" = 0 ] && echo 0 || echo 1)
	cp "$TMP/manifest.good" "$XMAGE_SBV2_DIR/build/BUILD-MANIFEST.json"
}
check_refused "a different XMage commit" 's/"xmage_commit": "[0-9a-f]*"/"xmage_commit": "1111111111111111111111111111111111111111"/' "xmage_commit"
check_refused "a different spellbench revision" 's/"spellbench_source_revision": "[0-9a-f]*"/"spellbench_source_revision": "2222222222222222222222222222222222222222"/' "spellbench_source_revision"
check_refused "a tampered patch digest" 's/-xpat-\([0-9a-f]\)/-xpat-0\1/' "rules_snapshot_id"
check_refused "a stock (unpatched) series" 's/"patch_series": "applied"/"patch_series": "none"/' "not applied"
check_refused "a different pool identity" 's/"card_pool_identity": "xmage-sets-[0-9a-f]*/"card_pool_identity": "xmage-sets-3333/' "card_pool_identity"
check_refused "a non-21 JDK" 's/version \\"21\./version \\"17./' "not JDK 21"
check_refused "a different CABT pin" 's/"cabt_commit": "[0-9a-f]*"/"cabt_commit": "4444"/' "cabt_commit"
echo 'drift' >> "$XMAGE_SBV2_DIR/spellbench/README.drift"
out=$("$LAUNCH" </dev/null 2>&1 >/dev/null); rc=$?
check "refuses an edited/untracked spellbench clone" $([ "$rc" = 2 ] && has "$out" "not a clean checkout" && echo 0 || echo 1) "rc=$rc $out"
rm -f "$XMAGE_SBV2_DIR/spellbench/README.drift"
echo 'other-scan' > "$XMAGE_SBV2_DIR/db.done"
out=$("$LAUNCH" </dev/null 2>&1 >/dev/null); rc=$?
check "refuses a db template scanned from a different build" $([ "$rc" = 2 ] && has "$out" "different build" && echo 0 || echo 1) "rc=$rc $out"
"$LAUNCH" --setup >/dev/null 2>&1   # setup repairs (rebuilds + rescans) a cache that does not verify
check "--setup rebuilds a cache that fails its check, and it verifies again" $("$LAUNCH" --verify >/dev/null 2>&1 && echo 0 || echo 1)
echo tamper > "$XMAGE_SBV2_DIR/build/lib/a.jar"
out=$("$LAUNCH" --verify 2>&1); rc=$?
check "--verify (deep) catches a jar that no longer matches lib_digest" $([ "$rc" = 2 ] && has "$out" "lib_digest" && echo 0 || echo 1) "rc=$rc $out"
"$LAUNCH" </dev/null >/dev/null 2>&1; rc=$?
check "  ...while a plain serve (identity strings only) does not rehash" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc"

# 4. builds that violate the pins are refused by the freshly-built check (before db.done is trusted).
fresh stock
out=$(STUB_BUILD=stock "$LAUNCH" --setup 2>&1); rc=$?
check "a stock build is refused at setup (exit 2)" $([ "$rc" = 2 ] && has "$out" "fresh build fails its identity check" && echo 0 || echo 1) "rc=$rc $out"
fresh jdk
out=$(STUB_JAVA_VERSION=17.0.1 "$LAUNCH" --setup 2>&1); rc=$?
check "a JDK that is not 21 is refused before the build (exit 2)" $([ "$rc" = 2 ] && has "$out" "not a JDK 21" && [ "$(count build.sh)" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
out=$(XMAGE_SBV2_JDK=$TMP/nojdk "$LAUNCH" --setup 2>&1); rc=$?
check "a JRE-only JDK path is refused (no javac)" $([ "$rc" = 2 ] && has "$out" "no bin/javac" && echo 0 || echo 1) "rc=$rc $out"
fresh wrongx
out=$(XMAGE_SBV2_XMAGE_REF=5555555555555555555555555555555555555555 "$LAUNCH" --setup 2>&1); rc=$?
check "an XMage pin that disagrees with spellbench's pins.env is refused before the build" $([ "$rc" = 2 ] && has "$out" "disagrees" && [ "$(count build.sh)" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
fresh failb
out=$(STUB_BUILD_FAIL=1 "$LAUNCH" --setup 2>&1); rc=$?
check "a failing build leaves no manifest, so the next launch rebuilds (not trusts)" $([ "$rc" != 0 ] && [ ! -f "$XMAGE_SBV2_DIR/build/BUILD-MANIFEST.json" ] && echo 0 || echo 1) "rc=$rc"

# 5. the heavy-lock wrapper is honoured.
fresh lock
printf '#!/bin/sh\necho "heavylock $*" >> "$STUB_LOG"\nshift\nexec "$@"\n' > "$TMP/lockwrap"; chmod +x "$TMP/lockwrap"
HEAVY_LOCK=$TMP/lockwrap "$LAUNCH" --setup >/dev/null 2>&1
check "the setup build goes through HEAVY_LOCK (name xmage-sbv2-setup)" $(/usr/bin/grep -q '^heavylock .*--name xmage-sbv2-setup' "$LOG" && echo 0 || echo 1)

# 6. two first launches racing (xclient -workers) build once.
fresh race
for i in 1 2; do ( printf '' | "$LAUNCH" >/dev/null 2>&1; echo $? > "$TMP/race.$i" ) & done; wait
check "two concurrent first launches both succeed with a single build" $([ "$(cat "$TMP/race.1")" = 0 ] && [ "$(cat "$TMP/race.2")" = 0 ] && [ "$(count build.sh)" = 1 ] && echo 0 || echo 1) "rc=$(cat "$TMP/race.1"),$(cat "$TMP/race.2") builds=$(count build.sh)"

# 7. --selftest passes a good engine and fails a bad or crashing one.
out=$("$LAUNCH" --selftest 2>&1); rc=$?
check "--selftest passes an engine that answers hello and malformed_json" $([ "$rc" = 0 ] && has "$out" "selftest: ok" && echo 0 || echo 1) "rc=$rc $out"
out=$(STUB_ENGINE=wrongcode "$LAUNCH" --selftest 2>&1); rc=$?
check "--selftest fails an engine that gets the exchange wrong (exit 1)" $([ "$rc" = 1 ] && has "$out" "hello answer lacks" && echo 0 || echo 1) "rc=$rc $out"
out=$(STUB_ENGINE=crash "$LAUNCH" --selftest 2>&1); rc=$?
check "--selftest fails a crashing engine (exit 1)" $([ "$rc" = 1 ] && has "$out" "engine exited 3" && echo 0 || echo 1) "rc=$rc $out"

[ "$fails" = 0 ] && echo "xmage_sbv2_smoke: all passed" || echo "xmage_sbv2_smoke: $fails FAILED"
exit $((fails != 0))
