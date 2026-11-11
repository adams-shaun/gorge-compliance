<!-- Audit notes written 2026-11-10 for the phase.rs / Manabrew follower design. Read-only static audit; local paths (/tmp/..., /mnt/sata/...) are from the audit machine and abbreviations are defined at the top. Historical, like every design note. -->

# Manabrew as a gorge-compliance follower: audit notes

Audited read-only and statically (no Rust toolchain on this box).

- Clone: `/tmp/mb-probe/manabrew` @ `dadee73` ("chore(release): 1 package(s)", 2026-11-10). All paths below are relative to that clone unless prefixed.
- Engine paths: `E = manabrew-rs/crates/manabrew-engine/src`, `AI = manabrew-rs/crates/manabrew-agent-interface/src`, `P = manabrew-rs/crates/parity/src`.
- Forge template: `forge-oracle/src/main/java/forge/oracle/*` in the scratchpad Forge clone, abbreviated `FO/`.
- gorge-pin Forge res, a full working tree at exactly `84ce1603d9`: `/mnt/sata/gorge-training/xengine/forge-trek6/forge-gui/res`. That checkout is blobless too, but its `res/` tree is checked out and clean, so the scanner reads it directly.
- Scanner: `scratchpad/mb_scan.py`. Its outputs are `scratchpad/mb_scan_{fdn,commons,all}.json`, plus `scratchpad/rust_trigger_modes.txt`.

---

## 0. TL;DR

- **Feasible, but it has to live in a fork.** The engine is a synchronous Forge-shaped port of about 157k LOC. The engine owns control flow and calls a `PlayerAgent` trait object for every decision; that trait has 83 methods. The parity runner already shows how to build a game headlessly: archive, then `GameState`, then direct library placement, then `run_turn`. It bypasses `GameLoop::setup` and the mulligan.
- **Three fork patches are unavoidable:**
  - (a) a mid-priority state-edit hook. Forge's StepMachine adds mana and taps "after untap" from inside the priority callback. The Rust agent only ever gets `&GameState`.
  - (b) a public raw-placement API. `zones` is private, and `move_card` runs entry replacements.
  - (c) optionally, a strict-mode switch for the silent fallbacks.
- **Biggest risk is silent improvisation.**
  - 74 of the 83 trait methods have defaults that silently pick first/min/false/true.
  - An illegal priority action is downgraded to a pass, with only a warning notification.
  - An unknown `SP$`/`AB$`/`DB$` name becomes `api=None`, which is a no-op. An unknown trigger `Mode$` drops the trigger. An unknown static `Mode$` or replacement `Event$` becomes `Other(..)` and is inert. `TriggerCondition::Unsupported` evaluates to *true*. A `Count$` expression the engine doesn't recognise evaluates to 0.
  - The driver must override every method and treat these channels as errors.
- **Static support estimate:**
  - FDN (517 cards incl. precon product): about 93% (479) carry no static red flag. 37 cards use a param key that appears nowhere in the Rust source, and 1 has a deck-rule keyword.
  - Commons pool (10,373 scripts): about 94.6% (9,809) unflagged, 9,679 of them single-face.
  - All of cardsfolder (34,442): about 87.5%.
  - These are upper bounds. Presence of a key in Rust does not mean it is correct.
- **Drift is real and asymmetric.**
  - The Rust port lags even its *own* Forge pin. `ApiType::FlipCoin` was renamed from `FlipACoin` in Forge on 2026-04-02 (3dd90182a1). Rust still only knows `FlipACoin` (`E/ability/api_type.rs:103,344`), so all 84 FlipCoin cards silently do nothing. `Empower`, `Recruit` and `HealDamage` are missing too.
  - gorge's pin adds `PutSticker` and `Assimilate`, plus 469 new and 495 modified card scripts relative to the merge base.
  - Engine development has slowed: only about 4 non-release engine commits in the last 30 days, so staleness will keep growing.

---

## 1. Build

### 1.1 Toolchain

- **Toolchain version.** There is no `rust-toolchain(.toml)`. CI uses `dtolnay/rust-toolchain@stable` (`.github/workflows/build-checks.yml:412`), and every crate is edition 2021. Any current stable works, and the box has none: provisioning it is the harness owner's job, and I did not install one.
- **CI's engine job** (`build-checks.yml:400-431`):
  - Build: `cargo build --release -p forge-foundation -p forge-carddb -p manabrew-engine -p manabrew-agent-interface`
  - Test: `cargo test --release -pmanabrew-engine --lib`
  - Linux CI links with mold. That is optional for us.
- **Release profile.** `Cargo.toml` sets `[profile.release] opt-level="z", lto=true`, which is size-optimised. For a CPU-bound oracle, add a profile with `opt-level=3` (wasm already overrides this). `[profile.parity]` inherits release plus `debug-assertions`.

### 1.2 Crates a headless driver needs

- `forge-foundation` 0.7.1: core types (ZoneType, PhaseType, ManaCost, CardTypeLine).
- `forge-card-script` 0.1.3: script IR and parser (winnow).
- `forge-cardset-archive` 0.4.4: the rkyv archive format. With feature `build` it provides `build_archive_from_sources` and the `build-cardset-archive` bin.
- `forge-carddb` 0.4.0: `CardDatabase`, which loads from the archive only.
- `manabrew-engine` 0.13.3: everything else. Its dependencies are rand 0.8, indexmap, strum, typetag, serde.
- `manabrew-agent-interface` 0.19.4 is **optional**. It is useful for `game_view_dto::card_to_dto` (`AI/game_view_dto.rs:569`), which serialises a card with its post-layer P/T, counters and visibility. It is also the reference for `PromptAgent`. It pulls in `manabrew-protocol` and `manabrew-relay-protocol` (ts-rs), which are heavy but pure Rust.
- `parity` should not be a dependency (deprecated). Copy its patterns instead.
- Dependency direction is documented in `manabrew-rs/README.md:45-52`.

### 1.3 How cards load

- **Archive only.** `CardDatabase` has no directory loader. `CardDatabase::load_from_archive(&[u8]) -> ArchiveBundle` (`manabrew-rs/crates/forge-carddb/src/database.rs:293-345`) leaks a 16-aligned copy and validates it with `forge_cardset_archive::load_checked`.
- **The archive stores raw script text.** `Card { name_lower, raw }`, `forge-cardset-archive/src/lib.rs:26-31`. Scripts are parsed lazily on first lookup (`database.rs:144-167`, `lazy_parse_by_lower`). `force_parse_all` (`database.rs:206`) parses eagerly and returns a `LoadResult` with a failed count. A driver should call it at startup and log failures.
- **Building an archive from any cardsfolder.** `build-cardset-archive [cardsfolder] [tokenscripts] [editions] [blockdata] [TypeLists.txt] [out.rkyv]` (`forge-cardset-archive/src/bin/build.rs:6-63`, `build.rs:27-128`).
  - It walks `*.txt` and keys on the first `Name:` line (`build.rs:139-176,208-210`), so file renames don't matter.
  - It dedups by lowercase name.
  - A missing `TypeLists.txt` is a hard error (`build.rs:103-108`).
  - It also embeds `res/{formats,lists,defaults,effects,ai}` as `extras` (`lib.rs:22`).
- **So yes, it can point at gorge's 84ce1603 res.** Build once per Forge pin, then mmap the result (`P/runner.rs:908-920`; the env var `CARDSET_ARCHIVE` overrides the path).
- **In-memory archives for tests.** `build_test_archive(&[(&str,&str)])` (`lib.rs:105`) builds a synthetic archive.
- **Globals that must be loaded once per process:**
  - `CardDatabaseRegistry::load(Arc<CardDatabase>)` (`E/game.rs:97-110`)
  - `TypeRegistry::load(archive.type_lists)` (`E/game.rs:29-62`). Without it the engine panics on changeling or ChooseType cards.
- **Token templates.** Build them from the archive's token DB with `CardInstance::from_rules`, then `GameLoop::register_token(stem, template)`. Example: `P/runner.rs:993-997,1127-1129`. The token art-variant maps exist only for RNG parity (`P/runner.rs:1131-1136`), so we can skip them.

### 1.4 Parser tolerance

- **Card-level parser** (`forge-carddb/src/parser.rs:72-293`):
  - It matches on the key's first byte. Unknown keys (`AI:`, `DeckHints`, `SETCOLORID`, …) are ignored (`parser.rs:289-291`).
  - A, T, S, R, K and SVar are stored as raw strings.
  - It never fails on unknown content. `build()` errors only on structural problems.
- **Ability params.** `Key$ Value | …` is parsed into a generic map (`Params`/`ParsedParams`). Unknown keys are kept and never read, so they are *silently ignored*. `ParsedParams::parse_with_diagnostics` reports only syntax issues: missing delimiter, empty key, duplicate key (`P/card_pool.rs:1062-1101`).
- **Unknown values degrade silently:**
  - **API names.** `ApiType::smart_value_of` returns `None` (`E/ability/api_type.rs:230-236`). The SA is built with `api=None` (`E/ability/ability_factory.rs:105-109,464-466`). At resolution, `api=None` is a deliberate no-op (`E/ability/effects/effect_resolver.rs:41-48`). A known-but-unhandled API logs `eprintln!("Unimplemented effect API type")` (`effect_resolver.rs:50-56`).
  - **Trigger modes.** `parse_trigger` does an exact, case-sensitive `match` on `Mode$`. Unknown modes `return None` (`E/trigger/trigger.rs:1076-1303`), so the trigger vanishes.
  - **Static modes.** These become `StaticMode::Other(String)` (`E/staticability/static_ability_mode.rs:9-15`) and are inert.
  - **Replacement events.** These become `ReplacementType::Other(String)` (`E/replacement/replacement_type.rs:60-67`).
  - **Trigger conditions.** `TriggerCondition::Unsupported(_) => true` (`E/trigger/trigger.rs:818`). The trigger fires as if the condition held.
  - **Count expressions.** An unrecognised `Count$` logs "Unrecognized Count expression, returning 0" (`E/svar/mod.rs:2016`).
  - **Required params.** A missing one logs `[parse] missing required param` (`E/parsing/mod.rs:782`).

---

## 2. Setup: building a GameState and placing cards without ETB

### 2.1 Primitives

- `GameState::new(&["p0","p1"], 20)` (`E/game.rs:236-281`) supports N players. Turn state starts at turn 1, Untap, with player 0 active (`E/phase/mod.rs:145-176`).
- `game.create_card(CardInstance::from_rules(rules, owner)) -> CardId`. It allocates a sequential arena id and places nothing (`E/game.rs:283-…`). `from_rules` is at `E/card/mod.rs:1044` and `E/card/card_factory.rs:24`.
- `game.move_card(id, zone, owner)` (`E/action.rs:33`) calls `move_card_internal(.., agents=None, trigger_handler=None, apply_move_replacement=true, ..)` (`E/action.rs:155-…`).
  - With no trigger handler, **no triggers are queued**. ETB, ChangesZone and CounterAdded all stay silent, because triggers only flow through the `TriggerHandler` owned by `GameLoop`.
  - But it **does** apply Moved replacements (enters tapped, etbCounter, Sagas' lore, sunburst; `action.rs:201-280,289-305`) and **adds loyalty counters** (`action.rs:255-270`).
  - Forge's SetupBuilder explicitly does *not* apply entry replacements (`FO/SetupBuilder.java:49-52`).
  - So `move_card` is fine for hand, library, graveyard and exile. It is wrong for the battlefield if the scenario restates tapped and counters.
- **Raw placement must be done by hand, and is not fully possible from outside the crate.**
  - `zones` is private (`E/game.rs:134-135`), and `add_card_to_zone` is `pub(crate)` (`E/game.rs:467`).
  - The public escape hatch is `replace_zone_cards(zone, owner, Vec<CardId>)` (`E/game.rs:545`), wrapping `ZoneStore::replace_cards` (`E/zone/zone_store.rs:139`). Combine it with these public fields and helpers:
    - `card.set_zone` (`E/card/mod.rs:2531`), `set_controller` (`:1983`), `set_tapped` (`:1971`), `set_counters_map` (`:2527`), `set_summoning_sick` (`:2210`)
    - `game.assign_zone_timestamp` (`E/game.rs:654`)
    - `game.attach_to(aura, host)` / `attach_to_player` (`E/action.rs:1715-1730`). These set `attached_this_turn=true`, which Forge resets via `onCleanupPhase`, so clear it as well.
  - Recommend a fork patch that adds `GameState::place_for_setup(card, zone, controller, PlaceOpts{tapped, counters, face, attach})`, mirroring `FO/SetupBuilder.java`. It is about 60 LOC, and keeps the driver off brittle field pokes.
- **After placement:**
  - `staticability::layer::apply_continuous_effects(&mut game)` (`E/staticability/layer.rs:175`, public)
  - `loop.trigger_handler.reset_active_triggers(&game)` (`E/trigger/trigger_handler.rs:1082`). This registers battlefield triggers so they *will* fire for future events. `run_turn` does it anyway (`E/game_loop.rs:705-708`).
- **Library order.** Rust's top of library is the **last** element (`P/runner.rs:1185-1192`: "Rust draws via pop()"). Use `replace_zone_cards(Library, pid, bottom..top)`.
- **Turn, phase and active player.** `game.turn.{turn_number, active_player, priority_player}` are public (`E/phase/mod.rs:145-161`).
  - `run_turn` always enters at Untap. `TurnMachineState` is private and has no "start at phase X" entry (`E/game_loop/phase_handler.rs:4-…`, `E/game_loop.rs:124-135`).
  - That matches the Forge follower, which also runs from untap and stops at the first Main1 priority of the scenario turn (`FO/StepMachine.java:237-249`). Forge handles `tapped_after_untap` by tapping at that first priority (`FO/SetupBuilder.java:65-69`), which needs the state-edit hook (§3.4).
  - On turn 1 in a 2-player game the draw is skipped (`phase_handler.rs:112`).
- **No port of `forge.ai.GameState` or puzzle code.** There is no `puzzle` module; I grepped for puzzle, cheat and spawn_card. The closest analogues are the tests and the parity runner:
  - `manabrew-engine/tests/shock_counterspell_bug.rs:241-330` builds cards by hand, uses `move_card` to the battlefield, and pushes `StackEntry` directly.
  - `tests/control_magic_aura.rs` hand-builds `CardInstance` and statics.
  - `P/runner.rs:1049-1287` is the full deck, library and hands bootstrap. It shuffles with JavaRandom, draws 7 directly, **never calls `GameLoop::setup`** (so no d20 roll and no mulligan), then calls `GameRuntime::from_parts` → `run_opening_hand_actions` → loops `run_turn`.
- **Per-turn history.** Forge calls `game.onCleanupPhase()` twice to clear it (`FO/SetupBuilder.java:107-133`). The Rust equivalent is `game.new_turn_for_player(active)` (`E/action.rs:1598-1630`), which `run_turn` calls at turn start (`E/game_loop.rs:672`). Setup-time life changes should be written with `PlayerState::set_life` (`E/player/state.rs:290`), not damage or lose_life, to avoid "life lost this turn".

### 2.2 Mapping to `FO/SetupBuilder`

| Forge (SetupBuilder)                      | Rust                                                                                                                |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `th.suppressMode(ChangesZone…)`           | Not needed: no `TriggerHandler` is passed to `move_card`, and raw placement fires nothing                           |
| `CardFactory` / `PaperCard`               | `db.get_by_card_name(name)` then `CardInstance::from_rules`; `None` means a harness "unknown card" row (`P/utils/decks.rs:108-133`) |
| set back face                             | Card state switch; check `E/card/card_state.rs` and set_state_effect. **Unverified API**, needs a probe              |
| tapped / counters                         | `set_tapped`, `set_counters_map` (BTreeMap<CounterType,i32>, `E/card/counter_type.rs:10-31`)                        |
| attached                                  | `attach_to`, `attach_to_player`                                                                                     |
| tokens (silent mint)                      | `loop.token_templates[stem].clone()`, then `create_card`, then raw battlefield placement                            |
| `setSickness(false)`                      | `set_summoning_sick(false)`                                                                                         |
| `checkStaticAbilities`                    | `apply_continuous_effects`                                                                                          |

---

## 3. Driving

### 3.1 Control flow

- **The engine owns the loop.** `GameLoop::run_turn(&mut game, &mut [Box<dyn PlayerAgent>], rng)` (`E/game_loop.rs:656-711`) runs the private turn state machine.
  - Each step calls `priority_round(game, agents, is_main)` (`E/game_loop/priority.rs:37-…`), which loops until every player passes in succession.
  - Each time a player holds priority:
    1. SBA and trigger processing run (`priority.rs:65-79`).
    2. `agents[p].get_pass_until()` is consulted (`priority.rs:84-110`).
    3. `snapshot_state(game, pools)` is called (`priority.rs:141`).
    4. `agents[p].choose_action(p, Option<&PriorityActionSpace>, &mut request_fn) -> PlayerAction` is called (`priority.rs:151-155`).
- **Calls are blocking and synchronous**, which is the same shape as Forge's `chooseSpellAbilityToPlay`. The StepMachine-inside-the-callback design from `FO/StepMachine.java:8-13` therefore maps 1:1.
- **Granularity.**
  - With `provide_priority_action_space=true`, an empty action space auto-passes **without asking the agent** (`priority.rs:119-127`). Set it to `false`, as the parity runner does (`P/runner.rs:1122`, `game_loop.rs:181`), so every priority reaches the agent. Request the space lazily via the closure.
  - Mana payment is its own interactive loop that calls `pay_mana_cost` repeatedly (`E/game_loop/mana_payment.rs:116-345`).
- **Illegal actions are swallowed.**
  - An action not in the action space becomes a *pass*, plus a `notify(GameNotification::Event(GameLogEvent::warning("Illegal action ignored: …")))` to every agent (`priority.rs:205-217,247-262,410,615,650`; `E/agent/game_log.rs:26-30`).
  - The scripted agent must (a) validate its action against `request_action_space()` before returning it, and (b) treat any such warning in `notify` as a hard error.
  - A failed cast (cancelled targeting, unpayable cost) rolls back and returns to priority; detect it the same way Forge's `checkActed` does (`FO/StepMachine.java` cast/activate completion).
- **Abort.** `GameLoop::set_abort_signal(Arc<AtomicBool>)` (`game_loop.rs:189`) stops at the next check. Use it for the loop-guard and turn-cap equivalents of `FO/StepMachine.java:150-163`.
- **Undo hooks.** `take_restore_request` plus `restore_checkpoint` (`game_loop.rs:327-381`) roll back to a turn-start checkpoint. They are not needed, but they prove the "engine polls agent for a request" pattern that §3.4 reuses.

### 3.2 PlayerAgent: all 83 methods

The trait is at `E/agent/mod.rs:32-1028`. `grep -c '^    fn '` reports 93 because it also counts `PassAgent`'s 10 impls. Only 9 methods are required (no default): `mulligan_decision`, `choose_action`, `choose_attackers`, `choose_blockers`, `choose_targets_for`, `choose_target_player`, `choose_target_card`, `choose_target_any`, `choose_land_or_spell`. **Every other method has a default that silently picks first, min, false or true.**

Rules for the ScriptedAgent:

- Override **all 83**, using a macro like `P/runner.rs:424-495` (`delegate_*!`), which already lists every signature (`P/runner.rs:807-860`).
- Each override either answers from the DecisionQueue, answers deterministically when there is exactly one legal answer (as `ScriptedController` does, `FO/ScriptedController.java:8-21`), or returns `Err(StrictMiss)`.
- Since trait methods can't return `Result`, record the failure in the agent, flip the abort signal, and return a harmless value.

| Group                          | Methods (line in `E/agent/mod.rs`)                                                                                                                                                                                                                                                                                         | Scripted answer                                                                                                                                                                                                                                                                                                         | Forge oracle equivalent                                                                                                          |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Observe / plumbing (no decision) | snapshot_state 35, take_restore_request 38, reveal_cards 42, get_pass_until 59, clear_pass_until 65, set_targeting_cancellable 292, await_display_ack 967, pays_right_after_decision 983, notify 1019                                                                                                                        | `snapshot_state` caches `&GameState`-derived snapshots; this is where SnapshotWriter runs. `get_pass_until` returns None. `notify` scans for warnings and errors.                                                                                                                                                       | SnapshotWriter calls, `notes`                                                                                                    |
| Mulligan                       | mulligan_decision 70 (+_send 76, _recv 87), choose_cards_to_bottom 99 (+_send 109, _recv 113)                                                                                                                                                                                                                              | Scenarios: never asked, because the driver skips `setup()`. Tapes: either let the driver apply gorge's post-mulligan hand and library directly (recommended), or answer keep/mull and bottom from the tape. The mulligan's own shuffle uses the `rand::Rng` argument, not `GameRng` (`E/mulligan/mod.rs:175-182`). | mulliganKeepHand 1008, tuckCardsViaMulligan 1013                                                                                  |
| Priority                       | choose_action 127, choose_land_or_spell 1015, get_ability_to_play 478                                                                                                                                                                                                                                                      | StepMachine: `CastSpell(PlayOption{card_id,mode,alt_cost_index})`, `ActivateAbility(AbilityRef{card_id,ability_index})`, `ActivateMana`, `PassPriority` (`E/player/actions/player_action.rs:11-41`). Match gorge's ability line to `ActivatableAction.description/ability_index` (`E/agent/types.rs:88-99`).       | chooseSpellAbilityToPlay 99, getAbilityToPlay 109                                                                                 |
| Targeting                      | choose_targets_for 285, choose_target_player 296, choose_target_card 304, choose_target_card_from_zone 312, choose_target_any 323, choose_target_spell 431 (stack ids `u32`)                                                                                                                                              | Delegate `choose_targets_for` to `spellability::choose_targets_by_kind` (`E/spellability/mod.rs:1891`), then answer per call from the refs. Divided damage lives in `sa.target_chosen.divided_map`; for divided spells, set it in `choose_targets_for` directly.                                                    | chooseTargetsFor 212, chooseNewTargetsFor 217, chooseTarget 229                                                                   |
| Combat                         | choose_attackers 137, exert_attackers 148, enlist_attackers 156, choose_blockers 162, choose_blocker_for 175, choose_damage_assignment_order 192, assign_combat_damage 210, pay_combat_cost 878                                                                                                                             | Direct from the attack/block step and decisions. `assign_combat_damage`'s default is a real lethal-first algorithm, so script it whenever gorge logged one.                                                                                                                                                        | declareAttackers 754, declareBlockers 759, exert 764, enlist 769, orderBlockers 737, assignCombatDamage 774, payCombatCost 388   |
| Yes / no                       | choose_optional_trigger 503, confirm_replacement_effect 513, confirm_action 527 (mode string ↔ `PlayerActionConfirmMode`, `E/player/player_action_confirm_mode.rs`), confirm_payment 539, pay_cost_to_prevent_effect 551, choose_binary 569, flip_coin_call 860, choose_phyrexian_pay_life 867                              | `m.yesNo(seat, kind)` from the queue                                                                                                                                                                                                                                                                                    | confirmTrigger 330, confirmReplacementEffect 308, confirmAction 275, confirmPayment 342, payCostToPreventEffect 352, chooseBinary 336, chooseFlipResult 867 |
| Optional and additional costs  | choose_kicker 594, choose_buyback 612, choose_multikicker 625, choose_replicate 639, help_pay_assist 605, choose_number_for_keyword_cost 784                                                                                                                                                                                | From the cast step's `cast_mode` and optional costs                                                                                                                                                                                                                                                                     | chooseOptionalCosts 450, chooseNumberForKeywordCost 551, helpPayForAssistSpell 501                                                |
| Mana and costs                 | pay_mana_cost 938, specify_mana_combo 997, choose_convoke 922, choose_improvise 909, choose_delve 896, choose_tap_type_for_cost 687, decide_cost_part 970, order_cost_parts 988, choose_sacrifice 334                                                                                                                       | See §3.3                                                                                                                                                                                                                                                                                                                | payManaCost 396, applyManaToCost 403, specifyManaCombo 519, chooseCardsForConvokeOrImprovise 466, chooseCardsToDelve 496, getCostDecisionMaker 440 (ScriptedCostDecision.java), orderCosts 445, chooseCardsForCost 460 |
| Object picks                   | choose_cards_for_effect 672, choose_entities_for_effect 700, choose_single_entity_for_effect 469, choose_single_card_for_zone_change 711, choose_cards_for_zone_change 725, choose_discard 397, choose_discard_any_number 404, choose_random_discard 419, choose_legend_keep 493 (default `duplicates[0]`)                    | refs → CardIds. **No min/max on several of these** (`choose_single_entity_for_effect` has only `is_optional`), so validate the count against gorge's `min/max`.                                                                                                                                                       | chooseCardsForEffect 573, chooseSingleEntityForEffect 585, chooseEntitiesForEffect 597, chooseSingleCardForZoneChange 670, chooseCardsForZoneChange 682, chooseCardsToDiscardFrom 615, choosePermanentsToSacrifice 603 |
| Library manipulation           | choose_scry 346, choose_surveil 358, choose_dig 371, choose_reorder_library 385                                                                                                                                                                                                                                            | From picks; reorder from gorge's order                                                                                                                                                                                                                                                                                  | arrangeForScry 711, arrangeForSurveil 721, orderMoveToZoneList 702, willPutCardOnTop 731                                          |
| Modes and abilities            | choose_mode 448 (descriptions only), choose_spell_abilities_for_effect 459                                                                                                                                                                                                                                                  | Match by description text. **Weak**: no stable mode id, so match against `sub_ability` description strings                                                                                                                                                                                                            | chooseModeForAbility 260, chooseSpellAbilitiesForEffect 644                                                                       |
| Names, colours, types, numbers | choose_color 652, choose_colors 657, choose_type 741, choose_counter_type 751, choose_card_name 763, choose_number 770, choose_number_from_list 795                                                                                                                                                                         | From picks                                                                                                                                                                                                                                                                                                              | chooseColor 872, chooseColors 889, chooseSomeType 789, chooseCounterType 926, chooseCardName 979, chooseNumber 850/859, announceRequirements 238 (X) |
| Dice                           | choose_roll_to_ignore 806, choose_roll_to_swap 816, choose_dice_to_reroll 826, choose_roll_to_modify 836, choose_roll_swap_value 846                                                                                                                                                                                        | From picks; the roll values themselves come from GameRng (§3.5)                                                                                                                                                                                                                                                         | chooseRollToIgnore 820 …                                                                                                          |
| Replacement order              | choose_single_replacement_effect 1022 (descriptions only)                                                                                                                                                                                                                                                                   | By description                                                                                                                                                                                                                                                                                                          | chooseSingleReplacementEffect 943                                                                                                 |

**No clean scripted mapping, or missing entirely:**

- **Trigger ordering.** There is no `orderSimultaneousSa` equivalent. APNAP ordering of simultaneous triggers is done internally (`E/game_loop/trigger_handler.rs`, `docs/forge-dsl-semantics.md:756-769`). A tape where gorge chose an order cannot be replayed, so the result is a known-divergence row.
- **Pile split** (`chooseCardsPile`), **vote**, **sector**, **sticker**, **sprocket**, **protection-type**, **keyword-for-pump**, **card face/state**, **divideShield**, `chooseSingleStaticAbility`, `confirmStaticApplication`, `chooseManaFromPool`: none of these has a trait method. The engine decides internally, often first-option. Look for `ctx.agent…` calls in the corresponding effects before relying on them.
- **choose_mode and choose_single_replacement_effect** expose only description strings. The identity mapping to gorge's mode index is by text, which is fragile.
- **choose_target_spell** uses stack ids (`u32`) that are assigned at push time. Map them via `game.stack` entries (source card plus SA text).
- **Deterministic first-pick defaults** need care: `choose_legend_keep` (`duplicates[0]`), `choose_sacrifice`, `choose_discard`. Override them all to require an answer when there is more than one candidate.

**Alternative.** `PromptAgent<R: Responder>` (`AI/agent_impl/mod.rs:74-130,502`) implements 75 of the 83 methods over about 19 serialisable prompt kinds (`manabrew-protocol/src/prompts/mod.rs:59-81`), and each prompt carries a `GameViewDto`. It does not override `choose_blocker_for`, `choose_delve`, `choose_number_for_keyword_cost`, `choose_tap_type_for_cost`, `decide_cost_part`, `order_cost_parts`, `pays_right_after_decision` or `take_restore_request`.

Writing a `Responder` would be less code, but I don't recommend it as the primary design:

- It flattens the decision kind into generic `ChooseFromSelection` plus a title string (e.g. `choose_mode` sends "Choose Mode", `AI/agent_impl/choices.rs:228-246`).
- It improvises on a malformed reply (`recv_selection(..).unwrap_or_else(default)`).

Use it only as a reference, and maybe for the GameViewDto snapshot.

### 3.3 Mana payment

- **Engine side.** `pay_mana_cost_session_generic` (`E/game_loop/mana_payment.rs:63-345`) loops on `agent.pay_mana_cost(...) -> ManaCostAction`. The options are:
  - `TapForMana{card_id, mana_ability_index, express_choice}`
  - `Untap(card)`
  - `Pay{auto:true}` → engine autopay (`E/mana/auto_pay.rs:33-…`)
  - `Pay{auto:false}` → pay from pool only (`mana_payment.rs:325-330`); more than 3 invalid attempts fail
  - `AttemptedAndFailed` → cancel
  - **The default answer is `AttemptedAndFailed`** (`E/agent/mod.rs:938-965`), so an un-overridden agent can never cast.
- **Matching the Forge oracle.** It pays "from the floating pool only … no land is ever tapped" (`FO/ScriptedController.java:22-23`), with mana added by the `mana` step op (`FO/StepMachine.java:259-262`) or `Request.payment(i)` (`FO/Request.java:85-94`). The Rust driver should add gorge's spend to `loop.mana_pools[pid]` via the §3.4 hook, then answer `Pay{auto:false}`.
- **Tapes with real lands.** Answer `TapForMana` per gorge's recorded sources (the agent-interface `auto_pay::choose_pay_mana_cost_action`, `AI/auto_pay.rs:101-169`, is a heuristic and not for scripting). Answer `specify_mana_combo` for "any colour" sources.
- **Non-mana cost parts.** `decide_cost_part(player, source, &CostPart, &GameState) -> Option<PaymentDecision>` defaults to `None` with a TODO (`E/agent/mod.rs:970-981`). Grep `decide_cost_part` callers in `E/game_loop/cost_payment.rs` (3,765 lines) to see what `None` means per part. This is the Rust analogue of `FO/ScriptedCostDecision.java`. **This is unverified and needs a runtime probe.**

### 3.4 The state-edit problem (fork patch #1)

- **Forge does three edits from inside the priority callback:** it adds pool mana (`addMana`), applies `SetupBuilder.tapAfterUntap`, and in some ops touches state directly.
- **Rust agents can't.** They receive `&GameState` at best, while the loop holds `&mut GameState`, so `Rc<RefCell<GameState>>` aliasing is impossible.
- **Minimal patch.** Add a trait method modelled on `take_restore_request` (`E/game_loop.rs:358-381`, polled at `priority.rs:176`):
  ```rust
  fn take_state_edit(&mut self) -> Option<Box<dyn FnOnce(&mut GameState, &mut [ManaPool])>> { None }
  ```
  Poll it in `priority_round` right before `choose_action` (around `priority.rs:128`) and inside the mana-payment loop before `pay_mana_cost` (around `mana_payment.rs:134`), then call `apply_continuous_effects`. That is about 25 LOC in two files.

### 3.5 RNG, shuffles and library order

- **GameRng.** `GameRng` (`E/game_rng.rs:23-35`) has `shuffle_cards(&mut [CardId])` and `next_int(bound)`. It is a public field, `game_loop.game_rng: Box<dyn GameRng>` (`E/game_loop.rs:59`).
  - All effect shuffles go through `game.shuffle_zone_cards(Library, pid, ctx.rng)` → `ZoneStore::shuffle_cards(.., &mut dyn GameRng)` (`E/game.rs:549-556`, `E/zone/zone_store.rs:157`). Callers include shuffle, change_zone/hidden (tutor), move_cards, change_zone_all, make_card, restart_game and rearrange.
  - Coin flips (`cost_flip_coin.rs`, flip_coin_effect) and dice (`cost_roll_dice.rs`, roll_dice_effect) also go through it.
- **Forcing order.** Implement a `ScriptedRng: GameRng` holding a queue of forced results:
  - For a shuffle, the result is gorge's resulting order mapped by name, or by gorge ref through a shared `Rc<RefCell<CardId→ref>>`. Validate the multiset and fail on mismatch.
  - For `next_int`, return gorge's recorded value, or fail.
- **Paths that bypass GameRng:**
  - `CardLists::shuffle` uses `rand::thread_rng()` (`E/card/card_lists.rs:80-83`). It has no callers found, but treat it as a hazard.
  - `roll_for_first_player` and the mulligan shuffle use the `rand::Rng` argument (`game_loop.rs:437-516`, `mulligan/mod.rs:182`). Both are avoided by skipping `setup()`.
  - `run_turn`'s `_rng` argument is unused (`game_loop.rs:660`).
- **Initial library.** Placed by the driver directly. No shuffle.

### 3.6 CardId stability

- `CardId(u32)` is an arena index assigned in creation order (`create_card`). It is **stable across zone changes**: the same `Card` slot moves, unlike Forge, where a zone change makes a new object but keeps the id.
- New-object semantics are tracked by `zone_timestamp` (`E/card/mod.rs:730`, `game.assign_zone_timestamp`). Tokens and copies get fresh ids.
- So a gorge-ref → CardId `RefTable`, as in `FO/RefTable.java`, works if setup creates cards in gorge's deck order, as `FO/SetupBuilder.java:33-37` does.
- Stack objects are addressed by `u32` stack ids (`game.stack.push` returns one; `choose_target_spell` uses them).

---

## 4. Observation and snapshots

- **Where to snapshot.** `snapshot_state(&GameState, &[ManaPool])` is called before every priority decision (`priority.rs:136-142`), before each mana-payment ask (`mana_payment.rs:134`) and at turn start (`game_loop.rs:678-680`). Snapshot from inside the agent, exactly where Forge's StepMachine calls `SnapshotWriter.snapshot` (`FO/StepMachine.java:243-245`).
- **The full object.** `GameState` derives Clone and Serialize (`E/game.rs:126-…`). `GameSnapshot` (`E/game_snapshot.rs:7-33`) is a clone used for rewind, not an observation format.
- **Fields for gorge's snapshot schema** (`FO/SnapshotWriter.java`: active, attached_to, attacking, blocking, colors, controller, damage, face_down, kind, library_count, life, name, owner, pool, priority, pt, seat, source, step, tapped, token, turn):
  - **zones:** `game.cards_in_zone(zone, pid) -> &[CardId]`. Library order is reversed (top = last).
  - **name:** `card.card_name`. A face-down card should be empty, following SnapshotWriter; `card.face_down` is at `E/card/mod.rs:290`. Rooms use `full_name` (`AI/game_view_dto.rs:664-668`).
  - **P/T after layers:** `card.power()` and `card.toughness()` (`E/card/mod.rs:1055-1076`). These sum the static set/modify fields that `apply_continuous_effects` maintains, plus P1P1 and M1M1. **Call `apply_continuous_effects(&mut game)` first.** You can't from `&GameState`, so either rely on the engine having just done it (it does when `provide_priority_action_space` is true, or when `request_action_space()` is called, `priority.rs:113-118,145-148`), or call `request_action_space()` in `choose_action` before snapshotting.
  - **P/T caveats:** layer 7d is folded into 7c and 7e (switch) is unimplemented (`docs/forge-dsl-semantics.md:618-623,884`).
  - **counters:** `card.counters: BTreeMap<CounterType,i32>` (`E/card/mod.rs:327`). `CounterType` variants are `P1P1, M1M1, Loyalty, …, Named(String)` (`E/card/counter_type.rs:10-31`). Normalise them to gorge's spelling (P1P1, LOYALTY, CHARGE). The DTO uses `format!("{k:?}")` (`AI/game_view_dto.rs:625-631`).
  - **tapped, damage, owner, controller, is_token, attached_to and attachments:** public fields (`E/card/mod.rs:234-454`). A player attachment is `attached_to_player`.
  - **life:** `game.player(pid).life`; player counters such as poison are on `PlayerState`.
  - **pool:** `loop.mana_pools[pid]`, which the agent receives as `&[ManaPool]`.
  - **tokens:** `card_name` comes from the tokenscript `Name:` (for example "Goblin Token", `tokenscripts/r_1_1_goblin.txt:1`). That is identical to Forge, because it uses the same scripts.
  - **stack:** `game.stack` (`MagicStack`). Each `StackEntry` has `spell_ability.source`, `target_chosen` and the description (`E/spellability/mod.rs:132`). Emit top first.
  - **combat:** `loop.combat` (CombatState) and `game.turn.combat_block_assignments` (`E/phase/mod.rs:155-158`). The agent does not see `CombatState` (it is not passed), so derive attacking state from `card` fields or `turn`. **Check card-level `attacking` flags.**
  - **step:** `game.turn.phase` (`PhaseType`), mapped with the same table as `SnapshotWriter.stepName`.
- **Ready-made card serialiser.** `card_to_dto` (`AI/game_view_dto.rs:569-…`) already handles visibility, morph and room names, so it can be reused.

---

## 5. Support signal and static estimate

### 5.1 Runtime detection (layered, cheapest first)

1. **Load-time:** `db.get_by_card_name` returning `None` means unknown card (hard refuse). `force_parse_all().failed` lists cards whose parse failed.
2. **Static pre-flight per card**, done in the driver at request time. Port `mb_scan.py`, or better, use the engine's own parsed IR:
   - (a) every A, T, S and R line plus SVar abilities: `SP$`/`AB$`/`DB$` must resolve via `ApiType::smart_value_of` *and* be in `IMPLEMENTED_API_TYPES` (public, `E/ability/effects/mod.rs:233-234`; the parity filter does exactly this, `P/card_pool.rs:1103-1142`);
   - (b) `T: Mode$` must be in `parse_trigger`'s match arms (`E/trigger/trigger.rs:1081-1303`; 163 modes extracted to `scratchpad/rust_trigger_modes.txt`);
   - (c) static `Mode$` must not be `StaticMode::Other`;
   - (d) `R: Event$` must not be `ReplacementType::Other`;
   - (e) every `Key$` must be one the Rust side reads. A fork patch could add a `Params` read-tracker, which is more reliable than grepping literals;
   - (f) walk the IR for `DefinedRef::Unsupported`, `SpellAbilityMode::Unsupported`, `AbilityDuration::Unsupported` and `TriggerCondition::Unsupported` (`E/ability/ability_ir.rs:1206`, `E/spellability/runtime_types.rs:14,63,122,142`, `E/trigger/trigger.rs:818`).
3. **Runtime stderr capture.** Redirect fd 2 or, better, patch the engine to route these to a hook. Any of the following during a scenario fails the row:
   - "Unimplemented effect API type"
   - "Unrecognized Count expression"
   - "[parse] missing required param"
   - "[parse] failed to parse"
   - "[ControlGainVariant] Unimplemented mode"
   - "[WARN] Unhandled cost part refund"
   - "[WARN] Unknown cost adjustment"
   - "SubAbility chain exceeded depth"
   - "SVar resolution exceeded depth"
   - "[selector-drift]"
   - The sources are listed by `grep eprintln!` (34 sites, `E/**`); the relevant ones are `effect_resolver.rs:55`, `svar/mod.rs:551,2016`, `parsing/mod.rs:782,1606,1628`, `control_gain_variant_effect.rs:76`, `cost/cost_payment.rs:372`, `cost/cost_adjustment.rs:143`, `ability_factory.rs:499` and `card/valid_filter.rs:541`.
   - Trace output gated by env vars (FORGE_CARD_TRACE, FORGE_ZONE_TRACE, the stack and trigger traces) must stay unset.
4. **Notifications.** "Illegal action ignored …" warnings arrive via `notify`.
5. **Panics.** The engine has 32 `panic!`, 4 `unreachable!`, 66 `.expect(` and 44 `.unwrap()`, plus ~105 raw `self.cards[..]` indexes in `E/**`.
   - Wrap each scenario in `catch_unwind(AssertUnwindSafe(..))`. The agents hold `Rc` and the engine uses thread-locals (`SUB_ABILITY_CHAIN_DEPTH`, `SVAR_RESOLUTION_DEPTH`, perf scopes).
   - After a panic those may be left dirty, so either restart the worker process or run one scenario per forked child.
   - Classify panics as `engine_panic` rows, not harness errors.

### 5.2 Static estimate

Method: `scratchpad/mb_scan.py` against Rust literals and enums in `manabrew-engine/src` plus `forge-card-script/src`, over gorge-pin scripts in `/mnt/sata/gorge-training/xengine/forge-trek6/forge-gui/res`.

- AI-only and cosmetic keys are excluded: AILogic, AITgts, Ultimate, Image, descriptions, MayPlayText, …
- A flag means the Rust code never even names the thing. Absence of a flag is **not** support.

| Pool                                                                     | Cards  | Unflagged    | Flags                                                                                                                                   |
| ------------------------------------------------------------------------ | ------ | ------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| FDN, `[cards]` plus `[precon product]` (517)                             | 517    | 479 (92.6%)  | key_unref 37; kw 1 (Hare Apparent's deck rule, harmless); 0 API, trigger, static or replacement gaps; 0 multiface                      |
| Commons proxy: every `C` printing in Core, Expansion, Draft and Starter editions | 10,373 | 9,809 (94.6%), 9,679 single-face | key_unref 498, kw 49, multiface 131, api_unknown 16 (Empower 11, Recruit 4, FlipCoin 1), static 5, trigger 2 (FacesDilemma)           |
| All cardsfolder                                                          | 34,442 | 30,138 (87.5%) | api_unknown 173 (FlipCoin 84, PutSticker 38, Empower 35, Recruit 10, Assimilate 8, HealDamage 3), trigger 18, static 20, kw 246, key_unref 3,918 |

**FDN flagged cards** (the key that Rust never reads):

| Key(s)                                    | Cards                                                                                                                    |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| ShuffleNonMandatory                       | Campus Guide, Solemn Simulacrum, Hoarding Dragon, Fierce Empath, Springbloom Druid                                       |
| MayPlayLimit                              | Muldrotha, the Gravetide                                                                                                 |
| MayChooseTarget                           | Thousand-Year Storm, Teach by Example, Pyromancer's Goggles                                                              |
| ClearChosenCard                           | Chandra, Flameshaper; Strongbox Raider                                                                                   |
| ClearChosenCard + ControlledByPlayer      | Liliana, Dreadhorde General                                                                                              |
| ClearChosenPlayer                         | Sorcerous Spyglass                                                                                                       |
| ClearChosenPlayer + NewController         | Wishclaw Talisman                                                                                                        |
| NewController                             | Harmless Offering                                                                                                        |
| ForceRevealToController                   | Squad Rallier, Vivien Reid                                                                                               |
| StaticEffect                              | Valkyrie's Call, Infernal Vessel                                                                                         |
| DefinedCards                              | Curator of Destinies                                                                                                     |
| TriggerAmount                             | Faebloom Trick                                                                                                           |
| Stackable                                 | Kaito, Cunning Infiltrator                                                                                               |
| AlternativeDecider                        | Uncharted Voyage                                                                                                         |
| TargetsWithSameController                 | Soul-Shackled Zombie                                                                                                     |
| TargetsWithDifferentControllers           | Run Away Together                                                                                                        |
| NoResolvingCheck                          | Courageous Goblin, Ruby, Daring Tracker                                                                                  |
| ExcessSVar + ExcessSVarCondition          | Goblin Negotiation                                                                                                       |
| DamageSource                              | Felling Blow, Bite Down                                                                                                  |
| RevealRandomOrder + RevealedLibraryPosition | Spinner of Souls                                                                                                       |
| RememberRemoved                           | Fishing Pole                                                                                                             |
| TriggerController                         | Fake Your Own Death                                                                                                      |
| TargetUnique                              | Drakuseth, Maw of Flames                                                                                                 |
| UseAllOriginZones                         | Finale of Revelation                                                                                                     |
| TargetsValid                              | Gnarlback Rhino                                                                                                          |
| FirstAttack                               | Aurelia, the Warleader                                                                                                   |

**Realistic FDN expectation:**

- Static cleanliness only says the vocabulary exists. The Rust port has never been checked against gorge, and the parity harness that used to catch divergences is dead.
- I'd budget **60–80% of FDN singletons** passing cast-resolve scenarios on first contact, with long-tail failures in replacement and copy interactions.
- Expect fewer passes for **whole games**, because one divergence anywhere in a game breaks the tape.

**Script drift for FDN.** Only 4 FDN scripts differ between the merge base and the gorge pin (Grappling Kraken, Prayer of Binding, Demonic Pact, Wilt-Leaf Liege). FDN is the safest set to start on.

---

## 6. Hurdles and risks

- **AGPL-3.0-or-later** (`Cargo.toml` workspace license; `LICENSE.md` dual AGPL/GPL). The driver links the engine and needs engine patches, so it must live in a public fork with source offered to network users (AGPL §13). gorge-compliance must keep it out of process (stdin/stdout JSONL), as the Forge oracle already does with GPL Forge.
- **Churn is low now.**
  - Engine commits since 2026-10-10: 14, about 4 of them non-release (`gh api …/commits?path=manabrew-rs/crates/manabrew-engine`).
  - `agent/mod.rs` has had 2 commits since August, so the trait surface is stable.
  - Workspace releases are daily (CHANGELOG v3.79.x). Pin a SHA; rebasing patches should be cheap.
- **Divergence between pins:**
  - Manabrew's Forge pin `be9e0c8` is merge-base `4b6936186d` (2026-09-21) plus 63 fork commits, which touch only 2 card scripts: Fireball and Officious Interrogation.
  - gorge pin `84ce1603` is 1,049 commits past that merge base: cardsfolder +469 added, 495 modified, ~343 renamed, 2 deleted, and `forge-game/src` 149 files changed against be9e0c8.
  - **More important: Rust lags its own pin** (FlipCoin, Empower, Recruit, HealDamage missing in Rust ApiType), so "Manabrew follows Forge X" is not true at any X. Using gorge-pin scripts adds PutSticker and Assimilate to the unknown list, plus modified scripts that may use params Rust never learned.
  - Mitigation: the pre-flight refuse (§5.1) runs on whatever cardsfolder the archive was built from.
- **Known CR deviations** (`docs/forge-dsl-semantics.md:876-885`):
  - The 5-category replacement model.
  - Layers 1–6 then 7a–7c then 8. 7d is folded and 7e (P/T switch) is unimplemented, so "switch P/T" cards will diverge.
  - The fizzle check doesn't use source LKI.
  - Simultaneous triggers are ordered APNAP but internally, with no agent hook (`:756-769`).
  - Trigger parse is case-sensitive while Java's `smartValueOf` isn't. A difference in case silently drops the trigger.
- **Multiplayer.** The engine is N-player (`GameLoop::new(n)`, PlayerCollection), but the turn-1 draw skip is `players.len()==2`-specific (`phase_handler.rs:112`) and parts of control-exchange are "multiplayer-specific … skipped" (`control_gain_variant_effect.rs:74-77`). gorge is 2-player, so this is fine.
- **Deprecated parity.** README and AGENTS.md say not to run parity and that changes need not preserve it (`manabrew-rs/README.md:3`, `AGENTS.md:3`). So the engine's Forge-faithfulness is no longer guarded by CI; only `cargo test --lib` runs. Expect regressions to land unnoticed. This argues for running our own nightly gorge-vs-manabrew corpus.
- **Global state.** `CardDatabaseRegistry`, `TypeRegistry` and thread-local depth counters argue for one process per worker, loaded once, with a restart after a panic.
- **Archive build tooling** is a separate bin behind `--features build`. It is cheap: one run per Forge pin.
- **Performance.** The release profile is `opt-level="z"`, so add a `[profile.oracle] inherits="release", opt-level=3` in the fork.

---

## 7. Proposed driver design

### 7.1 Layout (fork `adams-shaun/manabrew`, branch `oracle`)

```
manabrew-rs/crates/manabrew-oracle/          (new bin crate; AGPL)
  Cargo.toml   deps: forge-foundation, forge-carddb, forge-cardset-archive, manabrew-engine,
                     (opt) manabrew-agent-interface, serde_json, clap
  src/main.rs            CLI: `manabrew-oracle scenarios <in.jsonl> <out.jsonl> --archive cardset.rkyv`
                                `manabrew-oracle games     <in.jsonl> <out.jsonl> ...`
                                `manabrew-oracle build-archive --res <forge res>`  (wraps build_archive_from_sources)
                                `manabrew-oracle preflight --pool <names.txt>`     (support table)
  src/request.rs         Request/GameRequest parsing (port FO/Request.java, FO/GameRequest.java)
  src/decision_queue.rs  port FO/DecisionQueue.java (step-scoped consumption, leftover report)
  src/ref_table.rs       port FO/RefTable.java (gorge ref <-> CardId / stack id / PlayerId)
  src/setup.rs           port FO/SetupBuilder.java over the new GameState::place_for_setup
  src/step_machine.rs    port FO/StepMachine.java: ops mana/cast/activate/play/resolve/pass/pass_to/attack/block
  src/scripted_agent.rs  ScriptedAgent: impl PlayerAgent (all 83 methods via macro), shares Rc<RefCell<StepMachine>>
  src/scripted_rng.rs    ScriptedRng: impl GameRng (forced shuffles / next_int from script; fail otherwise)
  src/snapshot.rs        port FO/SnapshotWriter.java (gorge vocabulary normalisation)
  src/support.rs         preflight: API/trigger/static/replacement/key/IR-Unsupported scan
  src/stderr_guard.rs    fd-2 capture + pattern classifier (until patch #3 lands)
  src/result_row.rs      port FO/ResultRow.java (same JSON so oraclediff reads it unchanged)
engine patches (small, upstreamable):
  #1 E/agent/mod.rs + priority.rs + mana_payment.rs: take_state_edit hook (~25 LOC)
  #2 E/game.rs/action.rs: pub fn place_for_setup(..) raw placement (~60 LOC)
  #3 E/**: route the eprintln! diagnostics through a pub `diagnostics` sink (thread-local Vec) (~60 LOC)
  #4 (optional) Params read-tracker to list keys never consumed (~40 LOC)
  #5 (optional) agent hook for simultaneous-trigger ordering (~40 LOC)
```

### 7.2 Main loop (scenario)

1. Load the archive and registries once. Start a worker per core, using process isolation.
2. For each request line:
   1. Parse it.
   2. **Pre-flight** every named card. On a refusal, emit an `unsupported` row naming the card and reason. Never improvise.
3. Build `GameState::new(["p0","p1"], 20)` and `GameLoop::new(2)`, set `provide_priority_action_space(false)`, and register tokens.
4. Run setup, which mirrors the Forge object order: per seat battlefield, hand, graveyard, library, exile, library_top, then the Wastes filler. Raw placement, then counters, tapped, attachments, life (`set_life`) and summoning sickness. Then `apply_continuous_effects`.
5. Set `turn.turn_number = scenario turn`, `active_player = p0`, `game_rng = ScriptedRng`, and the abort signal.
6. Build `GameRuntime::from_parts(game, loop, [ScriptedAgent(p0), ScriptedAgent(p1)])` and loop `run_turn` until `step_machine.done` or `failure`, with a loop and turn cap like `FO/StepMachine.java:150-163`.
   - Inside `choose_action` the StepMachine runs exactly as in Forge. It skips until the first Main1 priority of the active player on the scenario turn, then takes the "setup" snapshot, then plays the ops.
   - Mana ops and tap-after-untap go through `take_state_edit`.
   - Completion checks read the `&GameState` passed into `snapshot_state`.
7. Finish: check `catch_unwind`, the diagnostics sink and notification warnings, then emit snapshots, notes and `leftover`.

**Whole-game tape.** The same pipeline without the scenario setup:

1. Build both decks.
2. Apply gorge's post-mulligan hands and libraries directly.
3. Set the starting player.
4. Run the turn loop with ScriptedAgents answering every decision from the tape, and ScriptedRng answering every shuffle and roll.
5. Checkpoint at each recorded decision.

A BridgeController analogue, a live gorge bot over stdio, is possible later via the same `PlayerAgent` (manabot already plays via `PromptAgent`). It is not phase 1.

### 7.3 Error taxonomy (row fields)

| Code                    | Meaning                                                                                                                                                              |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `unsupported_card`      | Name not in the archive, or failed parse                                                                                                                             |
| `unsupported_mechanic`  | Pre-flight failed: unknown API, trigger mode, static or replacement, unread key, or an IR `Unsupported`. Record the card, line and token                              |
| `engine_diagnostic`     | One of the §5.1 stderr or sink messages during the run                                                                                                               |
| `illegal_action`        | Engine downgraded our action, or a cast or activation did not reach the stack                                                                                        |
| `strict_miss`           | Engine asked a question the script doesn't cover (kind, step, detail). Same text as `FO/StrictMiss.java`                                                             |
| `leftover`              | gorge decisions not consumed                                                                                                                                         |
| `rng_miss`              | Shuffle or roll requested with no scripted answer, or the multiset mismatched                                                                                        |
| `engine_panic`          | Message and backtrace                                                                                                                                                |
| `harness`               | Our bug, a bad request, or the loop guard tripped                                                                                                                    |
| `known_divergence`      | Hit a documented CR deviation: trigger ordering choice, 7e, …                                                                                                       |

**No loose mode.** Forge's loose mode lets the AI answer; the Rust defaults are worse than an AI, so never fall back to them.

### 7.4 Estimated LOC (Rust)

| Component                                   | LOC                  |
| ------------------------------------------- | -------------------- |
| Request, DecisionQueue, RefTable, ResultRow | ~900                 |
| setup                                       | ~450                 |
| StepMachine                                 | ~1,300               |
| ScriptedAgent (83 methods)                  | ~1,400               |
| snapshot                                    | ~350                 |
| ScriptedRng                                 | ~120                 |
| support pre-flight                          | ~350                 |
| stderr guard and main                       | ~250                 |
| **Driver total**                            | **~5,100**           |
| Engine patches                              | +~200                |
| Tests                                       | +~1,500              |
| Whole-game tape mode                        | +~700                |

For comparison, the Forge oracle is 8,008 Java LOC.

### 7.5 Phases

0. **Probe**, 1–2 days, needs a toolchain. Build the archive from the 84ce1603 res. Write a 100-line bin that places a board and casts Shock via a hard-coded agent. Verify the open questions: raw placement, `decide_cost_part`'s `None` semantics, attack flags, and back-face setup.
1. **Setup-only.** Patches #2 and #3, plus setup, snapshot and pre-flight. Diff the "setup" checkpoint against gorge for the FDN fixture corpus. Done when the setup snapshots match on 95% or more of FDN fixtures with every card pre-flight-clean.
2. **Cast-resolve.** Patch #1, StepMachine ops mana, cast, activate and resolve, ScriptedAgent targeting, yes/no and picks, and ScriptedRng. Run the gorge scenario corpus restricted to FDN. Triage rows into engine-bug vs. driver-bug.
3. **Combat and pass_to.** Attack and block ops, plus damage assignment.
4. **Whole-game FDN tapes.** Mulligan bypass, full decision tapes, and per-decision checkpoints. Add a nightly corpus because upstream parity CI is gone.
5. **Optional.** Upstream patches #1–#3 as generic hooks (they're engine-neutral), and a bridge seat for head-to-head play.

### 7.6 Open questions to settle in the probe

- How back-face and transformed setup is done (`card_state.rs` and `set_state_effect.rs:136`).
- What `decide_cost_part -> None` does for each CostPart (`cost_payment.rs`).
- Whether any effect asks the agent for trigger order or pile split anywhere. Grep effects for `agents[` calls I didn't enumerate.
- Whether `card.attacking` and blocking flags exist on `Card` or only in `CombatState` (the agent can't see `CombatState`).
- How `pay_cost_to_prevent_effect` and "unless" costs re-enter the mana loop.
