#!/usr/bin/env bash
# xmage-sbv2.sh [--setup | --verify | --selftest]
#
# Runs SpellBench upstream's XMage engine as a SpellBench v2 *engine*: an NDJSON server on this
# process's stdin and stdout (docs/site/api/spellbench-v2.md in gorge), so four-yay's
#   cmd/xclient -engine sbv2 -engine-cmd scripts/xmage-sbv2.sh -format standard-2022-25-bo1 ...
# seats a bot at an XMage table (docs/design/2026-11-24-xmage-seat-route.md, route (a)).
# Diagnostics go to stderr; stdout carries protocol lines only.
#
# The engine is MIT code out of tree (jackmaiorino/spellbench `engines/xmage`, XMage plus its
# determinism patch series and the mage.player.spellbench overlay). This script owns what
# gorge-compliance owns: the two pins, the out-of-tree build, the identity check, and the
# memory-capped scope. Nothing is vendored, gorge is never touched.
#
#   scripts/xmage-sbv2.sh              # serve one engine session on stdio (builds on first use)
#   scripts/xmage-sbv2.sh --setup      # fetch, build, scan the card db, verify; idempotent, heavy
#   scripts/xmage-sbv2.sh --verify     # re-check the cached build against the pins (also rehashes jars)
#   scripts/xmage-sbv2.sh --selftest   # verify, then a hello / malformed-line exchange with the engine
#
# Pins (Makefile, one line each, like XMAGE_REF; the env wins):
#   XMAGE_SBV2_REF         the spellbench revision (build.sh, patches, overlay, CABT vendor)
#   XMAGE_SBV2_XMAGE_REF   the XMage commit that revision builds (its pins.env must agree)
# This build is NOT the compliance oracle's: XMAGE_REF stays at its own commit and a verdict is
# never measured on one and cited on the other.
#
# Layout under XMAGE_SBV2_DIR (default .oracle-build/xmage-sbv2, gitignored; share it across
# clones like XMAGE_ORACLE_DIR): spellbench/ (shallow clone at the pin), xmage-src/ (shallow
# clone at the XMage pin), jdk/ maven/ m2/ (toolchain), build/ (lib/ + BUILD-MANIFEST.json),
# db/ (card database template, ~260 MB, copied per engine process), work/ (per-process dirs).
#
# Overrides:
#   XMAGE_SBV2_DIR        cache root
#   XMAGE_SBV2_REPO       spellbench git URL (default https://github.com/jackmaiorino/spellbench.git)
#   XMAGE_SBV2_JDK        a full JDK 21 (javac present); default $DIR/jdk, downloaded (Temurin 21) if missing.
#                         A JRE-only java-21 is not enough: XMage compiles with --release 8.
#   XMAGE_SBV2_XMX        engine JVM -Xmx (default 2g)
#   XMAGE_SBV2_MEM        serve scope MemoryMax (default 4G)
#   XMAGE_SBV2_WORK       per-process working dirs (default $DIR/work; /tmp is often RAM-backed)
#   XENGINE_SCOPE=none    skip systemd-run (a box without a user manager)
#
# Not set here: -Dspellbench.priorityMana=true (engine default off; a follow-up question).
#
# Exit: the engine's own status when serving (0 on clean end of input), 2 for a precondition
# (pin, identity, JDK, a failed build step), 1 for a failed selftest.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
die() { echo "xmage-sbv2: $*" >&2; exit 2; }
log() { echo "xmage-sbv2: $*" >&2; }

mode=serve
case "${1:-}" in
  "") ;;
  --setup) mode=setup ;;
  --verify) mode=verify ;;
  --selftest) mode=selftest ;;
  *) die "usage: xmage-sbv2.sh [--setup | --verify | --selftest]" ;;
esac
[ $# -le 1 ] || die "usage: xmage-sbv2.sh [--setup | --verify | --selftest]"

sbref=${XMAGE_SBV2_REF:-$(mk XMAGE_SBV2_REF)}
xref=${XMAGE_SBV2_XMAGE_REF:-$(mk XMAGE_SBV2_XMAGE_REF)}
[[ $sbref =~ ^[0-9a-f]{40}$ ]] || die "XMAGE_SBV2_REF unset or not a full commit sha: '$sbref'"
[[ $xref =~ ^[0-9a-f]{40}$ ]] || die "XMAGE_SBV2_XMAGE_REF unset or not a full commit sha: '$xref'"
root=${XMAGE_SBV2_DIR:-$here/$(mk XMAGE_SBV2_DIR)}
[ -n "$root" ] || die "XMAGE_SBV2_DIR unset"
root=$(realpath -m "$root")
case "$root" in /tmp/*) die "$root is under /tmp, which is often RAM-backed; the build needs a directory on disk" ;; esac
sburl=${XMAGE_SBV2_REPO:-https://github.com/jackmaiorino/spellbench.git}
jdk=$(realpath -m "${XMAGE_SBV2_JDK:-$root/jdk}")
mvn_version=3.9.11
sb=$root/spellbench
xsrc=$root/xmage-src
build=$root/build
db=$root/db
manifest=$build/BUILD-MANIFEST.json
pins=$sb/engines/xmage/pins.env

sha256() { sha256sum | cut -c1-64; }
# jf FIELD: a top-level string field of BUILD-MANIFEST.json (one "key": "value" per line).
jf() { sed -n "s/^ *\"$1\": *\"\(.*\)\",\{0,1\}\$/\1/p" "$manifest" | head -1; }
pin() { sed -n "s/^$1=//p" "$pins" | head -1; }

# The spellbench clone must be exactly at the pin with nothing edited or added.
check_sb_clone() {
  [ -d "$sb/.git" ] || return 1
  [ "$(git -C "$sb" rev-parse HEAD 2>/dev/null)" = "$sbref" ] || return 1
  [ -z "$(git -C "$sb" status --porcelain -- . ':!**/__pycache__/**' 2>/dev/null)" ] || return 1
  [ -f "$pins" ]
}

# The patch-series digests build.sh derives, recomputed here from the pinned patches so the
# manifest is checked against the source, not against itself.
expect_xpat() { ls "$sb"/engines/xmage/patches/xmage/*.patch | LC_ALL=C sort | xargs cat | sha256; }
expect_sets_xpat() { ls "$sb"/engines/xmage/patches/xmage/*.patch | LC_ALL=C sort | xargs cat | awk '/^diff --git /{keep = ($3 ~ /^a\/Mage\.Sets\//)} keep' | sha256; }

# verify [deep]: identity strings in BUILD-MANIFEST.json against the pins. Prints the reason and
# returns 1 on the first mismatch. deep also rehashes lib/*.jar against lib_digest.
verify() {
  local deep=${1:-} want got tree
  [ -f "$manifest" ] || { echo "no $manifest"; return 1; }
  check_sb_clone || { echo "$sb is not a clean checkout of XMAGE_SBV2_REF $sbref"; return 1; }
  [ "$(pin XMAGE_COMMIT)" = "$xref" ] || { echo "XMAGE_SBV2_XMAGE_REF $xref disagrees with $sbref's pins.env XMAGE_COMMIT $(pin XMAGE_COMMIT)"; return 1; }
  [ "$(jf spellbench_source_revision)" = "$sbref" ] || { echo "manifest spellbench_source_revision '$(jf spellbench_source_revision)' is not the pin $sbref"; return 1; }
  [ "$(jf xmage_commit)" = "$xref" ] || { echo "manifest xmage_commit '$(jf xmage_commit)' is not the pin $xref"; return 1; }
  [ "$(jf cabt_commit)" = "$(pin CABT_COMMIT)" ] || { echo "manifest cabt_commit '$(jf cabt_commit)' is not pins.env CABT_COMMIT"; return 1; }
  [ "$(jf patch_series)" = applied ] || { echo "manifest patch_series is '$(jf patch_series)', not applied (a stock build is the negative control)"; return 1; }
  want="xmage-$xref-xpat-$(expect_xpat)"
  [ "$(jf rules_snapshot_id)" = "$want" ] || { echo "manifest rules_snapshot_id '$(jf rules_snapshot_id)' != $want"; return 1; }
  got=$(jf card_pool_identity)
  if [ -d "$xsrc/.git" ] && tree=$(git -C "$xsrc" rev-parse "$xref:Mage.Sets" 2>/dev/null); then
    want="xmage-sets-$tree-xpat-$(expect_sets_xpat)"
    [ "$got" = "$want" ] || { echo "manifest card_pool_identity '$got' != $want"; return 1; }
  else
    [[ $got == xmage-sets-*-xpat-$(expect_sets_xpat) ]] || { echo "manifest card_pool_identity '$got' lacks the pinned Mage.Sets patch digest"; return 1; }
  fi
  [[ $(jf jdk) =~ version[^0-9]*21[.] ]] || { echo "manifest jdk '$(jf jdk)' is not JDK 21 (upstream pins 21/23)"; return 1; }
  [ -s "$build/lib/classpath.txt" ] || { echo "no $build/lib/classpath.txt"; return 1; }
  [ -s "$db.done" ] && [ -d "$db" ] || { echo "no card database template at $db"; return 1; }
  [ "$(cat "$db.done")" = "$(jf lib_digest)" ] || { echo "card database template was scanned from a different build (db.done != lib_digest)"; return 1; }
  if [ -n "$deep" ]; then
    got=$(cd "$build/lib" && LC_ALL=C ls ./*.jar | sed 's|^\./||' | while read -r j; do printf '%s  %s\n' "$(sha256 <"$j")" "$j"; done | sha256)
    [ "$got" = "$(jf lib_digest)" ] || { echo "lib/*.jar digest $got != manifest lib_digest $(jf lib_digest)"; return 1; }
  fi
}

ensure_jdk() {
  if [ ! -x "$jdk/bin/javac" ]; then
    [ -z "${XMAGE_SBV2_JDK:-}" ] || die "XMAGE_SBV2_JDK=$jdk has no bin/javac (a JRE is not enough)"
    local tgz=$root/temurin-21-jdk.tar.gz
    log "downloading Temurin 21 to $jdk"
    curl -fsSL -o "$tgz" "https://api.adoptium.net/v3/binary/latest/21/ga/linux/x64/jdk/hotspot/normal/eclipse" >&2
    rm -rf "$jdk" && mkdir -p "$jdk"
    tar -xzf "$tgz" -C "$jdk" --strip-components=1
  fi
  "$jdk/bin/java" -version 2>&1 | awk '/version "/{print; exit}' | /usr/bin/grep -Eq 'version "21[.]' || die "$jdk is not a JDK 21"
}

ensure_maven() {
  local mvn=$root/maven/bin/mvn
  if [ ! -x "$mvn" ]; then
    local tgz=$root/apache-maven-$mvn_version-bin.tar.gz
    curl -fsSL -o "$tgz" "https://archive.apache.org/dist/maven/maven-3/$mvn_version/binaries/apache-maven-$mvn_version-bin.tar.gz" >&2
    rm -rf "$root/maven" && mkdir -p "$root/maven"
    tar -xzf "$tgz" -C "$root/maven" --strip-components=1
  fi
}

# fetch_pin DIR URL SHA: a shallow clone with exactly SHA checked out, fetching only if absent.
fetch_pin() {
  local dir=$1 url=$2 sha=$3
  if [ ! -d "$dir/.git" ]; then
    git init -q "$dir"
    git -C "$dir" remote add origin "$url"
  fi
  git -C "$dir" cat-file -e "$sha^{commit}" 2>/dev/null || git -C "$dir" fetch -q --depth 1 origin "$sha"
  git -C "$dir" checkout -q --detach "$sha"
}

# setup: everything heavy, serialised by the caller's lock. Never writes to stdout.
setup() {
  mkdir -p "$root"
  fetch_pin "$sb" "$sburl" "$sbref" >&2
  check_sb_clone || die "$sb is not a clean checkout of $sbref after fetch"
  [ "$(pin XMAGE_COMMIT)" = "$xref" ] || die "XMAGE_SBV2_XMAGE_REF $xref disagrees with $sbref's pins.env XMAGE_COMMIT $(pin XMAGE_COMMIT); bump both pins together"
  fetch_pin "$xsrc" "$(pin XMAGE_UPSTREAM)" "$xref" >&2
  ensure_jdk
  ensure_maven
  mkdir -p "$root/m2" "$root/work"

  local scope=(systemd-run --user --scope --quiet -p MemoryMax=7G -p CPUQuota=800% --)
  [ "${XENGINE_SCOPE:-systemd}" != none ] || scope=()
  local heavy=("$here/scripts/heavy-lock.sh" --wait 20000 --mem 8G --name xmage-sbv2-setup --)
  rm -rf "$root/build.new"
  log "building the engine at spellbench ${sbref:0:12} / XMage ${xref:0:12} (first build about 10 minutes)"
  # build.sh needs mvn, java and python3 on PATH and prints the manifest on stdout (sent to stderr).
  PYTHONDONTWRITEBYTECODE=1 JAVA_HOME=$jdk PATH="$jdk/bin:$root/maven/bin:$PATH" MAVEN_OPTS=${MAVEN_OPTS:--Xmx4g} \
    "${heavy[@]}" "${scope[@]}" \
    bash "$sb/engines/xmage/scripts/build.sh" --out "$root/build.new" --xmage-repo "$xsrc" --m2 "$root/m2" >&2
  rm -rf "$root/build.new/src"
  rm -f "$db.done"
  rm -rf "$build" "$db" "$root/dbwork"
  mv "$root/build.new" "$build"

  # The card database template: DeterminismCheck --scan-only writes ./db, copied per process.
  log "scanning the card database template"
  mkdir -p "$root/dbwork"
  ( cd "$root/dbwork" && JAVA_HOME=$jdk PATH="$jdk/bin:$PATH" "${heavy[@]}" "${scope[@]}" \
      "$jdk/bin/java" -Xmx2g -cp "$build/lib/*" mage.player.spellbench.x1.DeterminismCheck --scan-only >&2 )
  [ -d "$root/dbwork/db" ] || die "the card scan produced no db directory"
  mv "$root/dbwork/db" "$db"
  rm -rf "$root/dbwork"
  jf lib_digest >"$db.done"

  local why
  why=$(verify deep) || die "the fresh build fails its identity check: $why"
  log "built: rules_snapshot_id=$(jf rules_snapshot_id)"
  log "       card_pool_identity=$(jf card_pool_identity)"
}

# ensure: the cached build verified, built first when there is none. A cache that exists but
# fails the check is an error, never a silent rebuild: re-run --setup after a pin bump.
ensure() {
  local why
  mkdir -p "$root"
  if [ -f "$manifest" ] && [ -d "$db" ]; then
    why=$(verify) || die "cached build fails its pin check: $why; run scripts/xmage-sbv2.sh --setup (or make xmage-sbv2-setup)"
    return 0
  fi
  # First use. xclient -workers starts engines together: one builds, the rest wait and re-check.
  exec 9>"$root/.setup.lock"
  flock 9
  if [ ! -f "$manifest" ] || [ ! -d "$db" ]; then setup; fi
  flock -u 9
  exec 9>&-
  why=$(verify) || die "cached build fails its pin check: $why"
}

case $mode in
  setup)
    mkdir -p "$root"
    exec 9>"$root/.setup.lock"
    flock 9
    # idempotent: a build that already verifies is left alone
    if [ -f "$manifest" ] && [ -d "$db" ] && why=$(verify deep); then
      log "already built and verified: rules_snapshot_id=$(jf rules_snapshot_id)"
    else
      setup
    fi
    exit 0 ;;
  verify)
    why=$(verify deep) || die "verification failed: $why"
    echo "xmage-sbv2: verified (spellbench=$sbref xmage=$xref rules_snapshot_id=$(jf rules_snapshot_id))"
    exit 0 ;;
esac

ensure
work=$(realpath -m "${XMAGE_SBV2_WORK:-$root/work}")
mkdir -p "$work"
scope=(systemd-run --user --scope --quiet -p MemoryMax="${XMAGE_SBV2_MEM:-4G}" -p CPUQuota=400% --)
[ "${XENGINE_SCOPE:-systemd}" != none ] || scope=()
# engine.sh is committed non-executable, so it is run through bash. The JDK 21 on PATH is the
# `java` it launches (the distro default may be a newer, untested JDK).
engine=("${scope[@]}" env JAVA_HOME="$jdk" PATH="$jdk/bin:$PATH" \
  bash "$sb/engines/xmage/scripts/engine.sh" --build "$build" --db "$db" --work "$work" --xmx "${XMAGE_SBV2_XMX:-2g}")

if [ "$mode" = serve ]; then
  exec "${engine[@]}"
fi

# --selftest: one real engine process, one hello, one deliberately malformed line, then end of
# input. The engine must answer hello_ok under spellbench/v2 with the request id echoed, answer
# the bad line with malformed_json and an empty request_id without dying, and exit 0.
reply=$(mktemp "$work/xmage-sbv2-selftest.XXXXXX")
trap 'rm -f "$reply"' EXIT
# A here-string, not a pipe: under `pipefail` an engine that exits before reading
# the exchange turns printf's SIGPIPE into its exit code.
selftest_in=$(printf '%s\n%s\n' \
  '{"request_type":"hello","protocol":"spellbench/v2","request_id":"selftest-hello","protocol_minor":0}' \
  '{"request_type":"hello","request_type":"hello"}')
timeout "${XMAGE_SBV2_SELFTEST_TIMEOUT:-180}" "${engine[@]}" <<< "$selftest_in" >"$reply" || die_rc=$?
if [ -n "${die_rc:-}" ]; then
  echo "xmage-sbv2 selftest: engine exited $die_rc" >&2
  exit 1
fi
fail() { echo "xmage-sbv2 selftest: $1" >&2; echo "--- engine output:" >&2; cat "$reply" >&2; exit 1; }
[ "$(wc -l <"$reply")" = 2 ] || fail "want 2 response lines, one per request"
l1=$(sed -n 1p "$reply"); l2=$(sed -n 2p "$reply")
for want in '"response_type":"hello_ok"' '"protocol":"spellbench/v2"' '"request_id":"selftest-hello"'; do
  [[ $l1 == *"$want"* ]] || fail "hello answer lacks $want"
done
for want in '"response_type":"error"' '"code":"malformed_json"' '"request_id":""'; do
  [[ $l2 == *"$want"* ]] || fail "duplicate-key line answer lacks $want"
done
echo "xmage-sbv2 selftest: ok (spellbench=$sbref xmage=$xref)"
