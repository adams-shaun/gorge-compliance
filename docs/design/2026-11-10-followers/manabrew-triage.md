<!-- Triage written 2026-11-12 for MANABREW_ORACLE_REF 75c1399a6de7 (the answerer ticket, Manabrew follower 9/10). Historical, like every design note: it states what the sweep showed at that ref. Card names and short descriptions only; no Forge script text, no Oracle text. -->

# Manabrew triage: FDN-commons, level A

`scripts/follower-check.sh manabrew --pool oracle/xengine/pools/fdn-commons.txt` at the ref above: 196 level-A
items, 12 refused by the preflight, 184 sent. **175 AGREE; 9 do not.** Before the answerer (ref af05437209be)
the same sweep was 107 AGREE, 74 HARNESS (70 of them strict misses on targets, modes, orders and picks), 3 DIVERGE.

The three steps that took it from 107 to 175 were the answerer itself (`answer.rs`, the `ScriptedAgent`
methods), registering the whole token database at setup (nine rows died on a token script the engine did
not know, a panic), and treating `next_int(1)` as the non-random call it is (a token's art variant among one).

One row per non-AGREE id follows (the count is `jq -r 'select(.verdict.status != "AGREE") | .id'
.oracle-build/follower-check/manabrew/diff.jsonl | wc -l`). Causes:

- **Manabrew engine bug**: the engine's result breaks the rules; gorge is right.
- **driver gap**: this follower's own limitation (setup, snapshot, answers); no rules claim either way.
- **gorge suspect**: gorge may be wrong; none here.
- **gorge does not log the decision**: the engine asked something gorge never recorded, so there is nothing to answer from.

The Forge column: no Forge row exists for this pool (the Forge sweep is Java-backed and the operator's), so
there is no F-versus-M disagreement to split yet; every row reads "no Forge row".

| id | status | field or message | cause | Forge agrees with gorge? |
|---|---|---|---|---|
| Bushwhack/cast-resolve/v1 | HARNESS | `rng_miss: shuffle of 38 cards at step 1` | driver gap: `ScriptedRng` refuses the shuffle after the library search; the classifier leaves library order out of the comparison once a window has shuffled, so an identity shuffle with a note would let the row compare (follow-up filed) | no Forge row |
| Grow from the Ashes/cast-resolve/v1 | HARNESS | `rng_miss: shuffle of 38 cards at step 1` | driver gap: same shuffle after a search | no Forge row |
| Mystical Teachings/cast-resolve/v1 | HARNESS | `rng_miss: shuffle of 38 cards at step 1` | driver gap: same shuffle after a search | no Forge row |
| Gnarlid Colony/cast-resolve/v1 | DIVERGE | permanents: gorge 2/2; Manabrew 4/4 with two +1/+1 counters | Manabrew engine bug: an unkicked cast (the kicker was not payable and `choose_kicker` was never asked) still enters with the kicked counters; the condition on the enters-with-counters keyword is ignored | no Forge row |
| Goblin Boarders/cast-resolve/v1 | DIVERGE | permanents: gorge 3/2; Manabrew 4/3 with a +1/+1 counter | Manabrew engine bug: enters with the counter although nothing attacked; same ignored condition on the enters-with-counters keyword | no Forge row |
| Imprisoned in the Moon/cast-resolve/v1 | DIVERGE | permanents: gorge Grizzly Bears is a land with no colour or P/T; Manabrew keeps `bear creature land`, `{G}`, 2/2 | Manabrew engine bug: the aura's type, colour and ability-loss effect is applied only as an added land type | no Forge row |
| Three Tree Mascot/cast-resolve/v1 | DIVERGE | permanents: gorge `allcreaturetypes artifact creature`; Manabrew `artifact creature shapeshifter` | driver gap: the snapshot writer prints the printed subtype for a changeling instead of gorge's `allcreaturetypes` (follow-up filed) | no Forge row |
| Witness Protection/cast-resolve/v1 | DIVERGE | permanents: gorge names the enchanted creature Legitimate Businessperson, `citizen creature`, `{WG}`, 1/1; Manabrew keeps Grizzly Bears, `bear citizen creature`, `{G}`, 1/1 | Manabrew engine bug: the aura's "becomes a named 1/1" effect sets P/T and adds the type but does not set the name, replace the old subtype or set the colours | no Forge row |
| Uncharted Haven/play-land/v1 | HARNESS | `Missing order answer at step 0: choose_single_replacement_effect` (1 follower checkpoint, gorge 2) | gorge does not log the decision: two replacement effects apply as the land enters and gorge recorded only the colour pick, never an order | no Forge row |

## What the answerer does (Forge driver's rules, ported)

Every `PlayerAgent` method either answers from the step's decisions of the asking seat, answers
deterministically when exactly one legal answer exists, or records a strict miss; none returns the trait
default. Notes for reading rows:

- Targets are taken by target spec per ref (`take_target_refs`), so a creature-only prompt skips a player ref
  that is still there for a later any-target prompt (a partly consumed decision stays a leftover). A spell
  target is the stack entry whose source card the ref names. Several targets of one spell arrive through
  `choose_cards_for_effect` inside `choose_targets_for` and are answered the same way.
- Divided damage sets `target_chosen.divided_map` from the `damage_split` decision (card shares only; the
  engine has no division of damage to players or any use of the map at this pin, so a divided-damage row
  would diverge as a Manabrew engine bug).
- Optional costs read the cast step's `cast_mode`; none is a decline. Followed: ordinary, `adventure_alt`,
  `warped`, `kicked`, `kicked1`, `buyback`, `entwined`, `multikicked`, `replicated`. Not followed (harness
  rows, and `CastModes` in `oracle/xengine/follower/follower.go` omits them): `omen_alt`, `modal_spell`,
  `plot` (the engine has no such cast or special action), `kicked2`, `kickedboth` (one kicker only),
  `offspring`, `optionalcost` (no cast prompt). No level-A FDN-commons item carries a cast mode, so these are
  covered by the driver's own tests, not by this sweep.
- A trigger order gorge logged with several candidates is a harness row `known_divergence: trigger order`
  (the engine orders its own triggers, no agent hook). None occurs in this pool.
- Mana-ability colour choices, convoke, delve, improvise, tap-type costs, dice, mulligans and combat remain
  strict misses: nothing in this pool asks for them.
