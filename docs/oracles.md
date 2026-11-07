# The oracles

This page says what each check of gorge sees, what it can't see, and how to run it. Read it
to decide how much a passing check tells you. Run commands from the repository root after
the [setup](../README.md#setup).

| Check | Compares gorge with | Unit | Plays a game? | Blocks CI? |
|---|---|---|---|---|
| [Hand scenarios](#hand-scenarios) | Oracle text and the Comprehensive Rules, as read by an author | One card's behaviour | Short script | Yes |
| [XMage ports](#xmage-ports) | XMage's own test assertions | One XMage test | Short script | Yes |
| [XMage verdicts](#xmage-verdicts) | XMage's engine | One generated scenario per card or ability | Short script | Yes, on committed verdicts |
| [Forge adjudication](#forge-adjudication) | Forge's engine | The same scenarios | Short script | No |
| [xengine](#xengine) | XMage and Forge | A window or prefix of a full game | Full games, replayed in part | No |
| [mtgish](#mtgish) | An independent encoding of each card | One card face's definition | No | Yes |

## Hand scenarios

**Checks:** one JSON file per card under `scenarios/oracle/`, written from the card's Oracle
text and the rules without reading gorge's script for the card. Each scenario sets up a
board, takes steps through gorge's runner and asserts what a player can observe: zones, life,
P/T, counters, the stack, what is offered.

**Can't check:** a card or behaviour nobody wrote a scenario for. An author can misread a card,
and nothing here checks the author against another engine. `known-divergent/` lists scenarios
that fail today, and `known-unconsumed/` lists scenarios that leave a scripted answer unused;
each row is a known gorge problem, not a pass.

**Run:** `make oracle-audit ORACLE_RUN=Fatal_Push`. A stale row, meaning gorge now passes,
fails the test until the row is deleted.

## XMage ports

**Checks:** `scenarios/xmageport/` holds XMage's Mage.Tests methods, translated by
`cmd/xmageport` and run on gorge. The expectations come from XMage's assertions.

**Can't check:** what XMage's tests don't cover. The translator refuses what it can't express,
including multiplayer and commander tests, because the runner plays two seats. Passing tests
only are committed, along with `known_bug` rows that must keep failing
(`cmd/xmageport/known_bugs.tsv`).

**Run:** `make oracle-audit`. To regenerate, run `go run ./cmd/xmageport -mage <Mage.Tests test
root> -out <dir>`, which reads an XMage checkout.

## XMage verdicts

**Checks:** `oraclediff gen` writes scenarios per card from the set's manifest:
one per card at level A, and at level B one more per activated ability, trigger, static and
attack or block. The Java driver in `oracle/xmageoracle` replays each in XMage, and `oraclediff
diff` compares the two engines' snapshots at each checkpoint, writing a verdict in
`oracle/compliance/verdicts/`. Disagreements are triaged by shape (`oracle/compliance/rulings/`,
`oracle/compliance/triage/`), and `oracle/compliance/ratchet.json` holds each set's outstanding
count so it can only fall.

**Can't check:**

- State the snapshot doesn't compare, or a field the two engines name differently.
- Boards beyond the scenario: a scenario has few cards, and interactions between cards go
  untested at both levels.
- Bugs XMage shares with gorge. Agreement isn't proof, and a `xmage_wrong` ruling rests on
  a person's reading of the rules.
- A requirement no template can generate, which is reported as a `template gap`. An unsupported
  card can't be generated at all.
- Unfinished XMage sets: a card XMage lacks needs a hand scenario instead.

XMage answers a choice the script didn't expect using its AI, so a row can flip on a rerun:
replay a scenario twice before believing a flip.

CI doesn't run XMage. `TestDeclaredSetsCompliantChunkNN` and `TestCertificationRatchet` check that
gorge at the pin still meets each committed verdict's frozen expectation. A stale verdict
needs a replay, covered in the [runbook](../oracle/validation/oracle/README.md).

**Run:** `make compliance-pass SETS=BIG` replays only stale scenarios and writes verdicts.
`LEVEL=B` selects level B. Set `COMPLIANCE_PASS_NO_WRITE=1` to compute without writing.

## Forge adjudication

**Checks:** the same scenarios replayed in Forge through a driver in
[our Forge fork](https://github.com/adams-shaun/forge) (branch `oracle-harness`), giving each
scenario a pattern: `AGREE3`, `FX|G` (a likely gorge bug), `GF|X`, `GX|F` or `G|F|X`. Forge's
output stays out of the tree because Forge is GPL-3.0. The pass writes a ledger of gorge's own
vocabulary to `oracle/compliance/adjudication/`, which holds no committed rows today.

**Can't check:** a wrong card script. gorge compiles Forge's scripts, so gorge and Forge agreeing
says little: `GF|X` can be a shared script bug. What we've confirmed in the other engines, and whether upstream knows, is in [upstream-bugs.md](upstream-bugs.md).

**Forge verdicts:** where XMage has not compared a level-A scenario, forge-diff's `AGREE` writes a row
with `source: "forge"` holding gorge's frozen fields and the two pins, and no Forge output. An XMage
comparison replaces it; an XMage harness or `xmage_lacks` does not, since XMage compared nothing. Such
a row checks gorge's reading of Forge's own card script, not the script, so a wrong script both
engines read alike passes. CI (`TestDeclaredSetsCompliantChunkNN`, `TestCertificationRatchet`) holds
gorge to the frozen fields like any row.

**Run:** the XMage pass first, at the same level, then Forge:

```sh
LEVEL=B make compliance-pass SETS=BIG
scripts/forge-pass.sh BIG
```

For a set XMage has not compared, `make forge-verdicts SETS=TRK` runs at level A with gen -printed
and writes the scenarios Forge agrees with as Forge-only verdict rows; record the split afterwards
with `go run -trimpath ./cmd/oraclediff status -all -sets TRK -write-ratchet`.

A pass over named sets writes partial ledger shards, so treat it as a survey. Add `--force` to
replay every request. The comparison with XMage reads XMage's result cache and skips what isn't
there, with the reason in `adjudicate-report.jsonl`. A scenario with a fresh verdict is never
replayed, so a new build's cache stays empty until scenarios go stale, and the pass then reports
`no cached XMage result` for them.

## xengine

**Checks:** gorge plays full games with a bot that explores its options, between two-colour decks drawn
from a set's supported cards and weighted toward the least-exercised ones. Each game is cut
into windows and prefixes that XMage and Forge replay, and each difference is classified
and written as a repro directory. It reaches board states no scenario sets up. A coverage
ledger records which cards each sweep exercised and compared.

A follower that stops partway, because the game parted from the script, is read up to where it stopped: a difference there
is a `diverge`, not a `harness`. When both followers differ from gorge they are compared up to the first checkpoint
either differs at, since neither follows the same game after it. An item XMage is never sent (`xprep.jsonl`) is
`not_sent`, and a whole-game tape that agreed as far as the Forge driver's turn cap is `turn_cap`.

**Can't check:** windows it can't lower. In a 2-game FDN sweep on 2026-11-02, 25 of 46 windows
were refused, and of the 21 replayed, 3 in XMage and 1 in Forge came back as a `decision_shape`
difference, so a follower can fail to express a position without any engine being wrong. A sweep rules on nothing and commits nothing. Its findings are leads for a person.

Forge has one known difference of its own here. After a first-strike combat's first damage step, when
no creature is left to deal damage in the second, Forge goes straight to end of combat, where
CR 510.4 gives the phase a second damage step and players get priority in it. gorge and XMage do. Those
items get the class `forge_skips_damage_step` and aren't compared in Forge past that checkpoint.

Three `FX|G` patterns are the followers agreeing with each other against the rules, read from their code and not fixed in gorge. A crewed Vehicle whose
type-adding static entered before the crew effect (Lifecraft Engine, March of the World Ooze): the static's layer-4 addition depends on crew's animation (CR 613.8a), but XMage orders the static by timestamp and Forge drops
the added subtype while the Vehicle is still a non-creature artifact; both are `crew_type_dependency` (the source trace is in
[upstream-bugs.md](upstream-bugs.md)). Erratic Apparition enchanted by Unable to Scream: the Aura's "loses all abilities" applies as it enters,
so CR 603.6a leaves nothing to trigger, but both followers fire Eerie.

**Run:** `make xengine-sweep SETS=FDN CHUNKS=1 GAMES=2`, then read
`.oracle-build/xengine/seed1/SUMMARY.md`. A sweep resumes where it stopped. It needs `jq`, and
waits while two or more JVMs are running.

**dirac as a fourth side:** with `XENGINE_DIRAC=1` the decks hold only cards the set's dirac table
plays, and each game is played in lockstep between gorge and dirac (`dirac/oracle`) under the explore
policy before being replayed as the sweep's game, so the windows lowered from it are the positions dirac
was compared at. `<chunk>/dirac.jsonl` says how each game went on dirac. `<chunk>/adjudicate/dirac.jsonl` gives
each item dirac's answer beside the three-way pattern: `D=G`, `D!=G`, or `D-` where dirac has no answer
(a pair its table refuses, a game past a stall or a divergence). `AGREE3 D!=G` points at dirac; `FX|G D=G` is
a gorge suspect dirac shares. `play`'s error names the command that builds the tables, which are keyed by the pin
and the corpus. The lockstep runs on the autopay surface. A difference on a card dirac lists as unverified breaks no promise of
dirac's: `unverified` in the row counts those deck lines.

**In the background:** `scripts/xengine-random.sh` plays random-card games chunk after chunk until stopped:
even games across all the sets given, odd games within one set's dirac-playable cards. It is meant for
a user unit that caps the whole tree, from a worktree that nothing moves, because every stage runs from the checkout:

```sh
git worktree add --detach "$dir" develop && ln -s "$PWD/.cards" "$dir/.cards"
systemd-run --user --unit=xengine-random --collect --working-directory="$dir" \
  -p MemoryMax=8G -p CPUQuota=400% -p Nice=15 -p IOSchedulingClass=idle \
  --setenv=PATH="$PATH" --setenv=XENGINE_ROOT="$root" \
  --setenv=XMAGE_ORACLE_DIR="$xmage" --setenv=FORGE_ORACLE_DIR="$forge" \
  "$dir/scripts/xengine-random.sh" --run random1
```

`--forge-only FILE` adds a file of card names, one per line, to the cross-set draw: a set XMage has no class for. An
item naming a card no XMage set lists isn't sent to XMage (`not_sent`, `xmage_lacks_card`), so those games are compared with Forge alone.

`systemctl --user stop xengine-random` stops it, and the same command resumes it. It writes `$root/SUMMARY.md` after every chunk and
keeps each chunk's rows and the repros of diverged items; `xengine-sweep.sh --force`
rebuilds the rest of a chunk at the same pin.

## mtgish

**Checks:** [mtgish](https://github.com/i5jb/mtgish) (MIT) encodes every card from its Oracle text.
`cmd/mtgishxc` normalises its definitions and gorge's compiled IR to one token summary per
face, in four tiers (T1 characteristics, T2 ability shape, T3 targets, T4 effect verbs), and
diffs them. It finds a wrong card script that no scenario has exercised yet. Each difference is
triaged as `forge-bug`, `gorge-compile`, `mtgish-bug`, `equivalent` or `digital-variant` in
`oracle/mtgish/known/`, and `TestMtgishLedger` holds the corpus to that ledger in both directions.

**Can't check:** behaviour. No game is played, so a correct definition gorge executes wrongly passes.
The summary is coarse: a wrong amount where one side has no literal doesn't show.

**Run:** `make mtgish-fetch mtgish-xc` writes `.coverage/mtgish/report-ALL.md` and runs the ledger.
FDN and BLB, the sets the dirac kernel plays, get tables of their own. The data carries Oracle text and stays in the gitignored `.mtgish/`.

## What the XMage and Forge oracles need

| Build | Command | Default directory | Size (2026-10-31, `du -sh`) |
|---|---|---|---|
| XMage | `make xmage-oracle-setup` | `.oracle-build/xmage` | 1.1 GB for the source and JDK, plus Maven's downloads |
| Forge | `make forge-oracle-setup` | `.oracle-build/forge` | 850 MB for the source and its Maven cache |

The setup scripts download a JDK and Maven and need `curl`, `git`, `python3`, `flock`, `/usr/bin/time`
and systemd user scopes (`systemd-run --user`), which also cap the JVMs when they run. Set
`XMAGE_ORACLE_DIR` and `FORGE_ORACLE_DIR` to share one build across clones. Forge's driver uses
XMage's JDK, so build XMage first. Passes start a JVM per batch; on a shared machine, set
`HEAVY_LOCK` to whatever serialises heavy jobs there (`scripts/heavy-lock.sh` has the interface).
