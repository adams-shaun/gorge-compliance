#!/usr/bin/env bash
# hunt-box.sh: bug hunting on the landing box's spare capacity. Runs ON the box (copied there as
# $HUNT_ROOT/hunt.sh) as a systemd user unit; docs/hunt.md has the systemd-run line (Nice=19,
# CPUWeight=1, IOWeight=1, MemoryMax).
#
# Waves of three legs, each an independent check of gorge (G) and dirac (D):
#   pod4  mtgsim -seats 4 -format commander -verify   G: terminates, no livelock, replay-identical event chain
#   duel  mtgsim -seats 2 -format commander -verify   the same, 1v1 Commander (the format dirac plays)
#   lock  kfuzz -kernel <pool> -allow-unverified       G|D lockstep, every decision compared: cmd5 (1v1 Commander)
#                                                       or standard-meta (HUNT_FORMAT=standard)
#   std2  mtgsim -seats 2 -format constructed -verify  G: 1v1 Standard on real 60-card lists (HUNT_FORMAT=standard)
#
# Yields: no game starts while a landing gate (rgate.scope) or a train (ftrain.scope) is active, and
# games already running are killed when one starts (the gate needs the memory more than we need the
# seed; a killed seed is replayed later). Workers are re-sized every wave from measured idle cores and
# MemAvailable.
#
# Layout under $H (default /scratch/hunt):
#   gorge/            gorge worktree at the commit under test (.cards linked)
#   bin/              kfuzz, mtgsim built from gorge/ (docs/hunt.md, Setup on the box)
#   pools/cmd5        the Commander lockstep pool (DIRAC_POOL_CMD5)
#   decks/            extra Commander deck JSONs copied into gorge's deck dir
#   run/<commit12>/<leg>/ledger.tsv   seed<TAB>status<TAB>decks<TAB>summary
#   run/<commit12>/<leg>/fails/<seed>.txt   full output + REPRO line
#   run/<commit12>/signatures.tsv     leg<TAB>signature<TAB>count<TAB>first_seed<TAB>example   (rebuilt each wave)
set -uo pipefail
set -m   # job control: every wave is its own process group, so a yield kills all of it
H=${HUNT_ROOT:-/scratch/hunt}
G=$H/gorge
BIN=$H/bin
MAXW=${HUNT_MAX_WORKERS:-12}       # never more workers than this
RESERVE=${HUNT_RESERVE:-4}         # idle cores always left to everyone else
GAME_MEM_MB=${HUNT_GAME_MEM_MB:-3500}   # a 4-seat -verify game peaks ~3.2 GB
WAVE=${HUNT_WAVE:-48}              # games per leg per wave
LEGS=${HUNT_LEGS:-pod4 duel lock}
export PATH=/home/ubuntu/sdk/go1.26.3/bin:$PATH GOWORK=off
export GORGE_CARDS=$G/.cards DIRAC_POOL_CMD5=$H/pools/cmd5

log() { echo "$(date -u +%FT%TZ) $*" >> "$H/hunt.log"; }

gate_busy() {   # 0 when a gate or train scope is active
  systemctl --user is-active --quiet rgate.scope && return 0
  systemctl --user is-active --quiet ftrain.scope && return 0
  return 1
}

idle_cores() {  # whole cores idle over a 3 s sample
  local a b
  read -r _ u1 n1 s1 i1 w1 x1 y1 z1 _ < /proc/stat; sleep 3
  read -r _ u2 n2 s2 i2 w2 x2 y2 z2 _ < /proc/stat
  local busy=$(( (u2+n2+s2+x2+y2+z2) - (u1+n1+s1+x1+y1+z1) )) idle=$(( (i2+w2) - (i1+w1) ))
  echo $(( $(nproc) * idle / (busy + idle + 1) ))
}

workers() {
  local c m w
  c=$(( $(idle_cores) - RESERVE ))
  m=$(( ($(awk '/MemAvailable/{print int($2/1024)}' /proc/meminfo) - 8192) / GAME_MEM_MB ))
  w=$c; [ "$m" -lt "$w" ] && w=$m; [ "$MAXW" -lt "$w" ] && w=$MAXW; [ "$w" -lt 0 ] && w=0
  echo "$w"
}

COMMIT=$(git -C "$G" rev-parse HEAD 2>/dev/null) || { echo "no gorge worktree at $G"; exit 2; }
C12=${COMMIT:0:12}
RUN=$H/run/$C12
mkdir -p "$RUN"
FORMAT=${HUNT_FORMAT:-commander}   # commander: Commander deck files; standard: $H/decks-standard/*.json
if [ "$FORMAT" = standard ]; then
  rm -f "$G"/testkit/testutil/decks/hunt-std-*.json   # copies from an earlier start
  for f in "$H"/decks-standard/*.json; do cp "$f" "$G/testkit/testutil/decks/hunt-std-$(basename "$f")"; done
  mapfile -t DECKS < <(ls "$G"/testkit/testutil/decks/hunt-std-*.json | xargs -n1 basename | sed 's/\.json$//' | sort)
else
  [ -d "$H/decks" ] && cp -n "$H"/decks/*.json "$G/testkit/testutil/decks/" 2>/dev/null
  mapfile -t DECKS < <(/usr/bin/grep -l '"commander"' "$G"/testkit/testutil/decks/*.json | xargs -n1 basename | sed 's/\.json$//' | sort)
fi
# decks.allow (one deck name a line): only decks the deck audit passed (deckaudit/REPORT.md)
if [ "$FORMAT" = commander ] && [ -s "$H/decks.allow" ]; then
  mapfile -t DECKS < <(printf '%s\n' "${DECKS[@]}" | /usr/bin/grep -x -F -f "$H/decks.allow")
fi
[ ${#DECKS[@]} -ge 4 ] || { echo "fewer than 4 $FORMAT decks"; exit 2; }
printf '%s\n' "${DECKS[@]}" > "$RUN/decks.txt"
# mtgsim embeds its deck directory at build time (testkit/testutil/decks), so the copies above need a
# rebuild; one binary per deck set, rebuilt when the set or the commit changes.
DSUM=$(cat "$G"/testkit/testutil/decks/*.json | sha256sum | cut -c1-12)
MTGSIM=$BIN/mtgsim-$C12-$DSUM
if [ ! -x "$MTGSIM" ]; then
  ( cd "$G" && nice -n 19 go build -o "$MTGSIM.tmp" ./cmd/mtgsim ) >> "$H/hunt.log" 2>&1 && mv "$MTGSIM.tmp" "$MTGSIM" \
    || { log "mtgsim build failed"; exit 1; }
fi
export MTGSIM
log "start $C12 decks=${#DECKS[@]} legs='$LEGS'"

# pick: n distinct decks for seed s (seat 0 rotates through every deck)
pick() {
  local s=$1 n=$2 k=${#DECKS[@]} i=1 j; local out=("${DECKS[$(( s % k ))]}")
  while [ ${#out[@]} -lt "$n" ]; do
    j=$(( (s * 2654435761 + i * 40503) % k )); i=$((i+1))
    [[ " ${out[*]} " == *" ${DECKS[$j]} "* ]] || out+=("${DECKS[$j]}")
  done
  (IFS=,; echo "${out[*]}")
}

# sig: a failure's signature, numbers and names stripped
sig() {
  /usr/bin/grep -m1 -E 'LIVELOCK|FAILED|panic:|mismatch|diverge|did not terminate|fatal error|rejected' "$1" 2>/dev/null \
    | sed -E 's/^seed [0-9]+: //; s/[0-9]+ (seats|intents|events|turns)//g; s/0x[0-9a-f]+/H/g; s/[0-9]+/N/g; s/ +/ /g' | cut -c1-160
}

play_mtgsim() {  # leg seed
  local leg=$1 s=$2 seats=4 fmt=commander
  case $leg in duel) seats=2 ;; std2) seats=2 fmt=constructed ;; esac
  local d; d=$(pick "$s" "$seats")
  # Bot policies rotate by seed (HUNT_POLICIES), so the same decks are steered into different lines of
  # play. Seat 0 takes policy s mod n, seat 1 policy (s / n) mod n: every ordered pair in n*n seeds.
  local P=(${HUNT_POLICIES:-bot lethal-pressure az-redeal forge-heuristic forge-heuristic-hold}) n
  n=${#P[@]}
  local pa=${P[$(( s % n ))]} pb=${P[$(( (s / n) % n ))]}
  # Mulligans alternate with the seed's parity after the policy pairs (0 or HUNT_MULLIGANS, default 1):
  # a kept seven and a London mulligan exercise different opening code.
  local mull=0; [ $(( (s / (n * n)) % 2 )) -eq 1 ] && mull=${HUNT_MULLIGANS:-1}
  local cmd="$MTGSIM -dir $G/.cards -decks $d -seats $seats -format $fmt -games 1 -seed $s -a $pa -b $pb -mulligans $mull -verify"
  local out rc
  out=$(cd "$G" && timeout 900 $cmd 2>&1); rc=$?
  if [ $rc -eq 0 ] && printf '%s\n' "$out" | /usr/bin/grep -q 'replay OK'; then
    printf '%s\tok\t%s\t%s\n' "$s" "$d $pa/$pb m$mull" "$(printf '%s\n' "$out" | /usr/bin/grep -m1 '^seed ' | cut -c1-200)"
  else
    mkdir -p "$RUN/$leg/fails"
    { printf 'REPRO: cd %s && %s\nrc=%s\n' "$G" "$cmd" "$rc"; printf '%s\n' "$out"; } > "$RUN/$leg/fails/$s.txt"
    printf '%s\tFAIL rc=%s\t%s\t%s\n' "$s" "$rc" "$d $pa/$pb m$mull" "$(printf '%s\n' "$out" | tail -1 | cut -c1-200)"
  fi
}
export -f play_mtgsim pick; export G BIN RUN MTGSIM

wave_mtgsim() {  # leg workers
  local leg=$1 w=$2 L=$RUN/$1/ledger.tsv; mkdir -p "$RUN/$leg"
  local s; s=$(cat "$RUN/$leg/next-seed" 2>/dev/null || echo 1)
  echo $((s + WAVE)) > "$RUN/$leg/next-seed"     # claim the range first: a killed wave never replays the same seeds twice in a row
  DECKS_STR=$(printf '%s\n' "${DECKS[@]}")
  ( seq "$s" $((s + WAVE - 1)) | DECKS_STR="$DECKS_STR" xargs -P "$w" -I{} bash -c 'mapfile -t DECKS <<<"$DECKS_STR"; play_mtgsim '"$leg"' {}' >> "$L" ) &
  watch_gate $!
}

wave_lock() {  # workers
  local w=$1 d=$RUN/lock; mkdir -p "$d"
  local s; s=$(cat "$d/next-seed" 2>/dev/null || echo 2)
  echo $((s + 1)) > "$d/next-seed"
  local o=$d/s$s; mkdir -p "$o"
  # The lockstep pool follows the deck set: cmd5 (the Foundations Commander precons) or standard-meta
  # (the MTGGoldfish Standard metagame decks, gorge's committed slice). HUNT_LOCK_POOL overrides it.
  local pool=${HUNT_LOCK_POOL:-cmd5}; [ "$FORMAT" = standard ] && pool=${HUNT_LOCK_POOL:-standard-meta}
  # No DIRAC_TABLES: kfuzz finds the tables of its own build key under the corpus; the newest directory
  # by mtime is not necessarily this binary's.
  ( cd "$G" && timeout 3600 "$BIN/kfuzz" -kernel "$pool" -allow-unverified -pairs "$WAVE" -seed "$s" \
      -workers "$w" -keep-going -shrink 0 -implicates-all -policy random -out "$o/repro" -coverage "$o/coverage.tsv" > "$o/out.txt" 2>&1
    echo "rc=$?" >> "$o/out.txt" ) &
  watch_gate $!
}

watch_gate() {  # pid: wait for it, killing its process tree if a gate starts
  local p=$1
  while kill -0 "$p" 2>/dev/null; do
    if gate_busy; then
      log "gate active: killing wave pid $p"
      kill -TERM -- "-$p" 2>/dev/null; sleep 2; kill -KILL -- "-$p" 2>/dev/null
      break
    fi
    sleep 5
  done
  wait "$p" 2>/dev/null
}

signatures() {
  { for leg in pod4 duel std2; do
      for f in "$RUN/$leg"/fails/*.txt; do [ -e "$f" ] || continue
        printf '%s\t%s\t%s\n' "$leg" "$(sig "$f")" "$f"; done
    done
    for o in "$RUN"/lock/s*/out.txt; do [ -e "$o" ] || continue
      /usr/bin/grep -E '^(bad-game|pair [0-9]+ .*(divergence|panic|hang|error))' "$o" | sed -E 's/\t.*//; s/seed [0-9]+/seed N/; s/pair [0-9]+/pair N/; s/[0-9]+/N/g' | cut -c1-160 \
        | while read -r l; do printf 'lock\t%s\t%s\n' "$l" "$o"; done
    done
  } | awk -F'\t' '{k=$1"\t"$2; n[k]++; if(!(k in ex)) ex[k]=$3} END {for (k in n) print k"\t"n[k]"\t"ex[k]}' | sort -t$'\t' -k3,3nr > "$RUN/signatures.tsv.tmp" \
    && mv "$RUN/signatures.tsv.tmp" "$RUN/signatures.tsv"
}

touch "$H/HEARTBEAT"
while true; do
  [ -e "$H/STOP" ] && { log "STOP file: exiting"; exit 0; }
  if gate_busy; then sleep 30; continue; fi
  for leg in $LEGS; do
    gate_busy && break
    w=$(workers)
    if [ "$w" -lt 1 ]; then log "no spare capacity"; sleep 60; break; fi
    case $leg in
      pod4|duel|std2) wave_mtgsim "$leg" "$w" ;;
      lock) wave_lock "$w" ;;
    esac
    log "wave $leg workers=$w done"
  done
  signatures
  touch "$H/HEARTBEAT"
done
