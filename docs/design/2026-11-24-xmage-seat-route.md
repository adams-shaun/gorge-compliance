# XMage seat route (ec-10 spike)

Status: decision recorded (2026-11-24, spike round r1). The question ec-10 answered: how does
our bot sit at an XMage table — (a) SpellBench upstream `engines/xmage/`, (b) our own in-JVM
bridge player modelled on ec-09's Forge `V2Controller`, or (c) a client of a stock `Mage.Server`.
**Decision: route (a)** — adopt SpellBench upstream's XMage engine as the seat, driven by
four-yay `cmd/xclient -engine sbv2`; the ec-11 brief is at the end of this note.

The spike ran entirely out of tree (XMage clone, Maven build, engine jars, card database,
transcripts under `/tmp/opencode/build/`); nothing here but this note and the README table row
is committed. No public XMage server was contacted; every run was a local JVM on stdio.

## What route (a) actually is

SpellBench (github.com/jackmaiorino/spellbench, MIT, cloned at
`1945abdae4c22c2a3a95cc8acaabcf49bd777bdf`, 2026-11-22) ships `engines/xmage/`, sub-project X:
XMage at pin `fd40ad5c29a92cef824cf12ba6d0e4daa25db975` (2026-09-18) plus a four-patch
determinism series (pluggable RNG router; purpose-tagged draws; deterministic ids; hash-order
tokens) plus the `mage.player.spellbench` overlay (server, observe, decide, rng, ids) plus
CABT's deck/validation layer vendored at `bc1cf38c`.

Route (b) is not a competitor to (a): it **is** what upstream already built. `decide.SeatPlayer`
is the XMage `Player` subclass on both seats that parks the game thread per prompt
(`decide.Exchange` publishes the seat decision to the protocol thread and blocks), with
`server.GameSession` around a duel and `server.EngineServer` serving NDJSON v2 on stdin/stdout —
exactly the ec-09 `BridgeController`/`V2Controller` design for Forge, already written and
evidenced. Upstream's committed evidence (README, `tests/x1..x5/`):

| Stage | Evidence |
|---|---|
| X1 determinism | scripted game + soak replays byte-identical across fresh JVMs and processes on Windows and Linux; stock XMage diverges (negative control) |
| X3 observation | 360 games, 539,524 observations, zero build failures |
| X4 mapping | full XMage-callback → v2-kind table, 460 soak games, 0 validator violations, 0 halts, replays identical |
| X5 conformance | 10,112 live-validated games, all natural, 0 validator violations, 0 host halts; paired-world leak tests PASS; "fairness: validator only" |

## What this spike ran (all measured here, 2026-11-24)

Environment: 8-core/120 GB seat box; OpenJDK **21.0.13.1** (a full JDK is required — see the
JRE trap below), Apache Maven 3.9.9 under `/tmp`, no XMage checkout reused from the oracle.

| Step | Command (shape) | Wall | Result |
|---|---|---|---|
| clone | `git clone --depth 1 jackmaiorino/spellbench` | <1 min | 92 MB tree |
| XMage pin | `git fetch --depth 1 magefree/mage fd40ad5c` | ~1 min | ok |
| build engine | `engines/xmage/scripts/build.sh --out DIR --xmage-repo DIR --m2 DIR` | ~6 min first build (Maven deps 101 MB) | 41 jars, 86 MB `lib/`, `BUILD-MANIFEST.json` |
| card db | `java -cp lib/* mage.player.spellbench.x1.DeterminismCheck --scan-only` | ~1.5 min | 259 MB `db/` template |
| conformance | `spellbench conformance engine --format standard-2022-25-bo1 --deck Standard16-RG --deck Standard16-UB -- scripts/engine.sh --build ... --db ...` | ~4 min | **21/21 PASS** (full list below) |
| 10 games | `tests/x4/soak.py --games 10 --shards 1 -- <engine.sh>` (first vs uniform) | 26 s | 10/10 natural, 0 violations, 0 halts |
| 10 games | same with `--bots heuristic,uniform` | 29 s | 10/10 natural, heuristic 8–2 |
| xclient | `/tmp xclient -engine sbv2 -engine-cmd '<engine.sh argv>' -policy heuristic -games 10` | 1 min | **fails at reset**: `unsupported_format` (gorge finding below) |

Build reproducibility on this box: the generated `BUILD-MANIFEST.json` carries the same identity
strings the upstream README commits —
`rules_snapshot_id xmage-fd40ad5c29a92cef824cf12ba6d0e4daa25db975-xpat-6f8a902bb6681c2fe4ec79823cf05d5cc46042b2298a33f5ea73e0bbc61a2822`
and `card_pool_identity xmage-sets-3c333c4538a45690b640b4520b6572c40da80cb9-xpat-94cbe27a...`.
The two known platform/JDK gotchas:

- **JRE-only `java-21` is not enough.** The distro's `/usr/lib/jvm/java-21-openjdk-amd64` ships
  no `javac`; Maven dies with `Fatal error compiling: error: release version 8 not supported`
  (the in-process compiler refuses XMage's `java.release=8`). A full JDK 21 (Temurin used here)
  builds cleanly. JDK 25 is the distro default and is untested upstream (they pin 21/23).
- `scripts/engine.sh` and friends are committed non-executable; the conformance runner and any
  launcher must invoke them via `bash` or `chmod +x` once (git-archive clones lose the bit).

### Conformance, verbatim (2026-11-24, this build)

```
PASS hello
PASS protocol_mismatch
PASS malformed_json
PASS malformed_request
PASS malformed_request_non_object
PASS unsupported_rule
PASS game_id_reuse
PASS never_cached
PASS step_check_order
PASS step_before_reset
PASS unsupported_format
PASS unsupported_deck
PASS unsupported_request
PASS game_already_active
PASS game_id_mismatch
PASS expected_step_mismatch
PASS candidate_id_out_of_range
PASS semantic_echo_mismatch
PASS retransmission
PASS game_already_terminal
PASS games
```

### 10-game log summary (through P's host, live validator on)

`first` vs `uniform`, catalog decks Standard16-RG/UB, London mulligan, one engine process,
run secret `219584a16e4291076cbc622958b73042461a4492208dc956a3a6c3cef9024a43`:

| | value |
|---|---|
| games | 10, all `natural` (5 by p0 life 0, 5 by p1 life 0) |
| validator violations / host halts | 0 / 0 |
| decisions checked (live validator) | 4,676 |
| steps per game | mean 467.6, median 465.5, min 336, max 636 |
| wall per game | mean 1.43 s (max 1.95), 23.5 games/min |
| per-game digests | `sha256:813e2a46…`, `ac4c60b8…`, `eb644996…`, `7a6b943c…`, `a6ba5040…`, `11400a79…`, `0a401fdd…`, `240310f7…`, `f3dcf1b0…`, `7aeee1f1…` |

`heuristic` vs `uniform` batch (run secret `c754948f…`): 10/10 natural, 0 violations,
mean 1.46 s/game, heuristic won 8. Full JSONL rows kept out of tree only (`/tmp/opencode/build/logs/`,
per the licence rule: transcripts of engine traffic are not gorge-compliance artefacts).

## Seat facts recorded (from the code and the runs)

- **Card pool.** The engine's catalog (`catalog.json` in the overlay resources) holds exactly
  **32 decks**: 16 Standard 2022-25 (`Standard16-*`) and 16 FDN limited (`FDN_top_*`), offered
  for `fdn-limited-bo1` and `standard-2022-25-bo1` only. Arbitrary decks are playable via
  `reset.seats[].deck.decklist` (CABT `DeckValidation` refuses cards XMage cannot play and names
  them), but the formats are fixed: the engine refuses any other `format` string.
- **Decisions posed.** 26 declared kinds (`EngineProfile.DECISION_KINDS`): `pass`, `play_land`,
  `cast_spell`, `activate_ability`, `special_action`, `choose_target`,
  `finish_target_selection`, `choose_cost_target`, `choose_cast_method`, `choose_spell_mode`,
  `choose_option`, `choose_color`, `choose_number`, `choose_boolean`, `choose_name`,
  `select_object`, `finish_selection`, `optional_cost`, `mulligan`, `order_pick`,
  `arrange_card`, `choose_replacement`, `declare_attack`, `declare_block`, `distribute`,
  `choose_pile`. Groups, rewinds and combat declarations are posed whole (upstream X4 table).
- **Auto-answered.** `playMana` is engine autopay always (`AutoPayPlayer`, deterministic
  producer order; `hello_ok` declares `engine_autopay`); `activate_mana_ability` at ordinary
  priority only when the JVM runs `-Dspellbench.priorityMana=true` (default off); never posed:
  `choose_starting_player` (starting seat is host-assigned), `choose_cost_option`,
  `optional_cast` (they surface as `choose_boolean`/`choose_cast_method`/`optional_cost`).
- **Seat limit.** One active game per engine process, two seats, unique `game_id` per process;
  a process replays games in sequence after `terminal`. Each process copies its own 259 MB db
  (two JVMs on one db crash); concurrency is per-process, so a parallel run is N processes
  (upstream: 12 processes, ~40–48 games/min).
- **Build steps.** Shallow-fetch the XMage pin; `build.sh` (patches → CABT → overlay →
  `mvn package` with pinned timestamps → `lib/` + `BUILD-MANIFEST.json`); one
  `DeterminismCheck --scan-only` db template reused by every process. ~10 min first time,
  ~7 min warm, all out of tree.

## Why not (b) and (c)

- **(b)** would re-derive `decide.SeatPlayer` + `Exchange` + `GameSession` + the four XMage
  patches + the boot warm-up + `ManaCostCache` snapshotting — several thousand lines of
  MIT-licensed work that already exists, with a decade-equivalent of evidence attached. The
  only (b)-shaped code we keep is the forge-sidecar lesson from ec-09: a v2 engine around a
  parked JVM game thread is the right *shape*; here someone already filled it in.
- **(c)** is dead on facts: there is **no `Mage.Client.Headless`** in XMage at our oracle pin or
  at `fd40ad5c` (0 classes match), and a stock `Mage.Server` speaks the JBoss Remoting client
  protocol — an undocumented, unversioned wire with no headless client. It would also be one
  step from a public-server contact, which the fleet forbids.

## The gorge finding (blocks xclient → engine end to end)

four-yay's `cmd/xclient -engine sbv2` (gorge `server/spellbench/minihost`) is otherwise
wire-compatible with this engine: hello + catalog exchange, the reset (including
`rules.mulligan`, `starting_player: host_assigned`, `game_secret`, `max_decisions`/`max_steps`,
catalog deck ids) all parse — the engine answers the reset with

```
engine error unsupported_format: format pauper-bo1 is not one of [fdn-limited-bo1, standard-2022-25-bo1]
```

xclient summary line, 10 games attempted: `over=0/10 … protocol_errors=0 errors=10`.

Root cause: gorge `server/spellbench/minihost/host.go:64` hardcodes `"format": "pauper-bo1"`
in its reset construction — right for gorge's own engines, fatal for any foreign catalog
(XMage here; Manabrew/phase.rs engines later). The fix is structural, not a workaround: give
`Host` a `Format` field (default `pauper-bo1`, so every gorge engine and test is unchanged) and
a `-format` flag on `cmd/xclient`, defaulting to the engine's first declared format when unset.
That is a **gorge** change: file it in gorge's pipeline (`## Gorge finding`), gorge is
read-only here. ec-11 depends on it.

## Decision and consequences

Adopt route (a). Consequences:

1. Two XMage builds coexist and must not be conflated: the compliance oracle replay driver
   stays at `XMAGE_REF 4142596fb72b9dc03a0eae0ec34f4544cfd6d59e`; the seat engine uses
   SpellBench's `fd40ad5c` and its identity strings. A verdict is never measured on one and
   cited on the other.
2. The engine build + db template live out of tree (≈550 MB src + 86 MB lib + 259 MB db),
   like `.oracle-build/xmage`; ec-11's launcher pins the spellbench revision and verifies
   `BUILD-MANIFEST.json`'s digests instead of trusting a cached build.
3. Upstream is actively developed (HEAD two days old at spike time). Our contract with it is:
   pin a revision, verify the identity strings at build time, and treat protocol drift as a
   launcher preflight failure, not a silent assumption. gorge's protocol page already states
   the specification governs upstream.
4. If ec-11 needs a knob the engine lacks, the path is a small MIT patch series of ours on top
   (the same pattern as `scripts/xmage-oracle-*.sh`), never a fork of the overlay.
5. Determinism: the engine's seeded streams mean our xclient runs are replayable; ec-11 should
   assert digest equality on a held-out seed, matching the upstream X4 replay methodology.

## ec-11 status

- **ec-11a (gorge-compliance, landed):** `scripts/xmage-sbv2.sh` + `make xmage-sbv2-setup` + pins
  `XMAGE_SBV2_REF` / `XMAGE_SBV2_XMAGE_REF` / `XMAGE_SBV2_DIR` in the Makefile, with a stub smoke
  (`ops/scripts/tests/xmage_sbv2_smoke.sh`, in `make smoke`). See docs/oracles.md. The launcher's identity
  check recomputes both patch digests from the pinned patches; the formulas reproduce upstream's committed
  `xpat-6f8a902b...` and sets `xpat-94cbe27a...` at the pinned revision.
- **Open (other repos / operator):** gorge `minihost.Host.Format`; four-yay `cmd/xclient -format`, hello
  format check and the `Dialer` format; the real build and the 100-game / 20-record / replay-digest runs. The
  launcher was exercised against stubs only (no Maven build was run in the seat).

## ec-11 brief (ready to queue; replaces the parked placeholder)

> Kind: feature. Complexity: medium. Priority: P2. Depends-On: ec-10, plus the gorge ticket for
> the minihost format field below. Repo: four-yay `cmd/xclient` + gorge-compliance
> `scripts/` (launcher only; XMage/SpellBench code stays out of tree).

### Goal

Our bot sits at an XMage table through four-yay `cmd/xclient -engine sbv2` against SpellBench
upstream's XMage engine (route (a), chosen by ec-10; design:
`docs/design/2026-11-24-xmage-seat-route.md`).

### Behaviour

- gorge `server/spellbench/minihost` gains a `Format` field (default `pauper-bo1`; **gorge
  ticket**, four-yay only consumes it) and `cmd/xclient` a `-format` flag that defaults to the
  engine hello's first declared format when unset and errors when the chosen format is not in
  `hello_ok.formats`.
- gorge-compliance `scripts/xmage-sbv2.sh` is the launcher: it pins the SpellBench revision and
  XMage pin (one line each, like `XMAGE_REF`), shallow-fetches both out of tree on first use,
  runs `scripts/build.sh` with the box's full JDK 21 and writes the engine build + db template
  under an out-of-tree cache (default `.oracle-build/xmage-sbv2/`, override like
  `XMAGE_ORACLE_DIR`), verifies `BUILD-MANIFEST.json`'s identity strings against the pins, and
  then execs `engines/xmage/scripts/engine.sh --build … --db …` so it can serve directly as
  `-engine-cmd`. Never edits gorge, never vendors spellbench or XMage.
- A run is `go run ./cmd/xclient -engine sbv2 -engine-cmd '<scripts/xmage-sbv2.sh>' -policy
  heuristic -format standard-2022-25-bo1 -deck Standard16-RG,Standard16-UB -games N`:
  our seat answers with the registered policy, the other seat with `-opponent` (house policies
  are out of scope — the engine poses every seat decision).

### Implementation steps

1. gorge: `minihost.Host.Format` (default preserves today's bytes); file the gorge ticket and
   reference its id from this brief before starting.
2. four-yay: `cmd/xclient -format` + hello format check; extend the sbv2 `Dialer` to carry the
   format through `PlayDecks`.
3. gorge-compliance: `scripts/xmage-sbv2.sh` (pins, fetch, build, db, verify, exec) + `make
   xmage-sbv2-setup` mirroring `xmage-oracle-setup`'s "run alone" discipline.
4. Record 20 games at a held-out seed with `-record`; commit nothing from them (transcripts are
   engine traffic); keep digests in the report.

### Tests

- four-yay `make test-one PKG=./training/codec/sbv2 RUN='^TestSidecarTranscriptsValidate$'`
  over the 20 recorded XMage games (the ec-09 harness, pointed at the new records).
- `go run ./cmd/xclient -engine sbv2 -engine-cmd '<launcher>' -policy heuristic -format
  standard-2022-25-bo1 -games 100`: 100/100 Over, 0 protocol errors; paste the summary line.
- A replay check: one game of the 100 re-run in a fresh engine process has the same game digest.

### Done means

- [ ] the gorge minihost ticket is filed and its four-yay consumption lands
- [ ] `TestSidecarTranscriptsValidate` passes on 20 recorded XMage games
- [ ] the 100-game xclient run: 100/100 Over, 0 protocol errors, summary line pasted in the report
- [ ] the replay digest check passes

### Out of scope

Public XMage servers, the JBoss Remoting client, the CABT/MAD/MCTS policy families
(kit entries), and `-Dspellbench.priorityMana=true` (record it as a follow-up question, not a
default).

### Collisions

`oracle/xmageoracle` build scripts (the seat launcher is a sibling, not a change); four-yay
`cmd/xclient` (ec-02 landed it; this extends it).
