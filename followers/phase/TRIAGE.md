# phase.rs follower: FDN-commons level-A triage

Produced by `scripts/follower-check.sh phase --pool oracle/xengine/pools/fdn-commons.txt` (196
level-A items of the 200-name pool; 4 names have no level-A scenario) at the gorge pin in
`GORGE_PIN` and the phase pin in `followers/phase/PHASE_PIN`. Result: 193 AGREE, 2 DIVERGE,
1 HARNESS. One row below per item whose `diff.jsonl` verdict is not AGREE (3 rows). Card names and
short descriptions only.

Causes, as the follower-check brief names them: phase rules difference, driver gap, gorge suspect,
gorge does not log the decision.

| id | status | field or message | cause |
|---|---|---|---|
| Three Tree Mascot/cast-resolve/v1 | DIVERGE | `permanents` at step 1 (resolve): gorge `[allcreaturetypes artifact creature]`, phase `[artifact creature shapeshifter]` | driver gap: the Changeling's type list. gorge's snapshot shows the all-creature-types marker, the follower's snapshot lists only the printed subtype Shapeshifter. Same creature, same 2/1 body; the follower's snapshot projection (`snapshot.rs`) does not expand the marker. |
| Uncharted Voyage/cast-resolve/v1 | HARNESS | `3 gorge checkpoints, 2 xmage`: the row stops at step 1 with the strict miss `Missing TopOrBottomChoice answer at step 1` | driver gap: the spell puts the target on the top or bottom of its owner's library. gorge logs the choice (a `choose_n` with `resume` `changezone_alternative`, pick `top`) and phase asks `TopOrBottomChoice`, but this ticket's arm table does not map that prompt, so it is a strict miss by design. |
| Witness Protection/cast-resolve/v1 | DIVERGE | `permanents` at step 1 (resolve): gorge `Legitimate Businessperson [citizen creature] 1/1`, phase `[bear citizen creature] 1/1` | phase rules difference: the Aura makes the creature a Citizen and the card's reminder text says it loses its other creature types (CR 205.1a); gorge drops Bear, phase keeps it. Not a replay or answer issue (the target decision was consumed). |

## Notes on rows that AGREE

- Incinerating Blast/cast-resolve/v1 agrees but leaves one gorge decision unconsumed (`step 1 mode
  [Don't pay]`, `resume` `unless_pay`): gorge asks whether to pay an unless-cost at resolution,
  phase poses no such prompt in this position. Cause: driver gap or phase rules difference (phase
  does not offer the payment); the decision shape finding downstream is expected until that is
  looked at.
- 44 rows (Incinerating Blast among them) used to report a gorge target decision as unconsumed because phase puts a
  spell or ability with exactly one legal target on the stack without a prompt. The follower now
  consumes a gorge target ref that names what the stack entry actually targets (`answer.rs`,
  `sweep_stack_targets`); a decision naming some other object still reports as unconsumed.
- Prompts gorge does not log today (replacement order, combat damage assignment among several
  blockers, Phyrexian per-pip choices, cost-reduction order) stay strict misses and classify as
  `decision_shape`. No FDN-commons item reached one.
- Optional costs phase resolves without a prompt are consumed at the end of the replay: a declined
  trigger cost (`trigger_cost_decline`, auto-declined when no branch is payable, CR 608.2d) and an
  either-or additional cost whose preferred branch is unpayable (`altaddcost`, the fallback is paid
  without asking, CR 601.2b). A `ResolutionOptionalPaymentChoice` phase does pose is answered from
  the same decision. Equip with one legal creature is phase's auto-target path (no `EquipTarget`
  prompt), so the stack sweep consumes its target decision, including one logged on the mana step;
  when phase does pose `EquipTarget`, that prompt accepts the same earlier-step decision.
- Cards that print or grant banding (including `BandsWithOther`) are refused by the preflight:
  phase has no CR 702.22c band choice, so the game would ask a prompt phase never poses.
- Scry and surveil: phase cannot be told gorge's bottom order, so the cards bottomed by a scry
  that bottoms two or more go in looked-at order (a row note says so).
- `kicked1`, `kicked2`, `kickedboth`, `multikicked` and `replicated` are paid as one optional-cost
  prompt, once (the count gorge settles separately is not scriptable here); a row note records it.
  No FDN-commons item exercises them.
- Cast modes: `plot` is not followed. gorge's `plot` is the Plot special action (the card is
  exiled and plotted); phase's plot variant only casts a card already plotted, so a hand cast
  cannot replay it, and a plot step is a driver gap (the follower reports `cast_mode plot
  unsupported`). A followed mode that no prompt honoured (a kicker the card lacks, a variant phase
  never offered) is a harness row, `cast_mode X not honoured`, not a silent plain cast.
  `adventure_alt` and `omen_alt` from hand answer phase's `CastOffer` face prompt. No FDN-commons
  item exercises any of these.
- Prompt arms added for `EquipTarget` (gorge's target decision), `CrewVehicle` (the `tapcost` pick),
  `TopOrBottomChoice` (the `changezone_alternative` ask), `ManifestDreadChoice` (the
  `manifest_dread` pick), `ReplacementChoice` of kind `OptionalBranch` (the `unless_pay` or yes/no
  decision; 0 accepts, 1 declines) and `DiscardChoice`/`DiscardToHandSize`/`ConniveDiscard` (gorge's
  `Discard <card>` mode decisions, one per card). Still unsupported, a strict miss that names the
  prompt: `ReplacementChoice` of kind `Order` or `SearchFoundDestination` (gorge logs neither); the
  Prowler-style "enter as a copy" replacement (gorge's `clone` pick, `resume` `etb`) is not mapped,
  nor the `CopyTargetChoice` that follows it.
- Keyword activations `ability_action` maps: `Equip`, `Crew`, `Saddle` (empty selection starts the
  announcement), `Cycling` and `TypeCycling` (phase's hand-zone ability tagged `Cycling`, told apart
  by its effect: a draw or a library search) and `Class:<level>:...` (the `SetClassLevel` ability of
  that level). A card with two cycling abilities of the same kind is a harness row (`several
  cycling abilities`), not a guess. The `SaddleMount` prompt that follows has no answer arm yet (a
  strict miss that names it); every other keyword stays `keyword ability X unsupported`.
- Setup entry effects: a battlefield placement gets the card's own enters-with counters and
  as-enters choices (`Builder::entry_effects`: the real entry replayed on a clone of the state taken before the raw placement, counters
  and `chosen_attributes` carried over; a choice prompt is answered with its first option). Still
  not applied: a card whose entry parks on any other prompt (Furycalm Snarl's reveal, Devour) keeps
  the raw placement, and a "when this enters ... choose a color" TRIGGER is not run (Puca's Eye's
  chosen colour: phase models it as an ETB trigger, not an entry replacement, and replaying it
  would draw a card), so those rows stay GFX|P until phase gets an entry-only hook.

## Filed upstream

The fork's issue tracker is disabled (`adams-shaun/phase` has issues off), so a finding is filed
against upstream `phase-rs/phase`; the `oracle` branch is the upstream `b14f974` commit at the pin.
One link per finding.

- [phase-rs/phase#11310](https://github.com/phase-rs/phase/issues/11310) — Cat Collector +
  Dazzling Angel, `phase-seat` replay seeds 0-2 over gorge's `fdn-limited` decks: Cat Collector's
  "Whenever you gain life for the first time during each of your turns" trigger loses its
  first-life-gain-of-your-turn limit and re-triggers on Dazzling Angel's life gain, so the game
  never ends (turn 8 for seed 0; the stack refills with fresh Cat Collector and Dazzling Angel
  `triggered_ability` entries). Root cause: `strip_first_time_each_turn_qualifier` in
  `crates/phase-parser/src/parser/oracle_trigger.rs` recognizes `"for the first time each turn"`
  and `"for the first time during each of their turns"` but not the `"your"` wording; the card data
  carries `confirmed_misparse: dropped_qualifier`. Tale of Katara and Toph prints the same wording
  and is covered by the same fix.
