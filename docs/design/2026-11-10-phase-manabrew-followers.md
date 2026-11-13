# phase.rs and Manabrew as xengine followers

Status: design, 2026-11-10. Nothing here is built yet. It rests on a read-only, static audit of
gorge-compliance at `0d6f91b`, gorge at GORGE_PIN `04ad74f2`, phase.rs at `c53dd8a` (v0.122.0) and
Manabrew at `dadee73`. None of the engine claims has been built or run yet. Rust is now on the box (rustup in `~/.cargo/bin`: stable 1.98 and phase's `nightly-2026-04-19`). The
audits, with file:line citations, are in [2026-11-10-followers/](2026-11-10-followers/):
[the follower contract](2026-11-10-followers/audit-contract.md), [phase.rs](2026-11-10-followers/audit-phase.md),
[Manabrew](2026-11-10-followers/audit-manabrew.md).

## Goal

Add two Rust engines as followers beside XMage (X) and Forge (F), and fold them in slowly as a
proof of concept. They start on the simplest games, 1v1 with 40-card decks from FDN commons, and widen
from there. The drivers are written so that the phase.rs and Manabrew projects could take them over.

| Engine | Letter | Card source | Independent of | Licence → where the driver lives |
|---|---|---|---|---|
| phase.rs | P | Its own parse of MTGJSON Oracle text (`card-data-<hash>.json`) | F, X, G (no Forge script, no XMage code) | MIT/Apache → in this repo, `followers/phase/` |
| Manabrew | M | Forge scripts, read by a Rust port of Forge's engine | X only. It shares scripts with G and F, and engine design with F | AGPL → fork `adams-shaun/manabrew`, branch `oracle`, built out of tree like Forge |

**Why both.** P is the prize. It is a second independent engine beside X, with its own card
encoding, so a card where G, X and P agree has three independent readings behind it. M adds little
independence because it belongs to the Forge family. What M adds:

- A second Forge-semantics replay without a JVM and without Forge's turn cap. In Forge, most
  whole-game tapes come back `turn_cap`.
- A check on the Forge driver: when F and M read the same script differently, one of the two drivers
  is suspect.
- Throughput.

## What a follower is (summary of the contract)

Full detail: [audit-contract.md](2026-11-10-followers/audit-contract.md) §0–§3.

- A batch process: `RUNNER IN.jsonl OUT.jsonl`. One output row per input line, in input order,
  flushed per row. A bad line becomes a harness row and never stops the batch. Exit 0 unless setup
  itself failed (exit 2).
- **Every input line is one scenario in one schema.** That covers level-A/B oracle scenarios, xengine
  one-turn windows and whole-game tapes (templates `xengine-window` and `xengine-tape`). There is no
  separate game-tape format. Forge's `GameRequest`/`GameReplay` is an unrelated tool and is out of scope.
- The input line is what `oraclediff forge-export` writes:
  `{id, scenario_sha, item, gorge_decisions, abilities}`.
  - `item.setup` places a two-seat board at turn 1, p0 active. Battlefield cards are placed with no
    ETB triggers, no entry replacements and no summoning sickness. Library order comes from
    `library_top`; xengine puts the whole library there. Also in setup: tapped, `tapped_after_untap`,
    counters added on top of entry counters, tokens by Forge tokenscript stem, attached Auras, life,
    speed.
  - `item.steps` uses the ops `play cast activate pass pass_to resolve attack block attach move mana`.
  - `item.payment[i]` is the exact pool mana gorge spent on step i.
- Decisions come from `gorge_decisions`: targets, modes, yes/no, choose_n, order, arrange. Refs are
  `pN`, `pN:Name#k` (owner plus creation order) and `pN:token:Sub#k`.
  - A prompt gorge never logged is a `strict_miss`, and an unused decision is a `leftover`. Both are
    classed `decision_shape`.
  - Mana is paid from the pool only. In xengine every mana tap arrives as its own `activate` step,
    keyed by gorge's `ability_index`.
- Output row: `{id, name, engine, strict, strict_miss?, leftover?, harness?, ms, snapshots[], notes?}`.
  - Snapshots are taken at `"setup"` and after each step (`"step i (op)"`), in gorge's vocabulary:
    step names, WUBRG colours, counter kinds `P1P1`/`LOYALTY`, Forge card spellings, tokens anonymised.
  - `oraclediff.CompareOpts` does the comparison. `priority`, `command` and refs are not compared.
- A harness row keeps the snapshots taken before the stop. If gorge already differs within that
  stretch, the class is `diverge`; otherwise `harness`.

Two facts make the first version simpler than the engine audits feared:

- **Shuffles are already handled by the lowering.** A tape is cut at the first RNG-consuming ask, and
  a window that shuffles carries `ignore: ["library_top"]`. A hand difference after a recorded RNG
  draw is class `rng`, not `diverge`. Neither follower needs shuffle injection in the first version.
- **No mulligans.** Setup places hand and library directly, so neither engine's mulligan or
  starting-player roll runs.

## Part 1: shared harness work (gorge-compliance, Go)

This is engine-neutral and can start before any Rust exists. Each item names the files the contract
audit found hard-coding `xmage`/`forge`.

### H1. A follower registry instead of literal engine names

Add `oracle/xengine/follower` with one entry per engine:

```go
type Follower struct {
    Name           string // "forge", "xmage", "phase", "manabrew"
    Family         string // "forge" (forge, manabrew), "xmage", "phase" — see H5
    Runner         string // scripts/<name>-oracle-run.sh
    Prep           string // "xprep" | "forge-export" | "follower-export"
    ConsumesPayment bool  // reads item.payment (today classify.consumesPayment says xmage only; forge reads it too)
    TurnCap        int    // 0 = none; forge = scenario turn + 4
    CastModes      []string // cast_mode values the driver honours; others → harness
    SkipsDamageStep bool  // forge_skips_damage_step quirk
    LacksCardMsg   string // harness text prefix meaning "I don't have this card"
}
```

These call sites read the registry instead of string literals:

- `cmd/xengine/diff.go:34,53-83`: the `-engine` whitelist and the row reader.
- `oracle/xengine/classify/classify.go:123-650`: every `engine == "forge"` / `"xmage"` branch.
- `classify/repro.go:228-232`: the per-engine runner map.
- `cmd/oraclediff/forge.go:570-573`: forge-diff insists on `engine == "forge"`. Rename it
  `follower-diff -engine NAME`, keeping `forge-diff` as an alias.
- `compare.go:55-67`: today only XMage's `Couldn't find a card:` is recognised as "lacks card".

Done when every existing test passes with the registry holding only `forge` and `xmage`. No behaviour
changes.

### H2. Sweep script: generic followers

In `scripts/xengine-sweep.sh`:

- Replace the `forge|xmage` whitelist (lines 122-124) and the `follower_rows` case (193-213) with a
  loop over `XENGINE_FOLLOWERS` that calls each follower's prep and runner.
- The JVM-slot wait (163-176) applies only to JVM followers. The Rust followers use `heavy-lock.sh`
  with their own `XENGINE_<F>_MEM` and a CPU quota.
- `finish_chunk` already loops followers for `xengine diff`. The SUMMARY histogram already loops too.
- Follower output stays outside the repo or under `.oracle-build/`. Manabrew's output is AGPL, and the
  existing rule already covers it.

### H3. A follower-export stage, and a per-follower `not_sent`

Add `oraclediff follower-export -engine NAME -scenarios items.jsonl -out req.jsonl`. It does everything
`forge-export` does, plus three things.

**1. Refusals.** Before writing an item, ask the follower which cards it lacks: run
`<runner> --preflight names.txt` once per chunk, which prints `name\treason` (see P-support,
M-support). An item naming a refused card is written to `<chunk>/<engine>-prep.jsonl` as
`{id, reason:"<engine>_lacks_card", detail}`. `xengine diff` reads that as `not_sent`, exactly as it
reads `xprep.jsonl` for XMage today (`xprep.go:30-75`).

**2. Keep Forge's rewrites.** The `pass` → `pass_to attackers|blockers` rewrites (forge.go:186-281)
encode the CR 507/509 order: the declaration prompt comes before the step's first priority. phase.rs
follows the CR, and Manabrew mirrors Forge, so both want the rewritten item.

**3. Engine-neutral identities, in a new `neutral` object beside `abilities`.** They're written for P
and harmless for M:

- `neutral.ability[i]` for each step with `ability_index`:
  - `class`: `mana` | `activated` | `loyalty` | `intrinsic_mana`
  - `ordinal`: index among the face's abilities of the same class, in script order
  - `cost`: normalised cost string
  - `text`: Forge `SpellDescription`, with `CARDNAME` replaced by `~`
  - `produces` (mana only): WUBRGC letters, or `any`
  - The ordinal is the primary key, because Forge script order follows Oracle order for ordinary
    cards. The text is the check.
- `neutral.token[stem]` for each setup token: name, types, subtypes, colours, P/T, keywords, and
  ability text if any. These are read from gorge's tokenscript so a follower can mint the token by
  characteristics. It replaces the XMage-only `xmageSetupTokens` table for new followers.
- `neutral.mode[i]` for each mode decision: the chosen mode's `ordinal` among the spell's modes, plus
  its text. gorge already logs `pick_idx`; this records it explicitly against the mode list order.

`scenario_sha` is still computed before any rewrite, so all followers share one sha per item.

#### Preflight protocol, as built

Built in `cmd/oraclediff/follower_export.go` (its file comment is the normative copy).

- **Call.** `oraclediff follower-export -engine NAME -scenarios items.jsonl -out req.jsonl
  [-cards .cards] [-runner PATH]`. `-engine` must name a registered follower whose Prep is
  `forge-export` or `follower-export`; `-runner` overrides the registry's runner (tests, stubs).
  follower-export runs `RUNNER --preflight NAMES.txt` once per run, before writing anything.
- **NAMES.txt.** One card name per line, sorted, no duplicates, in Forge/gorge spelling. It holds
  every card any item names: each seat's battlefield, tapped, back_face, hand, graveyard, exile,
  library, library_top, counters and attached cards, and the card refs in the steps (card, targets,
  attackers, blocks, attached_to) with `pN:` and `#k` stripped. Tokens, player refs and names
  gorge's corpus does not know are left out.
- **Output.** On stdout, one `NAME<TAB>REASON` line per card the follower lacks. NAME is exactly
  as asked, and REASON is free text with no tab. A supported card gets no line, and blank lines are
  ignored. stderr passes through.
- **Exit codes.** 0 means the answer is complete. Any other status fails the whole export. So does
  a line without a tab or a NAME that was not asked about, and nothing is written.
- **Refusals.** An item naming a refused card is written to `<dir of -out>/<NAME>-prep.jsonl` as
  `{id, reason:"<NAME>_lacks_card", detail:"Card: reason; ..."}` and not to req.jsonl. The file is
  always rewritten, even when empty. `xengine diff -engine NAME` reads it as `not_sent` when the
  follower's Prep is `follower-export`. XMage still reads `xprep.jsonl`, and Forge reads nothing.
- **Requests.** Every other line is forge-export's line for the item, byte for byte (the same
  `scenario_sha`, decisions, abilities and rewrites), with `"neutral"` appended last.
- **`neutral` as built.** Every map is always present. A field that cannot be derived is left out,
  never guessed, and the command prints a census of what it filled and what it left out.
  - `ability`, keyed by step:
    - `class`. A loyalty ability is one with `Planeswalker$ True`. A mana ability is
      `cards.IsManaAbilitySA`. intrinsic_mana is the CR 305.6 ability gorge appends.
    - `ordinal`. It is 0-based and counts script abilities only. An ability minted from a keyword
      line (Equip, Crew, ...) carries `keyword: "Equip:2"` instead, because its Oracle position is
      not the script's.
    - `cost`. Mana symbols are braced and run together, then `{T}`/`{Q}`, then loyalty as
      `+N`/`-N`. Any other part keeps Forge's spelling with `~`. Parts are joined with `, `.
    - `text`. The SpellDescription with `~`. It is absent for intrinsic_mana.
    - `produces`. The set of letters an activation can add, in WUBRGC order (`"UG"` for Combo G U),
      or `any`. It is absent for Chosen, ColorIdentity and ManaReflected.
  - `token`, keyed by stem: `name` (the script's, e.g. "Goblin Token"), `supertypes`, `types`,
    `subtypes`, `colours` (WUBRG letters, "" colourless), `power`/`toughness` (numeric only),
    `keywords`, `text` (the script's Oracle line).
  - `mode`, keyed by the index in this request's `gorge_decisions`. It covers decisions of kind
    `mode` with resume `cast_modes` or `modes`, written as `{step, picks:[{ordinal, text}]}`. The
    ordinal is the 0-based Choices$ position of a Charm on the source card, matched by gorge's mode
    label. A label that matches no Charm, or more than one position, leaves the decision out. So
    does a non-Charm choice, such as Manifold Mouse's KWChoice.
- **FDN chunk 0 (574 items), with no refusals.**
  - 3,107 ability steps got class, cost and ordinal, except 29 keyword-minted ones, which got
    `keyword` instead of an ordinal.
  - 320 ability steps got text. The other 2,787 are intrinsic, which has no text.
  - 2,900 of 2,915 mana steps got `produces`. The 15 left out are "chosen color".
  - All 64 token stems and all 7 modal decisions were filled.

### H4. A pool-only deck mode (for "Pauper/FDN")

`-forge-only` only adds to the union of `-random` sets (play.go:221). Add
`xengine play -pool FILE`, which uses `decks.CandidatesNamed(reg, names, opt)` alone. It keeps the
existing gorge-supported filter and the 23-spell, 17-basic, two-colour pair builder (decks.go:266-326),
and is also passed through as `XENGINE_POOL` in the sweep. Pools to ship, under
`oracle/xengine/pools/`:

| Pool | Source | Size | Use |
|---|---|---|---|
| `fdn-commons.txt` | FDN ∩ MTGJSON `legalities.pauper == "legal"` | 196 (P supports all 196) | First corpus for both followers |
| `fdn.txt` | the FDN manifest (what `-set FDN` draws today) | 517 (P: 494, M: ≤479 unflagged) | Second step |
| `pauper.txt` | Pauper-legal ∩ gorge-supported | ~11k before the gorge filter (P: 93.5%) | Later; cross-set interactions |

Each pool is also intersected per follower with that follower's preflight, so nothing a follower
refuses gets drawn. A follower that refuses a card a deck holds turns every item from that game into
`not_sent`. The sweep therefore draws from the intersection of the pool with every enabled follower's
support (a `-pool-filter <engine>` list), and the ledger records which follower narrowed it.

### H5. N-way adjudication by family

Today `adjudicate` takes exactly X plus F and builds the patterns from three pairwise legs
(`threeway.go:16-113`). Generalise it as follows:

1. Collect every follower's result for an item. Drop followers that are `harness`, `not_sent`,
   `turn_cap` or `decision_shape`, recording them as `–`.
2. Cut every leg to the shortest compared stretch: the minimum partial stop, then the first checkpoint
   where any side differs (adjudicate.go:90-166 already does this for two followers).
3. Normalised field equality is transitive (threeway.go:97-99), so partition {G, followers…} into
   agreement classes. The pattern string lists classes, for example `G|FMPX` or `GF|M|PX`.
4. **Count votes by family, not by engine.** Families: {G, D}, {F, M}, {X}, {P}. Within the forge
   family F and M are one vote; if they disagree, that is a `forge_family_split` finding against the
   drivers, not a vote.
5. Labels:
   - `gorge_suspect`: at least two non-forge families (or the forge family plus at least one other),
     agreeing with each other, and none agreeing with G.
   - `<engine>_suspect`: one follower alone against an agreeing G plus at least one other family.
   - `split`: no class holds a majority of families.
   - `forge_family_split`.
   - `gorge_forge_only` stays as it is.
6. The `AGREE3`/`FX|G` strings for the two-follower case stay byte-identical. The dirac `D=G`/`D!=G`
   column is unchanged.

`findings.jsonl` gets `{pattern, families_against_g, followers{engine: class}}`. The ticket drafts
gain the follower list.

#### The phase.rs column, as built (2026-11-12)

The first slice of H5 adds phase.rs (P) as a fourth column and changes nothing the three-way
adjudication writes.

- **Core.** `oraclediff.NWay` (`oracle/compliance/oraclediff/nway.go`) is pure. It takes which pairs
  of engines agree, unions the agreeing engines into classes and spells the pattern. Classes are
  ordered by size, then gorge's class first; within a class gorge comes first, then the letters
  alphabetically. A follower that did not compare is appended with a mark: `P?` means unsupported
  (it lacks a card), `P–` means not compared. Examples: `FPX|G`, `GP|FX`, `GFX|P`, `FX|G P?`,
  `AGREE4`. With exactly G, F and X it spells the three-way class strings. Labels are
  `gorge_suspect`, `<engine>_suspect`, `split` and `all_differ`. `Vote("P")` gives `with_gorge`,
  `with_others`, `alone`, `unsupported` or `uncompared`. Engines are counted one by one, not by
  family; Manabrew is not a letter yet.
- **xengine.** `xengine adjudicate -phase DIFF_P [-phase-rows ROWS]` writes `fourway.jsonl` and
  `patterns4.tsv` beside the unchanged `findings.jsonl`, `patterns.tsv`, ledger and drafts.
  - The G/F/X pairs are the three-way legs themselves.
  - P is compared with G, F and X over the stretch the three-way verdict turned on: up to the first
    checkpoint where X or F differs from G.
  - A P row that stopped earlier counts only if it had already parted from G, and is then `alone`.
  - Not-sent items whose reason is `phase_lacks_card`, and run-time `xmage_lacks` rows, are
    unsupported.
  - Items where only P diverged enter the table too (`GFX|P`).
  - `scripts/xengine-sweep.sh` passes `-phase` when phase is among `XENGINE_FOLLOWERS`.
- **oraclediff.** `oraclediff adjudicate -phase-diff PATH [-phase-cache DIR] [-phase-prep PATH]`
  adds `four`, `four_label`, `phase` and `gp_field` to each classified report row. The ledger shards
  and every three-way field are unchanged.

**What this buys in confidence.** An xengine item where G, X and P agree is backed by two independent
card encodings beside gorge's. Today that's one. A `FX|G` item whose P also agrees with F and X is a
strong gorge suspect. A `FX|G` item where P sides with G is a likely shared Forge/XMage reading, the
case today's docs settle by hand (`crew_type_dependency`). Agreement is still not proof: P and X can
share a misreading of the rules, and P parses Oracle text with heavily AI-assisted development.

## Part 2: phase.rs follower (`followers/phase/`, in this repo)

Detail and citations: [audit-phase.md](2026-11-10-followers/audit-phase.md).

### Layout and pin

```
followers/phase/
  rust-toolchain.toml   nightly-2026-04-19            (phase's own pin; required: cargo-features=["codegen-backend"])
  Cargo.toml            [workspace] members=["phase-follower"]; [profile.follower] opt-level=2, thin LTO, panic="unwind"
  PHASE_PIN             "<engine sha> <card-data hash16> <sha256>"
  phase-follower/       bin; depends on phase-engine only (path dep into the fetched checkout), no features
scripts/phase-oracle-setup.sh   fetch phase at PHASE_PIN into .oracle-build/phase (shallow, detached), fetch
                                https://data.phase-rs.dev/card-data-<hash16>.json, verify sha256, build
scripts/phase-oracle-run.sh     RUNNER IN OUT [--preflight names.txt]; checks the pin; RUST_MIN_STACK
```

- **Pin invariant.** Engine sha and card-data hash are pinned together. At bump time,
  `card-data-meta.json`'s `commit` must equal the engine pin, or have the same
  `scripts/engine-source-hash.sh` fingerprint. Never pair an engine with "latest" data. The meta
  currently names `b14f974`, not HEAD.
- **Bump cadence.** phase made 15 releases in 29 days with no API promise, so bump monthly at most.
  The driver matches exhaustively over `WaitingFor` and `GameAction`, with no `_` arm, so new
  variants are compile errors, not silent passes.
- **Runtime.** Run the engine on a thread with a 64 MiB stack: phase assumes at least 16 MiB, and
  1 MiB aborts. Wrap each row in `catch_unwind`.
- **Licence note.** phase is MIT OR Apache-2.0, so it is compatible with this repo's Apache-2.0.
  MTGJSON is MIT. Add phase to `NOTICE`. The fetched checkout and card data stay in `.oracle-build/`,
  uncommitted, because they're large, not because of the licence.

### Interfaces

**I/O.** The request is the `follower-export` line (H3). The row is the shape in the contract
summary, with `engine:"phase"` and `phase_ref:"<sha>+<hash16>"`.

**P-support (preflight).** For each name, `CardDatabase::get_face_by_name` must resolve.
`resolve_deck_list` silently drops unknown names; never use it. Then, for every face including the
back face, require `card_face_gaps(face).is_empty() && face.metadata.confirmed_misparse.is_none()`. 21
of FDN's 23 unsupported cards are `Misparse:*` that parse fine, so the misparse check is essential.
Also refuse cards whose abilities draw from the RNG (coin, die, random discard) or use
`PhysicalContest`. Output `name\treason`.

**Names.** Map corpus (Forge) spellings to phase face names on the way in and back on the way out:

- split and room cards by first half;
- multi-face cards: phase keys each face separately, and `object.name` is the current face;
- accents via NFKD;
- tokens: phase uses CR 111.4 names ("Goblin"), Forge uses "Goblin Token". The comparator anonymises
  tokens to `token`, so only the snapshot `name` field needs care.

**Setup** (`setup.rs`). Use the production debug actions, not `GameScenario`. `add_real_card` runs the
real entry pipeline, so enters-tapped and enters-with-counters would apply and break the contract.

1. `GameState::new_two_player(seed = crc32(id))`, `debug_mode = true`.
2. Per seat, in gorge's creation order (battlefield, hand, graveyard, library, exile, library_top,
   Wastes filler, then tokens), call
   `create_debug_cards(DebugCardCreateRequest{owner, zone, run_etb:false, ...})`. Bind each ref to its
   returned `(ObjectId, incarnation)`. Library index 0 is the top.
3. Apply `SetTapped`, `ModifyCounters` (raw, no replacements), `SetSummoningSickness(false)`,
   `Attach{target}` for `attached`, and `SetLife`. Tokens come from `CreateToken` with custom
   characteristics from `neutral.token[stem]`. A back face uses `SetFaceState{transformed}`.
4. Set `turn_number`, `active_player = p0`, the phase, and `waiting_for`. Then `debug_mode = false`
   and `flush_layers`.
5. Run from untap of turn 1 to p0's first main-1 priority and take the `"setup"` snapshot. Apply
   `tapped_after_untap` at that priority: a debug `SetTapped` with `debug_mode` briefly re-enabled,
   recorded in `notes`.
6. Speed: unverified (`game/speed.rs`). Refuse with a harness row until a setter is found.

**Driving** (`steps.rs` and `answer.rs`). There is one entry point,
`apply(state, actor, GameAction) -> ActionResult`, and the engine issues the legal candidates for
every prompt (`AiDecisionContract::issue`). The step machine turns gorge ops into `apply` calls.

| gorge op | phase actions |
|---|---|
| `play` | `PlayLand` on the bound object |
| `activate` | Mana: `ActivateManaSource{source:(id,inc), ability_index:<matched>, mana_type}`, which completes at once. Other abilities: `ActivateAbility` with `payment_mode: Manual`, paid from the pool. The ability is matched through `neutral.ability[i]` (class + ordinal, text check). |
| `cast` | `CastSpell{payment_mode: Manual}`, then targets, modes and X from `gorge_decisions`, then the `ManaPayment` prompt paid by `SpendPoolMana` with `payment[i]` units first, then `PassPriority` to finalise. `cast_mode` maps to the casting variant (kicker, adventure, flashback ...); one with no mapping is a harness row. |
| `pass` | Exactly one `PassPriority` by the named seat; any other seat holding priority is a harness row. phase grants priority in upkeep, draw and a separate begin-combat window, as CR 117.3a and 507.2 say, and so does gorge. Any extra phase-only window is a `priority_shape` harness row. |
| `pass_to` | Loop `PassPriority` until the named decision (`DeclareAttackers`/`DeclareBlockers`/`Priority`) or the named step. A non-priority prompt met mid-loop is answered from the script or misses. |
| `attack`, `block` | `DeclareAttackers{attacks}` / `DeclareBlockers` built from the refs and submitted directly. Combat sets are not enumerated as candidates. |
| `resolve` | Pass until the stack is empty and the active player holds priority. |
| `attach`, `move` | Debug `Attach` / `MoveToZone{simulate:false}` (debug mode on for that action only), then one empty priority round, as the Forge driver does. |
| `mana` | Debug `AddMana`. |

**Answerer rules.**

- One match arm per `WaitingFor` variant: all 147 are tabled in the audit, §3.
- A prompt answered from the script must find its answer among the candidates. A miss produces
  `Missing <what> answer at step i`, in Forge's StrictMiss wording so classify reads it.
- A prompt with exactly one candidate may be auto-answered, and is noted `forced`.
- A multi-candidate prompt is never guessed.
- A `disposition` of reversed (the cast was rolled back) is a miss.
- There is no loose re-run in the first version.

Prompts refused by design: `ResolveAll*` and loop shortcuts (phase-only conveniences), `PhysicalContest`,
coin and die outcomes, and pregame, Bo3 and Alchemy prompts.

Prompts that need data gorge may not log today: trigger order (gorge does log `order` with pick kind
`trigger`), replacement order, combat damage assignment among more than one blocker, Phyrexian per-pip
choices, any-colour mana (gorge logs a colour answer for Any/Combo producers), and scry bottom order.
Until gorge logs one of these, it is a strict miss, which classify already reads as `decision_shape`.

**Identity.** ObjectIds survive zone changes, and `incarnation` marks the new object of CR 400.7.
RefTable maps gorge refs to `(ObjectId, incarnation)`. It rebinds on zone-change events, and binds
objects created mid-step (tokens, copies) by (name, controller, zone, event order).

**Snapshot.** Read `GameState` directly:

- P/T are post-layer; call `flush_layers` after any debug mutation.
- `counters` maps to gorge kinds.
- Stack entries name their source card.
- Combat comes from `state.combat`.
- Step names map from phase's `Phase`.
- Sort hand and exile; keep graveyard order and library top-5.

### Milestones (phase)

| # | Scope | Corpus | Exit criterion | Rough size |
|---|---|---|---|---|
| P0 | Toolchain, setup script, pin, preflight | FDN pools | `--preflight` reproduces the published FDN numbers (494/517; 196/196 commons) | ~300 LOC + scripts, 1–2 days |
| P1 | Setup only | `forge-oracle/fixtures/setup` plus FDN-commons level-A scenarios, setup checkpoint only | `"setup"` snapshot agrees with gorge on ≥95% of FDN-commons items | ~900 LOC, 3–4 days |
| P2 | cast / activate / resolve / pass, the answerer for targets, modes, X, yes/no, choose_n | `oraclediff gen -level A` on FDN commons, then FDN | Every row agrees or carries a classified, explained reason. Triage table committed under `oracle/xengine/followers/phase/` | +1,300 LOC, 1–1.5 weeks |
| P3 | Windows: pass_to, attack/block, attach prelude, the answerer's combat arms | `xengine-sweep` with `XENGINE_POOL=fdn-commons`, `XENGINE_FOLLOWERS='forge xmage phase'`, 1 chunk × 10 games | diff-phase classes histogrammed; `decision_shape` < 20% of sent windows | +600 LOC, ~1 week |
| P4 | Whole-game tapes and `:tape:tN` prefixes | same sweep, then `fdn.txt` | Tapes run to the lowering's cut (no follower turn cap); `harness` < 10% | +300 LOC, ~3 days |

## Part 3: Manabrew follower (fork `adams-shaun/manabrew`, branch `oracle`)

Detail and citations: [audit-manabrew.md](2026-11-10-followers/audit-manabrew.md).

### Layout and pin

```
manabrew-rs/crates/manabrew-oracle/     (new bin crate, AGPL, in the fork)
  src/main.rs           scenarios IN OUT --archive A | preflight NAMES | build-archive --res R
  src/request.rs        port of FO/Request.java (+ neutral, ignored)
  src/decision_queue.rs port of FO/DecisionQueue.java
  src/ref_table.rs      port of FO/RefTable.java  (ref ⇄ CardId / stack id / PlayerId)
  src/setup.rs          port of FO/SetupBuilder.java over patch #2
  src/step_machine.rs   port of FO/StepMachine.java
  src/scripted_agent.rs impl PlayerAgent — all 83 methods, via a delegate macro (parity/src/runner.rs:424-495 lists the signatures)
  src/scripted_rng.rs   impl GameRng — fails on any draw the script doesn't supply
  src/snapshot.rs       port of FO/SnapshotWriter.java
  src/support.rs        preflight (below)
  src/result_row.rs     port of FO/ResultRow.java
gorge-compliance:
  MANABREW_ORACLE_REF in the Makefile; scripts/manabrew-oracle-setup.sh (clone fork at ref into
  .oracle-build/manabrew, cargo build --profile oracle, build the card archive from gorge's FORGE_REF res);
  scripts/manabrew-oracle-run.sh (checks both pins, one worker process per core, restarts a worker after a panic)
```

- **Card pin.** Manabrew reads Forge scripts from an rkyv archive built from any `res/` tree. Build it
  from gorge's `FORGE_REF` res, never from Manabrew's own Forge submodule (`be9e0c8`). That submodule
  is upstream 2026-09-21 and 1,049 commits behind. The runner checks that the archive's recorded
  cardsfolder and tokenscripts tree hashes equal `FORGE_REF`'s: the same invariant
  `forge-oracle-setup.sh --check-pin` enforces.
- **Build profile.** Add `[profile.oracle] inherits="release", opt-level=3`; the release profile is
  size-optimised. Any stable toolchain works.
- **Licence.** AGPL, out of process: JSONL over files, the same as GPL Forge today. The fork is public.

### Engine patches (small; meant to go upstream as generic hooks)

1. **`take_state_edit` agent hook** (~25 LOC): polled in `priority_round` before `choose_action` and
   in the mana-payment loop. Forge's StepMachine adds pool mana and taps `tapped_after_untap` from
   inside the priority callback, but a Rust agent only sees `&GameState`. The same polling pattern as
   the existing `take_restore_request`.
2. **`GameState::place_for_setup(card, zone, controller, PlaceOpts{tapped, counters, face, attach})`**
   (~60 LOC). `zones` is private, and `move_card` applies entry replacements and adds loyalty.
3. **A diagnostics sink** (~60 LOC): route the engine's `eprintln!` diagnostics to a readable
   thread-local list. That covers "Unimplemented effect API type", "Unrecognized Count expression",
   "missing required param", "Unimplemented mode" and the `[WARN]` cost lines. Until it lands, capture
   fd 2 and match patterns.
4. Optional, later: a Params read-tracker, which lists keys a script set but the engine never read,
   and a simultaneous-trigger-order hook.

### Interfaces

**M-support (preflight)** is the hard part. Manabrew never refuses a card. It improvises silently:

- An unknown `SP$`/`AB$`/`DB$` is a no-op.
- An unknown trigger `Mode$` drops the trigger.
- An unknown static or replacement mode is inert.
- An unsupported trigger condition evaluates to true.
- An unknown `Count$` is 0.
- 74 of the 83 agent methods default to first/min/false/true.

For each name, using the engine's parsed IR:

- (a) every API resolves and is in `IMPLEMENTED_API_TYPES`;
- (b) every trigger `Mode$` is one `parse_trigger` matches. The match is case-sensitive, while
  Forge's isn't;
- (c) no static is `StaticMode::Other`, and (d) no replacement is `ReplacementType::Other`;
- (e) no `Unsupported(..)` IR variant: `DefinedRef`, `SpellAbilityMode`, `AbilityDuration` or
  `TriggerCondition`;
- (f) every `Key$` is one the Rust source reads. Use the read-tracker once patch 4 lands; until then,
  port `2026-11-10-followers/mb_scan.py`'s literal scan.

Static estimate: FDN 479/517 (92.6%) clean, commons 94.6%. Only 4 FDN scripts differ between the two
Forge pins, which makes FDN the safest start. Known misses beyond FDN:

- `FlipCoin`: the Rust code only knows Forge's old name, `FlipACoin`, so 84 cards silently do nothing;
- `Empower`, `Recruit`, `HealDamage`;
- at gorge's pin, `PutSticker` and `Assimilate`;
- P/T switching (layer 7e).

**At run time, any of these fails the row:**

- a diagnostics-sink entry;
- an "Illegal action ignored" notification (the engine downgrades an illegal action to a pass);
- a cast or activation that doesn't reach the stack;
- a ScriptedRng draw with no scripted answer.

**ScriptedAgent.** Override all 83 methods.

- Call `provide_priority_action_space(false)`, so every priority reaches the agent rather than
  auto-passing.
- Answer `pay_mana_cost` with `Pay{auto:false}` after patch 1 has added `payment[i]` to the pool.
  The default answer is `AttemptedAndFailed`, which can never cast.
- Validate every action against `request_action_space()` before returning it.
- Trait methods can't return errors. So record the miss, raise the abort signal, and return a
  harmless value.
- Answer gorge's decisions with Forge's own mapping rules (StepMachine.java 922-1735): targets by
  spec, modes by SpellDescription text, trigger order reversed, arrange keeping picks on top.
  Manabrew uses the same scripts, so the `abilities` script line and the mode texts match directly.

**No clean mapping yet:**

- Simultaneous-trigger order: the engine orders APNAP internally. A tape where gorge chose an order is
  a `known_divergence` row until patch 4.
- Pile split, vote, `chooseSingleStaticAbility`, `chooseManaFromPool` and the other prompts with no
  trait method.

**Setup and run.**

1. `GameState::new(["p0","p1"], 20)`, then create cards in gorge's order. The Rust library top is the
   last element.
2. `place_for_setup` for each placement, then `apply_continuous_effects`.
3. Skip `GameLoop::setup` (no d20 roll, no mulligan).
4. `run_turn` from untap; the StepMachine starts at p0's first main-1 priority, as the Forge driver
   does.

**Snapshot.**

- Read from the agent's `snapshot_state(&GameState, pools)`, after calling `request_action_space()` so
  continuous effects are fresh. Card ids are stable arena indices.
- Token names come from the same tokenscripts as Forge's.
- Open question: whether attacking and blocking show on the card or only in `CombatState`, which the
  agent doesn't see. Settle it in M0.

### Milestones (Manabrew)

| # | Scope | Exit criterion | Rough size |
|---|---|---|---|
| M0 | Probe: build, card archive from FORGE_REF res, a 100-line bin that places a board and casts Shock. Settles back-face setup, `decide_cost_part → None` semantics, attack flags, and "unless" costs | Questions answered in this doc's follow-up | 1–2 days |
| M1 | Patches 2 and 3, setup, snapshot, preflight | `"setup"` agrees with gorge on ≥95% of FDN-commons items whose cards pass preflight | ~1,200 LOC |
| M2 | Patch 1, cast/activate/resolve, ScriptedAgent targeting, picks and yes/no, ScriptedRng | Level-A FDN commons, then FDN: every row agrees or carries a classified reason; F≠M items triaged into Manabrew bug vs driver bug | +2,500 LOC |
| M3 | Combat, pass_to, windows | Sweep with `XENGINE_FOLLOWERS='forge xmage manabrew'` on the commons pool | +900 LOC |
| M4 | Whole-game tapes | Tapes run past Forge's turn-5 cap | +500 LOC |

Total about 5,100 LOC Rust, plus about 200 LOC of engine patches and 1,500 of tests. For comparison,
the Forge driver is 8,008 lines of Java.

## Order of work

1. **H1 → H2 → H3 → H4.** Pure Go, no Rust needed. Ship as small units through the agentctl
   pipeline. H5 waits until a third follower produces rows.
2. **P0 → P1 → P2** on FDN commons. The toolchains are installed (2026-11-10).
3. **H5**, once phase rows exist. Then **P3 → P4**.
4. **M0**, then M1–M4. Manabrew is lower priority: it adds replay capacity and a check on the Forge
   driver, not independence.
5. Widen pools: FDN commons → FDN → Pauper → the declared Standard sets.

## Handing off upstream

The aim is that phase.rs or Manabrew could own their follower.

- **One engine-neutral protocol document.** Once H3 is done, add `docs/follower-protocol.md`, written
  from audit-contract.md §1–§3: request and row schemas, the step vocabulary, ref grammar, decision
  kinds, snapshot fields, and the error-message conventions classify reads. The drivers cite it, not
  the Forge Java.
- **A conformance pack an upstream CI can run without gorge.** About 200 FDN-commons requests, gorge's
  snapshots for them, and a tiny `cmp` binary (`oraclediff` compare, built standalone). Publish it as
  a release artefact of this repo, keyed by GORGE_PIN and scenario_sha. A follower passes if every row
  agrees or matches a committed expected-difference list.
- **phase.rs.** Its `docs/AI-CONTRIBUTOR.md` rules out drive-by PRs, and mtg-rl-sim's upstream offer
  (#10736) sits unanswered. So open an issue or discussion first, offering `phase-follower` as a crate
  they could adopt. It depends only on `phase-engine`'s public API, so it can move into their
  workspace unchanged. Carry no card patches: the follower's value is reporting phase as it is.
- **Manabrew.** Offer patches 1–3 upstream as generic agent and diagnostic hooks. They're useful
  without gorge: patch 3 would have caught the `FlipCoin` rename. Upstreaming means following their
  AGENTS.md rules: Conventional Commits, no comments, mirroring Java names. Their parity harness is
  deprecated, so a nightly gorge-vs-Manabrew run could be the guard they no longer have.

## Risks

| Risk | Engine | Mitigation |
|---|---|---|
| Upstream churn breaks the build | P (very high), M (low) | Pin; exhaustive matches; bump on purpose |
| Silent improvisation makes a gap look like agreement or a rules difference | M | Preflight plus diagnostics sink plus override all 83 methods; no loose mode |
| Oracle-text errata vs Forge script drift reads as divergence | P | A `card_text_drift` triage class: compare gorge's script with the MTGJSON Oracle text for the card |
| Prompts gorge doesn't log | P, M | Strict miss → `decision_shape`; add gorge logging where the count justifies it |
| Ability/mode identity across encodings | P | `neutral` ordinals plus a text check (H3); a mismatch is a harness `ability_unmatched` |
| Build cost (phase engine ≈1.4M lines; multi-GB target dir) | P | Build once per pin into `.oracle-build/phase`; sccache |
| Engine panics and global state | P, M | `catch_unwind` per row; Manabrew: one worker process, restarted after a panic |
| Families agree on a shared CR misreading | all | Agreement is evidence, not proof; rulings still decide |

## Open questions

- ~~Is installing a Rust toolchain on the sweep box acceptable?~~ Yes (2026-11-10). rustup was
  already in `~/.cargo/bin`, off the default PATH. `nightly-2026-04-19` was added. Target
  directories go in `.oracle-build/`.
- Should gorge log the decisions phase asks for and Forge resolves internally (replacement order,
  damage assignment order, scry bottom order)? Only once P2's miss counts show which matter.
- Where does a Pauper-wide pool stop being "simple"? Pauper commons pull in cross-set mechanics gorge
  may not support; H4 filters to gorge-supported cards, so the pool shrinks rather than fails.
