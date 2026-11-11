<!-- Audit notes written 2026-11-10 for the phase.rs / Manabrew follower design. Read-only static audit; local paths (/tmp/..., /mnt/sata/...) are from the audit machine and abbreviations are defined at the top. Historical, like every design note. -->

# The xengine / oracle follower contract (audit for phase.rs and Manabrew)

Read-only audit, 2026-11-10. Nothing in any repo was edited or run (no JVM, no sweep).

Path abbreviations used in citations:

- `GC/` = /home/sadams/projects/gorge-compliance (HEAD 0d6f91b)
- `G/` = /home/sadams/go/pkg/mod/github.com/adams-shaun/gorge@v0.0.0-20261104204407-04ad74f2e4bf
  (what `go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge` resolves to; there is no go.work, so the
  module cache at GORGE_PIN 04ad74f2 is what gorge-compliance compiles against, NOT the
  /home/sadams/projects/gorge checkout)
- `FO/` = /tmp/claude-1000/-home-sadams-projects-gorge-compliance/80e3936c-c504-4123-a7ba-09bd32ba66b5/scratchpad/forge/forge-oracle/src/main/java/forge/oracle
- `FX/` = the same forge-oracle dir's `fixtures/`
- `RUN/` = /mnt/sata/gorge-training/xengine/croll-new/FDN/0 (a real finished FDN sweep chunk on disk, used
  for measured examples: 574 items = 48 whole-game tapes + 526 windows, 456 refused windows, 15 lower_gap)

---------------------------------------------------------------------------------------------------

## 0. The shape of the whole thing in one paragraph

A follower is a batch process: JSONL in, JSONL out, one output row per input line, in input order
(FO/ScenarioReplay.java:20-36, 128-143). Each input line is one *scenario*: a two-seat board placed at
turn 1 main phase 1 plus a list of steps (cast / activate / play / pass / pass_to / resolve / attack /
block / attach / move / mana). The follower places the board, plays the steps, and emits one *snapshot*
(engine-neutral observable state) at "setup" and after every step. gorge's own runner produced the
same snapshots for the same scenario; `oraclediff.CompareOpts` diffs the two lists field by field
(GC/oracle/compliance/oraclediff/compare.go:85-117). Every decision gorge's runner answered is handed
to the follower as `gorge_decisions` so both engines answer alike; a decision only the follower asks is
a `strict_miss`, one only gorge asked is a `leftover`. xengine's whole-game "tapes" and one-turn
"windows" are just very long scenarios of the same schema (templates `xengine-tape`,
`xengine-window`), so the level-A generator's items and xengine's items go through ONE follower
contract. There is no separate "game tape" wire format for followers: Forge's `GameRequest`/
`GameReplay` is a different, unrelated tool (Forge AI vs Forge AI or vs a gorge bot over a bridge)
and is NOT how xengine replays tapes (see 1.5).

---------------------------------------------------------------------------------------------------

## 1. Input: the request a follower consumes

### 1.1 Who writes it

- Forge's input is `forge-req.jsonl`, written by `oraclediff forge-export -scenarios items.jsonl -out
  forge-req.jsonl` (GC/scripts/xengine-sweep.sh:205-210; GC/cmd/oraclediff/forge.go:112-171). It
  replays every Item in gorge once (`rules.RunOracleScenarioJSON(reg, it.Raw())`, forge.go:150) and
  wraps it.
- XMage instead reads the bare Item enriched by `xengine xprep` (`xitems.jsonl`; xengine-sweep.sh:196-203).
- The level-A/B oracle pass writes the same sidecar from `oraclediff gen` output
  (GC/scripts/forge-pass.sh header, steps 1-4).
- forge-export refuses an output path inside the repo except under `.oracle-build/`
  (forge.go:135, 662-680): requests carry Forge script text.

### 1.2 Top-level request fields (one JSON object per line)

Go type `forgeRequest`, GC/cmd/oraclediff/forge.go:89-99; Java reader FO/Request.java:15-70.

| field | type | meaning |
|---|---|---|
| `id` | string | item id; Request falls back to `item.id` (Request.java:43-44) |
| `scenario_sha` | string | `gate.ItemSHA(item)` computed BEFORE forge-export's rewrites (forge.go:177) |
| `item` | object | the oraclegen.Item verbatim (plus the forge rewrites in 1.6). A bare Item line (no `item` key) is accepted for hand fixtures (Request.java:41-42) |
| `gorge_decisions` | array of rules.OracleDecision | the answer source (forge.go:93-95), see section 2 |
| `abilities` | object `{"<step index>": "<script line>"}` | for every step with `ability_index`, the raw Forge script line `Face.Abilities[ability_index].Line` of the step's card (forge.go:96-98, 294-324; abilityLine forge.go:434-439). Basic lands get the synthesized line `"intrinsic: basic land mana"` (seen in RUN/forge-req.jsonl; StepMachine.java:778) |

The output row may echo `request_sha` = sha256 of the request line's bytes without the newline,
lower-case hex (FO/ScenarioReplay.java:113-124; forge.go:87-88, 594). forge-diff drops a row whose echo
differs (forge.go:598-603). xengine diff does not check it.

**Forge-specific coupling to note for a Rust follower:** `abilities` is a *Forge script line* (gorge's
corpus IS Forge's cardsfolder + tokenscripts at gorge's FORGE_REF; G/Makefile:80-86). A follower that
does not read Forge scripts gets nothing from it and needs another key for "which activated ability"
(see 7.3). Likewise mode labels and trigger labels in gorge_decisions are Forge `SpellDescription`
text (FO/StepMachine.java:1198-1223, 1443-1450).

### 1.3 The Item / Scenario schema

Go: GC/oracle/compliance/oraclegen/gen.go:30-326 (oraclegen.Seat, Step, Scenario, Item). gorge's runner
side (the authoritative decoder, DisallowUnknownFields): G/engine/rules/oracle_run.go:48-196,
oracle_export.go:34-45.

Item-level fields (gen.go:278-326):

| field | follower must read? | notes |
|---|---|---|
| `id`, `card`, `template`, `name` | id/name echoed | `template` ∈ `cast-resolve`, `play-land`, `activate`, `combat`, `trigger`, `static`, `static.cost`, `counter-spell` (templates/*.go `Template{ID:...}`), `xengine-tape` (GC/oracle/xengine/lower/tape.go:17), `xengine-window` (lower/window.go:17); hand fixtures use e.g. `setup` (FX/setup/requests.jsonl) |
| `turn` | yes | absent/null = 1; otherwise integer 1..100 (Request.java:108-129). xengine items are always 1 (tape.go:110-116, window.go:157-163) |
| `players` | yes if non-zero | 0/absent = 2 seats; 3 = multiplayer (gen.go:257-262). Forge driver hard-codes 2 (StepMachine.java:136-144); xengine never emits 3 |
| `setup` | yes | map `"p0"`,`"p1"` -> Seat (below) |
| `setup_answers` | gorge-only | answers gorge's runner uses during setup drive; the follower gets the same info as `gorge_decisions` with `step: -1` |
| `steps` | yes | see 1.4 |
| `payment` | yes (recommended) | parallel to steps: the exact pool mana gorge spent on step i's cast/activate, WUBRGC letters, a multiset; "" = autopay (gen.go:310-318; forge.go:79-83). Forge reads it (Request.java:95-102, StepMachine.java:551-557, ScriptedController.java:403-436) |
| `ignore` | comparator-only | fields not compared (`library_top`, `p0.library_top@6`); follower ignores |
| `compare` | comparator-only | opt-in fields (`keywords`, `offered`, ...); xengine items carry none |
| `cr`, `why` | no | prose |
| `xmage_name`, `xmage_answers`, `xmage_ability`, `xmage_target_skips`, `xmage_pos` | no | XMage-only (Request.java:25-27) |

Seat setup (gen.go:30-76; runner oracle_run.go:73-127):

| key | type | semantics |
|---|---|---|
| `battlefield` | []name | placed directly, no ETB triggers fired, no entry replacements (enters-tapped etc.), not summoning sick (FO/SetupBuilder.java:30-53, 88-114) |
| `tapped` | []name | taps EVERY battlefield placement of that name (gen.go:33; SetupBuilder.java:179-181) |
| `tapped_after_untap` | []seat-relative ref (`"Forest"`, `"Grizzly Bears#2"`, `"token:Soldier"`) | tap one object after turn 1's untap step, at the first priority of the scenario turn, no trigger (gen.go:34-42; SetupBuilder.java:253-264; StepMachine.java:254-258) |
| `back_face` | []name | battlefield cards placed on face index 1; must also be in battlefield (gen.go:43-48; SetupBuilder.java:150-154,176-178) |
| `hand`, `graveyard` (zone order, oldest first), `exile` | []name | placed directly |
| `library` | []name | named library cards; gorge SHUFFLES them into its deal, so a seat naming `library` compares with library_top ignored (forge.go:641-656) |
| `library_top` | []name | top of library, first = top; deterministic. xengine puts the WHOLE library here in order (tape.go:117-122; gate.go:615) |
| `counters` | {name: {KIND: n}} | ADDED to what the card enters with (loyalty etc.), every placement of the name. Kinds in gorge spelling: `P1P1`, `M1M1`, `LOYALTY`, `CHARGE`... (gen.go:54-59; oracle_run.go:99-105, normCounterTab 2758) |
| `tokens` | [{script: Forge tokenscript stem, counters: {KIND:n}}] | minted after the seat's cards, in order, silently (no create/ETB trigger); counters are the COMPLETE set (gen.go:60-64, 87-93; SetupBuilder.java:56-62, 266-300) |
| `attached` | [{card: seat-relative ref, to: full ref}] | Auras placed already attached, after all seats/tokens, no trigger (gen.go:65-68, 78-85; SetupBuilder.java:63-69) |
| `life` | int | starting life (default 20) (gen.go:71-75) |
| `speed` | 0..4 | CR 702.179 speed (gen.go:69-70) |
| `command`, `sideboard`, `mana` | runner-only | Forge refuses non-empty `command` (SetupBuilder.java:143-145); generated items do not use them |

Card identity/order rules that matter for refs (FO/RefTable.java:12-27; SetupBuilder.java:35-38):

- Cards are created in gorge's order per seat: battlefield, hand, graveyard, library, exile, library_top,
  then Wastes filler, then tokens. Each named placement binds the ref `pN:Name` (first) / `pN:Name#k`.
- Library filler: `Wastes` x max(0, 40 - named cards); for turn > 1 at least turn/2+1
  (SetupBuilder.java:82-83, 221-228). Library layout top-first: library_top, filler, then named
  `library` cards (SetupBuilder.java:75-78, 229-236). gorge's own deck is the same 40 (FX/setup/gorge.jsonl:
  `library_count: 39` with one battlefield card). For xengine every seat names all 40 cards so filler is 0.
- Ordering: the scenario starts at turn 1, p0 active (Forge: `setupFirstTurn(seats[0])`,
  StepMachine.java:150); the first player skips the turn-1 draw; the "setup" checkpoint is the first
  main-1 priority of the scenario turn held by the active player, even with a non-empty stack
  (StepMachine.java:259-264).
- Setup must not leave per-turn history (Forge runs its cleanup bookkeeping twice, SetupBuilder.java:115-137).

### 1.4 Steps and the step vocabulary

Go Step: gen.go:124-174. Runner op table: G/engine/rules/oracle_run.go:2780-2808
(`mana, cast, activate, play, resolve, attack, block, pass, pass_to, move, life, attach`).

| op | fields | completion / checkpoint (as the Forge driver implements gorge's semantics, StepMachine.java:250-367) |
|---|---|---|
| `mana` | seat, mana (WUBRGC) | add to pool; immediate |
| `cast` | seat, card ref, mana?, targets?, cast_mode?, answers? | add `mana` to pool, put the spell on the stack; checkpoint when priority returns to the caster with the spell on the stack (checkActed 588-616). `cast_mode` "" = ordinary; Forge follows `plot, adventure_alt, omen_alt, modal_spell, warped, optionalcost, kicked, kicked1, kicked2, kickedboth, buyback, entwined, offspring, replicated, multikicked` (classify.go:462-472, StepMachine.java:686-737); others (e.g. `flashback`, 4 casts in RUN/) are classed harness for Forge |
| `activate` | seat, card ref, ability_index (gorge IR index into Face.Abilities) or ability (label), mana?, targets? | as cast; a mana ability completes at once. In xengine EVERY mana tap is an explicit `activate` step before the cast (lower/tape.go:608-661) — 3107 of ~22k steps in RUN/ |
| `play` | seat, card ref | land play; checkpoint when the land is on the battlefield |
| `resolve` | seat | pass until the stack is empty and the active player holds priority (never stop at a cleanup priority) (StepMachine.java:297-306) |
| `pass` | seat | the named seat passes exactly ONE priority; checkpoint at the next priority (the opponent's, or active player's after a resolution / step change). Wrong seat holding priority = harness "pass: pN does not hold priority" (StepMachine.java:307-320) — 16.7k of the RUN/ steps are passes |
| `pass_to` | seat, step?, active?, decision? | pass until a pending decision of kind `decision` (`priority`, `attackers`, `blockers`) or, after at least one pass, the named `step` with `active` seat (StepMachine.java:369-391). `decision:"priority"` completes immediately with no pass (forge.go:192-200) |
| `attack` | seat, attackers [refs], defender "pN" | declare in the declare-attackers callback; checkpoint at first priority after (StepMachine.java:328-345, 400-438). xengine never attacks planeswalkers/battles or splits defenders (tape.go:23-24, 695-716) |
| `block` | seat, blocks [[blocker, attacker]] | same in declare-blockers (StepMachine.java:440-477) |
| `move` | card, to (library/hand/battlefield/graveyard/exile) | direct zone change, then one empty priority round so SBAs/triggers land (StepMachine.java:346-362, 479-496) |
| `attach` | card, attached_to | direct attach; xengine window prelude for Equipment (window.go:190-197; gate.go:657) |
| `life` | seat, amount | runner op; Forge driver does not implement it (default -> harness, StepMachine.java:363-364); not emitted by xengine |

Step-level `answers` / `expect` / `observe` are gorge-runner inputs; a follower ignores them and reads
`gorge_decisions` (Forge uses step `targets` only for a bare fixture with no decisions,
StepMachine.java:943-950). Checkpoint after step i is named `step i (<op>)` (2.5/3.2).

Measured op/decision census over RUN/forge-req.jsonl (574 items): ops pass 16669, activate 3107,
play 898, cast 657, pass_to 489 (attackers 253, priority 200, blockers 36), attack 253, attach 68,
block 36, resolve 27. Setup keys: library_top 1148, hand 1143, life 1052, battlefield 905, tapped
427, graveyard 361, tapped_after_untap 188, tokens 61, counters 41, exile 39, attached 18.

### 1.5 Scenario requests vs whole-game tapes vs `GameRequest`

- **Level-A/B oracle scenarios** (`oraclediff gen`): tiny boards, 0-4 steps, `turn` 1 usually.
- **xengine window** (`xe:<SET>:<chunk>:<game>:w<turn>`, window.go:71-74): the live game cut at
  turn N's first main-1 priority of the active player, rebased to turn 1, seats relabelled so the
  active seat is p0 (window.go:180-218; gate.go table "Turn: rebased", "Active: relabelled"); steps
  are that turn's tape entries; ends at the turn's end. May start with `attach` prelude steps
  (window.go:190-197). A window whose turn consumed engine RNG (a shuffle) carries
  `ignore:["library_top"]` (window.go:203-215).
- **xengine tape** (`xe:<SET>:<chunk>:<game>:tape`, tape.go:68-76): the whole game from turn 1:
  setup = each seat's opening hand + entire library in order (`library_top`), no battlefield
  (tape.go:110-127). Lowered until the first thing it cannot express; a tape refuses RNG-consuming
  asks (`unsupported_kind:rng`, tape.go:405-408, rngOK false for tapes) so most tapes stop at the first
  shuffle. Optional `:tape:t<N>` prefixes cut at the end of turn N (`-tape-turns`, cmd/xengine/play.go:280-343).
- **`GameRequest` / `GameReplay`** (FO/GameRequest.java:13-33, FO/GameReplay.java:37-55): NOT part of
  the diff harness. Input `{"id","seats":[{"name","cards":[{"name","count"}]|"deck":path,"bridge","bridge_args"}],
  "max_turns":60,"max_ms":60000,"seed"}` (GameRequest.java:17-21, 72-116); output an outcome row
  `{engine,forge_ref,id,seed,seats:[{name,life,library,lost}],over,turns,reason,winner_seat,
  bridge_fallbacks,bridge_asks,decision_coverage,ms,game_sha}` (GameReplay.java:136-228). It lets
  Forge's AI (or a gorge bot over a stdio bridge, BridgeController.java:49-73) play a whole match.
  A new follower does not need it.

### 1.6 forge-export rewrites of xengine items (follower-relevant)

(forge.go:186-281)

- An `attack`/`block` that declares nothing becomes `pass_to decision:"priority"` (a no-op checkpoint).
- The `pass` immediately before a non-empty declaration becomes `pass_to decision:"attackers"|"blockers"`.
- gorge's own `attackers`/`blockers` decisions are dropped from gorge_decisions for those steps.
- Step indices never move, so `abilities`, `payment` and decisions stay aligned; the comparator
  matches checkpoints by index only (`sameStepCheckpoint`, compare.go:213-226), so a follower may
  label a rewritten step with its own op.
- `oraclegen.RetimeEntryTargets` moves an ETB trigger's target decision to the step where the
  follower is asked (forge.go:178). Divided allocation is a separate `choose_n` with
  `resume:"damage_split"` (forge.go:48-56, 151-156).

These rewrites encode Forge's priority model (Forge asks the declaration before the priority where a
gorge pass completes). A new follower may want them, or the raw item; that is a design choice for
its exporter.

### 1.7 Real trimmed examples

Hand fixture, template `setup` (FX/setup/requests.jsonl line 1, verbatim):

```json
{"abilities":{},"gorge_decisions":[],"id":"setup/gambit","item":{"id":"setup/gambit","card":"","template":"setup","ignore":["library_top"],"name":"setup-gambit","cr":["P1-2"],"why":"rules/oracle_run_setup_etb_test.go:41 TestOracleSetupPlacementFiresNoETB","setup":{"p0":{"battlefield":["Greed's Gambit"],"library":["Forest","Forest","Forest","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]},"scenario_sha":"784a3210e536bb0f2c7ff3fc21d2c446586dd629144525dbdcfdfdf4eb040df5"}
```

Level-A `cast-resolve` (FX/cast/requests.jsonl, verbatim):

```json
{"abilities":{},"gorge_decisions":[{"step":0,"seat":0,"kind":"target","options":1,"picks":["b"],"pick_idx":[0],"pick_refs":["p1"],"pick_kinds":["player"],"via":"target","gorge_kind":"target","first":"b","min":1,"max":1}],"id":"Ruthless Negotiation/cast-resolve/v1","item":{"id":"Ruthless Negotiation/cast-resolve/v1","card":"Ruthless Negotiation","template":"cast-resolve","name":"gen1-cast-resolve","cr":["601.2"],"why":"generated level-A scenario","setup":{"p0":{"hand":["Ruthless Negotiation"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Ruthless Negotiation","mana":"B","targets":["p1"]},{"op":"resolve","seat":0}]},"scenario_sha":"08bac75f4d6921b08996eb387730736eefe5b645c25a344be1606d70c1317260"}
```

Level-B `activate` with an `abilities` line (FX/cast/requests.jsonl):

```json
{"abilities":{"0":"AB$ Attach | Cost$ 3 | ValidTgts$ Creature.YouCtrl | TgtPrompt$ Select target creature you control | SorcerySpeed$ True | Keyword$ Equip | SpellDescription$ Equip 3"},"gorge_decisions":[{"step":0,"seat":0,"kind":"target","options":1,"picks":["Grizzly Bears (a)"],"pick_idx":[0],"pick_refs":["p0:Grizzly Bears"],"object_picks":["p0:Grizzly Bears"],"pick_kinds":["permanent"],"via":"target","gorge_kind":"target","first":"Grizzly Bears (a)","min":1,"max":1}],"id":"Lotus Ring/activate#0.0/v1","item":{"id":"Lotus Ring/activate#0.0/v1","card":"Lotus Ring","template":"activate#0.0","xmage_answers":[null,null],"xmage_ability":["Equip {3}",""],"name":"gen1-activate#0.0","cr":["602.2"],"why":"generated level-B scenario","setup":{"p0":{"battlefield":["Grizzly Bears","Lotus Ring"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Lotus Ring","mana":"CCC","targets":["p0:Grizzly Bears"],"ability_index":0},{"op":"resolve","seat":0}]},"scenario_sha":"bcff6d12f655780bf76b3d3610a529c78e90ff858725ba686c17b369ec1d55aa"}
```

xengine window (RUN/forge-req.jsonl, id xe:FDN:0:11:w2; library_top lists trimmed with "…"):

```json
{"id":"xe:FDN:0:11:w2","scenario_sha":"32b5be03…","item":{"id":"xe:FDN:0:11:w2","card":"","template":"xengine-window","payment":["","","R","","","","","","","","","","","","","",""],"name":"xengine window FDN-c0-g11-0-WR vs FDN-c0-g11-1-WG turn 2","turn":1,"cr":[],"why":"One turn of a recorded gorge game …","setup":{"p0":{"hand":["Burst Lightning","Mountain","Mountain","Deadly Riposte","Mountain","Mountain","Angel of Vitality","Mountain"],"library_top":["Valorous Stance","Plains","Hinterland Sanctifier", "… 32 cards in order …"],"life":20},"p1":{"battlefield":["Forest"],"hand":["Healer's Hawk","Skyknight Squire","Forest","Springbloom Druid","Twinblade Blessing","Cultivator's Caravan"],"library_top":["Joust Through","Plains", "… 33 cards …"],"life":20}},"steps":[{"op":"play","seat":0,"card":"p0:Mountain"},{"op":"activate","seat":0,"card":"p0:Mountain","ability_index":0},{"op":"cast","seat":0,"card":"p0:Burst Lightning","targets":["p1"]},{"op":"pass","seat":0},{"op":"pass","seat":1}, "… 12 more alternating passes …"]},"gorge_decisions":[{"step":2,"seat":0,"kind":"target","options":2,"picks":["b"],"pick_idx":[1],"pick_refs":["p1"],"pick_refs_inexact":[false],"pick_kinds":["player"],"via":"target","gorge_kind":"target","first":"a","min":1,"max":1}],"abilities":{"1":"intrinsic: basic land mana"}}
```

xengine whole-game tape (RUN/forge-req.jsonl, id xe:FDN:0:29:tape — the smallest; truncated at the
first shuffle, 4 steps):

```json
{"id":"xe:FDN:0:29:tape","item":{"template":"xengine-tape","turn":1,"payment":["","","G",""],
 "setup":{"p0":{"hand":["Mountain","Bushwhack","Forest","Forest","Swiftfoot Boots","Mountain","Forest"],"library_top":["Forest","Abrade","Strongbox Raider", "… whole library …"]},
          "p1":{"hand":["Swamp","Sanguine Indulgence","Thornweald Archer","Swamp","Cultivator's Caravan","Swamp","Aggressive Mammoth"],"library_top":["Swamp","Forest", "…"]}},
 "steps":[{"op":"play","seat":0,"card":"p0:Forest"},{"op":"activate","seat":0,"card":"p0:Forest","ability_index":0},
          {"op":"cast","seat":0,"card":"p0:Bushwhack","answers":[{"kind":"modes","pick":["p0:Bushwhack"]}]},{"op":"pass","seat":0}]},
 "gorge_decisions":[{"step":2,"seat":0,"kind":"mode","options":1,"picks":["Search your library for a basic land card, reveal it, put it into your hand, then shuffle."],"pick_idx":[0],"pick_refs":["p0:Bushwhack"],"pick_refs_inexact":[true],"option_refs":["p0:Bushwhack"],"object_picks":["p0:Bushwhack"],"pick_kinds":["mode"],"resume":"cast_modes","via":"answer","gorge_kind":"modes","first":"Search your library …","min":1,"max":1}],
 "abilities":{"1":"intrinsic: basic land mana"}}
```

---------------------------------------------------------------------------------------------------

## 2. Decision answering

### 2.1 What a gorge decision record holds

`rules.OracleDecision`, G/engine/rules/oracle_snapshot.go:92-220. Key fields:

- `step` (-1 = setup drive), `seat` (who was asked), `kind` (normalized cross-engine kind: `target`,
  `yesno`, `mode`, `choose_n`, `order`, `attackers`, `blockers`; oracleDecisionKind
  oracle_snapshot.go:239-255 — priority, mulligan and starting-player are never recorded),
  `gorge_kind` (raw: `choose`, `target`, `arrange`, `modes`, `trigger_optional`, `trigger_order`, ...).
- `options` (count), `picks` (labels), `pick_idx` (option indices), `pick_refs` (object refs `pN:Name#k`
  or player `pN`), `pick_refs_inexact` (ordinal counted, not bound), `object_picks` (selected object
  identities in submission order), `option_refs`, `option_groups`, `pick_kinds` (engine option kind of
  each pick, e.g. `permanent`, `player`, `mana`, `color`, `yes`, `discard`, `trigger`, `sacrifice`,
  `search`, `x`, `pay_G`, `activate`, `trigger_cost_pay`, `dig_bottom`), `resume` (e.g. `cast_modes`,
  `damage_split`, `look_ack`, `play`, `opp_pick`), `via` (`target`, `answer`, or a fallback), `first`,
  `min`, `max`, plus XMage helpers (`per_player`, `divided`, `unposed_slots`, `x_multi`,
  `copy_inherited`, `alt_payable`, `mana_colours`, `cost_posed`, ...).
- Measured in RUN/: kind choose_n 198, target 185, order 36, mode 25, yesno 13; top pick kinds
  permanent 173, mana 48, color 45, yes 29, discard 26, trigger 22, sacrifice 18, player 16, search 16.

The Forge reader keeps only step, seat, kind, gorge_kind, via, resume, picks, pick_refs, object_picks,
pick_kinds, min, max, divided (FO/DecisionQueue.java:26-57).

### 2.2 How references work

- Ref grammar (FO/RefTable.java:12-27, 70-106; gorge objRef G/engine/rules/oracle_snapshot.go:495-567):
  `pN` = seat N; `pN:Name` / `pN:Name#k` = a card OWNED by seat N; `pN:token:Sub#k` = the k-th
  battlefield token CONTROLLED by seat N whose name contains Sub (case-insensitive);
  `pN:ability:Name#k` = an ability on the stack (gorge snapshot only).
- Bound refs (from setup placement order) follow the card through every zone. An unbound ref is the
  k-th live object, in creation order, owned by seat N with that name (RefTable.java:143-171). The
  follower must create cards in gorge's order so its ids/creation order match (SetupBuilder.java:35-38).
  forge.go:57-59: "Name and owner are the identity; the driver resolves within the candidates Forge
  offers, which are one zone."
- Names match current or any face name (Forge collects every state name, StepMachine.java:644-662).

### 2.3 How the Forge driver maps decisions onto prompts

`DecisionQueue.take(step, predicate)`: first unconsumed decision of the current step (and, right after a
`pass_to` reached its named step, also the next step's, StepMachine.java:213-231) matching a
shape predicate (DecisionQueue.java:98-114). Mapping by Forge callback (ScriptedController.java
method list; StepMachine.java 922-1735):

- **targets** — by target SPEC, not log order: each Forge target ask takes, from this step's
  unconsumed target refs in log order, those it can legally target, up to its max
  (StepMachine.java:930-1007). Explicit empty answer to "up to" = no target. Up-to slot gorge never
  posed = left empty. Condition-gated sub-ability gorge settled false = inert first candidate.
  Divided: `damage_split` choose_n, picks repeat a target once per point (StepMachine.java:1079-1124).
- **yes/no** — `kind:"yesno"` or a choose_n with one yes/no-shaped pick, or pick_kind
  yes/no/opening_yes/trigger_cost_pay/decline (DecisionQueue.java:188-237). Empty answer declines.
- **modes** — `kind:"mode"`, matched by Forge SpellDescription text (StepMachine.java:1198-1223); generic
  choices by normalized prefix (1225-1261).
- **object picks** (search, discard, sacrifice, choose card) — by ref, then same name, carried across
  multiple Forge asks for one gorge decision (StepMachine.java:1321-1388; isObjectChoice
  DecisionQueue.java:243-256).
- **trigger order** — `order` with pick_kinds `trigger`: picks[0] goes on the stack FIRST (resolves
  LAST); Forge's list is resolution order so it is reversed (StepMachine.java:1443-1495).
- **scry/surveil/arrange** — `gorge_kind:"arrange"`: picks stay on top in pick order, the rest go to
  the pick_kind destination (StepMachine.java:1610-1643; forge.go:64-67).
- **X** — choose_n with pick kind `x` or label `X = n` (DecisionQueue.java:168-179).
- colour (`Add R`, "White"), card name, number, type, clone replacement, copy targets, optional costs
  (kicker etc. by cast_mode) — StepMachine.java:1272-1320, 1566-1735.
- **payment-window asks** (pick kinds `pay_<C>`, `pay_generic`, `pay_life`, `activate`) and `look_ack`
  are never counted as leftover (DecisionQueue.java:116-134). A logged `activate` pick inside the payment
  window (gorge tapped a source while paying) is replayed by activating that source BEFORE the cast so
  the mana floats (StepMachine.java:559-585).
- Deterministic answers where only one choice exists; combat damage assignment among >1 blockers,
  shield division and coin flips are strict misses (ScriptedController.java:774-786, 867-869).

### 2.4 Misses, leftovers, harness errors

- `StrictMiss` message `"Missing <what> answer at step <i>[: detail]"` (FO/StrictMiss.java:11-17).
  Strict attempt first; if the row's harness starts with `"Missing "`, the scenario is re-run LOOSE
  (the engine's AI answers) and the loose row is returned with `strict:false` and
  `strict_miss:<first message>` (FO/ScenarioReplay.java:56-79; StepMachine.java:194-201).
- `leftover`: `"unconsumed gorge decision(s): step S pN kind[pick_kinds] [picks]; ..."` for every
  decision never consumed except payment-window/look_ack (DecisionQueue.java:136-160; StepMachine.java:118-121).
- `HarnessError` (message always starts `harness: `): bad ref, unsupported op, cast refused, no
  progress after 4000 loop steps, turn cap, game ended early (FO/HarnessError.java:9-15;
  StepMachine.java:147-181). ANY exception for a line becomes that line's harness row and the batch
  continues (ScenarioReplay.java:20-27, 91-109). The row still carries every snapshot taken before the
  failure (StepMachine.java:122-129) — that is what makes the partial-stop rule work (3.4).
- How the harness reads them: strict_miss or leftover (when not a trailing Forge-only miss) =>
  class `decision_shape` (GC/oracle/xengine/classify/classify.go:154-156; trailingMiss 634-659).

### 2.5 Mana payment

- Scenario mana: a step's `mana` letters are added to the pool before the action (StepMachine.java:521-525,
  901-918). Pool only: the Forge driver never taps lands for mana itself (ScriptedController.java header
  "Mana is paid from the floating pool only").
- xengine: gorge's explore bot autopays (`EnableAutoPayMana`, GC/oracle/xengine/game/play.go:98); the
  lowering turns every mana ability fired into an explicit `activate` step with `ability_index` before the
  cast, with a colour answer for Any/Combo producers (lower/tape.go:640-693). So in xengine the follower
  must (a) activate mana abilities as standalone steps that complete immediately and (b) pay the cast
  from the floating pool.
- `payment[i]`: the exact multiset gorge spent; Forge spends those units first, then its own exact-colour
  pass, then WUBRGC order (ScriptedController.java:400-436). The harness code still believes only
  XMage consumes it (`consumesPayment`, classify.go:627-632), so a Forge pool split after a cast with
  ≥2 pool colours is classed `payment_choice` rather than `diverge` (classify.go:525-575). A new
  follower that consumes `payment` should be added to consumesPayment.
- Hybrid/Phyrexian pip picks (`pay_*`) are gorge payment-window asks the follower may ignore.

### 2.6 Mulligans, shuffles, library order, RNG

- No mulligans: Forge keeps every hand, a mulligan tuck is a harness error, no opening-hand actions
  (ScriptedController.java:1008-1030). Setup places cards directly. xengine tapes record the opening as the
  state at the first decision (hand in zone order, whole library top-first) (game/tape.go Opening;
  game/play.go:270-277); a card with an opening-hand effect refuses the window (gate.go:30-33, 125-131).
- Library order is fully specified for xengine (library_top = whole library) and IS compared
  (`pN.library_top`, compare.go:155-157) unless ignored.
- Shuffles / random picks: a tape stops at the first RNG-consuming ask (`unsupported_kind:rng`,
  tape.go:405-408). A window that shuffles gets `ignore:["library_top"]` (window.go:203-215); an ability
  whose label says "shuffle" adds `pN.library_top@K` from checkpoint K (tape.go:314-334, compare.go:178-202).
  A hand/graveyard/library_top difference after a recorded RNG draw is class `rng`, not `diverge`
  (classify.go:171, 700-758).
- Follower RNG: Forge seeds MyRandom from CRC32(id) so reruns are byte-identical apart from `ms`
  (FO/ForgeEngine.java:10-24). gorge's coin flips are recorded (`rules.OracleFlip`,
  oracle_snapshot.go:222-235) but Forge strict-misses a flip (ScriptedController.java:867-869).

---------------------------------------------------------------------------------------------------

## 3. Output: the result row and snapshot schema

### 3.1 Result row (one per input line, input order)

What the harness decodes is `oraclediff.XResult` (GC/oracle/compliance/oraclediff/compare.go:18-28) plus
`engine`:

```
{"name": str, "id": str, "harness": str?, "strict": bool, "strict_miss": str?, "leftover": str?,
 "ms": int, "snapshots": [Snapshot...], "engine": "forge", ...extra fields ignored}
```

Forge also writes `forge_ref`, `request_sha`, `notes` (FO/ResultRow.java:12-34, 75-104); caps: harness 800
chars, strict_miss/leftover 300 (ResultRow.java:21-22). xengine diff requires the `engine` field to equal
the follower name, and for `forge` requires it to be present (GC/cmd/xengine/diff.go:53-83). Rows over
256 MiB become harness rows rather than failing the chunk (diff.go:47-51; rowio.FollowerOversized
GC/oracle/xengine/rowio/rowio.go:100-110).

Real trimmed row (RUN/forge.jsonl, xe:FDN:0:11:w2, one of 18 snapshots shown):

```json
{"name":"xengine window FDN-c0-g11-0-WR vs FDN-c0-g11-1-WG turn 2","id":"xe:FDN:0:11:w2","strict":true,"ms":8,
 "snapshots":[{"checkpoint":"step 2 (cast)","turn":1,"step":"main1","active":0,"priority":0,"over":false,
   "players":[{"seat":0,"life":20,"hand":["Angel of Vitality","Deadly Riposte","Mountain","Mountain","Mountain","Mountain"],"graveyard":[],"exile":[],"command":[],"library_count":32,"library_top":["Valorous Stance","Plains","Hinterland Sanctifier","Crystal Barricade","Mountain"],"pool":""},
              {"seat":1,"life":20,"hand":["Cultivator's Caravan","Forest","Healer's Hawk","Skyknight Squire","Springbloom Druid","Twinblade Blessing"],"graveyard":[],"exile":[],"command":[],"library_count":33,"library_top":["Joust Through","Plains","Dryad Militant","Soulstone Sanctuary","Forest"],"pool":""}],
   "permanents":[{"name":"Mountain","controller":0,"owner":0,"token":false,"tapped":true,"face_down":false,"damage":0,"types":["Basic","Land","Mountain"],"colors":"","keywords":[],"attacking":false,"blocking":false},
                 {"name":"Forest","controller":1,"owner":1,"token":false,"tapped":false,"face_down":false,"damage":0,"types":["Basic","Forest","Land"],"colors":"","keywords":[],"attacking":false,"blocking":false}],
   "stack":[{"kind":"spell","source":"Burst Lightning","controller":0}]}],
 "engine":"forge","forge_ref":"ae39d915…","request_sha":"17cb5100…",
 "notes":["identity: basic-land at step 1","optional cost step 2: none of [Kicker – {4}] (gorge logged none)","target step 2: p1 -> p1","pay mana: pool 1 -> 0, paid=true for Burst Lightning"]}
```

### 3.2 Snapshot schema (per checkpoint)

gorge type `rules.OracleSnapshot` (G/engine/rules/oracle_snapshot.go:15-90); Forge writer
FO/SnapshotWriter.java:149-287 ("exactly the field set the XMage driver emits ... no offered, no decisions").

```
checkpoint  "setup" | "step <i> (<op>)"            (G/.../oracle_run.go:2572, 2591; StepMachine.java:262, 513)
turn        int (scenario turn; xengine: 1..)
step        gorge step name: untap upkeep draw main1 begin-combat declare-attackers declare-blockers
            combat-damage (both damage steps) end-combat main2 end cleanup   (SnapshotWriter.java:54-75)
active      seat int
priority    seat int                                (emitted, NOT compared)
over        bool
winner      int? (gorge only; not compared)
players[]   {seat, life, counters{KIND:n}? (player counters), hand[] (sorted), graveyard[] (zone order,
             oldest first), exile[] (sorted, cards OWNED by the seat), command[] (sorted; not compared),
             library_count, library_top[] (top first, at most 5), pool "WUBRGC letters", lost? (3+ seats)}
permanents[] {ref (gorge only), name ("" when face down), controller, owner, token, tapped, face_down,
             pt "P/T" (creatures only), damage, counters{KIND:n}?, types[] (core+super+sub, sorted),
             all_creature_types?, colors "WUBRG letters" ("" colourless), keywords[] (sorted),
             attached_to (host's CURRENT name, or "pN" for a player), attacking, blocking}
stack[]     top first: {kind "spell"|"ability", source (card name; gorge gives a ref), controller}
offered[]   gorge only, compared only with compare:["offered"]
```

Card names outside the battlefield: face-0 printed name; a face-down card in exile/library is still named;
a split/Room card by its first half (SnapshotWriter.java:133-145). Counter kinds: enum name (P1P1, M1M1,
LOYALTY, CHARGE, LORE) or display name upper-cased without spaces (SnapshotWriter.java:101-120).

### 3.3 What the comparator compares, and how it normalizes

`fields()` (compare.go:126-176) renders each snapshot to an ordered field list; `compareSnap` walks
them and returns the FIRST difference (compare.go:228-251). Compared fields, in order:

1. `turn`, `step` (follower side passed through XMageStep, which maps XMage enum names and leaves gorge
   names unchanged, compare.go:278-307), `active`, `over`, `players` (seat count).
2. per seat `pN.life`, `pN.counters` (`KIND=n` sorted; DEATHTOUCHED dropped, +1/+1→P1P1 etc.,
   compare.go:322-356), `pN.hand` (sorted list, or count with `hand_count`), `pN.graveyard` (ORDER
   matters, or count with `graveyard_count`), `pN.exile` (sorted), `pN.library_count`,
   `pN.library_top` (order; dropped with `no_library_order`), `pN.pool` (letters re-sorted WUBRGC),
   `pN.lost` only with 3+ seats.
3. `permanents`: each permanent rendered `c<ctrl> o<owner> <name|"token"> [<types lowercased,
   spaces removed, sorted>] {<WUBRG>} <P/T>? tapped? facedown? dmg=N? counters=...? kw=...?
   on=<host name>? attacking? blocking?`, the lines sorted and compared as a multiset (permKeysOpts
   compare.go:415-479). Tokens are anonymised to `token` and matched by characteristics. Keywords only
   with compare `keywords`/`keywords_named`/`keywords_hexproof_from` (folded to an evergreen vocabulary,
   keywords.go). A diverging `permanents` reports the symmetric difference (compare.go:243-246).
4. `stack`: `[kind:<source name>@pN, ...]` top first; source = RefName(ref) (strips `pN:`, `token:`,
   `#k`), an ability source uses the permanent's current name, Forge's ` (N)'s Effect` suffix stripped,
   the speed trigger's source blanked (compare.go:509-545); any token source compares as `Token`
   (offered.go:62-73).
5. `offered` only with compare `offered`.

NOT compared: `priority`, `winner`, `command`, permanent `ref`, stack `trigger`, `damage` of
non-permanents, `face_down` name for exile, colours outside WUBRG. At a checkpoint where BOTH sides
say `over`, only turn/step/active/over/players/`*.life` are compared (compare_gameover.go:5-21).

Ignore list: `item.ignore` entries match field names by SUFFIX (`library_top` ignores both seats);
`<suffix>@N` ignores from checkpoint index N (0 = setup) (compare.go:72-76, 178-211). xengine items get
ignore from the lowering only (tape.go:322-334, window.go:213); the level-A Forge path also adds
`library_top` when a seat names `library` cards (forge.go:641-656; xengine diff does not do this).

Checkpoint pairing: list lengths must be equal (else HARNESS "N gorge checkpoints, M xmage",
compare.go:108-110) and checkpoint i must have the same label or the same `step N` index
(compare.go:213-231). gorge-side fails take precedence: a gorge `harness:` fail = HARNESS gorge; a
gorge "unconsumed answer(s)" = DIVERGE field `decisions`; a gorge "step N (op): ..." = DIVERGE field
`action` (compare.go:92-107).

Card names: both sides use corpus (= Forge) spelling; XMage maps back via xmage_name. A follower whose
card DB spells a name differently must rewrite to the corpus spelling on output, as XMage's driver does
(gen.go:282-287).

### 3.4 Error/stop rows and how they are read

- **Harness row with partial snapshots** (`harness` set, snapshots so far). If gorge's result cut to the
  same number of checkpoints already DIVERGES, the difference is the finding (class `diverge`, msg "the
  driver then stopped: ...") — `classify.Partial` (classify.go:229-244, 126-130, 177-181). Otherwise
  `harness`.
- **Turn cap** (Forge only): StepMachine refuses to go past turn targetTurn+4 (TURN_SLACK, StepMachine.java:62-63,
  157-163) with "harness: passed turn 5 at step N". For a `:tape` id with message containing
  `"passed turn "`, class `turn_cap` (classify.go:146-147, 226-227). Measured: the most common Forge
  harness in RUN/ (e.g. "passed turn 5 at step 92").
- **No row** for an item: `harness` "no <engine> result"; an item xprep refused: `not_sent` (diff.go:210-215).
- **Game over before the last step**: harness "game ended at step i before every step ran"; game over
  ON the last step: a final checkpoint is still taken (StepMachine.java:168-180).
- Malformed line: harness row with the id regex-extracted (ScenarioReplay.java:91-101).

---------------------------------------------------------------------------------------------------

## 4. Harness integration points for a new follower

### 4.1 Sweep script (GC/scripts/xengine-sweep.sh)

- Follower list: `XENGINE_FOLLOWERS` default `'forge xmage'` (line 51, 111); whitelist `forge|xmage`
  (lines 122-124) — must be extended.
- `follower_rows()` case per follower (lines 193-213): optional prep (xprep / forge-export), `jvm_slot`
  wait (counts `pgrep -c -x java`, waits while ≥2, lines 163-176 — a Rust follower would need its own
  slot rule or none), then `heavy-lock.sh --wait --mem --name xengine-<f> -- <runner> IN OUT` (lines
  202-203, 209-210). Per-follower env `XENGINE_<F>_RUNNER`, `XENGINE_<F>_WAIT`, `XENGINE_<F>_MEM`.
- `finish_chunk()` (lines 237-261): for each follower: rows → `<chunk>/<f>.jsonl`, log `<f>.log`, then
  `xengine diff -play $d -engine $f -rows $d/$f.jsonl -out $d/diff-$f`; its `compared.tsv` joins the
  ledger merge. Adjudicate runs only when BOTH forge and xmage are followers (lines 251-255).
- `play_stage` passes `-xmage` only when xmage is a follower (line 226) — the analogous per-follower
  deck/token narrowing would need a flag.
- SUMMARY.md: per follower class histogram from `diff-$f/verdicts.jsonl` (lines 306-312).
- Output root must be outside the repo or under `.oracle-build/` (lines 126-131) (GPL output rule; same
  idea for any follower output).

### 4.2 Runner script conventions (model: GC/scripts/forge-oracle-run.sh)

- `RUNNER IN.jsonl OUT.jsonl` (+ `--compile`); exit 0 on success, non-zero (2 for setup errors via
  `die`) fails the chunk (forge-oracle-run.sh:1-36, 28). A per-row failure is NOT a non-zero exit: it
  is a harness row.
- Inputs split into `FORGE_ORACLE_CHUNK` (1000) lines per JVM, each in a systemd scope
  (`MemoryMax`, `CPUQuota=400%`; `XENGINE_SCOPE=none` disables), outputs concatenated in order,
  logs to `OUT.log` (lines 91-126).
- Compiled artefacts keyed by sha of driver source + classpath so worktrees never clobber each other
  (lines 70-85).
- Env passed to the driver: `FORGE_RES` (card scripts root), `FORGE_ORACLE_REF` (echoed as `forge_ref`)
  (lines 113-117).
- `heavy-lock.sh`: `[--wait S] [--mem M] [--name N] -- CMD`; delegates to `$HEAVY_LOCK heavy ...` when set,
  else just execs CMD (GC/scripts/heavy-lock.sh:1-17).

### 4.3 Pin invariant pattern

forge-oracle-setup.sh `--check-pin REF CLONE` (GC/scripts/forge-oracle-setup.sh:13-17, 39-60), called by
the runner before every run (forge-oracle-run.sh:61-64): gorge's FORGE_REF (corpus pin, read from gorge's
Makefile at the module go resolves) must be an ancestor of the driver ref AND `forge-gui/res/cardsfolder`
and `tokenscripts` trees identical at both; also no uncommitted/untracked script changes. Purpose: "a
script difference between gorge's corpus and Forge's runtime would look like an engine disagreement".
For phase.rs / Manabrew the analogue is: pin the follower's card-data revision and prove it corresponds to
gorge's corpus (or keep a name map + an "unknown card" class like `xmage_lacks`).

### 4.4 xengine diff (GC/cmd/xengine/diff.go)

- `-engine xmage|forge` whitelist (line 34). Reads items.jsonl, gorge.jsonl, lower.jsonl, the tape
  files, follower rows; XMage-only: xitems.jsonl, xprep.jsonl (not_sent) (lines 101-117, 150-162).
- Per lower row: refused → skipped; play/lower error → lower_gap; self-check gap → lower_gap; else
  Classify; credited classes add `Compared` counts to compared.tsv (lines 169-244, classify.Credited
  classify.go:661-671). Writes `verdicts.jsonl`, `compared.tsv`, `finds/<class>/...` repro dirs.
- Repro commands hard-code runner scripts per engine (GC/oracle/xengine/classify/repro.go:228-232).

### 4.5 Classify classes (GC/oracle/xengine/classify/classify.go:23-88, Classify 117-184)

Order tried: forge_skips_damage_step (Forge only, 135-141) → xmage_lacks → turn_cap (forge + `:tape`) →
harness → decision_shape → agree → Forge cleanup-stop harness (455-460) → Forge ignored cast_mode harness
(462-491) → XMage same-name trigger order harness (493-523) → payment_choice → rng → crew_type_dependency →
projection_gap (unmapped keyword `other:` or token-name-only difference, 760-799) → diverge.
Not in Classify: lower_gap (never sent), not_sent (diff.go). Engine-specific branches key on the literal
engine string (`engine == "forge"` / `"xmage"`, lines 123, 135, 144, 146, 150, 456, 480, 502, 632, 650):
each new follower needs its own entries (or a capability table) for SkipEarlyCasts-like priority quirks,
turn caps, consumesPayment, trailingMiss, cast-mode support lists.

- `forge_skips_damage_step` and `crew_type_dependency` are follower-quirk classes documented in
  GC/docs/oracles.md "xengine" section and classify.go:40-76.

### 4.6 Adjudication (three-way) and generalising to N followers

- Patterns (GC/oracle/compliance/oraclediff/threeway.go:16-44): `AGREE3`, `GF|X`, `FX|G` (gorge suspect),
  `GX|F`, `G|F|X`, `GF|–`, `G|F|–` (XMage harness/lacks), `F harness`, `INCONSISTENT`. Built from three
  pairwise CompareOpts legs gx, gf, fx (threeway.go:53-113).
- xengine wrapper GC/oracle/xengine/adjudicate/adjudicate.go: eligibility (one side diverged, the other
  agree/diverge/harness/not_sent/... , lines 47-76); legs cut to the partial stop (140-166) and, for
  G|F|X, to the first checkpoint either differs (90-108); F/X leg via ForgeOracleResult (118-121,
  threeway.go:146-153). Labels gorge_suspect / xmage_suspect / forge_suspect / three_way /
  gorge_forge_only (adjudicate.go:20-45). Inputs hard-wired to exactly xmage + forge
  (adjudicate/run.go:352-355; cmd/xengine/adjudicate.go:19-20). Output: adjudicate/findings.jsonl
  rows `{id, card, pattern, label, xmage_class, forge_class, gx_field, gf_field, fx_field}`
  (run.go:40-50), notes.md, patterns.tsv, ticket drafts.
- dirac adds a fourth column `D=G | D!=G | D-` beside the three-way pattern (docs/oracles.md xengine
  section; DiracRow run.go:72-91) — already a precedent for "an extra engine beside the pattern"
  rather than a new pattern table.
- Generalising: pairwise equality of normalized fields is transitive (threeway.go:97-99), so for N
  followers the natural generalisation is a partition of {gorge, f1..fn} into agreement classes over
  the compared stretch (cut to min partial stop / first difference), with "gorge alone vs all
  followers agreeing" as the gorge-suspect signal and harness/not_sent followers dropped from the
  partition. The current string patterns are the n=2 case.

### 4.7 xprep / which items a follower is sent

- Forge is sent EVERY lowered item (items.jsonl → forge-export); XMage only xprep-prepared windows
  (tapes refused `xmage_window_only` unless `XENGINE_XMAGE_TAPES=1`), and items with an XMage-unknown
  card are refused `xmage_lacks_card` (GC/oracle/xengine/xprep/xprep.go:30-75, 107-152;
  cmd/xengine/xprep.go:18-27, 90-91). Refusals → `<chunk>/xprep.jsonl` → class `not_sent` in diff.
- The equivalent for a new follower: a prep/export stage that refuses items naming a card or token
  stem the follower lacks, writing `{id, reason, detail}` refusals that diff reads as not_sent.
- Token stems XMage can place are a closed list in lower/tokens.go:21-75 (`xmageSetupTokens`); when
  XMage follows, windows with another setup token stem are REFUSED at lowering (`token_unmapped:`,
  tokens.go:13-19; window.go:33-36). A per-follower token table would follow the same pattern.

### 4.8 Coverage ledger

GC/oracle/xengine/cov/ledger.go:14-17, 107-127: TSV `card\tkey\texercised\tcompared\toffered`; keys
`cast`/`land`/`f<face>/a<i>`/`f<face>/t<i>` (inventory.go:13-29). play writes exercised.tsv; each
`diff-<f>/compared.tsv` adds `Compared` for credited classes; the sweep merges deltas atomically
(xengine-sweep.sh:81-88, 256-259). Decks are drawn weighted 1/(1+uses) from it (decks.go:328-353).
"Compared" counts per follower are summed, so a new follower raises the counter without a schema change.

---------------------------------------------------------------------------------------------------

## 5. Deck and game generation

- Candidates for a set: the set manifest (`oracle/compliance/manifests/<SET>.json`) minus unfinished,
  resolved to corpus spelling, kept only if gorge fully supports the card (`reg.Unsupported`), basics
  skipped; `-xmage` also drops cards XMage spells differently (GC/oracle/xengine/decks/decks.go:52-139).
- `CandidatesNamed(reg, names, opt)`: the same filter over an arbitrary name list
  (decks.go:73-81). Used only by `play -random ... -forge-only FILE` (one name per line) to add the
  names to the cross-set union (GC/cmd/xengine/play.go:88, 132-140, 191-236; sweep env
  XENGINE_FORGE_ONLY with XENGINE_RANDOM_SETS, xengine-sweep.sh:69-71, 230). Note: `-forge-only`
  requires `-random`; the cards must still be gorge-supported; and items holding them are not sent to
  XMage (`xmage_lacks_card`). For a Pauper-ish pool: put the commons list in a file and run
  `-random <sets> -forge-only pool.txt`; there is no "named pool only" mode today — the union always
  includes the `-random` sets' candidates (play.go:221). formats.json has keys
  `comment, formats, non_tournament_*` and no Pauper (checked).
- Pair: two 40-card two-colour decks: 23 distinct spells (without replacement, weight 1/(1+uses)) +
  17 basics split by pip ratio (7..10 each), colour pair rotating `(7*chunk+2*game+i) mod 10` skipping
  pairs with <23 eligible cards; pure function of (set, chunk, game, ledger, seed)
  (decks.go:41-46, 266-326). A pool needs ≥23 eligible cards in some colour pair (Pairable 223-237).
- Game seed: `seed*1_000_003 + chunk*10_007 + g` (play.go:260-262). Players: two
  `heuristic.NewExploreBot(seed ^ 0x9e3779b97f4a7c15*(i+1)).EnableAutoPayMana()` (game/play.go:68-72, 98).
  Caps: 30 turns, 20000 intents; a stall is recorded, not an error (game/play.go:21-25, 74-140).
- Per game, items emitted: the whole-game tape (lowered to its longest lowerable prefix), optional
  `:tape:t<N>` prefixes, one window per turn (each turn's first main-1 priority of the active player)
  unless refused (game/play.go recorder submit 270-283; lower/window.go:27-69; cmd/xengine/play.go:418-470).
  Docs: a 2-game FDN sweep refused 25 of 46 windows (GC/docs/oracles.md xengine "Can't check");
  RUN/ (50 games): 526 windows lowered, 456 refused, 48 tapes.
- Sweep defaults: sets `FDN BLB`, 4 chunks, 50 games, seed 1 (xengine-sweep.sh:94); Makefile
  `make xengine-sweep SETS=FDN CHUNKS=1 GAMES=2`.

---------------------------------------------------------------------------------------------------

## 6. What lowering refuses (the follower only sees lowered positions)

### 6.1 Window refusals (cut state setup cannot place) — GC/oracle/xengine/lower/gate.go, gate_table.go

Every exported field of state.Game / Player / Object is classified placed/derived/ignored/refuse; an
unclassified field is itself a refusal (gate.go:298-318; gate_table.go:1-44). Refused when live:

- Game: non-empty stack, game over, extra turns/phases, skipped turns, day/night, monarch, initiative,
  controlled player, delayed triggers, this-turn zone entries (when read), ResolvedThisTurn (when read),
  excess-damage history (gate_table.go:51-91).
- Player: lost, land drops this turn (active player), drew from empty, this-turn tallies (spells cast,
  life gained/lost, tokens created, attacks, coin flips), non-empty mana pool, restricted/persistent/combat
  mana, player counters (poison, energy, experience), commanders, dungeons, the Ring, city's blessing
  (gate_table.go:100-133).
- Object: copy effects, face-down / manifest / cloak / morph state, phased out, marked damage, Class level,
  merged/mutated/melded, entered this turn (always for battlefield), this-turn damage records, attack /
  exert / crew / saddle turn stamps, combat state (attacking/blocking), text changes, goad, suspect,
  monstrous, renowned, solved, plotted, suspend, remembered/imprint/cipher/exile links (when read),
  chosen name/type/number/modes, intrinsic keywords granted at entry, Room door state, attached to a
  player, soulbond, AbilityFace (gate_table.go:140-327).
- Continuous effects from resolved spells/abilities still live (`field:Engine.continuous`, gate.go:133-141).
- Special reasons (gate.go:18-66, 161-241, 441-465, 683-716): upkeep/draw phase triggers on the
  battlefield (`trigger:upkeep_or_draw`), mixed back_face/counters among same-named placements
  (`setup_by_name:`), counters below entry or dynamic entry counters, cards in other zones
  (`zone:` e.g. command, stack), no front face, unattached battlefield Aura, opening-hand-effect card in
  hand, self-exiled (warp) card, speed latch, non-active untapped card whose ETB replacement would tap it
  (`setup_entry_tap`), a placed permanent that triggers on setup's own Tap / CounterAdded / LifeGained /
  LifeLost events (`setup_trigger:*`), controller != owner, tokens that are copies/face-down/made a
  choice/lack a stem (`token_unscripted`, `token_choice`), and when XMage follows, unmapped stems
  (tokens.go:13-19).

### 6.2 Tape/window step truncations (lower/tape.go:19-28, 395-430, 535-727)

`attack_split`, `attack_nonplayer`, `unbindable_ref`, `stall`, `unsupported_kind:<k>` for: RNG asks on
tapes, re-answered intents, `_rest` intents, a pass that leaves a non-settleable ask pending
(`ask_after_pass:<k>`), mana fired during an ask for a non-cast decision (`ask_mana`), granted/triggered
mana abilities (`mana_ability`), priority picks other than pass/play_land/cast/ability/activate, multi-pick
priority, `prelude_<kind>`, `no_main1`. A no-op cast/activate (state and stack unchanged) is dropped
(tape.go:209-222).

### 6.3 Consequently the minimum construct set a follower must support

From the lowering's output vocabulary and RUN/ census:

- Setup: two seats; hand, library_top (whole library, ordered), battlefield (untapped and tapped,
  by-name `tapped`, per-object `tapped_after_untap`), graveyard (ordered), exile, life, counters (added
  over entry counters), back_face, tokens by Forge tokenscript stem with full counter sets, Auras via
  `attached`, Equipment via `attach` prelude steps; speed. No summoning sickness for setup permanents;
  no setup triggers; turn 1 begins with p0 active, no draw for p0 on turn 1, and the setup checkpoint at
  p0's first main-1 priority.
- Steps: play (land), activate by gorge ability_index (incl. basic-land mana and other mana abilities as
  standalone steps that resolve immediately; colour answers for Any/Combo), cast (ordinary; Adventure/
  modal/kicker/etc. via cast_mode; flashback appears), pass (exactly one priority, correct seat), pass_to
  (`priority`/`attackers`/`blockers` decisions), resolve, attack (all attackers at one player), block
  (pairs), attach.
- Decisions: targets (players, permanents, multiple, up-to, divided), modes, yes/no (optional triggers,
  trigger costs), choose_n (sacrifice, discard, search, colour, type, X, piles), trigger order, arrange
  (scry/surveil/dig), cleanup discard.
- Priority model exactly as gorge's: every pass is one priority decision; a priority is given in each step
  CR gives one (Forge's skipped second damage step is the canonical counter-example).
- Snapshots after every step, with the fields of 3.2.

---------------------------------------------------------------------------------------------------

## 7. Smallest end-to-end path

### 7.1 Common minimum (both a and b)

1. A batch binary `follower IN.jsonl OUT.jsonl` that reads the sidecar line shape (1.2), writes one row per
   line in order, flushes per row, never aborts the batch on one line, exit 0.
2. Row = `{"id","name","engine":"<name>","strict":true,"ms":0,"snapshots":[...]}` plus `harness` /
   `strict_miss` / `leftover` when relevant (3.1).
3. Snapshot writer in gorge vocabulary (3.2): step names, WUBRG colours, WUBRGC pool, counter kinds,
   sorted hand/exile, ordered graveyard, library_top top-5, types incl. subtypes, P/T for creatures,
   damage, tapped, attached_to host name, attacking/blocking, stack top-first by card name.
4. Setup placement from `setup` with gorge's object order (1.3), no triggers, no sickness, turn 1 p0
   active, snapshot `"setup"` at the first main-1 priority.
5. Ref resolution `pN`, `pN:Name#k`, `pN:token:Sub#k` (2.2).
6. Card-name parity with gorge's corpus (Forge spellings), and an "I lack this card" harness message
   the classifier can recognise (today only XMage's `Couldn't find a card:` is, compare.go:55-67).
7. Gorge-compliance side: add the engine name to `xengine diff -engine` and readFollowerRows
   (diff.go:34, 53-83), to xengine-sweep.sh's follower whitelist and `follower_rows` case, a runner
   script, repro.go's runner map; decide its classify quirks (consumesPayment etc.); an exporter that
   writes the sidecar (forge-export works as is if the new follower accepts the same shape —
   only the `abilities` field is Forge-specific).

### 7.2 (a) level-A `setup` and `cast-resolve`

- `setup` fixtures (FX/setup): steps `[]` — only placement + the `"setup"` snapshot. Must get
  battlefield placement without ETB, counters/tapped/back_face/tokens/attached for the richer fixtures,
  library filler (Wastes to 40 cards, library_top first, named library under the filler), and
  library_top ignored when `library` is named (the fixtures carry `ignore:["library_top"]`).
- `cast-resolve` (templates/cast.go:11-13): setup p0 hand = the card, opponent fixture (e.g. p1 Grizzly
  Bears); steps `cast` (with `mana` letters to add to the pool, `targets`) then `resolve`. Needs: pool
  mana from the step, cast from hand paying ONLY from pool, target binding from gorge_decisions
  `target` records (`pick_refs`), X from `choose_n` `X = n`, charm modes by label, then resolve until the
  stack is empty and the active player has priority. Three checkpoints: setup, `step 0 (cast)` (spell on
  stack, pool spent), `step 1 (resolve)`.
- Diff path: `oraclediff gen -level A -manifest .../FDN.json -out scen.jsonl` →
  `oraclediff forge-export -scenarios scen.jsonl -out req.jsonl` → follower → today's
  `oraclediff forge-diff -req req.jsonl -forge rows.jsonl -cache DIR -out diff.jsonl`
  (forge.go:485-639) — forge-diff insists `engine == "forge"` (forge.go:570-573), so either a generic
  `follower-diff` or an engine flag is needed. The hand-fixture harness FX/../scripts/test-fixtures.sh
  shows the minimal loop (`FORGE_ORACLE_CMP cmp -scen S -forge F`).

### 7.3 (b) one xengine whole-game FDN prefix

- Produce it: `xengine play -set FDN -chunk 0 -games 1 -seed 1 -out DIR [-tape-turns 2]` writes
  items.jsonl/gorge.jsonl/lower.jsonl/tapes/ (cmd/xengine/play.go:75-148, 345-492);
  `oraclediff forge-export -scenarios DIR/items.jsonl -out DIR/req.jsonl`; run the follower;
  `xengine diff -play DIR -engine <f> -rows DIR/<f>.jsonl -out DIR/diff-<f>` (after adding `<f>`).
  A `:tape:t2` prefix is the smallest whole-game item that still covers two turns.
- Must additionally support: hand + full ordered library setup with no battlefield; real turn
  structure from turn 1 (untap, upkeep, draw — p1 draws on its first turn, p0 does not on turn 1),
  land plays, explicit mana-ability `activate` steps keyed by gorge ability_index, casting from the pool
  with `payment` units, `pass` as exactly one priority per step with gorge's priority sequence through
  every step (including combat steps), pass_to attackers/blockers + attack/block declarations,
  cleanup discard, combat damage, SBAs, and enough turns: no turn cap below the tape's length (Forge's
  turn-1+4 cap is why tapes are `turn_cap` there).
- ability_index identity: gorge's index is into gorge's compiled Face.Abilities (Forge script order;
  gen.go:149-154; tape.go:623-624, 653). Forge resolves it through the `abilities` script line with a
  4-tier match (StepMachine.java:753-876). A Rust follower that does not load Forge scripts needs an
  exporter-side key it can match (e.g. the ability's cost + effect text, or "mana: produces X"), or an
  index table built from the same Forge script order if its card data derives from Forge.
