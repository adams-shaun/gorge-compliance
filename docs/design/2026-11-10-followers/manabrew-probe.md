<!-- Probe notes written 2026-11-12 for the Manabrew follower; MANABREW_ORACLE_REF dadee731dc1c. -->

# Manabrew probe: answers to the seven open questions

The design ([audit-manabrew.md](audit-manabrew.md)) was built from a static read. This note records what a
throwaway probe established by running code against the pinned engine. Nothing here is Manabrew source: it names
functions, types and files and describes behaviour (Manabrew is AGPL-3.0, and the card archive is built from
Forge scripts, which are GPL-3.0, so no script text is quoted either).

## How the probe was run

- A new bin crate `probe` under `manabrew-rs/crates/` of the dev clone (`$MANABREW_ORACLE_DIR/manabrew`), on a
  local branch `probe` cut from `MANABREW_ORACLE_REF`. The branch is never pushed and the code is not committed
  here. Built and run capped with `systemd-run --user --scope -p MemoryMax=8G -p CPUQuota=400%` and
  `cargo build --release -p probe`; the binary takes the archive path and a scenario name.
- Bootstrap, as the brief laid out: `CardDatabase::load_from_archive` on `cardset-<forgeref12>.rkyv` (34,444
  cards), `CardDatabaseRegistry::load`, `TypeRegistry::load(archive.type_lists)`, `GameState::new(&["p0","p1"], 20)`,
  `GameLoop::new(2)` with `set_provide_priority_action_space(false)`, `GameRuntime::from_parts`, one `run_turn`.
  Cards are made with `game.create_card(CardInstance::from_rules(rules, owner))` and placed without `move_card`
  (`replace_zone_cards` plus `set_zone`, `set_controller`, `set_tapped`, `set_summoning_sick`,
  `set_counters_map`, then `staticability::layer::apply_continuous_effects`). `GameLoop::setup` is skipped.
- Paths below are relative to `manabrew-rs/crates/manabrew-engine/src/` unless they start with another crate name.
- Scenarios: `shock` (the brief's run), `nolayer` (same, but the layer pass is skipped at setup), `dfc`
  (a double-faced card, no game), `tv` (a spell with a mana part and a discard part), `leak` (a counter-unless-pay
  spell), `combat` (one attacker, one blocker).

## Q1

**How is a back face / transformed state set at setup?** There is no separate "state" object on the live card.
`Card` holds the face it currently shows in its own fields plus `other_part: Option<CardOtherPart>` holding the
*other* face (`card/mod.rs`, struct `CardOtherPart` near the top, `Card::other_part`, `Card::is_transformed`).
`Card::transform()` swaps every face-dependent field with `other_part` (name, type line, mana cost, colour, base
P/T, keywords, abilities, triggers, static and replacement abilities, svars), resets per-face transients, re-parses
the activated abilities, and toggles `is_transformed`. `Card::change_to_state()` and `change_card_state()` are
aliases of it, so they toggle back if called twice. `Card::set_transformed(bool)` only writes the flag and does
**not** swap anything. In-game the path is `SetStateEffect`'s Transform mode (`ability/effects/set_state_effect.rs`),
which applies the transform replacement event, calls `Card::transform()`, then fires the Transformed trigger and
`reset_active_triggers`. `card/card_state.rs` (`CardState`) is a Java-parity type that `card/card_assembly.rs` builds for per-door
room states; it is not how a transformed permanent is represented.
**Setup recipe:** create the card from the front-face rules, then call `Card::transform()` once; do not use
`set_transformed`. Face-down is a separate flag (`set_face_down`, `Card::face_down`).
Ran (`dfc`): the front face reads name "Delver of Secrets", 1/1, `is_transformed` false; after
`set_transformed(true)` alone the name and 1/1 are unchanged (flag true); after `transform()` the card reads
"Insectile Aberration", 3/2, `Human Insect`, with `other_part` now naming the front face; a second
`change_to_state()` returns it to the front face with the flag false.

## Q2

**What does `decide_cost_part` returning `None` do for each `CostPart`?** At this pin, nothing, for every
part, because the live engine never calls it. `PlayerAgent::decide_cost_part` (`agent/mod.rs`) is reachable only
through the blanket `CostVisitor` impl in `cost/trait_cost_visitor.rs`, used by `cost/cost_payment.rs`
(`CostPayment::pay_cost` / `pay_computer_costs`) and `cost/trait_cost_decision_maker.rs`. A search of the whole
workspace finds no caller of `CostPayment::new`, `pay_cost` or `pay_computer_costs` outside that file. (If it were
live, `None` would make both functions return false, i.e. the cost fails.) The same holds for `order_cost_parts`
and `pays_right_after_decision`: only `cost/cost_payment.rs` calls them.
The live path is `GameLoop::pay_ability_cost` and `pay_additional_costs` in `game_loop/cost_payment.rs`. It
matches on each `CostPart` and asks the agent through *other* methods: `confirm_payment` first (via
`confirm_cost_part_payment`, only for parts where `should_confirm_payment` says so), then per part:
`Discard` -> `choose_discard`; `Sacrifice`, `Exert`, `Enlist`, `UntapType`, `GainControl`,
`ExiledMoveToGrave` -> `choose_sacrifice`; `TapType` -> `choose_tap_type_for_cost` / `choose_cards_for_effect`;
`Return`, `CollectEvidence`, `Behold`, `Blight` -> `choose_cards_for_effect`; `Exile*`, `PutCardToLib`, `Reveal`,
`Forage` -> the controller's cost-card choosers (`player/player_controller.rs`, `choose_cost_card_from_zone`,
`choose_cost_card_mixed`); `ChooseColor` -> `choose_color`; `ChooseCreatureType` -> `choose_type`;
`PromiseGift` -> `choose_target_player`; `RemoveAnyCounter` -> `choose_counter_type`; `Waterbend` ->
`choose_convoke`; `ExileFromStack` -> `choose_target_spell`; `Mana` -> the `pay_mana_cost` loop.
`Tap`, `Untap`, `PayLife`, `PayEnergy`, `PayShards`, `DamageYou`, `Draw`, `Mill`, `GainLife`, `AddMana`,
`SubCounter` and `AddCounter` make no agent call in their match arm beyond the confirm (`FlipCoin` and
`RollDice` go to helpers that make none directly, but the dice and coin effects they run have their own choosers).
Ran (`tv`, a spell with a mana part and a discard part): with logging overrides on `decide_cost_part` and
`order_cost_parts`, neither fired; `choose_discard` fired once (before mana payment), then `pay_mana_cost` three
times (tap, tap, confirm from pool). The driver can leave `decide_cost_part` at its default.

## Q3

**Do `Card` objects carry attacking/blocking flags, or only `CombatState`?** Attacking: yes. `Card` has
`attacking_player: Option<PlayerId>` (set in `game_loop/combat_phase.rs` at declare-attackers, cleared when the
creature leaves combat or combat ends) and `attacked_this_turn`. Note the value is the **defending** player, not the
attacker's controller, and it holds only a player, so a planeswalker or battle defender is not recoverable from it. Blocking: there is no
per-card blocking flag. `CombatState` (`combat/mod.rs`) lives on `GameLoop.combat` (`game_loop.rs`), and no
`PlayerAgent` method receives it. But `GameState.turn` (`TurnState` in `phase/mod.rs`) has a public
`combat_block_assignments: Vec<(blocker, attacker)>`, published from `combat.blockers` right after blockers are
declared (`game_loop/combat_phase.rs`) and cleared at end of combat, so a `snapshot_state` taken in the
declare-blockers step carries the pairs. `TurnState::combat_blockers_declared` stayed false in the snapshots, so do
not rely on it. `Card::get_s_var("BlockedThisTurn")` and `remembered_cards` stayed empty on the blocker.
Ran (`combat`): at the declare-attackers snapshot the attacker read `attacking_player=Some(p1)`,
`attacked_this_turn=true`; at the declare-blockers snapshot `combat_block_assignments=[(blocker, attacker)]`; at
Main2 both were cleared (`attacked_this_turn` stays true). `choose_blockers` itself receives only attacker and
available-blocker ids.

## Q4

**How do `pay_cost_to_prevent_effect` and "unless" costs re-enter the mana-payment loop?** Through the
effect resolver, in `ability/effects/cost_payment.rs`: `resolve_effect_with_unless_cost` works out the cost and
the payer (`resolve_unless_payers`), checks `can_pay_with_ability`, then asks the payer
`pay_cost_to_prevent_effect(player, cost_kind, message, source, api, can_pay, targets, effect_text)`. The default
is "false if cannot pay, otherwise `confirm_payment`" (so, true). On yes it calls `try_pay_unless_cost` ->
`try_pay_effect_cost`, which (only for the cost parts it supports: flip, damage, life, mana, energy, shards, draw,
mill, discard, sacrifice, add-counter, add-mana; any other part makes it return false) may call `confirm_payment`
again per part (`should_confirm_effect_cost_part`), and for a `Mana` part calls `pay_mana_cost_for_effect`, which
enters the same `pay_mana_cost_session_generic` (`game_loop/mana_payment.rs`) that spells use. So the agent sees
the identical `pay_mana_cost` loop (tap, tap, confirm from pool), with the unless amount as the cost and the
source card as `card_name`. Sacrifice and discard parts go through the normal choosers. Sacrifice-unless
(`ability/effects/sacrifice_effect.rs`, `sacrifice_all_effect.rs`) calls `pay_cost_to_prevent_effect` directly.
Two quirks for the driver: `targets` is built from player and card targets only, so a spell target (the case
below) arrives as an empty list; and `effect_text` is the ability's raw script line when the ability has no stack
description, so it must not be shown or stored as Oracle text.
Ran (`leak`): p0 casts Shock, p1 responds with a counter-unless-pay-{3} spell. Order of agent calls seen by p0:
`pay_cost_to_prevent_effect(kind "{3}", can_pay true, targets [])`, then `pay_mana_cost` four times (cost `{3}`,
pool 0, 1, 2, 3, last with `can_confirm_from_pool` true). Shock then resolved (p1 at 18).

## Q5

**Does any effect ask the agent for simultaneous-trigger order or a pile split?** Trigger order: no. A search of
every agent call in `ability/effects/` (about 70 files) finds no ordering call; `trigger/trigger_handler.rs`
(`run_waiting_triggers`) sorts the matched triggers with a fixed key and pushes them in that order. The key is:
active player's triggers first (APNAP), then non-"bucket 2" before "bucket 2", then the source's zone timestamp
(older first), then the trigger bucket, then the trigger's creation order. The only agent involvement is optional
triggers (`choose_optional_trigger`) and target choice (`choose_targets_for`). `reorder_zone_effect.rs` has a
comment that a full version would call `order_cards`, but it does nothing. Pile split: `TwoPilesEffect`
(`ability/effects/two_piles_effect.rs`) does ask: the controller's `choose_cards_for_effect(min 1, max n-1)`
picks pile 1, the rest is pile 2, then the opponent's `choose_optional_trigger` picks which pile (true = pile 1);
the default `choose_optional_trigger` is true. `MultiplePilesEffect` (`multiple_piles_effect.rs`) asks nothing: it
deals the cards round-robin into the piles, and the "random chosen" pile uses the engine rng. The oracle-side
consequence: scenarios that depend on trigger order or a free split are decided by the engine's fixed rule, not by
the agent, so the driver cannot steer them.

## Q6

**The `PlayerAgent` trait at the pin (`agent/mod.rs`).** 83 methods, 9 without a default, matching the audit.
Required: `mulligan_decision`, `choose_action`, `choose_attackers`, `choose_blockers`, `choose_targets_for`,
`choose_target_player`, `choose_target_card`, `choose_target_any`, `choose_land_or_spell`. The other 74 have
defaults. Defaults that silently decide (the driver must override any it can reach, or accept the choice):
- **first element:** `choose_sacrifice`, `choose_target_spell`, `choose_single_entity_for_effect`,
  `choose_legend_keep`, `choose_color`, `choose_type`, `choose_counter_type`, `choose_card_name`,
  `choose_number_from_list`, `choose_roll_to_ignore`, `choose_roll_to_swap`, `choose_roll_to_modify`,
  `choose_roll_swap_value` (`Power`), `choose_cards_to_bottom` (first `count`), `choose_discard` and `choose_random_discard` (first `num`),
  `get_ability_to_play` (index 0), `specify_mana_combo` (first colour for every unit),
  `choose_single_replacement_effect` (0), `choose_damage_assignment_order` (given order), `choose_target_card_from_zone`
  (delegates to `choose_target_card`), `choose_blocker_for` (delegates to `choose_blockers`).
- **minimum:** `choose_number` (`Some(min)`), `choose_colors`, `choose_discard_any_number`, `choose_mode`
  (first `min` modes), `choose_spell_abilities_for_effect` (first `num`).
- **maximum (take the first `max`):** `choose_dig`, `choose_cards_for_effect`, `choose_entities_for_effect`,
  `choose_delve`; `choose_tap_type_for_cost` takes every valid card.
- **true:** `choose_optional_trigger`, `confirm_replacement_effect`, `confirm_payment`, `flip_coin_call`,
  `pay_cost_to_prevent_effect` (when payable).
- **false / zero / empty:** `confirm_action`, `choose_binary` (via `confirm_action`), `choose_kicker`,
  `choose_buyback`, `choose_phyrexian_pay_life`, `choose_multikicker`, `choose_replicate`, `help_pay_assist`,
  `choose_number_for_keyword_cost`, `choose_improvise`, `choose_convoke`, `choose_dice_to_reroll`,
  `exert_attackers`, `enlist_attackers`, `pay_combat_cost` (decline), `pay_mana_cost` (cancels:
  `AttemptedAndFailed`), `decide_cost_part` (`None`, never called, see Q2).
- **keep everything on top:** `choose_scry`, `choose_surveil` (all cards stay), `choose_reorder_library`
  (unchanged order).
- **no-ops:** `snapshot_state`, `reveal_cards`, `notify`, `await_display_ack`, `set_targeting_cancellable`,
  `clear_pass_until`, the `_send` halves of the mulligan and bottom prompts.
`assign_combat_damage` has a greedy default: lethal damage to each blocker in the given order (deathtouch and
indestructible aware), the remainder to the last blocker or, with trample, to the defender.
`mulligan_decision_recv` and `choose_cards_to_bottom_recv` delegate to their blocking forms.

## Q7

**Is the `&GameState` passed to `snapshot_state` after `request_action_space()` post-layer?** The order at the
pin (`game_loop/priority.rs`, `priority_round`) is: snapshot to the priority player's agent first, then
`choose_action` with a `request_action_space` closure that runs `staticability::layer::apply_continuous_effects`
and then builds the action space. So the snapshot taken *before* `choose_action` does not depend on the closure; a
`GameState` seen by `snapshot_state` after the closure ran is the next snapshot, which is always post-layer
because the closure itself applied the layers. Layers are also applied at turn start (`game_loop.rs`, `run_turn`,
after the first snapshot loop), at stack resolution and state-based-action points
(`game_loop/stack_resolution.rs`), in combat (`game_loop/combat_phase.rs`) and when a non-pass action is taken.
`Card::power()` and `toughness()` (`card/mod.rs`) add the static modifier fields, so reading them from a snapshot
gives the post-layer value.
Ran (`shock` with a creature under a static anthem effect; the creature is 2/2 without it): every snapshot from
the start of the turn onward shows 3/3, including the ones taken before and after `request_action_space()` was
called inside `choose_action`. Ran (`nolayer`, the layer pass skipped at setup): the very first snapshot of the
turn, taken before the turn-start layer pass, shows the stale 2/2; the next snapshot, in the same Untap step,
shows 3/3, and it stays 3/3 at every priority afterwards. So the only stale `snapshot_state` is the turn-start one
when the harness never applied layers; a harness that calls `apply_continuous_effects` after setup, as the
brief says, never sees it. I did not find a way to make a priority snapshot stale without cheating: nothing in a
run changes a static source between the layer pass and the next snapshot.

## Shock run

Scenario `shock`: p0 has a Mountain on the battlefield and a Shock in hand; p1 has a Grizzly Bears and a static
anthem on the battlefield. The agents cast Shock at p1 (action space not provided, so it is fetched through
`request_action_space`), pay by tapping the Mountain, and pass. Probe commit (never pushed):
`639a17d04aabbed3b0dc9a4a3e9d997adebb80d1` on branch `probe` of the dev clone. Output, with the pass-priority
chatter filtered out:

```
loaded archive: 34444 cards
setup: bears 3/3 (layers applied: true)
active player before run_turn: PlayerId(0)
p0 snapshot_state (first with this value): phase Untap bears 3/3 stack 0
p1 snapshot_state (first with this value): phase Untap bears 3/3 stack 0
p0 choose_action -> CastSpell(CardId(2))
p0 snapshot_state (first with this value): phase Upkeep bears 3/3 stack 1
p0 choose_target_any players=[PlayerId(0), PlayerId(1)]
p0 pay_mana_cost Shock cost={R} can_confirm_from_pool=false tappable=[CardId(1)] pool_total=0
p0 pay_mana_cost Shock cost={R} can_confirm_from_pool=true tappable=[] pool_total=1
p1 snapshot_state (first with this value): phase Upkeep bears 3/3 stack 1
p0 snapshot_state (first with this value): phase Upkeep bears 3/3 stack 0
p1 snapshot_state (first with this value): phase Upkeep bears 3/3 stack 0
p0 life = 20
p1 life = 18
```

p1 ends at 18 as expected. The agent offered the target list `[p0, p1]` through `choose_target_any` (so the driver
must pick the opponent itself), and `pay_mana_cost` is entered twice for one `{R}`: once to tap, once to confirm
from the pool.
