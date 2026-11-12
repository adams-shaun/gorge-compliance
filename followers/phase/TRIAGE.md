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
