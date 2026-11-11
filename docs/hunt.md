# hunt: bug hunting on a shared box's spare capacity

`scripts/hunt-box.sh` plays waves of games on a machine that also runs landing gates, using only the
capacity nobody else needs, and records every failure with the command that reproduces it. It runs on
the remote gate box today, beside `cmd/farm` (lockstep sweeps) and the landing gate.

`scripts/deck-audit.py` and `cmd/deckcheck` audit the deck lists the hunter plays, because a game on a
wrong deck list (a card the deck never held, a banned card) wastes the hunt and misleads triage.

## What a wave plays

Each wave runs every leg in `HUNT_LEGS` once, `HUNT_WAVE` games per leg (default 48):

| Leg | Command | Checks |
|---|---|---|
| `pod4` | `mtgsim -seats 4 -format commander -games 1 -seed N -verify` | gorge alone: the game terminates, no livelock, rejected intent or panic, and a replay reproduces the event chain |
| `duel` | the same with `-seats 2` | the same, 1v1 Commander |
| `lock` | `kfuzz -kernel cmd5 -allow-unverified -keep-going -shrink 0 -implicates-all` | gorge against dirac in lockstep, every decision compared (1v1 Commander on the `cmd5` pool) |
| `std2` | `mtgsim -seats 2 -format constructed -games 1 -seed N -verify` | gorge alone, as `duel`, on real 60-card Standard lists (`HUNT_FORMAT=standard`) |

`HUNT_FORMAT` picks the deck set. `commander` (the default) uses the Commander deck files in the gorge
checkout's `testkit/testutil/decks/`, plus any in `$HUNT_ROOT/decks/`, narrowed to the names in
`$HUNT_ROOT/decks.allow` when that file exists. `standard` uses every deck file in
`$HUNT_ROOT/decks-standard/`, copied into the checkout as `hunt-std-<name>.json`. Seat 0 rotates through
every deck; the others are chosen from the seed.

`mtgsim` embeds its deck directory when it's built, so the hunter builds one `mtgsim` per gorge commit and
deck set (`bin/mtgsim-<commit12>-<decks sha12>`) at start.

## Deck sources

Both deck sets beyond gorge's own come from MTGGoldfish's metagame archetypes: `scripts/goldfish/`
fetches the lists as plain text, and gorge's `cmd/deckimport` turns each into a deck file. Scraped pages
and lists are never committed; the default output directory, `.goldfish/`, is ignored by git.

| File | What |
|---|---|
| `scripts/goldfish/fetch.sh <commander\|standard> [out] [count]` | Reads `https://www.mtggoldfish.com/metagame/<format>/full`, keeps the first `count` (default 50) `/archetype/<format>-...` slugs in `out/archetypes.txt`, then fetches each archetype page to `out/raw/` and writes its list to `out/txt/<slug>.txt`. `out` defaults to `.goldfish/<format>`. |
| `scripts/goldfish/ext.py PAGE` | Prints the list in a saved archetype page's `deck_input[deck]` field. |
| `scripts/goldfish/mkcmd.py OUT` | Commander only (`fetch.sh commander` runs it): moves the commander(s) under a `Commander` header. |

`fetch.sh` makes one request at a time, `GOLDFISH_SLEEP` seconds apart (default 3, and never less),
with a generic User-Agent, and skips a list already fetched, so a rerun only asks for what is missing.
An `archetypes.txt` that is already there (written by hand, or by an earlier run) is used as it is.

### Standard

```sh
scripts/goldfish/fetch.sh standard
for t in .goldfish/standard/txt/standard-*.txt; do
  s=$(basename "$t" .txt); n=${s#standard-}
  go run github.com/adams-shaun/gorge/cmd/deckimport -corpus <gorge>/.cards -format standard \
    -name "$n" -archetype "mtggoldfish $s" -notes "MTGGoldfish $s" -in "$t" -out "decks-standard/$n.json"
done
```

A Standard page's list is the main deck, a blank line, then the sideboard; `fetch.sh` puts a `Sideboard`
header over the second part and deckimport drops it (gorge has no sideboard). The deck files go to
`$HUNT_ROOT/decks-standard/` for the `std2` leg.

### Commander

```sh
scripts/goldfish/fetch.sh commander
for t in .goldfish/commander/txt/commander-*.txt; do
  s=$(basename "$t" .txt); n=gf-${s#commander-}
  go run github.com/adams-shaun/gorge/cmd/deckimport -corpus <gorge>/.cards \
    -name "$n" -archetype goldfish-edh -notes "MTGGoldfish $s" -in "$t" -out "decks/$n.json" > "decks/$n.report"
done
```

A Commander page's list holds the commander inside the 100 cards; the page has no section for it in
`deck_input[deck]`. `mkcmd.py` takes it from the slug: a card whose slugged name (the front face of a
`//` card) equals the slug after `commander-`, begins it, or ends it, at most two (a partner pair), goes
under a `Commander` header. It prints each archetype with the number found; a 0 needs a hand edit
(the page's `deck_input[commander]` and `deck_input[commander_alt]` fields name the commander exactly).

deckimport makes a `Commander` section the deck's `commander` field, sets the format to `commander`, and
exits non-zero, writing nothing, when a card is missing from the corpus or the commander is not eligible
(the `.report` says which). The `gf-` deck files go to `$HUNT_ROOT/decks/`, where the hunter's Commander
legs pick them up, narrowed by `decks.allow` after the audit below.

Pass `-notes`: deckimport's default note records the input file's absolute path.

### Checking them

`go run ./cmd/deckcheck <gorge>/.cards decks*/*.json` for gorge support (see "Auditing deck lists").
For a Standard list, MTGJSON's `legalities.standard` for legality (every card `Legal`, at least 60
main-deck cards, at most four of a non-basic); for Commander, `scripts/deck-audit.py`.

## How it yields

- **Gates and trains come first.** No game starts while `rgate.scope` (a landing gate) or `ftrain.scope`
  (a train) is active, and a wave already running is killed when one starts: each wave is its own
  process group. Its seeds are not replayed; the next wave claims new ones.
- **Capacity is measured every wave.** Workers = min(idle cores over a 3 s sample minus
  `HUNT_RESERVE`, (MemAvailable - 8 GB) / `HUNT_GAME_MEM_MB`, `HUNT_MAX_WORKERS`). Zero means wait a
  minute.
- **It runs at the bottom of the scheduler**, as a systemd user unit:

```sh
systemd-run --user --unit=hunt -p Nice=19 -p CPUWeight=1 -p IOWeight=1 \
  -p MemoryMax=20G -p MemoryHigh=18G -p Restart=on-failure -p RestartSec=2min \
  -E HUNT_LEGS="pod4 duel" -E HUNT_MAX_WORKERS=10 /scratch/hunt/hunt.sh
```

Stop it with `touch $HUNT_ROOT/STOP` (it exits between waves) or `systemctl --user stop hunt.service`.

## Setup on the box

`$HUNT_ROOT` (default `/scratch/hunt`) holds:

| Path | What |
|---|---|
| `gorge/` | a gorge worktree at the commit under test, with `.cards` linked. Make it from the gate's mirror: `git -C /scratch/gate/repo.git worktree add --detach /scratch/hunt/gorge farm-head` |
| `bin/kfuzz`, `bin/mtgsim` | built from `gorge/`: `go build -o bin/kfuzz ./dirac/oracle/cmd/kfuzz`, `go build -o bin/mtgsim ./cmd/mtgsim`, then `make dirac-tables` |
| `pools/cmd5/` | the `cmd5` lockstep pool (`DIRAC_POOL_CMD5`) |
| `decks/`, `decks.allow` | extra deck files and the audited allow-list |

To move to a newer gorge: push it to the box (`git push <box>:/scratch/gate/repo.git land:refs/heads/farm-head`),
check it out in `gorge/`, rebuild `bin/`, and restart the unit. Results are kept per commit.

## Results

Under `$HUNT_ROOT/run/<commit12>/`:

| File | What |
|---|---|
| `<leg>/ledger.tsv` | one line a game: seed, `ok` or `FAIL rc=N`, decks, summary |
| `<leg>/fails/<seed>.txt` | the full output, headed by a `REPRO:` line with the exact command |
| `lock/s<seed>/` | kfuzz's output, repro files and coverage |
| `signatures.tsv` | leg, failure signature (numbers and names stripped), count, an example: rebuilt every wave |
| `decks.txt` | the decks this commit's waves drew from |

`hunt.log` beside them records starts, waves, yields and idle periods.

## Auditing deck lists

```sh
go run ./cmd/deckcheck <gorge>/.cards deck.json... > gorgecheck.json      # gorge's reading of each deck
python3 -I scripts/deck-audit.py OUT AtomicCards.json.gz DeckList.json OFFICIAL_DECKS_DIR gorgecheck.json deck.json...
```

The MTGJSON inputs are public downloads (`AtomicCards.json.gz`, `DeckList.json`, and the official deck
files from `AllDeckFiles.tar.gz`). The audit writes `OUT/audit.json` and `OUT/REPORT-tables.md`: per deck,
card count, singleton, commander eligibility, colour identity, Commander legality, name resolution,
fidelity to the official product list when the deck claims to be one, and gorge's unsupported cards.
The hunter's `decks.allow` is the list of decks the audit passed.

## The pod farm (superseded)

`scripts/podfarm-box.sh` is the hunter's predecessor, kept as it ran on the gate box (as
`$FARM_ROOT/podfarm.sh`, a `pods` user unit beside `cmd/farm`). It plays endless 4-seat Commander pods,
one game each, replay-verified (`mtgsim -decks a,b,c,d -games 1 -seed N -verify`, 600 s per game). Seat 0
rotates through every deck file in the checkout that names a commander; seats 1-3 are three others chosen
from the seed.

```sh
podfarm-box.sh <gorge checkout> <run dir> [workers, default 8] [extra deck dir]
```

The extra deck directory's `*.json` (the `gf-` decks above) are copied into the checkout's
`testkit/testutil/decks/` before `mtgsim` is built, and left there. It plays 1000 seeds a round, `workers`
at a time, and keeps the next round's first seed in `<run dir>/next-seed`, so a restart resumes. Results:
`<run dir>/ledger/pods.tsv` (seed, `ok` or `FAIL rc=N`, decks, summary), `<run dir>/fails/seed-<N>.txt`
(full output), `<run dir>/ledger/podfarm.log` (starts). It ran under `Nice=19 CPUWeight=10 IOWeight=10
MemoryMax=12G`.

`hunt-box.sh`'s `pod4` leg replaces it: it yields to gates and trains, sizes its workers to free cores and
memory every wave, and records a `REPRO:` line per failure. Two known defects, not fixed because the
script is retired:

- **No `-format commander`.** mtgsim's format defaults to `constructed`, so the Commander decks play at
  20 life with no command zone: the commander is just a card in the library. `pod4` passes
  `-format commander`.
- **OOM-killed under its 12G cap.** `workers` games run at once with no memory check, and the unit is
  killed by its own `MemoryMax=12G` (Restart=on-failure brings it back to the same end). `hunt-box.sh`
  sizes workers to MemAvailable.
