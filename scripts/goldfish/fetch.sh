#!/usr/bin/env bash
# fetch.sh: MTGGoldfish metagame archetypes -> plain decklists that gorge's cmd/deckimport reads.
#
# Usage: scripts/goldfish/fetch.sh <commander|standard> [out dir] [count]
#   out dir  default <repo>/.goldfish/<format> (ignored by git: scraped lists are never committed)
#   count    how many archetypes to take from the metagame page, in its order (default 50)
#
# Writes <out>/archetypes.txt (one slug a line; written by hand or kept from an earlier run, it is used
# as is), <out>/raw/<slug>.html (the saved pages) and <out>/txt/<slug>.txt (the lists). A list already
# fetched (non-empty txt) is skipped, so a rerun only fetches what is missing.
#
# standard:  main deck, blank line, "Sideboard" and the sideboard (deckimport drops it).
# commander: main deck, blank line, "Commander" and the commander(s), found by mkcmd.py from the slug.
#
# One request at a time, GOLDFISH_SLEEP seconds apart (default 3, never less), generic User-Agent.
# docs/hunt.md ("Deck sources") has the deckimport step.
set -uo pipefail
F=${1:?usage: fetch.sh <commander|standard> [out dir] [count]}
case $F in commander) H=Commander ;; standard) H=Sideboard ;; *) echo "fetch.sh: format must be commander or standard" >&2; exit 2 ;; esac
D=$(cd "$(dirname "$0")" && pwd)
O=${2:-$D/../../.goldfish/$F}
N=${3:-50}
P=${GOLDFISH_SLEEP:-3}; [ "$P" -ge 3 ] 2>/dev/null || P=3
UA='Mozilla/5.0'
mkdir -p "$O/raw" "$O/txt" || exit 1
O=$(cd "$O" && pwd)

if [ ! -s "$O/archetypes.txt" ]; then
  curl -fsL -A "$UA" "https://www.mtggoldfish.com/metagame/$F/full" -o "$O/raw/metagame.html" \
    || { echo "fetch.sh: metagame page failed" >&2; exit 1; }
  /usr/bin/grep -o "href=\"/archetype/$F-[^\"#]*" "$O/raw/metagame.html" | sed 's#^href="/archetype/##' \
    | awk '!seen[$0]++' | head -n "$N" > "$O/archetypes.txt"
  echo "$(wc -l < "$O/archetypes.txt") archetypes"
  sleep "$P"
fi

while read -r a; do
  [ -n "$a" ] || continue
  [ -s "$O/txt/$a.txt" ] && continue
  if ! curl -fsL -A "$UA" "https://www.mtggoldfish.com/archetype/$a" -o "$O/raw/$a.html"; then
    echo "$a FETCH FAILED"; rm -f "$O/raw/$a.html"; sleep "$P"; continue
  fi
  python3 -I "$D/ext.py" "$O/raw/$a.html" | awk -v h="$H" 'BEGIN{b=0} /^$/{b=1;next} b==0{print;next} {c[++n]=$0} END{if(n){print "";print h;for(i=1;i<=n;i++)print c[i]}}' > "$O/txt/$a.txt"
  echo "$a $(wc -l < "$O/txt/$a.txt")"
  sleep "$P"
done < "$O/archetypes.txt"

if [ "$F" = commander ]; then python3 -I "$D/mkcmd.py" "$O"; fi
