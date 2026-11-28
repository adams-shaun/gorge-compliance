#!/usr/bin/env bash
# forge-sbv2.sh [--compile | --selftest]
#
# Runs Forge as a SpellBench v2 *engine*: an NDJSON server on this process's
# stdin and stdout (docs/site/api/spellbench-v2.md, "Engine role" in gorge), so
# four-yay's `cmd/xclient -engine sbv2 -engine-cmd scripts/forge-sbv2.sh` plays
# Forge with any policy. Diagnostics go to stderr; stdout carries protocol
# lines only.
#
# The engine itself is Java in the forge-oracle fork (package forge.oracle.sbv2,
# GPL-3, out of tree). This script owns what gorge-compliance owns: the pin
# invariant, the javac-per-source-sha compile, and the memory-capped scope. It
# is the same recipe as forge-oracle-run.sh and shares its compiled-class
# cache: the class directory key is the same hash of the classpath and the
# forge-oracle sources, so a tree compiled for the oracle driver is reused.
#
#   scripts/forge-sbv2.sh              # serve one engine session on stdio
#   scripts/forge-sbv2.sh --compile    # compile only; prints "driver_sha=... ref=..."
#   scripts/forge-sbv2.sh --selftest   # compile, then a hello / malformed-line exchange
#
# Needs the out-of-tree build scripts/forge-oracle-setup.sh makes (cp.txt and a
# JDK), or the overrides below.
#
# Overrides (the forge-oracle-run.sh ones carry over unchanged):
#   FORGE_ORACLE_SRC      forge-oracle project dir to compile (a candidate
#                         worktree's forge-oracle/); the pin invariant is then
#                         checked against that worktree's HEAD and its
#                         forge-gui/res is the runtime script root
#   FORGE_ORACLE_CLASSES  compiled-class root (default $FORGE_ORACLE_DIR/driver)
#   FORGE_ORACLE_TMP      java.io.tmpdir and working dir (default $FORGE_ORACLE_DIR/tmp)
#   FORGE_SBV2_MAIN       engine main class (default forge.oracle.sbv2.Sbv2Engine)
#   FORGE_SBV2_MEM        scope MemoryMax (default 4G)
#   FORGE_SBV2_HEAP       JVM -Xmx (default 2048m)
#   FORGE_SBV2_ARGS       extra JVM system properties / flags, word-split
#   XENGINE_SCOPE=none    skip systemd-run (a box without a user manager)
#
# Exit: the engine's own status when serving (0 on clean end of input), 2 for a
# precondition (pin, JDK, classpath, missing engine class), 1 for a failed selftest.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
die() { echo "forge-sbv2: $*" >&2; exit 2; }

mode=serve
case "${1:-}" in
  "") ;;
  --compile) mode=compile ;;
  --selftest) mode=selftest ;;
  *) die "usage: forge-sbv2.sh [--compile | --selftest]" ;;
esac
[ $# -le 1 ] || die "usage: forge-sbv2.sh [--compile | --selftest]"

root=${FORGE_ORACLE_DIR:-$here/$(mk FORGE_ORACLE_DIR)}
clone=${FORGE_ORACLE_CLONE:-$root/forge}
xmage=${XMAGE_ORACLE_DIR:-$here/$(mk XMAGE_ORACLE_DIR)}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
[ -n "$root" ] || die "FORGE_ORACLE_DIR unset"
root=$(realpath -m "$root")
clone=$(realpath -m "$clone")
jdk=$(realpath -m "$jdk")
main=${FORGE_SBV2_MAIN:-forge.oracle.sbv2.Sbv2Engine}

# Which tree supplies the sources and scripts, and at which commit: the setup
# clone at the pinned ref, or a candidate worktree at its own HEAD. The commit
# checked is the one on disk, and the script dirs must be unmodified.
if [ -n "${FORGE_ORACLE_SRC:-}" ]; then
  src=$(realpath "$FORGE_ORACLE_SRC")
  top=$(git -C "$src" rev-parse --show-toplevel) || die "FORGE_ORACLE_SRC $src is not in a git worktree"
  ref=$(git -C "$top" rev-parse HEAD)
else
  top=$clone
  want=${FORGE_ORACLE_REF:-$(mk FORGE_ORACLE_REF)}
  [ -n "$want" ] || die "FORGE_ORACLE_REF unset"
  ref=$(git -C "$top" rev-parse --verify -q "$want^{commit}") || die "FORGE_ORACLE_REF $want is not in $top; run make forge-oracle-setup"
  [ "$(git -C "$top" rev-parse HEAD)" = "$ref" ] || die "$top is not at FORGE_ORACLE_REF $ref; run make forge-oracle-setup"
  src=$top/forge-oracle
fi
"$here/scripts/forge-oracle-setup.sh" --check-pin "$ref" "$top" >&2
git -C "$top" diff --quiet HEAD -- forge-gui/res/cardsfolder forge-gui/res/tokenscripts || die "$top has uncommitted changes under forge-gui/res/{cardsfolder,tokenscripts}"
[ -z "$(git -C "$top" ls-files --others -- forge-gui/res/cardsfolder forge-gui/res/tokenscripts)" ] || die "$top has untracked scripts under forge-gui/res/{cardsfolder,tokenscripts}"
[ -d "$top/forge-gui/res" ] || die "no $top/forge-gui/res"
[ -d "$src/src/main/java" ] || die "no driver source at $src/src/main/java"

# The engine's Java lives in the fork. Without it there is nothing to serve, and
# the honest answer is a refusal before javac, not a hello_ok from a stand-in.
mainsrc=$src/src/main/java/$(echo "$main" | tr . /).java
[ -f "$mainsrc" ] || die "$ref has no SpellBench v2 engine ($mainsrc); the forge.oracle.sbv2 package is not in this forge-oracle tree"

[ -s "$root/cp.txt" ] || die "no $root/cp.txt; run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || die "no JDK at $jdk"

# Compiled classes are keyed by the sha of the forge-oracle source plus the
# classpath (the forge-oracle-run.sh key, so the class directory is shared). A
# lock serialises concurrent launches (xclient -workers starts several engines
# at once) and a finished directory is never removed: a running engine may be
# loading classes from it.
cp=$(cat "$root/cp.txt")
sum=$({ cat "$root/cp.txt"; find "$src/src/main/java" -name '*.java' -print0 | sort -z | xargs -0 sha256sum | sed 's|  .*/src/main/java/|  |'; } | sha256sum | cut -c1-12)
classroot=$(realpath -m "${FORGE_ORACLE_CLASSES:-$root/driver}")
classes=$classroot/$sum
if [ ! -f "$classes/.done" ]; then
  mkdir -p "$classroot"
  exec 9>"$classroot/.compile.lock"
  flock 9
  if [ ! -f "$classes/.done" ]; then
    tmp=$classes.tmp.$$
    rm -rf "$tmp" && mkdir -p "$tmp"
    mapfile -d '' srcs < <(find "$src/src/main/java" -name '*.java' -print0 | sort -z)
    javac_scope=(systemd-run --user --scope --quiet -p MemoryMax=4G -p CPUQuota=400% --)
    [ "${XENGINE_SCOPE:-systemd}" != none ] || javac_scope=()
    "${javac_scope[@]}" \
      "$jdk/bin/javac" -J-Xmx1g --release 17 -encoding UTF-8 -nowarn -cp "$cp" -d "$tmp" "${srcs[@]}" >&2
    touch "$tmp/.done"
    rm -rf "$classes" && mv "$tmp" "$classes"
  fi
  flock -u 9
  exec 9>&-
fi
[ -f "$classes/$(echo "$main" | tr . /).class" ] || die "compiled tree $classes has no class $main"

if [ "$mode" = compile ]; then
  echo "driver_sha=$sum ref=$ref"
  exit 0
fi

tmpdir=$(realpath -m "${FORGE_ORACLE_TMP:-$root/tmp}")
mkdir -p "$tmpdir"
scope=(systemd-run --user --scope --quiet -p MemoryMax="${FORGE_SBV2_MEM:-4G}" -p CPUQuota=400% --)
[ "${XENGINE_SCOPE:-systemd}" != none ] || scope=()
# shellcheck disable=SC2206
extra=(${FORGE_SBV2_ARGS:-})
engine=("${scope[@]}" \
  env FORGE_RES="$top/forge-gui/res" FORGE_ORACLE_REF="$ref" \
  "$jdk/bin/java" -Djava.awt.headless=true -Xmx"${FORGE_SBV2_HEAP:-2048m}" -XX:+UseSerialGC \
    -Djava.io.tmpdir="$tmpdir" "${extra[@]}" -cp "$classes:$cp" "$main")

if [ "$mode" = serve ]; then
  cd "$tmpdir"
  exec "${engine[@]}"
fi

# --selftest: one real engine process, one hello, one deliberately malformed
# line, then end of input. Checks the three things a host's first minute needs:
# the engine answers hello_ok under the v2 protocol with the request id echoed,
# a bad line gets malformed_json with an empty request_id and does not kill the
# session, and end of input exits 0.
cd "$tmpdir"
reply=$(mktemp "$tmpdir/forge-sbv2-selftest.XXXXXX")
trap 'rm -f "$reply"' EXIT
# A here-string, not a pipe: under `pipefail` an engine that exits before reading
# the exchange turns printf's SIGPIPE into its exit code.
selftest_in=$(printf '%s\n%s\n' \
  '{"request_type":"hello","protocol":"spellbench/v2","request_id":"selftest-hello","protocol_minor":0}' \
  '{"request_type":"hello","request_type":"hello"}')
timeout "${FORGE_SBV2_SELFTEST_TIMEOUT:-120}" "${engine[@]}" <<< "$selftest_in" >"$reply" || die_rc=$?
if [ -n "${die_rc:-}" ]; then
  echo "forge-sbv2 selftest: engine exited $die_rc" >&2
  exit 1
fi
fail() { echo "forge-sbv2 selftest: $1" >&2; echo "--- engine output:" >&2; cat "$reply" >&2; exit 1; }
[ "$(wc -l <"$reply")" = 2 ] || fail "want 2 response lines, one per request"
l1=$(sed -n 1p "$reply"); l2=$(sed -n 2p "$reply")
for want in '"response_type":"hello_ok"' '"protocol":"spellbench/v2"' '"request_id":"selftest-hello"'; do
  [[ $l1 == *"$want"* ]] || fail "hello answer lacks $want"
done
for want in '"response_type":"error"' '"code":"malformed_json"' '"request_id":""'; do
  [[ $l2 == *"$want"* ]] || fail "duplicate-key line answer lacks $want"
done
echo "forge-sbv2 selftest: ok (driver_sha=$sum ref=$ref)"
