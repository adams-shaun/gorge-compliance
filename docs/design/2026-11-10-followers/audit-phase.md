<!-- Audit notes written 2026-11-10 for the phase.rs / Manabrew follower design. Read-only static audit; local paths (/tmp/..., /mnt/sata/...) are from the audit machine and abbreviations are defined at the top. Historical, like every design note. -->

# phase.rs as a gorge-compliance follower engine — audit notes

Audited: shallow clone `/tmp/mb-probe/phase` at `c53dd8a81af2354a14e811a806670c21585a7c06`
(2026-11-10 22:15 UTC, workspace version 0.122.0, `Cargo.toml:15`). Static audit only; nothing was
built or run (no Rust toolchain on this box). Paths below are relative to `/tmp/mb-probe/phase/`
unless they start with `rlsim:` (= github.com/kevinbrar/mtg-rl-sim, files mirrored under
`.../scratchpad/rlsim/`) or `forge:` (= the Java follower at `.../scratchpad/forge/forge-oracle/src/main/java/forge/oracle/`).
Downloaded public data lives in `.../scratchpad/pdata/` (card-data-meta.json, coverage-summary.json,
coverage-data.json, card-data.json = card-data-d0ffa159dcde97c7.json).

TL;DR: **feasible, and a better fit than it first looks.** phase has (a) a production (non-test)
"debug action" setup surface that places real DB cards raw (no replacements, no triggers, no SBAs) and
sets tapped/counters/attachments/summoning-sickness/life/phase; (b) one reducer entrypoint
`apply(state, actor, action)` plus an engine-issued legal-candidate list for every prompt
(`AiDecisionContract::issue`); (c) a per-card `supported` signal that already folds in confirmed
misparses. The hard parts are hidden-randomness forcing (shuffles, coin/dice), gorge-side data for
ordering prompts (trigger order, replacement order), ObjectId/incarnation mapping, and a very high
upstream churn rate (release every 1–3 days).

---------------------------------------------------------------------------------------------------

## 1. Build

### Toolchain
- `rust-toolchain.toml:1-5`: `channel = "nightly-2026-04-19"`, components rustfmt/clippy/
  `rustc-codegen-cranelift-preview`, target `wasm32-unknown-unknown`.
- Nightly is *required*, not just preferred: root `Cargo.toml:1` has `cargo-features = ["codegen-backend"]`
  and profiles set `codegen-backend = "llvm"` (`Cargo.toml:80,106`). A stable cargo refuses the manifest.
- mtg-rl-sim pins the same nightly in its own `rust-toolchain.toml` and builds `phase-env` in a
  **separate workspace** whose only member is `phase-env` with phase as an excluded submodule
  (rlsim:`Cargo.toml`, `phase-env/Cargo.toml`: `engine = { path = "../phase/crates/engine", package = "phase-engine" }`).
  That is the template to copy: our crate gets its own `[profile.*]`; phase's workspace profiles do not apply.
- Profiles worth copying into the follower workspace: phase's `release` is the WASM-size profile
  (`opt-level='z'`, fat LTO, `codegen-units=1`, `panic='abort'`, `Cargo.toml:82-87`) — do **not** use it.
  Use the equivalent of `server-release` (`opt-level=2`, thin LTO, cgu 16, `panic='unwind'`,
  `Cargo.toml:95-100`). `panic='unwind'` matters: we want `catch_unwind` to turn engine panics into
  a row-level "engine panic" error instead of killing the batch.
- **Stack size:** `.cargo/config.toml` `[env] RUST_MIN_STACK = "16777216"` (16 MiB) — the engine
  assumes deep recursion (Effect ↔ AbilityDefinition, loop replay with by-value `GameState` frames;
  comment in `.cargo/config.toml` `[target.wasm32...]` block says 1 MiB stacks SIGABRT). The driver must
  run the engine on a `std::thread::Builder::new().stack_size(64<<20)` thread (main thread is 8 MiB on
  Linux, which is under phase's own assumption).
- Size: `crates/engine/src` ≈1.43 M lines of Rust (incl. inline tests), `phase-parser` ≈0.70 M,
  `phase-ast` ≈81 k. `game/engine.rs` alone is 31.6 k lines, `types/game_state.rs` 60.3 k. Expect
  multi-minute cold builds and several GB of target dir; CI uses Tilt rather than ad-hoc cargo.

### Crates needed for a headless driver
- Only **`phase-engine`** (lib name `engine`, `crates/engine/Cargo.toml:1-13`), which pulls
  `phase-ast` and `phase-parser` (path deps, `crates/engine/Cargo.toml:31-32`). Plus serde/serde_json/`im`.
  `engine` re-exports `im` (`crates/engine/src/lib.rs:26`).
- Native deps of `engine` are plain (`rand 0.9`, `rand_chacha 0.9`, `indexmap`, `petgraph`, `nom 8`,
  `im 15`, `rustc-hash`, `smallvec`, `web-time`) — `crates/engine/Cargo.toml:30-60`. Nothing wasm-only.
- **Avoid**: `engine-wasm` (wasm-bindgen 0.2.114, serde-wasm-bindgen; `crates/engine-wasm/Cargo.toml`),
  `draft-wasm`, `lobby-worker/broker-wasm` (excluded from workspace, `Cargo.toml:8-11`),
  `phase-server`/`server-core` (tokio server; but `server-core/src/session.rs:1966-2024` is a useful
  reference for how a transport materializes `Debug::CreateCard`), `phase-ai` (only needed if we
  want an AI opponent; it brings mimalloc as `#[global_allocator]` in its bins), `phase-llm`, `client/`.
- Features of `phase-engine` (`crates/engine/Cargo.toml:15-29`): `test-support` ("Exposes raw zone
  placement for test crates; never enabled by production consumers") turns on `game::scenario`,
  `game::scenario_db`, and makes `game::zones` public (`crates/engine/src/game/mod.rs:184-229`).
  It is a normal cargo feature (also used by the shipped `combo-verify` and
  `resolution-face-census` bins), so a follower *can* enable it, but §2 shows we don't need it.
  `cli` adds tracing-subscriber; `forge`, `audit`, `proptest`, `interaction-bindings` irrelevant.

### card-data.json (MTGJSON-derived) — generation and pinning
- The engine does **not** parse Oracle text at load time from MTGJSON; it loads a pre-parsed export:
  `CardDatabase::from_export(path)` (`crates/engine/src/database/card_db.rs:117`), keyed by lowercase
  face name. Generated by `scripts/gen-card-data.sh`: downloads MTGJSON `AtomicCards.json`, `Meta.json`,
  `CardTypes.json`, `SetList.json`, token sets, `AllDeckFiles` (weekly-refreshed cache,
  `gen-card-data.sh:31-110`), then runs the `oracle-gen` bin (`--stats --names-out ... --sidecar-dir`,
  `gen-card-data.sh:~296`), then `coverage-report --all --brief` for coverage-data.json
  (`gen-card-data.sh:~393`). `GATED_SETS_AS_OF` (default: today UTC) gates unreleased sets
  (`gen-card-data.sh:26-29`) — so regeneration is date-dependent; pin it.
- **Content addressing:** `DATA_HASH = sha256(card-data.json)[:16]`, published as
  `card-data-<hash16>.json` (`gen-card-data.sh:~371-382`). `card-data-meta.json` maps the current
  hash to the engine commit. Fetched today:
  ```json
  {"generated_at":"2026-11-10T17:31:45Z","commit":"b14f974787d86b6b6ade67aaaed67d32d68d7fd8",
   "mtgjson_version":"5.3.0+20261110","data_hash":"d0ffa159dcde97c7",
   "data_filename":"card-data-d0ffa159dcde97c7.json"}
  ```
  I downloaded `https://data.phase-rs.dev/card-data-d0ffa159dcde97c7.json` (100,959,880 bytes) and
  its sha256 prefix is `d0ffa159dcde97c7` — the name is a verifiable content hash. Old hashed files
  stay live (`card-data-5b455542437560f2.json`, rlsim's file for 988bc34, still returns 200), because
  shipped WASM bundles reference their own hash (`gen-card-data.sh:~365-370` comment; release.yml:1145).
  `coverage-data.json` / `semantic-audit.json` are stamped with the same `card_data_hash`
  (`.github/workflows/release.yml:398-406`); the downloaded coverage-data.json has
  `card_data_hash: d0ffa159dcde97c7`, so the pair is consistent.
- **Mapping hash → engine commit:** only `card-data-meta.json` at publish time says which commit made
  which hash; there is no historical index. The *parse-relevant* engine fingerprint is
  `scripts/engine-source-hash.sh <commit>` (git ls-tree hash of engine/phase-ast/phase-parser src+data,
  manifests, Cargo.lock, truncated to 16 hex) — this is what keys CI's parse-diff baseline and the
  cardgen cache. Card data generated at commit A is valid for commit B iff both have the same
  engine-source hash (and same MTGJSON vintage).
  Note: the current meta commit `b14f974` ≠ clone HEAD `c53dd8a` (HEAD is a later "refresh MTGJSON
  token & subtype catalogs" commit, #10755). **Never pair an engine commit with "latest" card data.**
  Recommended pin (what rlsim does in `ci/card-data.txt`): a file with `<engine sha> <card-data hash16>
  <sha256>`; at pin-bump time fetch `card-data-meta.json`, check its `commit` equals the engine pin
  (or same `engine-source-hash`), download the hashed file, verify sha256.
  Alternative for full reproducibility: build `oracle-gen` at the pinned commit and generate locally
  from a pinned MTGJSON vintage (rlsim does this for its patched parser).
- Card-data schema per entry (observed): `name, mana_cost, card_type, power, toughness, loyalty,
  defense, oracle_text, keywords, abilities, triggers, static_abilities, replacements,
  color_identity, legalities, printings, rarities, scryfall_oracle_id, metadata{source_printing_ids,
  related_token_ids, confirmed_misparse?, spellbook?}`. Multi-face cards are **flattened by face**:
  `fire`, `ice` are separate keys, `fire // ice` is not a key (lookup via `resolve_name` splits
  composite names and falls back to the front face, `card_db.rs:617-650`).

### Determinism and seeding
- One seeded generator: `GameRng` wraps `ChaCha20Rng::seed_from_u64(seed)`
  (`crates/engine/src/types/game_state.rs:122-160`), drawn via `rng.draw(RandomDraw::Placement|Outcome)`;
  `rng_seed` / `rng_word_pos` persisted on `GameState` (`game_state.rs:~23876-23888`).
  `GameState::new_two_player(seed)` (`game_state.rs:34285`).
- Starting player: `start_game` rolls d20s from the rng (`game/engine.rs:21024-21070`);
  `start_game_with_starting_player(state, p)` is the deterministic path (`engine.rs:21089`).
- Shuffles: library shuffles go through `game/library.rs:16-60` (`resolve_and_apply_library_shuffle`)
  which journals a `ResolvedLibraryShuffleCommand{precondition_order, resulting_order, pre/post word_pos}`
  and emits `GameEvent::PlayerPerformedAction{action: ShuffledLibrary}` (`library.rs:112-114`).
  The opening shuffle in `mulligan::start_mulligan` (`game/mulligan.rs:86-110`) and the mulligan
  re-shuffle `shuffle_library_of` (`mulligan.rs:1022-1030`) call `shuffle_vector` directly and do **not**
  go through the journal or emit that event.
- Engine-side wall clock / HashMap order: the AI had a RandomState-order leak (#4878, closed); the
  rules engine itself is designed to be replayable from `ReplayHeader{seed, deck_data,...}` + action
  list (`types/replay.rs:1-40`). Engine `std::HashMap`s are used in state (`game_state.rs:4`), so
  serialized snapshots must be normalized (sort) — never diff raw JSON.

## 2. Setup (board state without ETB)

There are two usable paths. **Recommendation: path B (debug actions), with A's helpers as reference.**

### A. `GameScenario` + `GameScenarioDbExt::add_real_card` (test-support only)
- Gated: `#[cfg(any(test, feature = "test-support"))] pub mod scenario; pub mod scenario_db;`
  (`game/mod.rs:184-187`). Usable from an external bin by enabling `features = ["test-support"]`.
- `add_real_card(player, name, zone, &db)` (`game/scenario_db.rs:80-152`): `get_face_by_name` (panics if
  missing), `create_object_from_card_face` (object created in Library), `populate_back_face_if_dfc`, then
  - non-battlefield: `add_to_zone` (= `push_back`; library index 0 is top, `scenario.rs:263-276`,
    `zones.rs:3092-3094`), so adding library cards top-first yields the given order;
  - battlefield: **`zone_pipeline::move_object(ZoneMoveRequest::effect(..))`** — i.e. the real entry
    pipeline: *entry replacements apply* (enters-tapped lands enter tapped, "enters with counters"
    applies, as-enters choices are abandoned by `abandon_as_enters_choice_for_scenario_setup`,
    `scenario_db.rs:28-68`; an Aura with no host panics, `:121-126`). Events are discarded so no ETB
    *triggers* fire. Then `summoning_sick=false`, `entered_battlefield_turn = turn-1`, reindex triggers.
  - This violates the Forge/XMage placement contract ("entry replacements do not apply; the scenario's
    own tapped and counters fields state the rest", forge:`SetupBuilder.java:22-27`).
- `GameScenario` knobs: `at_phase` (forces `turn_number=2`, priority to active player,
  `scenario.rs:213-221`), `with_life`, `with_counter` (raw counter write, `:323-340`),
  `with_mana_pool`, `CardBuilder::with_summoning_sickness`/`controlled_by`/`with_plus_counters`
  (`:1438-1495`); `GameScenario.state` is `pub(crate)` but `build() -> GameRunner` and
  `GameRunner::state_mut()` (`:1650`) give full `&mut GameState`. `GameRunner::act` adds a test-only
  shortcut that auto-passes BeginCombat before `DeclareAttackers` (`:1614-1640`) — don't use `act`;
  call `engine::game::engine::apply` directly.

### B. Production debug actions (no feature flag) — recommended
- `GameAction::Debug(DebugAction)` (`types/actions.rs:1206-1400`), gated on `state.debug_mode`
  (`game_state.rs:24727`) and the optional `debug_permitted` set (`engine.rs:945-957`), preflighted by
  `preflight_debug_action` (`engine.rs:18018`) and applied by `engine_debug::apply_debug_action`
  (`game/engine_debug.rs:44`) through the normal `apply` boundary (`engine.rs:12599-12611`).
- Card creation: `engine::game::{debug_card_entry_source, create_debug_cards, DebugCardCreateRequest}`
  are `pub` re-exports (`game/mod.rs:265-268`, `engine_debug.rs:1356-1420`). Request fields:
  `owner, zone, count, attach_to, run_etb, nonlegendary, creation_kind{Card|Token}`.
  `run_etb:false` = "place the card raw with no entry effects — mirrors MoveToZone{simulate:false};
  a planeswalker or battle still arrives with its printed loyalty/defense counters"
  (`actions.rs:1243-1252`). That is exactly gorge's contract (gorge's MoveZone grants loyalty/defense).
  `attach_to` sets `attached_to` before entry (`actions.rs:1231-1235`).
- Then, per placement, through `apply(state, P0, GameAction::Debug(..))`:
  `MoveToZone{object_id, to_zone, library_position, simulate:false}` — raw move under the
  `DebugCommand` cause, skipping replacement consult and delivery tail (`engine_debug.rs:51-86`;
  library placement Top/Bottom/index via `library_position`); `SetTapped`, `ModifyCounters{delta}`
  ("bypasses replacement effects", `actions.rs:1286-1292`), `SetSummoningSickness`, `Attach{target}`
  (object or player), `SetController`, `SetFaceState{transformed,flipped,face_down}`, `SetLife`,
  `ModifyPlayerCounters` (poison), `ModifyEnergy`, `AddMana`, `SetPhase{phase, active_player}`,
  `RunStateBasedActions`, `CreateToken{…}` (preset or custom; see `actions.rs:~1390+`).
  `GainLife/LoseLife/GivePlayerCounters/GiveEnergy` are *real events* and require a Priority window
  (`engine.rs:18031-18042`) — use `SetLife`/`Modify*` instead for setup.
- Every debug action appends `GameEvent::DebugActionUsed` (`engine.rs:12603-12609`); after setup set
  `state.debug_mode = false` so a script can never reach debug actions.
- Starting phase / active player / priority: `DebugAction::SetPhase{phase, active_player}`, or direct
  field writes (`turn_number, active_player, phase, priority_player, waiting_for` are `pub`,
  `game_state.rs:~23721-23725, 22211`). Set `waiting_for = WaitingFor::Priority{player: active}` and
  `priority_player` consistently, as `at_phase` does. Summoning sickness depends on
  `entered_battlefield_turn` vs `turn_number` (`game_object.rs:1102-1121`), so set `turn_number`
  *before* placing (gorge scenarios usually say "turn 2+, permanents pre-existing").
- Library order: create library cards top-first with `library_position: Bottom` (or place each with
  explicit index); hand/graveyard/exile append in given order. All of `Player.library/hand/graveyard`
  are `pub im::Vector<ObjectId>` (`types/player.rs:98-100`) and `GameState.exile/battlefield` are pub
  (`game_state.rs:~23864-23870`), so a final "assert order == requested" check is trivial.
- Tokens on the battlefield at setup: `CreateToken` debug action (catalog preset or explicit
  characteristics), or `creation_kind: Token` with a real card face (token copy of a card).
- After setup: `layers::flush_layers(&mut state)` (`game/layers.rs:5630`) before the first snapshot
  (debug actions call `mark_layers_full`; `apply` finalizes rules state at the boundary,
  `engine.rs:2190-2198`).
- What it doesn't give: a "tapped_after_untap" style hook — that is just ordering (place, then the
  scripted turn starts at the step after untap). Speed (Aetherdrift) exists in `game/speed.rs`;
  not checked for a debug setter.

## 3. Driving

### Entry point
- `pub fn apply(state: &mut GameState, actor: PlayerId, action: GameAction) -> Result<ActionResult, EngineError>`
  (`game/engine.rs:862-868`). `actor` must equal the authorized submitter (`check_actor_authorization`);
  for a follower use `state.waiting_for.acting_player()` (`game_state.rs:20899`) or the contract's
  `authorized_actor`. `apply_as_current` (`engine.rs:9499`) derives the actor itself.
  `ActionResult{events, waiting_for, log_entries, disposition}` (`game_state.rs:22209-22219`);
  `disposition` can be "reversed" (cast/activation rolled back, `EngineError::ActivationReversed/
  SpellCastReversed`, `engine.rs:116-129`) — treat as a script miss, not success.
- **Legal candidates for any prompt:** `engine::ai_support::legal_actions(state)` (flat, *excludes
  mana actions*, `ai_support/mod.rs:2378-2387`) and `legal_actions_full` (flat + per-object grouping
  that *includes* mana actions, `:2541`), or the contract used by mtg-rl-sim:
  `AiDecisionContract::issue(state, semantic_owner)` → `{semantic_owner, authorized_actor,
  state_revision, candidates: Vec<CandidateAction>}` and `apply_ai_action_proposal(state, &contract,
  actor, action)` → `AppliedAction | AppliedStackPass | Stale | Rejected` (`ai_support/context.rs:28-82`).
  Candidates are exact for discrete prompts; "combinatorial combat declarations are bounded by the
  authoritative combat validators" (`context.rs:23-26`) — i.e. attacker/blocker sets are not enumerated
  exhaustively; build `DeclareAttackers/DeclareBlockers` from the script and submit directly.
- Actions are serde-tagged JSON (`GameAction`, `types/actions.rs:157-905`, ~150 variants), so a
  script can in principle carry raw phase actions — but ObjectIds are phase-internal, so the adapter
  should translate gorge refs → phase actions and *then* require the result ∈ candidates (or, for
  combat, accepted by `apply`).

### WaitingFor → scripted answer (all 147 variants, `types/game_state.rs:17201-20006`)
Policy column legend: **OK** = discrete answer from gorge's decision log, matched to a candidate;
**order needed** = gorge must log an ordering; **FLAG** = no clean scripted answer;
**UX** = phase-only convenience prompt, never initiate it, fail if it appears;
**2p forced** = single legal candidate in 2-player, may be auto-answered *only* when the candidate list
has length 1 (log it as "forced", not "improvised"); **out of scope** = formats/mechanics gorge doesn't
produce — refuse the card up front.

| WaitingFor (game_state.rs line) | Answer GameAction(s) | Script source | Flag |
|---|---|---|---|
| `Priority` (:17202) | PassPriority / PlayLand / CastSpell / ActivateAbility / Equip / CrewVehicle / Foretell / PlayFaceDown / TurnFaceUp / ActivateNinjutsu / ActivateManaSource ... | gorge step: cast/activate/play/pass | OK |
| `ResolveAllConsent` (:17207) | RespondResolveAllConsent | none (phase UX batch-resolve) | UX: never initiate BeginResolveAll; fail if seen |
| `ResolveAllReady` (:17213) | RevokeResolveAllConsent | none | UX: as above |
| `MeldPairChoice` (:17219) | ChooseMeldPair | meld pair | OK (rare) |
| `MeldAttackTargetChoice` (:17225) | ChooseEntryAttackTarget | attack target of melded permanent | OK (rare) |
| `EntryAttackTargetChoice` (:17232) | ChooseEntryAttackTarget | "enters attacking" target | OK |
| `MulliganDecision` (:17260) | MulliganDecision{Keep|Mulligan|UseSerumPowder|FreeReveal}; then SelectCards for bottom (MulliganDecisionPhase) | tape mulligan decisions + bottomed cards | OK, but library must be re-forced after each mulligan shuffle (start_mulligan does not emit ShuffledLibrary) |
| `OpeningHandBottomCards` (:17277) | SelectCards | - | Tiny Leaders only; out of scope |
| `ManaPayment` (:17281) | TapLandForMana / ActivateManaSource / SpendPoolMana / UnspendPoolMana / UntapLandForMana / PassPriority(=finalize) / CancelCast | tape mana payments (source + color) | OK in CastPaymentMode::Manual; Auto never surfaces it |
| `ManaSourceSelection` (:17292) | ActivateManaSource / BackToManaPayment | which sacrificial mana source | OK (Auto/AutoExceptSacrificialMana stop here) |
| `AssistChoosePlayer` (:17305) | ChooseAssistPlayer | assist helper | 2p forced-ish; rare |
| `AssistPayment` (:17319) | CommitAssistPayment | assist amount | rare |
| `ChooseXValue` (:17341) | ChooseX | X | OK |
| `TargetSelection` (:17352) | SelectTargets{targets} / ChooseTarget{target} | cast/activate targets | OK; refs are TargetRef::Object(ObjectId)/Player |
| `DeclareAttackers` (:17368) | DeclareAttackers{attacks:[(id,AttackTarget)],bands} | gorge attack declaration | OK but build the action directly (candidates are bounded, not exhaustive) |
| `DeclareBlockers` (:17402) | DeclareBlockers{assignments:[(blocker,attacker)]} | gorge block declaration | OK, build directly |
| `UntapChoice` (:17461) | ChooseUntap | "may choose not to untap" | OK |
| `ChooseUntapSubset` (:17480) | SelectCards | untap subset | OK |
| `ExertChoice` (:17496) | ChooseExert | exert set | OK |
| `EnlistChoice` (:17508) | ChooseEnlist | enlist | OK |
| `GameOver` (:17515) | - | terminal | terminal |
| `ReplacementChoice` (:17518) | ChooseReplacement{index} / ChooseReplacementAndRemember | replacement order (CR 616.1) | NEEDS gorge to record order; index into engine-ordered candidate list => map by replacement source name |
| `EntryControllerChoice` (:17544) | ChooseEntryController | controller of entering permanent | rare |
| `OrderTriggers` (:17555) | OrderTriggers{order} | APNAP same-controller trigger order | NEEDS gorge to record trigger order; map by source name+ability text, ambiguous for identical triggers |
| `CopyTargetChoice` (:17561) | ChooseTarget | copy/clone target | OK |
| `ExploreChoice` (:17578) | ChooseTarget | explore keep/graveyard | OK |
| `ReturnAsAuraTarget` (:17600) | ChooseTarget | aura host on return | OK |
| `ChooseTokenHost` (:17630) | ChooseTarget | token host | OK |
| `EquipTarget` (:17639) | Equip | equip target | OK |
| `CrewVehicle` (:17645) | CrewVehicle | crew set | OK |
| `StationTarget` (:17664) | ActivateStation | station | OK |
| `SaddleMount` (:17672) | SaddleMount | saddle set | OK |
| `ScryChoice` (:17684) | SelectCards{cards = keep-on-top set, in order} | scry tops/bottoms | OK (mtg-rl-sim confirmed polarity: cards = kept on top) |
| `RippleRevealChoice` (:17693) | RippleChoice | ripple | rare |
| `RippleBottomOrder` (:17709) | SelectCards | bottom order | order needed |
| `RevealUntilBottomOrder` (:17730) | SelectCards | bottom order | order needed; gorge may randomise (CR says any order) |
| `DigBottomOrder` (:17748) | SelectCards | bottom order | order needed |
| `ArrangePlanarDeckTopChoice` (:17756) | SelectCards | planechase | out of scope |
| `RedistributeLifeTotals` (:17765) | SubmitLifeRedistribution | life split | rare |
| `CoinFlipKeepChoice` (:17772) | SelectCoinFlips | which flips to keep | FLAG: the flip OUTCOME comes from state.rng (RandomDraw::Outcome) and cannot be scripted |
| `DieKeepChoice` (:17796) | SelectDieRolls | which rolls to keep | FLAG: roll outcome from rng, not scriptable |
| `DigChoice` (:17805) | SelectCards | look-at-top-N pick | OK |
| `DigRestSplitChoice` (:17874) | SelectCards | split rest | OK |
| `SurveilChoice` (:17951) | SelectCards{cards = ordered keep-on-top set; rest to graveyard} (engine_resolution_choices.rs:5340) | surveil | OK |
| `RevealChoice` (:17955) | SelectCards | reveal choice | OK |
| `SearchChoice` (:17974) | SelectCards | tutor pick (incl. fail-to-find = empty) | OK; library ObjectIds are hidden info but visible to driver |
| `SearchPartitionChoice` (:18020) | SelectCards | partition | OK |
| `OutsideGameChoice` (:18033) | ChooseOutsideGameCards | wish/sideboard | out of scope (no sideboard in tapes) |
| `ChooseFromZoneChoice` (:18047) | SelectCards | choose from zone | OK |
| `BeholdChoice` (:18070) | SelectCards | behold | OK |
| `EmpowerJaceChoice` (:18079) | SelectCards | card-specific | rare |
| `SpellCopyOrderChoice` (:18092) | SelectCards | copy order | order needed |
| `ChooseOneOfBranch` (:18099) | ChooseBranch | "choose one" at resolution | OK |
| `ConniveDiscard` (:18138) | SelectCards | connive discard | OK |
| `DiscardChoice` (:18147) | SelectCards | discard pick | OK |
| `HeroEventChoice` (:18174) | SelectCards / DecideOptionalCost | Universes Beyond specific | rare |
| `EffectZoneChoice` (:18212) | SelectCards | effect zone pick | OK |
| `DrawnThisTurnTopdeckChoice` (:18319) | SelectCards | put-back drawn cards | OK |
| `LearnChoice` (:18329) | LearnDecision | learn (lesson/rummage/skip) | sideboard lesson out of scope; rummage/skip OK |
| `ManifestDreadChoice` (:18335) | SelectCards | manifest dread pick | OK |
| `TriggerTargetSelection` (:18342) | SelectTargets / ChooseTarget | trigger targets (ETB etc.) | OK; multi-slot needs per-slot map (mtg-rl-sim still fail-closed there) |
| `BetweenGamesSideboard` (:18373) | SubmitSideboard | - | Bo3 only; out of scope |
| `BetweenGamesChoosePlayDraw` (:18393) | ChoosePlayDraw | - | Bo3 only; out of scope |
| `NamedChoice` (:18399) | ChooseOption{choice} | choose a color/type/name/number | OK; string vocab must match phase (e.g. creature type spelling) |
| `OpponentGuess` (:18441) | ChooseOption | opponent guess | rare |
| `SpellbookDraft` (:18463) | SubmitSpellbookDraft | - | Alchemy; out of scope |
| `DamageSourceChoice` (:18473) | ChooseDamageSource | prevention source | OK |
| `ModeChoice` (:18479) | SelectModes{indices} | modal spell modes | OK; mode index = phase parse order, map by mode text |
| `DiscardToHandSize` (:18489) | SelectCards | cleanup discard | OK |
| `OptionalCostChoice` (:18497) | DecideOptionalCost | kicker/buyback/etc. | OK |
| `ChooseGiftRecipient` (:18517) | ChooseGiftRecipient | gift recipient | 2p forced |
| `SpliceOffer` (:18530) | RespondToSpliceOffer | splice | rare |
| `DefilerPayment` (:18537) | DecideOptionalCost | pay life instead | OK |
| `OrderCostReductions` (:18568) | OrderCostReductions | order cost reducers | FLAG: phase-specific ordering; no gorge analogue likely |
| `CastOffer` (:18591) | DecideOptionalEffect / CastSpellAsMiracle / CastSpellAsMadness / CastParadigmCopy | cast-or-not offers (madness, miracle, cascade...) | OK if gorge records accept/decline |
| `ModalFaceChoice` (:18614) | ChooseModalFace | MDFC face | OK |
| `AlternativeCastChoice` (:18639) | ChooseAlternativeCast | alt cost choice | OK |
| `MutateMergeChoice` (:18683) | ChooseMutateMergeSide | mutate over/under | OK |
| `CipherEncodeChoice` (:18693) | CipherEncode | cipher | rare |
| `CastingVariantChoice` (:18700) | ChooseCastingVariant | casting variant (adventure, prototype ...) | OK |
| `ChoosePermanentTypeSlot` (:18712) | SelectModes | type slot | rare |
| `MultiTargetSelection` (:18728) | SelectCards | multi-target selection | OK |
| `AbilityModeChoice` (:18739) | SelectModes | modal activated/triggered | OK |
| `OptionalEffectChoice` (:18773) | DecideOptionalEffect{accept} / DecideOptionalEffectAndRemember | "you may" | OK |
| `ResolutionOptionalPaymentChoice` (:18795) | ChooseResolutionOptionalPaymentBranch | pay-or-not at resolution | OK |
| `PairChoice` (:18802) | ChoosePair | soulbond pair | OK |
| `TributeChoice` (:18813) | DecideOptionalEffect | tribute (opponent) | OK |
| `MiracleReveal` (:18825) | CastSpellAsMiracle / DecideOptionalEffect | miracle | OK |
| `OpponentMayChoice` (:18832) | DecideOptionalEffect | opponent "may" | OK |
| `LoopShortcut` (:18853) | DeclineShortcut / DeclareShortcut | none | UX: decline deterministically; fail if gorge had a loop |
| `RespondToShortcut` (:18934) | RespondToShortcut | none | UX |
| `PrecastCopyShortcutOffer` (:18943) | PrecastCopyShortcut | none | UX |
| `RespondToPrecastCopyShortcut` (:18951) | PrecastCopyShortcut | none | UX |
| `UnlessPayment` (:18962) | PayUnlessCost / ChooseUnlessCostBranch / TapLandForMana / UntapLandForMana | pay "unless" (Mana Leak etc.) | OK; plus manual mana taps |
| `UnlessPaymentChooseCost` (:18996) | ChooseUnlessCostBranch | which unless cost | OK |
| `WardDiscardChoice` (:19026) | SelectCards | ward discard | OK |
| `WardSacrificeChoice` (:19042) | SelectCards | ward sacrifice | OK |
| `UnlessBounceChoice` (:19058) | SelectCards | bounce for unless | OK |
| `ChooseRingBearer` (:19066) | ChooseRingBearer | ring-bearer | OK |
| `ChooseRoomDoor` (:19077) | ChooseRoomDoor | room door | OK |
| `ChooseDungeon` (:19088) | ChooseDungeon | dungeon | OK |
| `ChooseDungeonRoom` (:19094) | ChooseDungeonRoom | room | OK |
| `SpecializeColor` (:19102) | ChooseSpecializeColor | Alchemy specialize | out of scope |
| `PayCost` (:19113) | SelectCards | sacrifice/discard/exile/tap cost objects | OK |
| `ActivationCostOneOfChoice` (:19127) | ChooseActivationCostBranch | "or" cost branch | OK |
| `CostTypeChoice` (:19138) | ChooseOption | cost type | OK |
| `BlightChoice` (:19145) | SelectCards | blight | OK |
| `PayManaAbilityMana` (:19163) | PayManaAbilityMana | mana ability with mana cost (filter lands) | OK, manual |
| `ManaAbilityManaPayment` (:19172) | TapLandForMana / ActivateAbility | paying a mana ability's mana cost | OK, manual |
| `ChooseManaColor` (:19181) | ChooseManaColor | any-color mana choice | OK; gorge must record produced color |
| `CollectEvidenceChoice` (:19188) | SelectCards | collect evidence | OK |
| `HarmonizeTapChoice` (:19197) | HarmonizeTap | harmonize | OK |
| `RevealUntilKeptChoice` (:19209) | DecideOptionalEffect | reveal-until keep | OK |
| `RepeatDecision` (:19232) | DecideOptionalEffect | repeat process | OK |
| `TopOrBottomChoice` (:19239) | ChooseTopOrBottom | top/bottom | OK |
| `PopulateChoice` (:19249) | ChooseTarget | populate | OK |
| `ClashChooseOpponent` (:19259) | ChooseClashOpponent | - | 2p forced |
| `ChooseFromZoneOpponentChooser` (:19275) | ChooseZoneOpponentChooser | - | 2p forced |
| `ChooseAnnouncingOpponent` (:19294) | ChooseAnnouncingOpponent | - | 2p forced |
| `ClashCardPlacement` (:19308) | ChooseTopOrBottom | clash placement | OK |
| `PhysicalContest` (:19326) | (contest_click etc. via game::contest; wall-clock) | none | FLAG: real-time/physical (Un-cards); no scripted answer - refuse card |
| `VoteChoice` (:19337) | SubmitVoteCandidate | vote | OK |
| `SeparatePilesChooseOpponent` (:19433) | ChoosePileOpponent | - | 2p forced |
| `SeparatePilesPartition` (:19460) | SubmitPilePartition{pile_a} | pile split (Fact or Fiction) | OK |
| `SeparatePilesChoice` (:19495) | ChoosePile | pile pick | OK |
| `SecretAgendaChoice` (:19517) | ChooseSecretAgendaNames | - | Conspiracy; out of scope |
| `CompanionReveal` (:19523) | DeclareCompanion / CompanionToHand | - | companion out of scope |
| `ChooseLegend` (:19531) | ChooseLegend | legend rule keep | OK |
| `CommanderZoneChoice` (:19540) | DecideOptionalEffect | - | Commander; out of scope |
| `BattleProtectorChoice` (:19551) | ChooseBattleProtector | battle protector | OK |
| `ProliferateChoice` (:19558) | SelectTargets | proliferate set | OK |
| `TimeTravelChoice` (:19571) | SelectTargets | time travel | OK |
| `ChooseObjectsSelection` (:19596) | SelectTargets / ChooseTarget | generic object choice | OK |
| `CategoryChoice` (:19617) | SelectCategoryPermanents | one-of-each-type keep (Cataclysm-likes) | OK |
| `EachPlayerCopyChosenSelection` (:19657) | SelectTargets | per-player chosen copies | rare |
| `KeepWithinTotalPowerChoice` (:19694) | ChooseKeptCreatures | keep within power | rare |
| `KeepExactPermanentsChoice` (:19722) | ChooseKeptPermanents | keep N permanents | OK |
| `CopyRetarget` (:19756) | ChooseTarget / KeepAllCopyTargets | new targets for copies | OK |
| `AssignCombatDamage` (:19810) | AssignCombatDamage | attacker damage assignment (multi-block/trample) | NEEDS gorge assignment; Forge/gorge often auto-assign - compare, do not invent |
| `AssignBlockerDamage` (:19837) | AssignBlockerDamage | blocker damage split (banding-like) | rare |
| `DistributeAmong` (:19850) | DistributeAmong | divide damage/counters | OK |
| `MoveCountersDistribution` (:19863) | ChooseCounterMoveDistribution | move counters | OK |
| `RemoveCountersChoice` (:19879) | ChooseCountersToRemove | which counters | OK |
| `PayAmountChoice` (:19899) | SubmitPayAmount | pay N life/energy | OK |
| `RetargetChoice` (:19911) | RetargetSpell | redirect | OK |
| `CombatTaxPayment` (:19980) | PayCombatTax | Propaganda-style tax | OK + manual mana |
| `PhyrexianPayment` (:19998) | SubmitPhyrexianChoices | phyrexian mana life-vs-mana | OK; gorge must record per-pip choice |

Notes on the table:
- `SelectCards` is the generic answer for ~40 prompts; its meaning is per-prompt (e.g. Scry/Surveil:
  *ordered keep-on-top set*, rest go bottom/graveyard, `engine_resolution_choices.rs:2296-2323, 5340-5360`).
  **Scry's bottom order is not choosable** — bottoms are pushed in looked-at order (`:2303-2322`);
  gorge must use the same convention or the follower must compare bottoms as a multiset.
- `PassPriority` is overloaded: at `ManaPayment` it means "finalize payment" (`engine.rs:1040-1057`
  comment on `verified_ai_stack_pass_player`). The adapter must key on the current `WaitingFor`.
- `CancelCast` is a UI back-out; exclude it from matching (rlsim DECISIONS 2026-11-10).

### Mana payment model
- `CastSpell{object_id, card_id, targets, payment_mode}` and `ActivateAbility{..., payment_mode}`
  (`actions.rs:173-195`); `CastPaymentMode = Auto (default) | AutoExceptSacrificialMana | Manual`
  (`game_state.rs:8540-8548`). `Auto` runs phase's planner and never stops at `ManaPayment`;
  `Manual` stops in `WaitingFor::ManaPayment` after targets so the player taps sources:
  `TapLandForMana{selection}` / `ActivateManaSource{selection: ManaSourceSelection}`
  (`actions.rs:288-296`; `ManaSourceSelection{source: ObjectIncarnationRef, ability_index, mana_type,
  output, ...}`, `types/mana.rs:164-173`), `SpendPoolMana`/`UnspendPoolMana`/`UntapLandForMana`, then
  `PassPriority` to finalize. Floating mana before casting = `ActivateManaSource` at Priority (mana
  actions are in `legal_actions_full`'s per-object map, not the flat list).
- For gorge tapes that record exact taps: use `Manual` and translate each tap (source ref + color) to
  the unique matching mana candidate for that object; fail on 0 or >1 matches. For scenario steps that
  don't specify payment: either `Auto` and compare tapped-state afterwards (what rlsim does, building an
  XMage mana plan from `GameEvent::ManaAdded` in the result, rlsim:`phase-env/src/main.rs` `applied_message`),
  or treat "unspecified payment" as gorge-must-specify. Recommend: Manual when the step carries a
  payment, Auto otherwise, and record which in the row.

### Targeting refs / ObjectId stability
- `TargetRef = Object(ObjectId) | Player(PlayerId)` (`crates/phase-ast/src/types/ability.rs:43219-43222`).
- **ObjectIds are storage identity, reused across zone changes**; a new object (CR 400.7) is
  distinguished by `GameObject.incarnation` (`game/game_object.rs:1085-1097`, `bump_incarnation` `:3537`).
  So a card that dies and is reanimated keeps its ObjectId. Mapping gorge objects → phase objects
  must be on `(ObjectId, incarnation)`; rlsim learned this the hard way (DECISIONS 2026-11-10:
  "bijective between (Phase ObjectId, incarnation) and XMage short IDs").
- Setup creation order is deterministic (`next_object_id` increments, `zones.rs:798-810`), so a
  gorge setup ref (seat, zone, index/name) can be bound to the ObjectId returned at creation; objects
  created later (tokens, copies) need binding by (name, controller, zone, creation order in the
  step's events).

### Mulligan
- `WaitingFor::MulliganDecision{pending, free_first_mulligan, declared}` (`game_state.rs:17260-17276`);
  answer `MulliganDecision{choice: Keep|Mulligan|UseSerumPowder|FreeReveal}` (`actions.rs:73-86,
  ~286`), then bottom selection via `SelectCards` in the `MulliganDecisionPhase` bottom step
  (`game/mulligan.rs:199, 798`). Decisions are simultaneous (any order).
- rlsim always Keeps (rlsim:`phase-env/src/main.rs` `keep_opening_hands`) — it never supported mulligans.

### Shuffle / RNG hooks — forcing library order
- **There is no injection hook for shuffle results** on the live `apply` path. The resolved-shuffle
  journal (`library.rs:16-60`, `apply_resolved_library_shuffle` `:62+`) *validates* a recorded
  permutation against `rng_word_pos`, it does not let you supply one.
- Workable approach (what rlsim does at game start, `order_opening_zones`): because
  `Player.library`/`hand` are pub, overwrite them directly after any shuffle:
  1. game start: build libraries (deck load or debug create), `start_game_with_starting_player`, then at
     the first `MulliganDecision` rewrite hand+library from the tape's opening order
     (rlsim maps by name, multiset-safe; `main.rs` `order_opening_zones`).
  2. after each `Mulligan` (shuffle_library_of, no event) and after every result whose events contain
     `PlayerPerformedAction{ShuffledLibrary}` for player p: replace `players[p].library` with the
     tape's post-shuffle order (same multiset of ObjectIds; fail loudly if it isn't).
  This requires the **gorge tape to carry each post-shuffle library order** (or at least the order
  of the cards that will be drawn/revealed). If gorge only records the initial order, any shuffle
  makes the rest of the game unreplayable except by name-level comparison.
  Caveat: direct writes desync `resolved_rules_journal`/`rng_word_pos` bookkeeping; nothing on the
  normal path re-validates it, but replay/takeback features would. Keep `rng` untouched otherwise.
- Coin flips / die rolls (`effects/flip_coin.rs`, `effects/roll_die.rs`, `RandomDraw::Outcome`) and
  random discard / random-order effects draw from the same rng — **cannot be scripted**; either refuse
  cards with random outcomes or accept divergence. (Possible patch: an override queue consulted by
  `GameRng::draw`; small, but a local patch.)

### Turn structure / priority granularity
- Every step that grants priority surfaces `WaitingFor::Priority` (upkeep and draw always, per CR 117.3a,
  `game/turns.rs:4103-4119`); auto-passing is a client concern (`run_auto_pass_loop`, `engine.rs:11211`;
  `SetAutoPass`/`SetPhaseStops` actions). The follower must explicitly `PassPriority` for each player
  at each window the script doesn't act in; a gorge "pass"/"resolve" step expands to a
  pass-until-predicate loop (stack empty / step reached), but **only through Priority prompts** —
  any other `WaitingFor` encountered mid-loop must be answered from the script or fail.
- `PreCombatMain → BeginCombat` has its own priority window before declare attackers (CR 507.2;
  `scenario.rs:1615-1620` comment). Declaring no attackers = `DeclareAttackers{attacks: []}` at the
  `DeclareAttackers` prompt.
- Stack resolution: both players pass → top resolves; triggers go on the stack at next priority (with
  `OrderTriggers` / `TriggerTargetSelection` prompts first).
- `AppliedStackPass` / `ResolveAll*` are phase's batch-resolution UX; never send `BeginResolveAll`.

## 4. Observation (snapshot)

Read straight off `GameState` after `apply` (layers are finalized at the action boundary; after direct
mutation call `layers::flush_layers`):
- Players: `state.players[i]` → `life`, `poison_counters`, `energy`, `hand`, `library`, `graveyard`,
  `mana_pool`, `lands_played_this_turn` (`types/player.rs:78-136`).
- Zones: `state.battlefield`, `state.exile`, `state.command_zone`, `state.stack: im::Vector<StackEntry>`
  (`game_state.rs:~23864-23874`); objects in `state.objects: ObjectMap` (`:~23763`).
- Per object (`game/game_object.rs:705+`): `name` (current face), `owner`, `controller`, `zone`,
  `tapped`, `face_down`, `transformed`, `flipped`, `damage_marked`, `attached_to`, `attachments`,
  `counters` (map CounterType→u32), `power/toughness` (**post-layer**; `base_power/base_toughness` are
  printed, `layer_base_*` are after 7a/7b, `:825-838, 1000-1001`), `loyalty`, `defense`, `keywords`
  (current), `card_types`, `color`, `is_token` (`:1437`), `incarnation`, `summoning_sick` /
  derived `has_summoning_sickness` (`:1121, 1318`; derived fields are computed by
  `derive_display_state`, `game/derived.rs:28`, which runs in `finalize_display_state` on the
  `Immediate` apply path), `phase_status` (phased out, `:1807`), `chosen_attributes`, `class_level`.
- Stack: `StackEntry{id, source_id, controller, kind: Spell|ActivatedAbility|TriggeredAbility|
  KeywordAction|CombatDamage}` (`game_state.rs:22273-22277, 22396+`; `CombatDamage` is type-only, never
  constructed in production). For a spell, name = `objects[source_id].name`; for abilities, use the
  source name + ability description.
- Combat: `state.combat: Option<CombatState>` (attackers/blockers).
- Ready-made projections exist but are too thin: `GameRunner::snapshot()` → `GameSnapshot{players{life,
  hand, graveyard}, battlefield{name, owner, power, toughness, tapped, damage, keywords}, stack, events}`
  (`scenario.rs:2283, 4497-4525`), rlsim's `snapshot()` (names only). Write our own projection
  matching forge:`SnapshotWriter.java` keys (`life, hand, graveyard, exile, library_count/top,
  permanents{name, p/t, counters, tapped, attached, token, face_down, controller}, stack, step, turn,
  priority`).
- Token naming: phase names tokens per CR 111.4 by the creating effect — "Goblin", "Spirit",
  "Treasure", "Monster Role", "Phyrexian Token" for one special case (`game/effects/token.rs:4756,
  5185`; tests `tests/integration/token_art_descriptor.rs:50-101`). Forge uses "Goblin Token"-style
  names. Normalize to gorge's convention in the follower (strip/add " Token"), keyed on `is_token`.

## 5. Support signal

- Per-face API (engine lib, no feature needed): `game::coverage::card_face_gaps(face) -> Vec<String>`
  (empty = supported; `game/coverage.rs:7723`), `card_face_has_unimplemented_parts(face)` (`:7650`),
  `analyze_coverage(&db) -> CoverageSummary` (`:7185`). Plus the committed misparse overlay:
  `face.metadata.confirmed_misparse: Option<..>` is baked into card-data (3,006 faces carry it today)
  and `phase_ast::types::confirmed_misparse::ConfirmedMisparses::committed()`
  (`crates/phase-ast/src/types/confirmed_misparse.rs:97`); `coverage-report` cross-checks both
  (`bin/coverage_report.rs:312-330`). Also `GameObject.unimplemented_mechanics` (`game_object.rs:1313`,
  `coverage.rs:6799`).
- `coverage-report` bin (`crates/engine/src/bin/coverage_report.rs`, `cargo coverage` alias) writes the
  published coverage-data.json: per card `{card_name, supported, gap_count, gap_details[{handler,
  source_text}], parse_details, printings, oracle_text}`; `supported` already ANDs in confirmed misparses
  (FDN examples below show `Misparse:*` handlers). `parser_gap_analyzer.rs` / `gap_analysis` aggregate
  top gaps / bundles; `semantic-audit.json` is a separate published report.
- **Driver refusal rule:** before setup, for every card name in the request: `db.get_face_by_name`
  must resolve (note `resolve_deck_list` **silently skips** unknown names, `deck_loading.rs:595-604` —
  never rely on it), and for each face (incl. back face) `card_face_gaps(face).is_empty() &&
  face.metadata.confirmed_misparse.is_none()`; else emit `unsupported_card{name, gaps}` without running.
  Optionally also refuse cards whose abilities use rng outcomes (coin/die/random discard) or
  `PhysicalContest`.
- **Numbers** (computed from `https://data.phase-rs.dev/coverage-data.json` + the hashed card-data,
  both `card_data_hash = d0ffa159dcde97c7`, engine commit b14f974, MTGJSON 5.3.0+20261110; scripts were
  inline python joining card-data `printings`/`legalities` with coverage `supported`):
  - Whole DB: 29,342 / 36,280 faces supported (80.9%).
  - **FDN (Foundations, cards with "FDN" in printings): 494 / 517 supported (95.6%)** — matches
    `coverage_by_set.FDN`. Unsupported 23: Abyssal Harvester, Cat Collector, Chandra Flameshaper,
    Curator of Destinies, Darksteel Colossus, Diamond Mare, Drakuseth Maw of Flames, Dryad Militant,
    Firespitter Whelp, Gate Colossus, Genesis Wave, Grappling Kraken, Hidetsugu's Second Rite,
    Joraga Invocation, Loot Exuberant Explorer, Mindsparker, Nine-Lives Familiar
    (`Effect:enters_with_gate`), Ordeal of Nylea, Quilled Greatwurm, Steel Hellkite, Strongbox Raider,
    Tinybones Bauble Burglar (`Effect:unknown`), Valkyrie's Call. 21 of 23 are `Misparse:*`
    (dropped_qualifier, wrong_object, missing_effect, wrong_zone, condition_wrong, target_wrong,
    trigger_wrong, wrong_player) — i.e. they *parse* but are known-wrong, so the misparse check is
    essential.
  - **FDN ∩ Pauper-legal (the FDN commons): 196 / 196 supported (100%).**
  - **Pauper-legal pool (MTGJSON `legalities.pauper == "legal"`): 10,405 / 11,123 (93.5%)** — my join
    reproduces `coverage_by_format.pauper` exactly. Standard: 5,129 / 5,725 (89.6%).
  - Caveat: "supported" = parser+handler coverage and no *known* misparse; it is not a correctness
    claim. rlsim's Standard work found cards marked supported that still needed fixes.

## 6. Hurdles and risks

- **Churn:** release v0.108 → v0.122 between 2026-10-12 and 2026-11-10 (15 releases in 29 days);
  ~548 commits on main since 2026-10-10, ~118 since 2026-11-03 (gh api). `rlsim` pin 988bc34 (v0.120.0)
  is 31 commits behind HEAD three days later; in that window `types/game_state.rs` +197/-15 and
  `ai_support/mod.rs` +58/-4 changed. No semver API promise (`publish=false`, `Cargo.toml:41`); the
  `GameAction`/`WaitingFor` enums grow constantly. Pin a commit + card-data hash; bump deliberately
  (monthly), with the driver compiled against exhaustive `match` so new variants are compile errors.
- **Engine is huge and slow to build;** debug-profile games ~14x slower than optimized (`.cargo/config.toml`
  ai-gate comment). Many `state.clone()`s per action (rollback snapshots; e.g. `engine_debug.rs:1406`,
  `engine.rs:21025, 21093`), mitigated by `im` persistent structures. No measured ms/action available
  offline; rlsim runs random-policy games end-to-end and 44–259-step differential games routinely.
  Budget: expect it fine for scenarios; for whole-game batches measure first.
- **Semantics/naming:** token names (CR 111.4 subtype names, no " Token" suffix); multi-face cards are
  per-face in the DB and `object.name` is the *current* face (a transformed DFC shows its back-face name) — normalize to gorge's convention; Oracle text comes from MTGJSON current Oracle
  (errata'd) while Forge scripts can lag errata — expect "drift" diffs on recently errata'd cards;
  phase's parser is Oracle-text-driven so templating changes in MTGJSON can flip support between data
  vintages. Unicode/accents in names (rlsim normalizes NFKD).
- **Rules-fidelity stance differs:** phase surfaces choices Forge/gorge auto-resolve (e.g.
  `OrderCostReductions`, every upkeep/draw priority window, separate `BeginCombat` window, manual
  payment staging). The adapter must absorb "engine-only" prompts that have a unique legal answer,
  but must never guess a multi-candidate one.
- **Prompts needing gorge data that it may not log:** trigger order, replacement order, library order
  after shuffles, combat damage assignment order, scry bottom order, per-pip Phyrexian choices,
  any-color mana choices.
- **Unscriptable randomness:** coin flips, die rolls, random discards, starting-player d20 (avoid with
  `start_game_with_starting_player`).
- **Multiplayer assumptions:** none blocking — engine is N-player (`GameState::new(format, n, seed)`,
  `scenario.rs:196-210`), `DeckList` has `player`/`opponent` + `ai_decks` (`deck_loading.rs:153, 262`).
  2-player is the default (`new_two_player`); "choose an opponent" prompts are forced in 2p.
- **Identity:** ObjectId reuse + incarnation (above). Library ObjectIds are visible to the driver (no
  hidden-info filtering unless we call `visibility::filter_state_for_viewer`).
- **Patches:** rlsim carries 30 local parser/engine patches (`phase-patches/0001..0030`, card fixes
  for Standard meta: Restless Prairie, Rottenmouth Viper, Lost in the Maze, Seasoned Cryomancer,
  Essence Burn, …) on 988bc34; *no* driver-infrastructure patches — `phase-env` needs nothing beyond
  the public API. Their upstream offer (phase-rs#10736) is open with 0 comments; phase's AI-contributor
  policy (`docs/AI-CONTRIBUTOR.md`) rules out drive-by PRs. We should not carry card patches; a
  follower's value is reporting phase-as-is.
- **License:** MIT OR Apache-2.0 (`Cargo.toml:16`) — compatible; card data from MTGJSON (MIT).
- **Stack overflow risk** on deep paths if not run on a big-stack thread (above).

## 7. Prior art: kevinbrar/mtg-rl-sim

- `phase-env/src/main.rs` (≈500 lines): JSONL loop: `new_game{seed, player.main_deck, opponent.main_deck,
  ordered_library}` → `CardDatabase::from_export`, `resolve_deck_list` + `load_and_hydrate_decks`,
  `start_game_with_starting_player(P0)`, optional `order_opening_zones` (rewrite hand/library by deck
  list order, reporting unresolved names), auto-Keep both hands, then per decision emit
  `{waiting_for (serde), owner, actor, candidates (serde GameAction), objects (metadata for referenced
  ids incl. incarnation, tapped, P/T, counters, attached_to, token, face_down), state}` and accept
  `{"index":k}` or a verbatim `{"action":{...}}` re-validated by `apply_ai_action_proposal`;
  `applied` replies include `ManaAdded` events (for mana-plan mapping).
- Solved: build/pin recipe (separate workspace, nightly pin, card-data hash pin + sha256 in
  `ci/card-data.txt`), engine-issued candidate contract, incarnation-aware identity, name normalization,
  scry polarity, CancelCast exclusion, mana plan from ManaAdded events, single-slot trigger targets.
- Not solved / still fail-closed: mulligans (always keep), shuffles after start, multi-slot trigger
  targets, `PayUnlessCost`, blocks were v0, scenario (board-setup) mode not implemented — they planned
  to use `game/scenario.rs` (rlsim:`docs/harness-design.md`). Their direction is phase-as-leader
  (phase chooses, XMage follows); ours is phase-as-follower, so their adapter logic is mostly inverted.

## 8. Proposed driver design (`phase-follower`)

### Layout
```
gorge-compliance/followers/phase/            (own cargo workspace; rust-toolchain.toml = nightly-2026-04-19)
  Cargo.toml          [workspace] members=["phase-follower"]; [profile.follower] = server-release-like, panic=unwind
  PHASE_PIN           "<engine sha> <card-data hash16> <sha256>"
  scripts/fetch.sh    clone phase at pin (shallow, detached), fetch card-data-<hash>.json, verify sha
  phase-follower/
    Cargo.toml        engine = { path = "../vendor/phase/crates/engine", package = "phase-engine" }  (no features)
    src/main.rs       arg parsing, big-stack worker thread, JSONL loop, catch_unwind per row
    src/request.rs    serde types mirroring forge:Request/GameRequest (scenario item + gorge_decisions; game tape)
    src/support.rs    up-front refusal (name resolve, card_face_gaps, confirmed_misparse, rng/contest denylist)
    src/setup.rs      SetupBuilder: debug_mode on → create_debug_cards(run_etb:false) per seat/zone in
                      gorge order, bind refs → (ObjectId, incarnation); Attach/SetTapped/ModifyCounters/
                      SetSummoningSickness/SetLife/CreateToken; SetPhase; debug_mode off; flush_layers
    src/refs.rs       RefTable: gorge ref ⇄ (ObjectId, incarnation); rebinding on zone change via events
    src/answer.rs     Answerer: (WaitingFor, script cursor) → GameAction; exhaustive match on WaitingFor
    src/steps.rs      StepMachine: scenario steps (cast/activate/play/attack/block/pass/resolve) →
                      sequences of apply() calls with pass-until loops
    src/tape.rs       GameReplay: deck load, starting player, opening-order override, mulligans,
                      post-shuffle library override, decision cursor
    src/snapshot.rs   SnapshotWriter: forge-compatible keys, normalized names/tokens, sorted
    src/error.rs      error taxonomy
```

### I/O (JSONL, one request per line, one result row per line — same row shape as the Forge follower)
- In (scenario): `{"id","scenario_sha","item":{setup per seat per zone, life, tapped, counters,
  attached, tokens, phase/active, steps[...]}, "gorge_decisions":[...], "abilities":{...}}`
  (forge:`Request.java:9-16`).
- In (tape): `{"id","seats":[{"cards":[...] , "library_order":[...]}], "starting_player",
  "decisions":[...], "shuffles":[{"player","after_decision","order":[...]}], "seed"}`.
- Out: `{"name","id","engine":"phase","phase_ref":"<sha>+<hash16>","harness":null|msg,"strict":true,
  "strict_miss":null|msg,"leftover":..,"ms":..,"snapshots":[{checkpoint, players, permanents, stack,
  step, turn, priority}],"notes":[...]}` (forge:`ResultRow.java:12-30`).

### Main loop (per request)
1. support check → maybe `unsupported_card` row.
2. build state (setup.rs or tape.rs), snapshot "setup".
3. for each step / decision: loop { w = state.waiting_for; if GameOver → done;
   action = answerer.answer(&state, &w, &mut cursor)?; check action ∈ candidates
   (`AiDecisionContract::issue`) except combat declarations; r = apply(state, actor, action)
   (in catch_unwind); if r.disposition reversed → script miss; refs.observe(&r.events);
   if shuffle event / mulligan → library override from tape; if checkpoint → snapshot }.
4. leftover script entries → `leftover`.

### Answerer contract
- One `match` arm per `WaitingFor` variant (exhaustive, no `_` arm), returning
  `Answer::Script(action) | Answer::Forced(action) /*single candidate, logged*/ | Err(ScriptMiss)`.
- Translation: gorge object refs via RefTable; ability index via the `abilities` map (match phase
  `obj.abilities[i].description` to gorge's line text, normalizing `{this}`↔`~` as rlsim does);
  modes by mode text; mana taps by (source ref, color) against `legal_actions_full` per-object mana
  actions.
- Strict mode = never Forced for multi-candidate; loose mode (retry) may accept Forced for
  "2p forced"/UX prompts only, recording `strict_miss`.

### Error taxonomy
- `unsupported_card{name, gaps|misparse|unresolved}` — refused before running.
- `script_miss{step, waiting_for, candidates_summary}` — engine asked something the script doesn't
  answer, or the scripted answer isn't among candidates / is rejected / reversed ("Missing … answer at
  step N", same wording as forge:`StrictMiss.java`).
- `harness{msg}` — bad ref, bad request, setup refused (e.g. Aura without host), library-override
  multiset mismatch, decision cap.
- `engine_panic{msg, backtrace}` — caught via `catch_unwind` (requires `panic=unwind`).
- `engine_error{EngineError}` — `apply` returned Err for an in-candidate action (phase bug signal).
- `divergence` is *not* the follower's call: it emits snapshots; gorge's comparator decides.

### Phases and estimated effort (Rust LOC, excluding tests)
1. **Setup-only** (support check, setup.rs, refs.rs, snapshot.rs, I/O): ~900 LOC, 3–4 days incl.
   pin/fetch scripts and first build. Exit criterion: setup snapshots for FDN scenarios equal gorge's
   setup snapshots (validates names, P/T after layers, counters, attachments, tokens).
2. **Cast-resolve scenarios** (steps.rs + answerer for Priority/TargetSelection/ManaPayment/
   ModeChoice/X/optional costs/OptionalEffect/TriggerTargetSelection/OrderTriggers/Search/Dig/Scry/
   Surveil/Discard/PayCost/DeclareAttackers/Blockers): +1,300 LOC, ~1–1.5 weeks.
3. **Whole-game FDN tapes** (tape.rs, mulligans, shuffle override, remaining WaitingFor arms, combat
   damage assignment, perf pass): +900 LOC, ~1–1.5 weeks; prerequisite: gorge tapes include
   post-shuffle orders, trigger/replacement order, mana taps, damage assignment.
Total ≈3,000–3,500 LOC Rust (Forge follower is ~8,000 Java incl. bridge code we don't need).
Start with FDN commons (196/196 supported) as the first corpus.
