#!/usr/bin/env bash
# podfarm.sh: endless 4-seat Commander pods in gorge (1 v 3), replay-verified. Runs ON the gate box.
# Seat 0 rotates through every commander deck in the checkout; seats 1-3 are three other decks chosen by seed.
# Usage: podfarm.sh <gorge checkout> <run dir> <workers> [extra deck dir: *.json copied into the checkout's deck dir]
#
# Kept as run (copied to the box as $FARM_ROOT/podfarm.sh); superseded by hunt-box.sh's pod4 leg. Known defects,
# left as they are (docs/hunt.md, "The pod farm"): mtgsim is never given -format commander, so the Commander decks
# play as constructed (20 life, no command zone); and under the 12G MemoryMax it ran with, the unit is OOM-killed.
set -uo pipefail
G=${1:?gorge checkout}; R=${2:?run dir}; W=${3:-8}; X=${4:-}
mkdir -p "$R/fails" "$R/ledger"
cd "$G" || exit 1
[ -n "$X" ] && cp "$X"/*.json testkit/testutil/decks/
go build -trimpath -o "$R/mtgsim" ./cmd/mtgsim || { echo "mtgsim build failed"; exit 1; }
mapfile -t DECKS < <(/usr/bin/grep -l '"commander"' testkit/testutil/decks/*.json | xargs -n1 basename | sed 's/\.json$//' | sort)
n=${#DECKS[@]}
echo "$(date -u +%FT%TZ) start $(git rev-parse --short HEAD) decks=$n workers=$W" >> "$R/ledger/podfarm.log"
printf '%s\n' "${DECKS[@]}" > "$R/decks.txt"
seed=$(cat "$R/next-seed" 2>/dev/null || echo 1)
pod() {
  local s=$1; shift; local D=("$@"); local n=${#D[@]}
  local a=$(( s % n )); local pick=("${D[$a]}") i=1
  while [ ${#pick[@]} -lt 4 ]; do
    local j=$(( (s * 2654435761 + i * 40503) % n )); i=$((i+1))
    [[ " ${pick[*]} " == *" ${D[$j]} "* ]] || pick+=("${D[$j]}")
  done
  local list; list=$(IFS=,; echo "${pick[*]}")
  local out; out=$(timeout 600 "$R/mtgsim" -decks "$list" -games 1 -seed "$s" -verify 2>&1); local rc=$?
  local line; line=$(printf '%s\n' "$out" | /usr/bin/grep -m1 '^seed ')
  if [ $rc -eq 0 ] && printf '%s\n' "$out" | /usr/bin/grep -q 'replay OK'; then
    printf '%s\tok\t%s\t%s\n' "$s" "$list" "$line"
  else
    printf '%s\n' "$out" > "$R/fails/seed-$s.txt"
    printf '%s\tFAIL rc=%s\t%s\t%s\n' "$s" "$rc" "$list" "$(printf '%s\n' "$out" | tail -1 | cut -c1-200)"
  fi
}
export -f pod; export R
while true; do
  seq "$seed" $((seed + 999)) | xargs -P "$W" -I{} bash -c 'pod {} "$@"' _ "${DECKS[@]}" >> "$R/ledger/pods.tsv"
  seed=$((seed + 1000)); echo "$seed" > "$R/next-seed"
done
