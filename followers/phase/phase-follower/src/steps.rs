//! The step machine: replays `item.steps` after setup for the ops `mana play activate cast pass
//! resolve pass_to attack block attach move`, with gorge's priority model and checkpoints.
//!
//! Each step ends with the checkpoint `step <i> (<op>)`, a snapshot (Forge's semantics, which
//! gorge's runner shares). Every phase action goes through [`Machine::act`], the one place that
//! counts `apply` calls (loop guard), picks the actor (`waiting_for.acting_player()`), turns a
//! reversed activation/cast into a strict miss and any other engine error into a harness row.
//! Prompts that are not a priority window go to [`crate::answer`]; the one prompt the machine
//! answers itself is `ManaPayment` (pin the pool units gorge spent, then finalise).

use std::cell::Cell;

use engine::ai_support::legal_actions_full;
use engine::game::combat::AttackTarget;
use engine::game::game_object::AttachTarget;
use engine::game::layers::flush_layers;
use engine::game::{apply, EngineError};
use engine::types::ability::TargetRef;
use engine::types::actions::{DebugAction, GameAction};
use engine::types::game_state::{ActionDisposition, ActionResult, CastPaymentMode, GameState, WaitingFor};
use engine::types::identifiers::ObjectId;
use engine::types::mana::{ManaCost, ManaCostShard, ManaPipId, ManaType};
use engine::types::phase::Phase;
use engine::types::player::PlayerId;
use engine::types::zones::Zone;
use follower_proto::batch::{RowBuilder, Stop};
use follower_proto::decisions::DecisionQueue;
use follower_proto::refs::Ref;
use follower_proto::request::{NeutralAbility, Request, Step};
use follower_proto::row::strict_miss_msg;
use follower_proto::vocab;
use serde_json::Value;

use crate::answer::{answer, variant_name, Cursor};
use crate::setup::Board;
use crate::snapshot::{snapshot, step_name};

/// Cap on `apply` calls inside one step.
const MAX_APPLY: usize = 4000;
/// Pool order of the generic remainder of a payment.
const WUBRGC: [ManaType; 6] =
    [ManaType::White, ManaType::Blue, ManaType::Black, ManaType::Red, ManaType::Green, ManaType::Colorless];

fn harness(msg: impl Into<String>) -> Stop {
    Stop::Harness(format!("phase follower: {}", msg.into()))
}

fn unmatched(step: usize, detail: impl std::fmt::Display) -> Stop {
    harness(format!("ability_unmatched at step {step}: {detail}"))
}

fn letter(t: ManaType) -> char {
    match t {
        ManaType::White => 'W',
        ManaType::Blue => 'U',
        ManaType::Black => 'B',
        ManaType::Red => 'R',
        ManaType::Green => 'G',
        ManaType::Colorless => 'C',
    }
}

fn mana_type(c: char) -> Option<ManaType> {
    WUBRGC.iter().copied().find(|t| letter(*t) == c.to_ascii_uppercase())
}

/// The units of a step's `mana` text: a run of digits is that many colourless, a letter of
/// `WUBRGC` one unit of that type.
fn parse_mana(text: &str) -> Result<Vec<ManaType>, Stop> {
    let mut out = Vec::new();
    let mut digits = String::new();
    let flush = |digits: &mut String, out: &mut Vec<ManaType>| -> Result<(), Stop> {
        if !digits.is_empty() {
            let n: usize = digits.parse().map_err(|_| harness(format!("mana {text:?} out of range")))?;
            if n > 1000 {
                return Err(harness(format!("mana {text:?} out of range")));
            }
            out.extend(std::iter::repeat(ManaType::Colorless).take(n));
            digits.clear();
        }
        Ok(())
    };
    for c in text.chars() {
        if c.is_ascii_digit() {
            digits.push(c);
        } else if let Some(t) = mana_type(c) {
            flush(&mut digits, &mut out)?;
            out.push(t);
        } else {
            return Err(harness(format!("mana {text:?} unsupported")));
        }
    }
    flush(&mut digits, &mut out)?;
    Ok(out)
}

/// The `cast_mode` values the answerer acts on, exactly the registry's `CastModes` for phase
/// (oracle/xengine/follower/follower.go). The mode only steers the prompts a cast raises
/// (optional costs, casting variants, faces, alternative costs); any other mode is a harness row.
/// `plot` is not here: gorge's `plot` is the Plot special action (the card is exiled, plotted),
/// while phase's plot variant casts a card already plotted, so a hand cast cannot replay it.
pub const CAST_MODES: [&str; 15] = [
    "", "optionalcost", "adventure_alt", "omen_alt", "modal_spell", "warped", "kicked", "kicked1", "kicked2",
    "kickedboth", "buyback", "entwined", "offspring", "replicated", "multikicked",
];

/// What the machine does at a priority window while driving a step.
enum Prio {
    Done,
    Act(GameAction),
    Fail(String),
}

struct Machine<'a> {
    req: &'a Request,
    board: Board,
    queue: DecisionQueue,
    /// `apply` calls in the current step.
    calls: usize,
    step: usize,
    last: bool,
    debug_noted: bool,
    /// The note for `attach`/`move` was written (once per row, like `debug_noted`).
    edit_noted: bool,
    /// The previous step was a `pass_to` that reached its named step: the queue's lookahead stays
    /// on through the next step (see `run`).
    carry_lookahead: bool,
    /// Indices of the mode decisions the answerer has taken (the key of `neutral.mode`).
    used: std::collections::BTreeSet<usize>,
    /// Stack entries whose chosen targets have been matched against gorge's target decisions.
    swept: std::collections::BTreeSet<ObjectId>,
    /// Targets the answerer chose through prompts, not yet matched to the stack entry they made.
    answered: Vec<TargetRef>,
    /// Whether an answer arm acted on the current cast step's `cast_mode`.
    honoured: bool,
}

/// Replay every step of `req` on `board`, pushing one snapshot per step to `rb`.
pub fn run(req: &Request, board: Board, rb: &mut RowBuilder) -> Result<(), Stop> {
    let mut m = Machine {
        req,
        board,
        queue: DecisionQueue::new(req.gorge_decisions.clone()),
        calls: 0,
        step: 0,
        last: false,
        debug_noted: false,
        edit_noted: false,
        carry_lookahead: false,
        used: std::collections::BTreeSet::new(),
        swept: std::collections::BTreeSet::new(),
        answered: Vec::new(),
        honoured: false,
    };
    let n = req.item.steps.len();
    for (i, step) in req.item.steps.iter().enumerate() {
        m.step = i;
        m.last = i + 1 == n;
        m.calls = 0;
        if !std::mem::take(&mut m.carry_lookahead) {
            m.queue.set_lookahead(false);
        }
        if matches!(m.board.state.waiting_for, WaitingFor::GameOver { .. }) {
            return Err(m.game_ended());
        }
        m.one_step(step, rb)?;
        flush_layers(&mut m.board.state);
        rb.push_snapshot(snapshot(&m.board.state, &format!("step {i} ({})", step.op), &m.board.names));
    }
    if let Some(l) = m.queue.leftover() {
        rb.leftover = Some(l);
    }
    Ok(())
}

impl Machine<'_> {
    fn state(&self) -> &GameState {
        &self.board.state
    }

    fn game_ended(&self) -> Stop {
        harness(format!("game ended at step {} before every step ran", self.step))
    }

    /// The live object a step's card ref names. A card ref follows its object through zone changes
    /// (the id survives; only the incarnation changes), unlike `Board::resolve`, which pins one.
    fn object(&self, card: &str, seat: i64) -> Result<ObjectId, Stop> {
        let text = if card.starts_with('p') && card[1..].starts_with(|c: char| c.is_ascii_digit()) && card.contains(':') {
            card.to_string()
        } else {
            format!("p{seat}:{card}")
        };
        let r = Ref::parse(&text).map_err(|e| harness(format!("step {}: {e}", self.step)))?;
        let found = match &r {
            Ref::Card { .. } => self
                .board
                .refs
                .bound(&r)
                .map(|(id, _)| id)
                .filter(|id| self.state().objects.contains_key(id)),
            _ => self.board.resolve(&r),
        };
        found.ok_or_else(|| harness(format!("step {}: {r} names no object", self.step)))
    }

    /// Apply `action` as the prompt's acting player. `what` names the miss on a reversal.
    fn act(&mut self, action: GameAction, what: &str) -> Result<ActionResult, Stop> {
        self.calls += 1;
        let i = self.step;
        if self.calls > MAX_APPLY {
            return Err(harness(format!("no progress at step {i}")));
        }
        let Some(actor) = self.board.state.waiting_for.acting_player() else {
            return Err(harness(format!("no acting player at step {i}: {}", variant_name(&self.state().waiting_for))));
        };
        let reversed = || Stop::StrictMiss(strict_miss_msg(what, i as i64, "reversed"));
        match apply(&mut self.board.state, actor, action) {
            Ok(r) if r.disposition == ActionDisposition::Reversed => Err(reversed()),
            Ok(r) => Ok(r),
            Err(EngineError::ActivationReversed { .. } | EngineError::SpellCastReversed { .. }) => Err(reversed()),
            Err(e) => Err(harness(format!("engine error: {e}"))),
        }
    }

    /// Apply the debug `action` (debug mode on for this action only).
    fn debug(&mut self, action: DebugAction) -> Result<(), Stop> {
        self.calls += 1;
        if self.calls > MAX_APPLY {
            return Err(harness(format!("no progress at step {}", self.step)));
        }
        let state = &mut self.board.state;
        state.debug_mode = true;
        let r = apply(state, PlayerId(0), GameAction::Debug(action));
        state.debug_mode = false;
        r.map(|_| ()).map_err(|e| harness(format!("engine error: {e}")))
    }

    /// Add `units` to `seat`'s pool with the debug action (debug mode on for this action only).
    fn add_mana(&mut self, seat: u8, units: Vec<ManaType>, rb: &mut RowBuilder) -> Result<(), Stop> {
        if units.is_empty() {
            return Ok(());
        }
        self.debug(DebugAction::AddMana { player_id: PlayerId(seat), mana: units })?;
        if !self.debug_noted {
            self.debug_noted = true;
            rb.note("mana added to the pool by a debug AddMana (debug mode on for that action only)");
        }
        Ok(())
    }

    fn one_step(&mut self, step: &Step, rb: &mut RowBuilder) -> Result<(), Stop> {
        let i = self.step;
        let seat = u8::try_from(step.seat).ok().filter(|s| *s < 2).ok_or_else(|| harness(format!("step {i}: seat {} out of range", step.seat)))?;
        match step.op.as_str() {
            "mana" => {
                let units = parse_mana(&step.mana)?;
                self.add_mana(seat, units, rb)
            }
            "play" => {
                self.need_priority(seat)?;
                let id = self.object(&step.card, step.seat)?;
                let card_id = self.state().objects[&id].card_id;
                self.act(GameAction::PlayLand { object_id: id, card_id }, "play")?;
                self.drive(rb, |s| {
                    if s.objects.get(&id).is_some_and(|o| o.zone == engine::types::zones::Zone::Battlefield) {
                        Prio::Done
                    } else {
                        Prio::Fail("play: the land did not reach the battlefield".into())
                    }
                })
            }
            "cast" => self.cast(step, seat, rb),
            "activate" => self.activate(step, seat, rb),
            "pass" => {
                let holder = match &self.state().waiting_for {
                    WaitingFor::Priority { player } => *player,
                    other => {
                        return Err(harness(format!("pass: no priority window at step {i} ({})", variant_name(other))));
                    }
                };
                if holder != PlayerId(seat) {
                    return Err(harness(format!(
                        "pass: p{seat} does not hold priority at step {i} (p{} does)",
                        holder.0
                    )));
                }
                self.act(GameAction::PassPriority, "pass")?;
                self.drive(rb, |_| Prio::Done)
            }
            "resolve" => self.drive(rb, |s| match &s.waiting_for {
                WaitingFor::Priority { player }
                    if s.stack.is_empty() && *player == s.active_player && s.phase != Phase::Cleanup =>
                {
                    Prio::Done
                }
                _ => Prio::Act(GameAction::PassPriority),
            }),
            "life" => Err(harness(format!("op life is a gorge-runner-only op (step {i})"))),
            "pass_to" => self.pass_to(step, rb),
            "attack" => self.attack(step, seat, rb),
            "block" => self.block(step, seat, rb),
            "attach" => self.attach(step, rb),
            "move" => self.move_card(step, rb),
            op => Err(harness(format!("unknown op {op:?} at step {i}"))),
        }
    }

    /// `pass_to`: gorge passes (and declares nothing) until the named decision is pending or,
    /// after at least one action, the named step is reached on the named active seat. A
    /// `priority` decision is satisfied by a priority window at once, with no pass.
    fn pass_to(&mut self, step: &Step, rb: &mut RowBuilder) -> Result<(), Stop> {
        let i = self.step;
        let decision = match &step.decision {
            Value::Null => "",
            Value::String(d) => d.as_str(),
            other => return Err(harness(format!("pass_to: decision {other} at step {i} is not a name"))),
        };
        if !matches!(decision, "" | "priority" | "attackers" | "blockers") {
            return Err(harness(format!("pass_to: decision {decision:?} unsupported at step {i}")));
        }
        let want = match step.step.as_str() {
            "" => None,
            name => Some(
                vocab::STEP_NAMES
                    .iter()
                    .position(|n| *n == name)
                    .ok_or_else(|| harness(format!("pass_to: unknown step {name:?} at step {i}")))?,
            ),
        };
        if decision.is_empty() && want.is_none() {
            return Err(harness(format!("pass_to: names neither a decision nor a step (step {i})")));
        }
        let active = seat_of(&step.active).map_err(|e| harness(format!("pass_to: active {e} at step {i}")))?;
        let turn = self.state().turn_number;
        let acted = Cell::new(0usize);
        let by_step = Cell::new(false);
        let check = |s: &GameState| -> Option<Prio> {
            let kind = decision_kind(&s.waiting_for);
            if !decision.is_empty() && kind == decision {
                return Some(Prio::Done);
            }
            if let Some(w) = want {
                let at = vocab::STEP_NAMES[w] == step_name(s.phase);
                if acted.get() > 0 && at && active.map_or(true, |a| s.active_player == a) {
                    by_step.set(true);
                    return Some(Prio::Done);
                }
            }
            // The declaration was passed without the prompt (nothing could attack or block):
            // stop with a row naming it, never spin on into later turns.
            let last = match decision {
                "attackers" => Some(Phase::DeclareAttackers),
                "blockers" => Some(Phase::DeclareBlockers),
                _ => None,
            };
            if let Some(last) = last {
                if s.phase > last || s.turn_number != turn {
                    return Some(Prio::Fail(format!("pass_to {decision}: no declaration prompt by {}", step_name(s.phase))));
                }
            }
            None
        };
        let r = self.drive_with(
            rb,
            |s| {
                check(s).unwrap_or_else(|| {
                    acted.set(acted.get() + 1);
                    Prio::Act(GameAction::PassPriority)
                })
            },
            |s| {
                check(s).unwrap_or_else(|| {
                    acted.set(acted.get() + 1);
                    Prio::Act(empty_declaration(&s.waiting_for))
                })
            },
        );
        r?;
        if by_step.get() {
            self.queue.set_lookahead(true);
            self.carry_lookahead = true;
        }
        Ok(())
    }

    /// `attack`: gorge answers decisions (passing priority) until the attackers decision of `seat`
    /// is pending, declares `step.attackers` at `step.defender`, and plays on to the next priority.
    fn attack(&mut self, step: &Step, seat: u8, rb: &mut RowBuilder) -> Result<(), Stop> {
        let i = self.step;
        let turn = self.state().turn_number;
        self.drive_with(
            rb,
            |s| {
                if s.phase > Phase::DeclareAttackers || s.turn_number != turn {
                    Prio::Fail(format!("attack: no DeclareAttackers prompt (the game passed {})", step_name(s.phase)))
                } else {
                    Prio::Act(GameAction::PassPriority)
                }
            },
            |s| match &s.waiting_for {
                WaitingFor::DeclareAttackers { player, .. } if *player == PlayerId(seat) => Prio::Done,
                wf => Prio::Act(empty_declaration(wf)),
            },
        )?;
        match &self.state().waiting_for {
            WaitingFor::DeclareAttackers { player, .. } if *player == PlayerId(seat) => {}
            other => {
                return Err(harness(format!("attack: no DeclareAttackers prompt at step {i} ({})", variant_name(other))));
            }
        }
        let mut attacks = Vec::new();
        if !step.attackers.is_empty() {
            let defender = seat_of(&Value::String(step.defender.clone()))
                .ok()
                .flatten()
                .ok_or_else(|| harness(format!("attack: defender {:?} at step {i} is not a seat", step.defender)))?;
            for a in &step.attackers {
                attacks.push((self.object(a, step.seat)?, AttackTarget::Player(defender)));
            }
        }
        self.act(GameAction::DeclareAttackers { attacks, bands: vec![] }, "attack")?;
        self.drive_with(rb, |_| Prio::Done, |_| Prio::Done)
    }

    /// `block`: like `attack`, for the blockers decision; `step.blocks` are `[blocker, attacker]`.
    fn block(&mut self, step: &Step, _seat: u8, rb: &mut RowBuilder) -> Result<(), Stop> {
        let i = self.step;
        let turn = self.state().turn_number;
        self.drive_with(
            rb,
            |s| {
                if s.phase > Phase::DeclareBlockers || s.turn_number != turn {
                    Prio::Fail(format!("block: no DeclareBlockers prompt (the game passed {})", step_name(s.phase)))
                } else {
                    Prio::Act(GameAction::PassPriority)
                }
            },
            |s| match &s.waiting_for {
                WaitingFor::DeclareBlockers { .. } => Prio::Done,
                wf => Prio::Act(empty_declaration(wf)),
            },
        )?;
        if !matches!(self.state().waiting_for, WaitingFor::DeclareBlockers { .. }) {
            return Err(harness(format!(
                "block: no DeclareBlockers prompt at step {i} ({})",
                variant_name(&self.state().waiting_for)
            )));
        }
        let mut assignments = Vec::new();
        for (blocker, attacker) in &step.blocks {
            assignments.push((self.object(blocker, step.seat)?, self.object(attacker, 1 - step.seat)?));
        }
        self.act(GameAction::DeclareBlockers { assignments }, "block")?;
        self.drive_with(rb, |_| Prio::Done, |_| Prio::Done)
    }

    /// `attach`: the debug Attach of `step.card` to `step.attached_to` (a creature).
    fn attach(&mut self, step: &Step, rb: &mut RowBuilder) -> Result<(), Stop> {
        let object_id = self.object(&step.card, step.seat)?;
        let host = self.object(&step.attached_to, step.seat)?;
        let holder = self.priority_holder();
        self.debug(DebugAction::Attach { object_id, target: AttachTarget::Object(host) })?;
        self.note_edit(rb);
        self.settle(holder, rb)
    }

    /// `move`: the debug MoveToZone of `step.card` to `step.to` (raw: no replacements, entry
    /// triggers or SBAs of its own, `simulate: false`).
    fn move_card(&mut self, step: &Step, rb: &mut RowBuilder) -> Result<(), Stop> {
        let to_zone = match step.to.as_str() {
            "library" => Zone::Library,
            "hand" => Zone::Hand,
            "battlefield" => Zone::Battlefield,
            "graveyard" => Zone::Graveyard,
            "exile" => Zone::Exile,
            other => return Err(harness(format!("move: unknown zone {other:?} at step {}", self.step))),
        };
        let object_id = self.object(&step.card, step.seat)?;
        let holder = self.priority_holder();
        self.debug(DebugAction::MoveToZone { object_id, to_zone, library_position: None, simulate: false })?;
        self.note_edit(rb);
        self.settle(holder, rb)
    }

    fn note_edit(&mut self, rb: &mut RowBuilder) {
        if !self.edit_noted {
            self.edit_noted = true;
            rb.note("attach/move done by a debug action (debug mode on for that action only; no replacements or entry triggers)");
        }
    }

    /// The seat holding priority now, if a priority window is open.
    fn priority_holder(&self) -> Option<PlayerId> {
        match &self.state().waiting_for {
            WaitingFor::Priority { player } => Some(*player),
            _ => None,
        }
    }

    /// The empty priority round after a debug edit: nothing is passed while the stack is empty (a
    /// pass pair would end the step); anything the edit put on the stack is passed away until the
    /// stack is empty with `holder` holding priority again.
    fn settle(&mut self, holder: Option<PlayerId>, rb: &mut RowBuilder) -> Result<(), Stop> {
        let Some(holder) = holder else { return Ok(()) };
        self.drive(rb, move |s| match &s.waiting_for {
            WaitingFor::Priority { player } if s.stack.is_empty() && *player == holder => Prio::Done,
            WaitingFor::Priority { player } if s.stack.is_empty() => {
                Prio::Fail(format!("priority moved from p{} to p{} after the edit", holder.0, player.0))
            }
            _ => Prio::Act(GameAction::PassPriority),
        })
    }

    /// The step needs `seat` to hold priority now.
    fn need_priority(&self, seat: u8) -> Result<(), Stop> {
        match &self.state().waiting_for {
            WaitingFor::Priority { player } if *player == PlayerId(seat) => Ok(()),
            WaitingFor::Priority { player } => Err(harness(format!(
                "{}: p{seat} does not hold priority at step {} (p{} does)",
                self.req.item.steps[self.step].op, self.step, player.0
            ))),
            other => Err(harness(format!(
                "{}: no priority window at step {} ({})",
                self.req.item.steps[self.step].op,
                self.step,
                variant_name(other)
            ))),
        }
    }

    fn cast(&mut self, step: &Step, seat: u8, rb: &mut RowBuilder) -> Result<(), Stop> {
        let i = self.step;
        if !CAST_MODES.contains(&step.cast_mode.as_str()) {
            return Err(harness(format!("cast_mode {} unsupported", step.cast_mode)));
        }
        self.need_priority(seat)?;
        self.honoured = false;
        let id = self.object(&step.card, step.seat)?;
        let units = parse_mana(&step.mana)?;
        self.add_mana(seat, units, rb)?;
        let card_id = self.state().objects[&id].card_id;
        let before = self.state().stack.len();
        self.act(
            GameAction::CastSpell { object_id: id, card_id, targets: vec![], payment_mode: CastPaymentMode::Manual },
            "cast",
        )?;
        self.drive(rb, move |s| on_stack(s, PlayerId(seat), before, i, "cast"))?;
        // The mode steers prompts; one that never posed (or whose origin did not match) leaves the
        // spell cast the ordinary way, and the difference is the follower's, not phase's.
        if !step.cast_mode.is_empty() && !self.honoured {
            return Err(harness(format!("cast_mode {} not honoured at step {i}: phase raised no prompt it maps", step.cast_mode)));
        }
        Ok(())
    }

    fn activate(&mut self, step: &Step, seat: u8, rb: &mut RowBuilder) -> Result<(), Stop> {
        let i = self.step;
        self.need_priority(seat)?;
        let id = self.object(&step.card, step.seat)?;
        let key = i.to_string();
        let ability = self
            .req
            .neutral
            .ability
            .get(&key)
            .ok_or_else(|| unmatched(i, "no neutral.ability entry"))?;
        match ability.class.as_str() {
            "mana" | "intrinsic_mana" => {
                let action = self.mana_source(id, ability)?;
                self.act(action, "activate")?;
                self.drive(rb, |_| Prio::Done)
            }
            "activated" | "loyalty" => {
                let action = self.ability_action(id, ability)?;
                let before = self.state().stack.len();
                self.act(action, "activate")?;
                self.drive(rb, move |s| on_stack(s, PlayerId(seat), before, i, "activate"))
            }
            other => Err(unmatched(i, format!("ability class {other:?}"))),
        }
    }

    /// The one mana-source candidate of `id` that makes the ability's colour.
    fn mana_source(&mut self, id: ObjectId, ability: &NeutralAbility) -> Result<GameAction, Stop> {
        let i = self.step;
        let inc = self.state().objects[&id].incarnation;
        let candidates: Vec<GameAction> = legal_actions_full(self.state())
            .2
            .get(&id)
            .map(|v| v.to_vec())
            .unwrap_or_default()
            .into_iter()
            .filter(|a| match a {
                GameAction::ActivateManaSource { selection } | GameAction::TapLandForMana { selection } => {
                    selection.source.object_id == id && selection.source.incarnation == inc
                }
                _ => false,
            })
            .collect();
        let produces = ability.produces.as_deref().unwrap_or("");
        let single: Vec<char> = produces.chars().filter(|c| mana_type(*c).is_some()).collect();
        let wanted: ManaType = if produces != "any" && single.len() == 1 {
            mana_type(single[0]).expect("filtered")
        } else {
            // "any" or several colours: gorge's colour decision for this step names the colour.
            let d = self
                .queue
                .take(i as i32, |d| {
                    d.kind == "choose_n"
                        && (d.pick_kinds.iter().any(|k| k == "color") || d.picks.iter().any(|p| p.starts_with("Add ")))
                })
                .ok_or_else(|| unmatched(i, format!("mana ability producing {produces:?} has no colour decision")))?;
            d.picks
                .first()
                .and_then(|p| colour_of_label(p))
                .ok_or_else(|| unmatched(i, format!("colour decision {:?} names no colour", d.picks)))?
        };
        let mut matching = candidates.into_iter().filter(|a| match a {
            GameAction::ActivateManaSource { selection } | GameAction::TapLandForMana { selection } => {
                selection.mana_type == wanted
            }
            _ => false,
        });
        match (matching.next(), matching.next()) {
            (Some(a), None) => Ok(a),
            (None, _) => Err(unmatched(i, format!("no mana source of {} produces {}", self.state().objects[&id].name, letter(wanted)))),
            (Some(_), Some(_)) => Err(unmatched(i, format!("several mana sources of {} produce {}", self.state().objects[&id].name, letter(wanted)))),
        }
    }

    /// The action activating the non-mana ability `ability` of `id`.
    fn ability_action(&mut self, id: ObjectId, ability: &NeutralAbility) -> Result<GameAction, Stop> {
        let i = self.step;
        if let Some(kw) = &ability.keyword {
            let base = kw.split(':').next().unwrap_or("");
            return match base {
                "Equip" => Ok(GameAction::Equip { equipment_id: id, target_id: id }),
                "Crew" => Ok(GameAction::CrewVehicle { vehicle_id: id, creature_ids: Vec::new() }),
                other => Err(unmatched(i, format!("keyword ability {other} unsupported"))),
            };
        }
        let ordinal = ability
            .ordinal
            .and_then(|o| usize::try_from(o).ok())
            .ok_or_else(|| unmatched(i, "ability has no ordinal"))?;
        let obj = &self.state().objects[&id];
        let (index, def) = obj
            .abilities
            .iter()
            .enumerate()
            .filter(|(_, a)| !engine::types::ability::is_mana_ability(a))
            .filter(|(_, a)| a.kind == engine::types::ability::AbilityKind::Activated)
            .nth(ordinal)
            .ok_or_else(|| unmatched(i, format!("{} has no non-mana ability #{ordinal}", obj.name)))?;
        if let Some(text) = &ability.text {
            let corpus = self.board.names.by_object.get(&id).map(String::as_str).unwrap_or("");
            let theirs = normalise(def.description.as_deref().unwrap_or(""), &obj.name, corpus);
            let ours = normalise(text, &obj.name, corpus);
            // Equal, or one contains the other (phase may omit or add cost text); never when either is empty.
            let loose = !ours.is_empty() && !theirs.is_empty() && (theirs.contains(&ours) || ours.contains(&theirs));
            if !(theirs == ours || loose) {
                return Err(unmatched(i, format!("phase ability {theirs:?} is not {ours:?}")));
            }
        }
        Ok(GameAction::ActivateAbility { source_id: id, ability_index: index, payment_mode: CastPaymentMode::Manual })
    }

    /// Drive the game until a priority window `on_priority` calls done. Prompts other than mana
    /// payment go to the answerer.
    fn drive(&mut self, rb: &mut RowBuilder, on_priority: impl FnMut(&GameState) -> Prio) -> Result<(), Stop> {
        self.drive_with(rb, on_priority, |_| Prio::Done)
    }

    /// [`Machine::drive`] with `on_declaration` for the two declaration prompts: they belong to the
    /// step machine (never the answerer), and a step that is not declaring just stops there, which
    /// is where gorge's `pass` leaves its pending decision.
    fn drive_with(
        &mut self,
        rb: &mut RowBuilder,
        mut on_priority: impl FnMut(&GameState) -> Prio,
        mut on_declaration: impl FnMut(&GameState) -> Prio,
    ) -> Result<(), Stop> {
        loop {
            // phase targets a sole legal target without asking: account for gorge's decision.
            crate::answer::sweep_stack_targets(&self.board, &mut self.queue, self.step, &mut self.swept, &mut self.answered);
            let wf = self.state().waiting_for.clone();
            match &wf {
                WaitingFor::GameOver { .. } => {
                    return if self.last { Ok(()) } else { Err(self.game_ended()) };
                }
                WaitingFor::Priority { .. } => match on_priority(self.state()) {
                    Prio::Done => return Ok(()),
                    Prio::Act(a) => {
                        self.act(a, "PassPriority")?;
                    }
                    Prio::Fail(m) => return Err(harness(format!("{m} (step {})", self.step))),
                },
                WaitingFor::DeclareAttackers { .. } | WaitingFor::DeclareBlockers { .. } => {
                    match on_declaration(self.state()) {
                        Prio::Done => return Ok(()),
                        Prio::Act(a) => {
                            self.act(a, "declaration")?;
                        }
                        Prio::Fail(m) => return Err(harness(format!("{m} (step {})", self.step))),
                    }
                }
                WaitingFor::ManaPayment { player, .. } => self.pay(*player)?,
                other => {
                    let cast_mode = self.req.item.steps.get(self.step).map(|s| s.cast_mode.as_str()).unwrap_or("");
                    let mut cur = Cursor {
                        queue: &mut self.queue,
                        board: &self.board,
                        req: self.req,
                        used: &mut self.used,
                        step: self.step,
                        cast_mode,
                        notes: Vec::new(),
                        honoured: false,
                    };
                    let a = answer(&self.board.state, other, &mut cur);
                    rb.notes.extend(cur.notes);
                    self.honoured |= cur.honoured;
                    let action = a?.action;
                    self.answered.extend(crate::answer::answered_targets(other, &action));
                    self.act(action, &variant_name(other))?;
                }
            }
        }
    }

    /// At `ManaPayment`: pin the pool units to spend, then finalise (`PassPriority`).
    fn pay(&mut self, player: PlayerId) -> Result<(), Stop> {
        let i = self.step;
        let gorge_spent = self.req.item.payment.get(i).map(String::as_str).unwrap_or("");
        let cost = self.state().pending_cast.as_ref().map(|p| p.cost.clone());
        let pool: Vec<(ManaType, ManaPipId)> = self
            .state()
            .players
            .iter()
            .find(|p| p.id == player)
            .map(|p| p.mana_pool.units().map(|u| (u.color, u.pip_id)).collect())
            .unwrap_or_default();
        for pip in pick_pins(pool, gorge_spent, cost.as_ref()) {
            // A pin phase refuses (ineligible unit) is a hint not taken; finalisation chooses.
            self.calls += 1;
            if self.calls > MAX_APPLY {
                return Err(harness(format!("no progress at step {i}")));
            }
            let actor = self.board.state.waiting_for.acting_player().unwrap_or(player);
            let _ = apply(&mut self.board.state, actor, GameAction::SpendPoolMana { pip_id: pip });
        }
        self.act(GameAction::PassPriority, "ManaPayment")?;
        if matches!(self.state().waiting_for, WaitingFor::ManaPayment { .. }) {
            return Err(harness(format!("mana payment incomplete at step {i}: the pool does not cover the cost")));
        }
        Ok(())
    }
}

/// gorge's decision kind for the prompts `pass_to` can stop on ("" for any other).
fn decision_kind(wf: &WaitingFor) -> &'static str {
    match wf {
        WaitingFor::Priority { .. } => "priority",
        WaitingFor::DeclareAttackers { .. } => "attackers",
        WaitingFor::DeclareBlockers { .. } => "blockers",
        _ => "",
    }
}

/// gorge's default answer to a declaration it is not scripted for: declare nothing.
fn empty_declaration(wf: &WaitingFor) -> GameAction {
    match wf {
        WaitingFor::DeclareBlockers { .. } => GameAction::DeclareBlockers { assignments: vec![] },
        _ => GameAction::DeclareAttackers { attacks: vec![], bands: vec![] },
    }
}

/// The seat a step's raw `active` (or a defender string) names: `"p1"`, `1`, or none (null/empty).
fn seat_of(v: &Value) -> Result<Option<PlayerId>, String> {
    let n = match v {
        Value::Null => return Ok(None),
        Value::String(s) if s.is_empty() => return Ok(None),
        Value::String(s) => s.strip_prefix('p').unwrap_or(s).parse::<u8>().ok(),
        Value::Number(n) => n.as_u64().and_then(|n| u8::try_from(n).ok()),
        _ => None,
    };
    match n {
        Some(n) if n < 2 => Ok(Some(PlayerId(n))),
        _ => Err(format!("{v} is not a seat")),
    }
}

/// Done when `seat` holds priority and the stack grew past `before` (the spell/ability is on it).
fn on_stack(s: &GameState, seat: PlayerId, before: usize, step: usize, what: &str) -> Prio {
    match &s.waiting_for {
        WaitingFor::Priority { player } if *player == seat && s.stack.len() > before => Prio::Done,
        _ => Prio::Fail(format!("{what}: priority returned without the item on the stack at step {step}")),
    }
}

/// The pool units to pin for a payment: gorge's own `payment` units first, then one exact-colour
/// unit per coloured pip, then the generic remainder in WUBRGC order. Never more than the cost.
fn pick_pins(mut avail: Vec<(ManaType, ManaPipId)>, spent: &str, cost: Option<&ManaCost>) -> Vec<ManaPipId> {
    let Some(ManaCost::Cost { shards, generic }) = cost else { return Vec::new() };
    let take = |avail: &mut Vec<(ManaType, ManaPipId)>, t: ManaType| -> Option<ManaPipId> {
        let at = avail.iter().position(|(c, _)| *c == t)?;
        Some(avail.remove(at).1)
    };
    // gorge's units, in the order gorge listed them.
    let mut gorge: Vec<(ManaType, ManaPipId)> = Vec::new();
    for c in spent.chars() {
        if let Some(t) = mana_type(c) {
            if let Some(pip) = take(&mut avail, t) {
                gorge.push((t, pip));
            }
        }
    }
    let mut pins = Vec::new();
    let mut need_any = *generic as usize;
    for shard in shards {
        let exact = match shard {
            ManaCostShard::White => Some(ManaType::White),
            ManaCostShard::Blue => Some(ManaType::Blue),
            ManaCostShard::Black => Some(ManaType::Black),
            ManaCostShard::Red => Some(ManaType::Red),
            ManaCostShard::Green => Some(ManaType::Green),
            ManaCostShard::Colorless => Some(ManaType::Colorless),
            _ => None,
        };
        match exact {
            Some(t) => {
                if let Some(at) = gorge.iter().position(|(c, _)| *c == t) {
                    pins.push(gorge.remove(at).1);
                } else if let Some(pip) = take(&mut avail, t) {
                    pins.push(pip);
                }
            }
            // Hybrid, Phyrexian, snow, X, ...: paid like one generic unit here.
            None => need_any += 1,
        }
    }
    for _ in 0..need_any {
        if !gorge.is_empty() {
            pins.push(gorge.remove(0).1);
        } else if let Some(pip) = WUBRGC.iter().find_map(|t| take(&mut avail, *t)) {
            pins.push(pip);
        }
    }
    pins
}

/// The colour a decision label names: `Add R`, `R`, `Red`.
fn colour_of_label(label: &str) -> Option<ManaType> {
    let word = label.trim().rsplit(' ').next()?;
    match word.to_ascii_lowercase().as_str() {
        "white" => Some(ManaType::White),
        "blue" => Some(ManaType::Blue),
        "black" => Some(ManaType::Black),
        "red" => Some(ManaType::Red),
        "green" => Some(ManaType::Green),
        "colorless" => Some(ManaType::Colorless),
        w if w.chars().count() == 1 => w.chars().next().and_then(mana_type),
        _ => None,
    }
}

/// Text for the ability check: the card's own names become `~`, spacing and case are folded.
fn normalise(text: &str, name: &str, corpus: &str) -> String {
    let mut t = text.to_string();
    for n in [name, corpus] {
        if !n.is_empty() {
            t = t.replace(n, "~");
        }
    }
    t.split_whitespace().collect::<Vec<_>>().join(" ").trim_end_matches('.').to_lowercase()
}

#[cfg(test)]
mod tests {
    use super::*;
    use follower_proto::row::Snapshot;
    use std::sync::OnceLock;

    fn db() -> &'static engine::database::card_db::CardDatabase {
        static DB: OnceLock<engine::database::card_db::CardDatabase> = OnceLock::new();
        DB.get_or_init(|| {
            let path = std::env::var("PHASE_CARD_DATA").expect("PHASE_CARD_DATA: run make phase-follower-test");
            crate::load_db(&path).expect("load card data")
        })
    }

    /// Setup plus steps for a hand-written request: the outcome and the row builder.
    fn go(json: &str) -> (Result<(), Stop>, RowBuilder) {
        let req = Request::parse(json).expect("request");
        let mut rb = RowBuilder::default();
        let out = crate::setup::run(db(), &req, &mut rb).and_then(|board| run(&req, board, &mut rb));
        (out, rb)
    }

    fn ok(json: &str) -> RowBuilder {
        let (out, rb) = go(json);
        out.unwrap_or_else(|e| panic!("stopped: {e:?}"));
        rb
    }

    fn on_bf(s: &Snapshot, controller: i64, name: &str) -> usize {
        s.permanents.iter().filter(|p| p.controller == controller && p.name == name).count()
    }

    #[test]
    fn steps_play_land() {
        let rb = ok(r#"{"id":"t","item":{"id":"t","setup":{"p0":{"hand":["Forest"]}},
            "steps":[{"op":"play","seat":0,"card":"p0:Forest"}]}}"#);
        assert_eq!(rb.snapshots.len(), 2);
        let s = &rb.snapshots[1];
        assert_eq!(s.checkpoint, "step 0 (play)");
        assert_eq!((s.step.as_str(), s.priority), ("main1", 0));
        assert_eq!(on_bf(s, 0, "Forest"), 1);
        assert!(s.players[0].hand.is_empty());
    }

    #[test]
    fn steps_cast_creature_from_pool() {
        let rb = ok(r#"{"id":"t","item":{"id":"t","setup":{"p0":{"hand":["Grizzly Bears"]}},
            "steps":[{"op":"cast","seat":0,"card":"p0:Grizzly Bears","mana":"1G"},{"op":"resolve","seat":0}]}}"#);
        assert_eq!(rb.snapshots.len(), 3);
        let (cast, resolve) = (&rb.snapshots[1], &rb.snapshots[2]);
        assert_eq!(cast.checkpoint, "step 0 (cast)");
        assert_eq!(cast.stack.len(), 1);
        assert_eq!((cast.stack[0].kind.as_str(), cast.stack[0].source.as_str()), ("spell", "Grizzly Bears"));
        assert_eq!(cast.players[0].pool, "");
        assert_eq!(on_bf(cast, 0, "Grizzly Bears"), 0);
        assert_eq!(resolve.checkpoint, "step 1 (resolve)");
        assert!(resolve.stack.is_empty());
        assert_eq!(on_bf(resolve, 0, "Grizzly Bears"), 1);
        assert_eq!((resolve.step.as_str(), resolve.priority), ("main1", 0));
        assert!(rb.notes.iter().any(|n| n.starts_with("mana added")), "{:?}", rb.notes);
    }

    #[test]
    fn steps_land_mana_then_cast() {
        let rb = ok(r#"{"id":"t","item":{"id":"t","setup":{"p0":{"hand":["Forest","Llanowar Elves"]}},
            "steps":[{"op":"play","seat":0,"card":"p0:Forest"},
                     {"op":"activate","seat":0,"card":"p0:Forest","ability_index":0},
                     {"op":"cast","seat":0,"card":"p0:Llanowar Elves"},
                     {"op":"resolve","seat":0}]},
            "neutral":{"ability":{"1":{"class":"intrinsic_mana","produces":"G"}}}}"#);
        assert_eq!(rb.snapshots.len(), 5);
        let (played, tapped, cast, resolved) = (&rb.snapshots[1], &rb.snapshots[2], &rb.snapshots[3], &rb.snapshots[4]);
        assert_eq!(on_bf(played, 0, "Forest"), 1);
        assert_eq!(tapped.checkpoint, "step 1 (activate)");
        assert_eq!(tapped.players[0].pool, "G");
        assert!(tapped.permanents.iter().find(|p| p.name == "Forest").unwrap().tapped);
        assert_eq!((cast.stack.len(), cast.players[0].pool.as_str()), (1, ""));
        assert_eq!(on_bf(resolved, 0, "Llanowar Elves"), 1);
    }

    #[test]
    fn steps_pass_wrong_seat_is_harness() {
        let (out, rb) = go(r#"{"id":"t","item":{"id":"t","steps":[{"op":"pass","seat":1}]}}"#);
        assert_eq!(
            out.err(),
            Some(Stop::Harness("phase follower: pass: p1 does not hold priority at step 0 (p0 does)".to_string()))
        );
        assert_eq!(rb.snapshots.len(), 1, "only the setup snapshot");
        // The right seat passes, and the checkpoint is at the next priority (p1's).
        let rb = ok(r#"{"id":"t","item":{"id":"t","steps":[{"op":"pass","seat":0}]}}"#);
        assert_eq!((rb.snapshots[1].checkpoint.as_str(), rb.snapshots[1].priority), ("step 0 (pass)", 1));
    }

    #[test]
    fn steps_prompt_strict_miss_names_variant() {
        // Lightning Strike with no scripted target: phase asks for a target and several are legal.
        let (out, rb) = go(r#"{"id":"t","item":{"id":"t","setup":{"p0":{"hand":["Lightning Strike"]}},
            "steps":[{"op":"cast","seat":0,"card":"p0:Lightning Strike","mana":"1R"}]}}"#);
        assert_eq!(out.err(), Some(Stop::StrictMiss("Missing TargetSelection answer at step 0".to_string())));
        assert_eq!(rb.snapshots.len(), 1);
    }

    #[test]
    fn steps_leftover_reported() {
        let rb = ok(r#"{"id":"t","item":{"id":"t","setup":{"p0":{"hand":["Forest"]}},
            "steps":[{"op":"play","seat":0,"card":"p0:Forest"}]},
            "gorge_decisions":[{"step":0,"seat":0,"kind":"yesno","picks":["yes"],"pick_kinds":["yes"]}]}"#);
        assert_eq!(rb.leftover.as_deref(), Some("unconsumed gorge decision(s): step 0 p0 yesno[yes] [yes]"));
        let rb = ok(r#"{"id":"t","item":{"id":"t","setup":{"p0":{"hand":["Forest"]}},
            "steps":[{"op":"play","seat":0,"card":"p0:Forest"}]}}"#);
        assert_eq!(rb.leftover, None);
    }

    #[test]
    fn steps_unsupported_ops_and_cast_mode_are_harness() {
        for (step, msg) in [
            (r#"{"op":"cast","seat":0,"card":"p0:Forest","cast_mode":"flashback"}"#, "phase follower: cast_mode flashback unsupported"),
        ] {
            let (out, _) = go(&format!(r#"{{"id":"t","item":{{"id":"t","setup":{{"p0":{{"hand":["Forest"]}}}},"steps":[{step}]}}}}"#));
            assert_eq!(out.err(), Some(Stop::Harness(msg.to_string())));
        }
    }

    /// A two-player item with `setup` (the inside of `"setup":{...}`) and `steps`.
    fn item(setup: &str, steps: &str) -> String {
        format!(r#"{{"id":"t","item":{{"id":"t","setup":{{{setup}}},"steps":[{steps}]}}}}"#)
    }

    const TO_ATTACK: &str = r#"{"op":"pass_to","seat":0,"decision":"attackers"}"#;
    const BEARS_ATTACK: &str = r#"{"op":"attack","seat":0,"attackers":["p0:Grizzly Bears"],"defender":"p1"}"#;
    const BEARS_ATTACK_SETUP: &str = r#""p0":{"battlefield":["Grizzly Bears"]}"#;

    #[test]
    fn combat_pass_to_attackers() {
        let rb = ok(&item(BEARS_ATTACK_SETUP, TO_ATTACK));
        let s = rb.snapshots.last().unwrap();
        assert_eq!(s.checkpoint, "step 0 (pass_to)");
        assert_eq!(s.step, "declare-attackers");
        assert!(!s.permanents.iter().any(|p| p.attacking), "pass_to never declares");
        // Still at the prompt: the same pass_to completes at once, and so does the attack after it.
        let rb = ok(&item(BEARS_ATTACK_SETUP, &format!("{TO_ATTACK},{TO_ATTACK},{BEARS_ATTACK}")));
        assert_eq!(rb.snapshots[2].step, "declare-attackers");
        assert!(rb.snapshots[3].permanents.iter().any(|p| p.name == "Grizzly Bears" && p.attacking));
    }

    #[test]
    fn combat_pass_to_without_a_prompt_is_a_named_harness_row() {
        // No creature can attack: phase poses no prompt, and pass_to must not run on to later turns.
        let (out, _) = go(&item(r#""p0":{"hand":["Forest"]}"#, TO_ATTACK));
        match out.err() {
            Some(Stop::Harness(m)) => assert!(m.contains("pass_to attackers: no declaration prompt by"), "{m}"),
            other => panic!("{other:?}"),
        }
        // attack outside a prompt: the same, in its own words.
        let (out, _) = go(&item(r#""p0":{"hand":["Forest"]}"#, r#"{"op":"attack","seat":0,"attackers":[],"defender":"p1"}"#));
        match out.err() {
            Some(Stop::Harness(m)) => assert!(m.contains("attack: no DeclareAttackers prompt"), "{m}"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn combat_pass_to_step_and_active() {
        // main1 of turn 1 to p1's upkeep: the turn passes, so does cleanup, and p0 never acts again.
        let rb = ok(&item(
            "",
            r#"{"op":"pass_to","seat":0,"step":"upkeep","active":"p1"},{"op":"pass_to","seat":0,"step":"main1","active":1}"#,
        ));
        assert_eq!((rb.snapshots[1].step.as_str(), rb.snapshots[1].active), ("upkeep", 1));
        assert_eq!((rb.snapshots[2].step.as_str(), rb.snapshots[2].active), ("main1", 1));
        let (out, _) = go(&item("", r#"{"op":"pass_to","seat":0,"step":"nowhere"}"#));
        assert_eq!(out.err(), Some(Stop::Harness("phase follower: pass_to: unknown step \"nowhere\" at step 0".to_string())));
    }

    #[test]
    fn combat_declare_attack_player() {
        let rb = ok(&item(
            BEARS_ATTACK_SETUP,
            &format!(r#"{TO_ATTACK},{BEARS_ATTACK},{{"op":"pass","seat":0}},{{"op":"pass","seat":1}},{{"op":"pass_to","seat":0,"step":"main2","active":"p0"}}"#),
        ));
        let declared = &rb.snapshots[2];
        assert_eq!(declared.checkpoint, "step 1 (attack)");
        assert_eq!(declared.step, "declare-attackers");
        assert_eq!(declared.priority, 0);
        let bears = declared.permanents.iter().find(|p| p.name == "Grizzly Bears").unwrap();
        assert!(bears.attacking && bears.tapped);
        assert_eq!(declared.players[1].life, 20);
        let after = rb.snapshots.last().unwrap();
        assert_eq!(after.players[1].life, 18, "the 2/2 connects");
    }

    #[test]
    fn combat_declare_blocks() {
        let setup = r#""p0":{"battlefield":["Grizzly Bears"]},"p1":{"battlefield":["Llanowar Elves"]}"#;
        let rb = ok(&item(
            setup,
            &format!(
                r#"{TO_ATTACK},{BEARS_ATTACK},{{"op":"pass_to","seat":1,"decision":"blockers"}},
                   {{"op":"block","seat":1,"blocks":[["p1:Llanowar Elves","p0:Grizzly Bears"]]}},
                   {{"op":"pass_to","seat":0,"step":"main2","active":"p0"}}"#
            ),
        ));
        let at_blockers = &rb.snapshots[3];
        assert_eq!(at_blockers.step, "declare-blockers");
        assert!(!at_blockers.permanents.iter().any(|p| p.blocking));
        let blocked = &rb.snapshots[4];
        assert!(blocked.permanents.iter().any(|p| p.name == "Llanowar Elves" && p.blocking));
        let after = rb.snapshots.last().unwrap();
        assert_eq!(after.players[1].life, 20, "blocked: no damage to the player");
        assert_eq!(on_bf(after, 1, "Llanowar Elves"), 0, "the 1/1 dies to the 2/2");
        assert_eq!(on_bf(after, 0, "Grizzly Bears"), 1);
    }

    #[test]
    fn combat_blocker_stays_blocking_after_its_attacker_dies() {
        // CR 509.1g: the 2/2 blocks and kills the 1/1; it is still a blocking creature afterwards.
        let setup = r#""p0":{"battlefield":["Llanowar Elves"]},"p1":{"battlefield":["Grizzly Bears"]}"#;
        let rb = ok(&item(
            setup,
            &format!(
                r#"{TO_ATTACK},{{"op":"attack","seat":0,"attackers":["p0:Llanowar Elves"],"defender":"p1"}},
                   {{"op":"pass_to","seat":1,"decision":"blockers"}},
                   {{"op":"block","seat":1,"blocks":[["p1:Grizzly Bears","p0:Llanowar Elves"]]}},
                   {{"op":"pass","seat":0}},{{"op":"pass","seat":1}}"#
            ),
        ));
        let s = rb.snapshots.last().unwrap();
        assert_eq!(s.step, "combat-damage");
        assert_eq!(on_bf(s, 0, "Llanowar Elves"), 0);
        assert!(s.permanents.iter().any(|p| p.name == "Grizzly Bears" && p.blocking && p.damage == 1), "{:?}", s.permanents);
    }

    #[test]
    fn combat_multi_blocker_damage_is_strict_miss() {
        let setup = r#""p0":{"battlefield":["Grizzly Bears"]},"p1":{"battlefield":["Llanowar Elves","Llanowar Elves"]}"#;
        let (out, _) = go(&item(
            setup,
            &format!(
                r#"{TO_ATTACK},{BEARS_ATTACK},{{"op":"pass_to","seat":1,"decision":"blockers"}},
                   {{"op":"block","seat":1,"blocks":[["p1:Llanowar Elves","p0:Grizzly Bears"],["p1:Llanowar Elves#2","p0:Grizzly Bears"]]}},
                   {{"op":"pass_to","seat":0,"step":"main2","active":"p0"}}"#
            ),
        ));
        assert_eq!(out.err(), Some(Stop::StrictMiss("Missing AssignCombatDamage answer at step 4".to_string())));
    }

    #[test]
    fn combat_attach_prelude() {
        let setup = r#""p0":{"battlefield":["Grizzly Bears","Short Sword"]}"#;
        let rb = ok(&item(setup, r#"{"op":"attach","seat":0,"card":"p0:Short Sword","attached_to":"p0:Grizzly Bears"}"#));
        let (before, after) = (&rb.snapshots[0], &rb.snapshots[1]);
        assert_eq!(after.checkpoint, "step 0 (attach)");
        let sword = |s: &Snapshot| s.permanents.iter().find(|p| p.name == "Short Sword").unwrap().attached_to.clone();
        assert_eq!(sword(before), None);
        assert_eq!(sword(after).as_deref(), Some("Grizzly Bears"));
        assert_eq!((after.step.as_str(), after.priority), (before.step.as_str(), before.priority));
        assert!(rb.notes.iter().any(|n| n.starts_with("attach/move done by a debug action")), "{:?}", rb.notes);
    }

    #[test]
    fn combat_move_then_empty_round() {
        let rb = ok(&item(
            r#""p0":{"hand":["Forest"]}"#,
            r#"{"op":"move","seat":0,"card":"p0:Forest","to":"graveyard"},{"op":"move","seat":0,"card":"p0:Forest","to":"battlefield"}"#,
        ));
        let (before, gy, bf) = (&rb.snapshots[0], &rb.snapshots[1], &rb.snapshots[2]);
        assert_eq!(before.players[0].hand, ["Forest"]);
        assert_eq!((gy.players[0].graveyard.as_slice(), gy.players[0].hand.len()), (&["Forest".to_string()][..], 0));
        assert_eq!((gy.step.as_str(), gy.priority), (before.step.as_str(), before.priority));
        assert_eq!(on_bf(bf, 0, "Forest"), 1);
        assert!(bf.players[0].graveyard.is_empty());
        assert_eq!(rb.notes.iter().filter(|n| n.starts_with("attach/move done")).count(), 1);
        let (out, _) = go(&item(r#""p0":{"hand":["Forest"]}"#, r#"{"op":"move","seat":0,"card":"p0:Forest","to":"nowhere"}"#));
        assert_eq!(out.err(), Some(Stop::Harness("phase follower: move: unknown zone \"nowhere\" at step 0".to_string())));
    }

    #[test]
    fn steps_pins_follow_gorge_payment_then_pips_then_wubrgc() {
        let pool: Vec<(ManaType, ManaPipId)> =
            [ManaType::Red, ManaType::Green, ManaType::White, ManaType::Green].iter().enumerate().map(|(i, t)| (*t, ManaPipId(i as u64 + 1))).collect();
        let cost = ManaCost::Cost { shards: vec![ManaCostShard::Green], generic: 1 };
        // gorge spent R for the generic part: the pins are G (the pip) and R (gorge's unit).
        assert_eq!(pick_pins(pool.clone(), "R", Some(&cost)), [ManaPipId(2), ManaPipId(1)]);
        // Nothing listed: G for the pip, then the generic in WUBRGC order (W).
        assert_eq!(pick_pins(pool, "", Some(&cost)), [ManaPipId(2), ManaPipId(3)]);
    }
}
