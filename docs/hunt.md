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

Decks are the Commander deck files in the gorge checkout's `testkit/testutil/decks/`, plus any in
`$HUNT_ROOT/decks/`, narrowed to the names in `$HUNT_ROOT/decks.allow` when that file exists. Seat 0
rotates through every deck; the others are chosen from the seed.

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
go run ./cmd/deckcheck testkit/.../*.json > gorgecheck.json      # gorge's reading of each deck
python3 -I scripts/deck-audit.py OUT AtomicCards.json.gz DeckList.json OFFICIAL_DECKS_DIR gorgecheck.json deck.json...
```

The MTGJSON inputs are public downloads (`AtomicCards.json.gz`, `DeckList.json`, and the official deck
files from `AllDeckFiles.tar.gz`). The audit writes `OUT/audit.json` and `OUT/REPORT-tables.md`: per deck,
card count, singleton, commander eligibility, colour identity, Commander legality, name resolution,
fidelity to the official product list when the deck claims to be one, and gorge's unsupported cards.
The hunter's `decks.allow` is the list of decks the audit passed.
