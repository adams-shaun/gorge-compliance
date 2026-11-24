# phase-seat

phase.rs (MIT OR Apache-2.0, pinned by `../PHASE_PIN`, never vendored) as a SpellBench v2 **engine**:
an NDJSON server on stdin/stdout that hosts a two-player game and poses each acting seat's `WaitingFor` as
a v2 seat decision. four-yay's `cmd/xclient -engine sbv2 -engine-cmd scripts/phase-sbv2.sh` plays it.
The protocol is gorge's `docs/site/api/spellbench-v2.md`; the phase-server WebSocket is not used.

| Module | What | Needs the engine |
|---|---|---|
| `json` | strict-JSON line rules, canonical JSON, `deck_id` / `domain_id`, HMAC-SHA-256 | no |
| `server` | framing, exact request field sets, request-id cache, `hello` / `validate_deck` / `reset` / `step`, error codes; the game sits behind `Backend` | no |
| `semantic` | the kind table and one builder per posed kind (a drifted builder panics in its test) | no |
| `observe` | the thirteen observation flags this engine declares | no |
| `waiting` | `classify`: one arm per `WaitingFor` variant (147 at the pin), no `_` arm | yes |
| `view` | `GameState` -> the seat's observation (phase's own `filter_state_for_viewer`, then projected) | yes |
| `game` | `PhaseBackend`: decks, reset, candidates per family, substep groups, apply, terminal | yes |

`phase-follower`'s `names.rs` and `support.rs` are compiled in by `#[path]` (card lookup and the
"which cards phase replays" rule), so there is one copy of each.

## What is posed

Sixteen families over 27 `WaitingFor` variants (and `GameOver` as the terminal), one `Family` each in
`waiting.rs`: priority (pass, play land, cast, activate); mulligan (keep / mulligan, and the London bottom
as `order_pick` `mulligan_bottom`); attackers and blockers (one substep per creature); targets
(`TargetSelection`, `TriggerTargetSelection`); optional effect (`OptionalEffectChoice`,
`OpponentMayChoice`); optional cost; X; mana payment (`ManaPayment`, `ManaSourceSelection`: finish a
payment the pool covers, else `activate_mana_ability` per source, or cancel); modes (`ModeChoice`,
`AbilityModeChoice`); trigger order (`OrderTriggers`); scry and surveil (`arrange_card` then
`order_pick`); top or bottom; and pool picks of `min..=max` cards (`DiscardToHandSize`, `DiscardChoice`,
`RevealChoice`, `SearchChoice`, `EffectZoneChoice`, `DigChoice`, the simple `PayCost` kinds, `ChooseLegend`).

`AssignCombatDamage` is host-answered, not posed (`combat_damage_assignment: "engine_order"`): lethal to each
blocker in phase's order, the rest to the last blocker, or to the defender under trample. Damage modes other
than `Normal` and trample over a planeswalker halt as `unsupported_damage_mode`.

`OpeningHandBottomCards` is Tiny Leaders' forced first mulligan (TL:R 906.6), not the London bottom: a
two-player game never raises it, so it stays unposed.

Everything else is `Pose::Unposed`: the game halts with `engine_contract_failure:unmapped_decision`, the
variant name goes to stderr, and the census (stderr at the end of the run) counts it as an `engine_error`
by name. A legal priority action with no `semantic` yet (equip, crew, mana abilities, ...) is left out of
the candidates and counted as `unmapped_action`. Nothing is answered on an agent's behalf.

The next variants to pose are the ones the census names most often over real games.

A spell phase is resolving (its card in the stack zone, its entry already off the list) is listed as the top
`stack` record, so a reflexive trigger ("When you do, ...") can name it as its source.

## House seat (`--house-seat`)

`phase-seat --house-seat p0|p1 [--house-difficulty NAME]` (a build with the `house-ai` feature; `scripts/phase-sbv2.sh`
passes the flags on and builds with it) answers that seat in-process with phase-ai, so one client policy plays
the other seat against a built-in opponent. `NAME` is `veryeasy`, `easy`, `medium` (default), `hard`, `veryhard`
or `cedh`; an unknown seat or name exits 2 with the usage line. The client learns the house seat from
`hello_ok.engine.version` (`.../autopay/house-p1-medium`); no protocol field changes.

* The house seat is answered in `PhaseBackend::pose` before `classify`, whenever it is among the acting players
  (a mulligan with both seats pending answers the house entry first, so the client is posed its own). A prompt
  the mapper does not pose is therefore never a halt on the house seat, and its actions are counted as
  `house_actions` in the census, never as `decisions` or `engine_error`. They count toward `max_steps`, so a
  house pass loop ends `truncated`. `choose_action` returning nothing, or `apply` refusing its action, halts as
  `house_no_action` / `house_rejected` (the `halts` bucket).
* The search runs in phase-ai's measurement mode (`into_measurement(seed)`): bounded by node count and depth,
  no wall clock, so an answer is a pure function of the state, the config and the rng. Both the config seed and
  the rng seed come from the `reset` `game_secret`: one `game_secret` and the client's answers replay the game.
* `phase-ai` and `rand_chacha` are optional (`house-ai`, off by default: the default build and
  `--no-default-features` are as small as before). `make phase-seat-test RUN='--features house-ai TestHouseSeat'`
  runs the house replay test.

## Testing

* `cargo test -p phase-seat --no-default-features` is the protocol layer alone: it does not compile the engine (cargo still needs the engine's git source to resolve the lockfile).
* `make phase-seat-test` is the whole crate at the repo's 8G scope; the engine's codegen needs more than 4G.
* `tests/replay.rs` plays seeded games twice with a fixed random policy and compares the transcripts byte for byte. It needs `PHASE_SEAT_DECKS=<deck dir>` (gorge deck JSON); `make phase-seat-test` sets it to gorge's `bots/spellbench/decks/fdn-limited` (under `GORGE_ROOT`) and plays seeds 3 and 5 (about 9 s), so a determinism break fails the default target. `PHASE_SEAT_SEEDS=0,1,2,3,4,5` is the full run (about 12 min), `PHASE_SEAT_ONCE=1` plays each once, and an empty `PHASE_SEAT_DECKS=` skips the test. A game that reaches the 4000-line cap prints its last three lines (stack included).
* `TestHouseSeatReplaysByteForByte` (`--features house-ai`) is the same replay with phase-ai answering p1 at medium; it also asserts p1 is never posed and no game halts. `PHASE_SEAT_HOUSE=p0|p1` makes the plain replay test do the same.
* `cargo test --no-default-features -p phase-seat parse_house` covers the `--house-seat` / `--house-difficulty` parser.
* Seeds 0-2 of that catalog never end: Cat Collector's "first time each turn" life-gain trigger fires on every Dazzling Angel gain, so the stack refills for ever (phase's engine at the pin; the host only passes). `max_steps` bounds such a game.
* `make smoke` runs `ops/scripts/tests/phase_sbv2_smoke.sh` over `scripts/phase-sbv2.sh` with stubs.
