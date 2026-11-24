#!/usr/bin/env bash
# forge-oracle-run.sh IN.jsonl OUT.jsonl
# forge-oracle-run.sh --preflight NAMES.txt
# forge-oracle-run.sh --compile
#
# Replays Forge-oracle requests (one JSON object per line) in
# Forge through the driver and writes one result row per request. Needs the
# out-of-tree build that scripts/forge-oracle-setup.sh makes. The driver is
# compiled with javac against $FORGE_ORACLE_DIR/cp.txt on first use and
# whenever its source (or the classpath) changes. The pass wrapper, not this
# script, takes the heavy lock.
#
#   --preflight   print NAME<TAB>REASON on stdout for each name in NAMES.txt
#                 the pinned Forge cannot resolve as a card (the protocol
#                 cmd/oraclediff/follower_export.go drives): a name whose
#                 Name: line is in no cardsfolder card file; a split card
#                 name ("Fire // Ice") whose every face's Name: line is in
#                 none either (Forge's card files hold one Name: line per
#                 face, so the joined name is never a Name line); and a name
#                 whose card file exists but which Forge's card DB filters
#                 (named anywhere in a Type=Funny or Border=Silver edition
#                 outside [eternal], as Gobland is: driver "Couldn't find a
#                 card"); this includes a split/adventure card named jointly
#                 ("Knowing // Half the Battle", "Curious Pair // Treats to
#                 Share") when the joined name or its first face is filtered.
#                 One grep pass over the pinned cardsfolder and one awk pass
#                 over its editions; no javac, no JVM, no lock.
#
# Overrides:
#   FORGE_ORACLE_SRC      the forge-oracle project dir to compile and replay (a
#                         candidate worktree's forge-oracle/); default is the
#                         setup clone's. The pin invariant is then checked
#                         against that worktree's HEAD and its forge-gui/res is
#                         the runtime script root.
#   FORGE_ORACLE_CLASSES  compiled-driver root (default $FORGE_ORACLE_DIR/driver)
#   FORGE_ORACLE_TMP      java.io.tmpdir (default $FORGE_ORACLE_DIR/tmp); point
#                         this and FORGE_ORACLE_CLASSES elsewhere when the build
#                         directory is read-only
#   FORGE_ORACLE_MAIN     driver main class (default forge.oracle.ScenarioReplay)
#   FORGE_ORACLE_MEM / FORGE_ORACLE_HEAP   scope MemoryMax / JVM heap (3G / 1536m)
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
die() { echo "forge-oracle-run: $*" >&2; exit 2; }

if [ "${1:-}" = --compile ]; then
  mode=compile
elif [ "${1:-}" = --preflight ]; then
  mode=preflight
  names=${2:?usage: forge-oracle-run.sh IN.jsonl OUT.jsonl | --preflight NAMES.txt | --compile}
else
  in=${1:?usage: forge-oracle-run.sh IN.jsonl OUT.jsonl | --preflight NAMES.txt | --compile}
  out=${2:?usage: forge-oracle-run.sh IN.jsonl OUT.jsonl | --preflight NAMES.txt | --compile}
  mode=run
fi
root=${FORGE_ORACLE_DIR:-$here/$(mk FORGE_ORACLE_DIR)}
clone=${FORGE_ORACLE_CLONE:-$root/forge}
xmage=${XMAGE_ORACLE_DIR:-$here/$(mk XMAGE_ORACLE_DIR)}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
[ -n "$root" ] || die "FORGE_ORACLE_DIR unset"
root=$(realpath -m "$root")
clone=$(realpath -m "$clone")
jdk=$(realpath -m "$jdk")

# Which tree supplies the scripts, and at which commit: the setup clone at the
# pinned ref, or a candidate worktree at its own HEAD. Either way the checked
# commit must be the one on disk, and the script dirs must be unmodified.
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
git -C "$top" diff --quiet HEAD -- forge-gui/res/cardsfolder forge-gui/res/tokenscripts forge-gui/res/editions || die "$top has uncommitted changes under forge-gui/res/{cardsfolder,tokenscripts,editions}"
# Untracked (even ignored) scripts are loaded too, but git diff cannot see them.
[ -z "$(git -C "$top" ls-files --others -- forge-gui/res/cardsfolder forge-gui/res/tokenscripts forge-gui/res/editions)" ] || die "$top has untracked scripts under forge-gui/res/{cardsfolder,tokenscripts,editions}"
[ -d "$top/forge-gui/res" ] || die "no $top/forge-gui/res"

# The preflight protocol owns stdout (the pin check printed its verdict on
# stderr): a name the pinned Forge cannot resolve as a card is one whose
# Name: line is in no cardsfolder card file — exactly the setup the driver
# fails with "Couldn't find a card" for, before it replays a step. A split
# card name ("Fire // Ice") resolves when every face's Name: line is in the
# cardsfolder, since Forge's card files hold one Name: line per face and no
# file holds the joined "Name:Fire // Ice" line. The pinned cardsfolder lags
# the corpus, so forge-export refuses such an item instead of replaying it
# into a harness row.
if [ "$mode" = preflight ]; then
  [ -r "$names" ] || die "cannot read $names"
  cardsfolder=$top/forge-gui/res/cardsfolder
  pats=$(mktemp) || die "no temp file for the preflight patterns"
  trap 'rm -f "$pats"' EXIT
  : > "$pats"
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    printf 'Name:%s\n' "$n" >> "$pats"
    case "$n" in
    *" // "*)
      face=${n%%" // "*}; rest=${n#*" // "}
      while :; do
        printf 'Name:%s\n' "$face" >> "$pats"
        case "$rest" in
        *" // "*) face=${rest%%" // "*}; rest=${rest#*" // "} ;;
        *) printf 'Name:%s\n' "$rest" >> "$pats"; break ;;
        esac
      done ;;
    esac
  done < "$names"
  # One fixed-string whole-line pass over the cardsfolder; the matched Name
  # lines, sorted, are the asked names (and, for a split name, its faces)
  # the pin resolves (both faces of a double-faced card are Name lines of
  # one file).
  hits=$(grep -rhx -F -f "$pats" "$cardsfolder" 2>/dev/null | sort -u || true)
  # every_face_in_hits NAME: 0 when each face of the split name NAME has its
  # Name: line in $hits.
  every_face_in_hits() {
    local rest=$1 face
    while :; do
      face=${rest%%" // "*}
      grep -qxF "Name:$face" <<<"$hits" || return 1
      case "$rest" in
      *" // "*) rest=${rest#*" // "} ;;
      *) return 0 ;;
      esac
    done
  }
  # The cardsfolder is not the card DB: the driver's StaticData is built with
  # loadNonLegalCards=false, which drops (StaticData's funnyCards/filtered) every
  # card NAMED in a Type=Funny or Border=Silver edition's card sections other
  # than [eternal] (the legal reprints), basic lands excepted. Such a card has
  # its cardsfolder file and still fails SetupBuilder.create's getCard with
  # "Couldn't find a card" (Gobland, Indicate and Mox Poison: Mystery Booster 2
  # playtest rows). StaticData always loads the editions, so a tree without
  # them cannot answer the preflight; refuse rather than silently skip the
  # filter (find -H so an editions dir reached through a symlink still counts).
  editions=$top/forge-gui/res/editions
  [ -d "$editions" ] || die "no $editions; the preflight's card-DB filter needs the pinned editions"
  funny=$(find -H "$editions" -maxdepth 1 -name '*.txt' -print0 | sort -z | xargs -r0 awk '
    FNR == 1 { if (n) flush(); n = 0; sec = ""; funny = 0 }
    function flush(  i) { if (funny) for (i = 1; i <= n; i++) print names[i] }
    /^\[/ { sec = $0; gsub(/^\[|\][ \t\r]*$/, "", sec); next }
    sec == "metadata" {
      k = $0; sub(/=.*/, "", k); v = $0; sub(/^[^=]*=/, "", v); sub(/\r$/, "", v)
      k = tolower(k); v = tolower(v)
      if ((k == "type" && v == "funny") || (k == "border" && v == "silver")) funny = 1
      next
    }
    sec ~ /^(cards|special slot|precon product|borderless|etched|showcase|full art|extended art|alternate art|retro frame|buy a box|promo|prerelease promo|bundle|box topper|jumpstart|rebalanced|conjured|scheme|printsheets)$/ {
      line = $0; sub(/\r$/, "", line); sub(/ @.*$/, "", line); sub(/ \$\{.*$/, "", line)
      if (line == "") next
      m = split(line, w, " ")
      if (m >= 3 && w[1] ~ /^.?[0-9A-Z-]+[^ ]*$/ && w[2] ~ /^[SCURML]$/) sub(/^[^ ]+ [SCURML] /, "", line)
      else if (m >= 2 && w[1] ~ /^[SCURML]$/) sub(/^[SCURML] /, "", line)
      if (line ~ /^(Snow-Covered )?(Plains|Island|Swamp|Mountain|Forest|Wastes)$/) next
      names[++n] = line
    }
    END { flush() }
  ' | sort -u)
  # funny_has NAME: 0 when NAME is one of the edition names StaticData filters.
  funny_has() { [ -n "$funny" ] && grep -qxF -e "$1" <<<"$funny"; }
  # funny_face NAME: 0 when a scenario's bare NAME can only be the first face
  # of a split card the filter drops. A scenario may name a split by its bare
  # first face (Knowing, for Knowing // Half the Battle); the editions list only
  # the joined name, so funny_has alone never sees it.
  # Decide from the card files, not a name prefix: Forge's filter is exact on
  # the joined name, and a bare name can be defined by several files. Each file
  # with a "Name:NAME" line is judged by its own joined name (its Name: lines in
  # order, joined with " // "). The bare name is refused only when EVERY such
  # file is a multi-face card whose first face is NAME and whose joined name is
  # filtered. A standalone card (Bind beside the Funny "Bind // Liberate") or a
  # legal split (Start // Finish beside the Funny "Start // Fire") still loads,
  # so any one of those accepts the name.
  funny_face() {
    [ -n "$funny" ] || return 1
    # Cheap prefilter: some filtered joined name must contain "NAME // " before
    # the per-file cardsfolder scan below (one grep over every card file) runs.
    grep -qF -e "$1 // " <<<"$funny" || return 1
    local f joined any=1
    while IFS= read -r f; do
      grep -q '^AlternateMode:' "$f" || return 1
      joined=$(awk '/^Name:/ { v = $0; sub(/^Name:/, "", v); sub(/\r$/, "", v); j = (j == "" ? v : j " // " v) } END { print j }' "$f")
      [ "${joined%%" // "*}" = "$1" ] || return 1
      funny_has "$joined" || return 1
      any=0
    done < <(grep -rlxF -e "Name:$1" "$cardsfolder" 2>/dev/null | sort)
    return $any
  }
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    # A name resolves when its own Name: line is in the cardsfolder, or (a
    # split/adventure name, which Forge never stores as a joined Name: line)
    # when every face's Name: line is.
    if ! grep -qxF "Name:$n" <<<"$hits"; then
      case "$n" in
      *" // "*)
        every_face_in_hits "$n" || { printf '%s\t%s\n' "$n" "not in the pinned cardsfolder"; continue; } ;;
      *)
        printf '%s\t%s\n' "$n" "not in the pinned cardsfolder"; continue ;;
      esac
    fi
    # Every resolved name goes through this one card-DB filter, so a split or
    # adventure name cannot skip it. Forge keys a COMBINE (split) card as the
    # joined "main // other" (CardRules.getPreInitName), and an adventure/MDFC
    # by its main face; the editions list each the same way, but the corpus
    # names both jointly. Refuse when the joined name or its first face is in
    # the filtered set; a bare first face is judged by funny_face's per-file
    # rule (refused only when every card file defining it is a filtered split).
    first=${n%%" // "*}
    if funny_has "$n" || { [ "$first" != "$n" ] && funny_has "$first"; } ||
       { [ "$first" = "$n" ] && funny_face "$n"; }; then
      printf '%s\t%s\n' "$n" "in the pinned cardsfolder but filtered from Forge's card DB (Funny/Silver-border edition card)"
    fi
  done < "$names"
  exit 0
fi

[ -d "$src/src/main/java" ] || die "no driver source at $src/src/main/java"

# item.die_rolls is only honoured when the driver has the step-scoped force
# (ForcedDieRandom, armed by StepMachine) and MyRandom forwards the bounded
# draw RollDiceEffect makes to it. A driver with either half missing silently
# rolls its own die: Ebony Fly animates to 5/5 where gorge has 3/3. Check the
# source at the ref about to run, so a pin bump cannot lose the force without
# this failing before a scenario is replayed.
fdr=$src/src/main/java/forge/oracle/ForcedDieRandom.java
if [ ! -f "$fdr" ] || ! grep -q 'class ForcedDieRandom' "$fdr"; then
  die "$ref has no step-scoped die force ($fdr); FORGE_ORACLE_REF predates item.die_rolls"
fi
for f in "$src/src/main/java/forge/oracle/ForgeEngine.java:new ForcedDieRandom" \
         "$src/src/main/java/forge/oracle/StepMachine.java:dice.arm("; do
  file=${f%%:*}; want=${f#*:}
  if [ ! -f "$file" ] || ! grep -qF "$want" "$file"; then
    die "$ref does not wire the forced die ($file lacks \"$want\"); FORGE_ORACLE_REF predates item.die_rolls"
  fi
done
mymy=$top/forge-core/src/main/java/forge/util/MyRandom.java
if [ ! -f "$mymy" ] || ! grep -qF 'delegate.nextInt(bound)' "$mymy"; then
  die "$ref does not forward nextInt(bound) ($mymy); FORGE_ORACLE_REF predates item.die_rolls"
fi

# item.coin_flips is only honoured when the driver reads it and arms it on the
# step that flips (ForcedDieRandom.nextBoolean, with the unbounded nextInt a
# next(bits)-routing wrapper lands on), MyRandom forwards the boolean draw,
# and the scripted controller calls the flip heads (the forced result is the
# outcome itself). A driver with any half missing silently flips from its own
# stream: Mana Clash deals 1 damage per tails it rolled itself and loops a
# different number of times than gorge's recorded game. Same check-the-source
# discipline as the die force above.
creq=$src/src/main/java/forge/oracle/Request.java
if [ ! -f "$creq" ] || ! grep -qF 'coin_flips' "$creq"; then
  die "$ref does not read item.coin_flips ($creq); FORGE_ORACLE_REF predates item.coin_flips"
fi
for f in "$src/src/main/java/forge/oracle/ForcedDieRandom.java:req.coinFlips(step)" \
         "$src/src/main/java/forge/oracle/ScriptedController.java:coinFlips(m.step())" \
         "$mymy:delegate.nextBoolean()"; do
  file=${f%%:*}; want=${f#*:}
  if [ ! -f "$file" ] || ! grep -qF "$want" "$file"; then
    die "$ref does not wire the coin force ($file lacks \"$want\"); FORGE_ORACLE_REF predates item.coin_flips"
  fi
done

[ -s "$root/cp.txt" ] || die "no $root/cp.txt; run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || die "no JDK at $jdk"

# Compiled classes are keyed by the sha of the driver source plus the
# classpath, so a candidate worktree never clobbers the pinned build.
cp=$(cat "$root/cp.txt")
sum=$({ cat "$root/cp.txt"; find "$src/src/main/java" -name '*.java' -print0 | sort -z | xargs -0 sha256sum | sed 's|  .*/src/main/java/|  |'; } | sha256sum | cut -c1-12)
classes=$(realpath -m "${FORGE_ORACLE_CLASSES:-$root/driver}")/$sum
if [ ! -f "$classes/.done" ]; then
  tmp=$classes.tmp.$$
  rm -rf "$tmp" && mkdir -p "$tmp"
  mapfile -d '' srcs < <(find "$src/src/main/java" -name '*.java' -print0 | sort -z)
  javac_scope=(systemd-run --user --scope --quiet -p MemoryMax=4G -p CPUQuota=400% --)
  [ "${XENGINE_SCOPE:-systemd}" != none ] || javac_scope=()
  "${javac_scope[@]}" \
    "$jdk/bin/javac" -J-Xmx1g --release 17 -encoding UTF-8 -nowarn -cp "$cp" -d "$tmp" "${srcs[@]}"
  touch "$tmp/.done"
  rm -rf "$classes" && mv "$tmp" "$classes"
fi
if [ "$mode" = compile ]; then
  echo "driver_sha=$sum ref=$ref"
  exit 0
fi

tmpdir=$(realpath -m "${FORGE_ORACLE_TMP:-$root/tmp}")
mkdir -p "$tmpdir"
out=$(realpath -m "$out")
in=$(realpath "$in")
chunk_size=${FORGE_ORACLE_CHUNK:-1000}
[[ "$chunk_size" =~ ^[1-9][0-9]*$ ]] || die "FORGE_ORACLE_CHUNK must be a positive integer (got: $chunk_size)"
run_tmp=$(mktemp -d "$tmpdir/forge-oracle-run.XXXXXX")
trap 'rm -rf "$run_tmp"' EXIT
mkdir -p "$(dirname "$out")"
: > "$run_tmp/combined.out"
: > "$out.log"
split -d -a 8 -l "$chunk_size" -- "$in" "$run_tmp/input."
chunk_number=0
scope=(systemd-run --user --scope --quiet -p MemoryMax="${FORGE_ORACLE_MEM:-3G}" -p CPUQuota=400% --)
[ "${XENGINE_SCOPE:-systemd}" != none ] || scope=()
cd "$tmpdir"
for chunk in "$run_tmp"/input.*; do
  [ -f "$chunk" ] || continue
  chunk_number=$((chunk_number + 1))
  chunk_name=$(printf 'chunk %d' "$chunk_number")
  chunk_out="$run_tmp/output.$(printf '%08d' "$chunk_number")"
  chunk_log="$run_tmp/log.$(printf '%08d' "$chunk_number")"
  if ! "${scope[@]}" \
    env FORGE_RES="$top/forge-gui/res" FORGE_ORACLE_REF="$ref" \
    "$jdk/bin/java" -Djava.awt.headless=true -Xmx"${FORGE_ORACLE_HEAP:-1536m}" -XX:+UseSerialGC \
      -Djava.io.tmpdir="$tmpdir" -cp "$classes:$cp" "${FORGE_ORACLE_MAIN:-forge.oracle.ScenarioReplay}" \
      "$chunk" "$chunk_out" >"$chunk_log" 2>&1; then
    cat "$chunk_log" >> "$out.log"
    echo "forge-oracle-run: $chunk_name failed (input $(basename "$chunk"))" >&2
    cat "$chunk_log" >&2
    exit 1
  fi
  cat "$chunk_log" >> "$out.log"
  cat "$chunk_out" >> "$run_tmp/combined.out"
done
cat "$run_tmp/combined.out" > "$out"
