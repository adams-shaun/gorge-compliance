#!/usr/bin/env bash
# xmage-oracle-run.sh IN.jsonl OUT.jsonl
#
# Replays oracle scenarios (one JSON object per line, the scenarios/oracle
# schema gorge's runner reads) in XMage through oracle/xmageoracle's ScenarioReplay driver and
# writes one snapshot line per scenario. Needs the out-of-tree build that
# scripts/xmage-oracle-setup.sh makes. The driver is compiled against
# Mage.Tests on first use and whenever its source changes.
#
# XMAGE_ORACLE_MEM caps the JVM's systemd scope (default 12G) and
# XMAGE_ORACLE_HEAP its heap (default: 1G under the scope, so a smaller
# scope never carries a heap the kernel would OOM-kill instead of GC).
set -euo pipefail

in=${1:?usage: xmage-oracle-run.sh IN.jsonl OUT.jsonl}
out=${2:?usage: xmage-oracle-run.sh IN.jsonl OUT.jsonl}
in=$(realpath "$in")
out=$(realpath -m "$out")
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
here=$(cd "$(dirname "$0")/.." && pwd)
src="$here/oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java"

export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
[ -d "$tests/target/test-classes" ] || { echo "xmage-oracle-run: no XMage build at $root; run make xmage-oracle-setup" >&2; exit 1; }

cpfile="$root/tests.cp"
if [ ! -s "$cpfile" ]; then
  ( cd "$root/mage" && "$root/maven/bin/mvn" -q -B -pl Mage.Tests dependency:build-classpath \
      -Dmdep.outputFile="$cpfile" -Dmdep.includeScope=test )
fi
cp="$tests/target/test-classes:$tests/target/classes:$(cat "$cpfile")"

sum=$(sha256sum "$src" | cut -d' ' -f1)
# Compiled classes are keyed by the driver source sha, as
# forge-oracle-run.sh's are. One shared directory recompiled per sha let a
# worktree's replay rm -rf the classes a concurrent replay of another driver
# was still loading lazily (2026-10-13: whole sets HARNESS with
# NoClassDefFoundError ScenarioReplay$Spelling, others silently run on the
# other session's driver). A built directory is never rewritten, only
# created whole by a rename. The lock still makes the first of several
# concurrent full-replay.sh jobs the only one that compiles. The root is
# drivers/, not the old flat driver/, because a worktree on an older base
# still rm -rf's driver/ whenever its stamp differs.
classes="$root/drivers/${sum:0:12}"
exec 9>"$root/driver.lock"
flock 9
if [ ! -f "$classes/.done" ]; then
  tmp=$classes.tmp.$$
  mkdir -p "$root/drivers" && rm -rf "$tmp" && mkdir -p "$tmp"
  javac -nowarn -d "$tmp" -cp "$cp" "$src" || { rm -rf "$tmp"; exit 1; }
  touch "$tmp/.done"
  rm -rf "$classes" && mv "$tmp" "$classes"
fi
flock -u 9; exec 9>&-

mem=${XMAGE_ORACLE_MEM:-12G}; mem_g=${mem%[Gg]}
heap=${XMAGE_ORACLE_HEAP:-$(( mem_g > 2 ? mem_g - 1 : 1 ))g}
# Parallel replays (oracle/validation/oracle/full-replay.sh, FULL_REPLAY_JOBS) must
# not share XMage's H2 card DB at ./db/cards.h2: H2's auto-server handoff
# fails a JVM at start-up ("Locked by another process", or "Connection
# refused" once the owning JVM exits). XMAGE_ORACLE_PRIVATE_DB=1 runs the
# JVM in a scratch cwd beside OUT whose db/ is a private copy (~300 MB) and
# whose other entries link back to Mage.Tests.
run_dir=$tests
if [ "${XMAGE_ORACLE_PRIVATE_DB:-0}" = 1 ]; then
  run_dir="$out.cwd"; rm -rf "$run_dir"; mkdir -p "$run_dir/db"
  trap 'rm -rf "$run_dir"' EXIT
  for e in "$tests"/*; do [ "$(basename "$e")" = db ] || ln -s "$e" "$run_dir/"; done
  cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run_dir/db/"
fi
cd "$run_dir"
systemd-run --user --scope --quiet -p MemoryMax="$mem" -- \
  java -Xmx"$heap" -Dlog4j.configuration=file:/dev/null -cp "$classes:$cp" \
  org.mage.test.oracle.ScenarioReplay "$in" "$out" >"$out.log" 2>&1
