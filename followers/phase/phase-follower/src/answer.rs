//! The answerer: what the step machine does at a prompt that is not a priority window.
//!
//! One arm per `WaitingFor` variant (147 at the pinned phase commit) and NO `_` arm, so a variant
//! phase adds later breaks the build here instead of falling into a default. `Priority` belongs to
//! the step machine.
//!
//! The general rule: a prompt is answered from gorge's decision log (`follower_proto::decisions`),
//! and the answer must be one phase offers (its own [`AiDecisionContract`] candidates, or the
//! prompt's own legal set for the combinatorial prompts: targets, card picks, modes). No decision,
//! or a decision whose answer phase does not offer, is a strict miss `Missing <what> answer at step
//! i[: detail]`. A prompt with exactly one candidate is answered by force and noted `forced`;
//! the answerer never picks among several candidates without a decision.
//!
//! Prompts gorge does not log today (replacement order, combat damage assignment among several
//! blockers, Phyrexian per-pip choices, cost-reduction order) and every variant no level-A
//! scenario needs stay in the last arm, which only ever forces.
//!
//! phase-only conveniences (resolve-all, loop shortcuts, precast-copy shortcuts) are never
//! initiated by the follower; meeting one is a harness row.

use std::collections::BTreeSet;

use engine::ai_support::AiDecisionContract;
use engine::types::ability::{AdditionalCost, TargetRef};
use engine::types::actions::{AlternativeCastDecision, GameAction, UnlessCostBranch};
use engine::types::game_state::{
    AlternativeCastKeyword, CastOfferKind, CastingVariant, GameState, ManaChoice, ManaChoicePrompt, TargetSelectionSlot, WaitingFor,
};
use engine::types::identifiers::ObjectId;
use engine::types::mana::ManaType;
use engine::types::player::PlayerId;
use follower_proto::batch::Stop;
use follower_proto::decisions::{
    is_arrange, is_damage_split, is_mode, is_object_choice, is_trigger_order, is_yes_no, trigger_order_stack_first,
    x_value, yes, DecisionQueue,
};
use follower_proto::refs::Ref;
use follower_proto::request::{Decision, Request};
use follower_proto::row::strict_miss_msg;

use crate::setup::Board;

/// What an answer step needs: the gorge decisions, the board (ref table, names), the step being
/// replayed and its cast mode, and the row notes.
pub struct Cursor<'a> {
    pub queue: &'a mut DecisionQueue,
    pub board: &'a Board,
    pub req: &'a Request,
    /// Indices (into `req.gorge_decisions`) of the mode decisions already taken; the key of
    /// `neutral.mode` is the decision's index.
    pub used: &'a mut BTreeSet<usize>,
    /// Index of the step being replayed.
    pub step: usize,
    /// The step's `cast_mode` ("" for any step that is not a cast).
    pub cast_mode: &'a str,
    pub notes: Vec<String>,
    /// Set when an arm acted on `cast_mode` (paid the cost, chose the variant, face or mode it
    /// names). A non-empty mode no arm honoured is a harness row: the spell was cast the ordinary
    /// way and the difference would be misread as a rules difference.
    pub honoured: bool,
}

/// The action to apply to answer a prompt.
#[derive(Debug, Clone)]
pub struct Answer {
    pub action: GameAction,
}

/// The variant name of `wf` (its `Debug` text up to the first non-identifier character).
pub fn variant_name(wf: &WaitingFor) -> String {
    format!("{wf:?}").split(|c: char| !c.is_ascii_alphanumeric()).next().unwrap_or("").to_string()
}

fn harness(msg: impl Into<String>) -> Stop {
    Stop::Harness(format!("phase follower: {}", msg.into()))
}

fn miss(wf: &WaitingFor, cur: &Cursor, detail: &str) -> Stop {
    Stop::StrictMiss(strict_miss_msg(&variant_name(wf), cur.step as i64, detail))
}

fn ok(action: GameAction) -> Result<Answer, Stop> {
    Ok(Answer { action })
}

/// Answer the one forced candidate of `wf`, or strict-miss.
fn forced(state: &GameState, wf: &WaitingFor, cur: &mut Cursor) -> Result<Answer, Stop> {
    let name = variant_name(wf);
    let Some(owner) = wf.acting_player() else {
        return Err(miss(wf, cur, ""));
    };
    let mut candidates = AiDecisionContract::issue(state, owner).candidates;
    if candidates.len() == 1 {
        cur.notes.push(format!("forced {name} at step {}", cur.step));
        Ok(Answer { action: candidates.remove(0).action })
    } else {
        Err(miss(wf, cur, ""))
    }
}

/// `action`, when phase offers it among its candidates for the prompt (an empty candidate list is
/// a prompt phase does not enumerate: the engine's own validation stands in).
fn offered(state: &GameState, wf: &WaitingFor, action: GameAction, cur: &Cursor) -> Result<Answer, Stop> {
    if let Some(owner) = wf.acting_player() {
        let candidates = AiDecisionContract::issue(state, owner).candidates;
        if !candidates.is_empty() && !candidates.iter().any(|c| c.action == action) {
            return Err(miss(wf, cur, "the scripted answer is not among phase's candidates"));
        }
    }
    ok(action)
}

/// `answer`, or the forced candidate when gorge logged no decision.
fn or_forced(state: &GameState, wf: &WaitingFor, cur: &mut Cursor, answer: Option<Result<Answer, Stop>>) -> Result<Answer, Stop> {
    match answer {
        Some(a) => a,
        None => forced(state, wf, cur),
    }
}

// ---- object and target resolution -------------------------------------------------------------

/// The live object a card or token ref names: its bound object (a ref follows the object through
/// zone changes), else the k-th live object of its owner whose name is the ref's name.
fn object_of(board: &Board, r: &Ref) -> Option<ObjectId> {
    match r {
        Ref::Card { .. } => {
            if let Some((id, _)) = board.refs.bound(r) {
                return board.state.objects.contains_key(&id).then_some(id);
            }
            let mut live: Vec<((ObjectId, u64), u8, Vec<String>)> = board
                .state
                .objects
                .iter()
                .map(|(id, o)| {
                    let mut names = vec![o.name.clone()];
                    names.extend(board.names.by_object.get(id).cloned());
                    ((*id, o.incarnation), o.owner.0, names)
                })
                .collect();
            live.sort_by_key(|(p, _, _)| *p);
            board.refs.resolve(r, &live).map(|(id, _)| id)
        }
        Ref::Token { .. } => board.resolve(r),
        Ref::Player(_) | Ref::Ability { .. } => None,
    }
}

pub(crate) fn target_of(board: &Board, r: &Ref) -> Option<TargetRef> {
    match r {
        Ref::Player(n) => Some(TargetRef::Player(PlayerId(*n))),
        other => object_of(board, other).map(TargetRef::Object),
    }
}

/// phase puts a spell or ability whose only legal target is one object on the stack without asking
/// (no `TargetSelection` prompt), while gorge logged a target decision for it. For each stack
/// entry not yet seen, the refs of this step's target decisions that name what the entry actually
/// targets are consumed, so the decision is not reported as unconsumed. A decision naming some
/// other target stays and is reported. Returns the number of refs consumed.
///
/// An entry whose every target the answerer itself chose through a prompt (`answered`) is not
/// swept: its decisions were consumed by that prompt, and taking more would starve a later
/// decision naming the same object (a spell and its cast trigger on one creature). `answered` is
/// cleared once such an entry is seen.
pub fn sweep_stack_targets(
    board: &Board,
    queue: &mut DecisionQueue,
    step: usize,
    seen: &mut BTreeSet<ObjectId>,
    answered: &mut Vec<TargetRef>,
) -> usize {
    let mut taken = 0;
    let mut prompted = false;
    for entry in board.state.stack.iter() {
        if seen.contains(&entry.id) {
            continue;
        }
        let Some(ability) = entry.ability() else { continue };
        let mut chosen: Vec<TargetRef> = Vec::new();
        chain_targets(ability, &mut chosen);
        if chosen.is_empty() {
            continue;
        }
        seen.insert(entry.id);
        if chosen.iter().all(|t| answered.contains(t)) {
            prompted = true;
            continue;
        }
        let got = queue.take_target_refs(step as i32, chosen.len(), |r| target_of(board, r).is_some_and(|t| chosen.contains(&t)));
        taken += got.len();
    }
    if prompted {
        answered.clear();
    }
    taken
}

/// The targets an answer to the target prompt `wf` chose.
pub fn answered_targets(wf: &WaitingFor, action: &GameAction) -> Vec<TargetRef> {
    match (wf, action) {
        (WaitingFor::TargetSelection { .. } | WaitingFor::TriggerTargetSelection { .. }, GameAction::ChooseTarget { target }) => {
            target.iter().cloned().collect()
        }
        (WaitingFor::TargetSelection { .. } | WaitingFor::TriggerTargetSelection { .. }, GameAction::SelectTargets { targets }) => {
            targets.clone()
        }
        (WaitingFor::MultiTargetSelection { .. }, GameAction::SelectCards { cards }) => {
            cards.iter().map(|id| TargetRef::Object(*id)).collect()
        }
        _ => Vec::new(),
    }
}

/// Every target the ability and its chain of sub-abilities hold.
fn chain_targets(ability: &engine::types::ability::ResolvedAbility, out: &mut Vec<TargetRef>) {
    out.extend(ability.targets.iter().cloned());
    for (_, child) in ability.child_links() {
        chain_targets(child, out);
    }
}

/// Lower-case alphanumerics only: two texts that differ in case, spacing or punctuation compare
/// equal.
fn fold(s: &str) -> String {
    s.chars().filter(|c| c.is_alphanumeric()).flat_map(char::to_lowercase).collect()
}

/// Equal after folding, or one contains the other; never when either is empty.
fn text_match(a: &str, b: &str) -> bool {
    let (a, b) = (fold(a), fold(b));
    !a.is_empty() && !b.is_empty() && (a == b || a.contains(&b) || b.contains(&a))
}

/// The object of `cards` a decision pick names: its ref when the ref resolves into `cards`, else
/// the first unused card of `cards` with the pick's name.
fn card_of(cur: &Cursor, d: &Decision, i: usize, cards: &[ObjectId], used: &[ObjectId]) -> Option<ObjectId> {
    let state = &cur.board.state;
    // An inexact ref was derived by counting live objects, not by identity: only the name is sound.
    let inexact = d.pick_refs_inexact.get(i).copied().unwrap_or(false);
    if !inexact {
        if let Some(r) = d.pick_refs.get(i).and_then(|s| Ref::parse(s).ok()) {
            if let Some(id) = object_of(cur.board, &r) {
                if cards.contains(&id) && !used.contains(&id) {
                    return Some(id);
                }
            }
        }
    }
    let name = d
        .picks
        .get(i)
        .map(String::as_str)
        .filter(|p| !p.is_empty())
        .or_else(|| d.object_picks.get(i).map(|r| follower_proto::refs::ref_name(r)))
        .unwrap_or("");
    cards.iter().copied().find(|id| {
        !used.contains(id)
            && state.objects.get(id).is_some_and(|o| {
                o.name == name || cur.board.names.by_object.get(id).is_some_and(|n| n == name)
            })
    })
}

/// The cards of `cards` gorge's decision `d` picked, in pick order; a pick that finds no card is
/// dropped.
fn cards_of(cur: &Cursor, d: &Decision, cards: &[ObjectId]) -> Vec<ObjectId> {
    let n = d.picks.len().max(d.pick_refs.len()).max(d.object_picks.len());
    let mut out: Vec<ObjectId> = Vec::new();
    for i in 0..n {
        if let Some(id) = card_of(cur, d, i, cards, &out) {
            out.push(id);
        }
    }
    out
}

/// Take the decision of this step matching `pred`.
fn take(cur: &mut Cursor, pred: impl Fn(&Decision) -> bool) -> Option<Decision> {
    cur.queue.take(cur.step as i32, pred)
}

// ---- targets ----------------------------------------------------------------------------------

/// Whether the queue still holds an unconsumed target ref of this step (any ref at all).
fn target_refs_left(cur: &Cursor) -> bool {
    let mut probe = cur.queue.clone();
    !probe.take_target_refs(cur.step as i32, 1, |_| true).is_empty()
}

/// An empty target decision is consumed by `take_target_refs` without a trace; this tells it
/// apart from "nothing there" by comparing what is left over.
fn took_empty_target(cur: &mut Cursor, max: usize, can: impl Fn(&Ref) -> bool) -> (Vec<Ref>, bool) {
    let before = cur.queue.leftover();
    let refs = cur.queue.take_target_refs(cur.step as i32, max, can);
    let consumed = refs.is_empty() && before != cur.queue.leftover();
    (refs, consumed)
}

/// One target slot (`ChooseTarget`): the scripted ref when it names a legal target; an optional
/// slot gorge left empty stays empty.
fn one_target(
    state: &GameState,
    wf: &WaitingFor,
    slots: &[TargetSelectionSlot],
    current_slot: usize,
    selected_any: bool,
    legal: &[TargetRef],
    cur: &mut Cursor,
) -> Result<Answer, Stop> {
    let optional = slots.get(current_slot).is_some_and(|s| s.optional);
    let board = cur.board;
    let (refs, consumed_empty) =
        took_empty_target(cur, 1, |r| target_of(board, r).is_some_and(|t| legal.contains(&t)));
    if let Some(t) = refs.first().and_then(|r| target_of(board, r)) {
        return ok(GameAction::ChooseTarget { target: Some(t) });
    }
    if optional {
        // gorge's explicit empty answer, or the tail of an "up to N" run gorge has already filled.
        if consumed_empty || (selected_any && !target_refs_left(cur)) {
            cur.notes.push(format!("optional target slot {current_slot} left empty at step {}", cur.step));
            return ok(GameAction::ChooseTarget { target: None });
        }
        return Err(miss(wf, cur, "no scripted target for an optional slot"));
    }
    if legal.len() == 1 {
        cur.notes.push(format!("forced {} at step {}", variant_name(wf), cur.step));
        return ok(GameAction::ChooseTarget { target: Some(legal[0].clone()) });
    }
    let _ = state;
    Err(miss(wf, cur, ""))
}

fn multi_target(
    wf: &WaitingFor,
    legal: &[ObjectId],
    min: usize,
    max: usize,
    cur: &mut Cursor,
) -> Result<Answer, Stop> {
    let board = cur.board;
    let (refs, consumed_empty) = took_empty_target(cur, max, |r| {
        matches!(target_of(board, r), Some(TargetRef::Object(id)) if legal.contains(&id))
    });
    let mut ids: Vec<ObjectId> = Vec::new();
    for r in &refs {
        if let Some(TargetRef::Object(id)) = target_of(board, r) {
            if !ids.contains(&id) {
                ids.push(id);
            }
        }
    }
    if ids.is_empty() && !consumed_empty && min > 0 {
        return Err(miss(wf, cur, ""));
    }
    if ids.len() < min || ids.len() > max {
        return Err(miss(wf, cur, &format!("gorge chose {} target(s), phase wants {min}..={max}", ids.len())));
    }
    ok(GameAction::SelectCards { cards: ids })
}

// ---- modes ------------------------------------------------------------------------------------

/// The mode indices gorge chose, or `None` when it logged no mode decision. `neutral.mode` (keyed
/// by the decision's index) gives the ordinals; without it the labels are matched against phase's
/// own mode text. Phase's mode index is its parse order.
fn modes(wf: &WaitingFor, descriptions: &[String], cur: &mut Cursor) -> Result<Option<Vec<usize>>, Stop> {
    let Some(d) = take(cur, |d| is_mode(d) && d.resume != "unless_pay") else { return Ok(None) };
    let index = cur
        .req
        .gorge_decisions
        .iter()
        .enumerate()
        .position(|(i, x)| x == &d && !cur.used.contains(&i));
    if let Some(i) = index {
        cur.used.insert(i);
    }
    let neutral = index.and_then(|i| cur.req.neutral.mode.get(&i.to_string()));
    let mut out: Vec<usize> = Vec::new();
    if let Some(n) = neutral {
        for p in &n.picks {
            let Some(ord) = usize::try_from(p.ordinal).ok().filter(|o| *o < descriptions.len().max(1)) else {
                return Err(miss(wf, cur, &format!("mode ordinal {} out of range", p.ordinal)));
            };
            if let Some(text) = descriptions.get(ord) {
                if !text.is_empty() && !p.text.is_empty() && !text_match(text, &p.text) {
                    return Err(miss(wf, cur, &format!("mode {ord} text differs from gorge's {:?}", p.text)));
                }
            }
            out.push(ord);
        }
    } else {
        for label in &d.picks {
            let found: Vec<usize> =
                (0..descriptions.len()).filter(|j| text_match(&descriptions[*j], label)).collect();
            match found.as_slice() {
                [j] => out.push(*j),
                _ => return Err(miss(wf, cur, &format!("no unique mode matches {label:?}"))),
            }
        }
    }
    Ok(Some(out))
}

// ---- optional costs and cast modes ------------------------------------------------------------

/// Whether the step's `cast_mode` pays the optional cost the prompt asks about.
fn pays_optional_cost(
    mode: &str,
    origin: engine::types::ability::AdditionalCostOrigin,
    cost: &AdditionalCost,
    times: u32,
) -> bool {
    use engine::types::ability::AdditionalCostOrigin as O;
    // A kicker is a kicker by its cost shape as well as by the origin the prompt carries.
    let origin = if matches!(cost, AdditionalCost::Kicker { .. }) { O::Kicker } else { origin };
    match (mode, origin) {
        ("optionalcost", _) => true,
        ("kicked" | "kicked1" | "kicked2" | "kickedboth" | "multikicked", O::Kicker) => times == 0,
        ("replicated", O::Replicate) => times == 0,
        ("offspring", O::Offspring) => true,
        ("buyback", O::Other) => true,
        _ => false,
    }
}

fn wanted_variant(mode: &str) -> Option<CastingVariant> {
    match mode {
        "" | "modal_spell" => Some(CastingVariant::Normal),
        "adventure_alt" => Some(CastingVariant::Adventure),
        "omen_alt" => Some(CastingVariant::Omen),
        "warped" => Some(CastingVariant::Warp),
        _ => None,
    }
}

// ---- the answerer -----------------------------------------------------------------------------

/// The answer to the prompt `wf` (always `state.waiting_for`).
pub fn answer(state: &GameState, wf: &WaitingFor, cur: &mut Cursor) -> Result<Answer, Stop> {
    match wf {
        // The step machine's: it never asks the answerer about priority.
        WaitingFor::Priority { .. } => Err(harness(format!("answerer asked about Priority at step {}", cur.step))),
        // The driver stops at game over before asking.
        WaitingFor::GameOver { .. } => Err(harness(format!("answerer asked about GameOver at step {}", cur.step))),
        // phase-only conveniences the follower never initiates.
        WaitingFor::ResolveAllConsent { .. }
        | WaitingFor::ResolveAllReady { .. }
        | WaitingFor::LoopShortcut { .. }
        | WaitingFor::RespondToShortcut { .. }
        | WaitingFor::PrecastCopyShortcutOffer { .. }
        | WaitingFor::RespondToPrecastCopyShortcut { .. } => {
            Err(harness(format!("phase-only prompt {} at step {}", variant_name(wf), cur.step)))
        }

        // Targets: one slot at a time, by target spec.
        WaitingFor::TargetSelection { target_slots, selection, .. }
        | WaitingFor::TriggerTargetSelection { target_slots, selection, .. } => one_target(
            state,
            wf,
            target_slots,
            selection.current_slot,
            selection.selected_slots.iter().any(Option::is_some),
            &selection.current_legal_targets,
            cur,
        ),
        WaitingFor::MultiTargetSelection { legal_targets, min_targets, max_targets, .. } => {
            multi_target(wf, legal_targets, *min_targets, *max_targets, cur)
        }

        // CR 715.3a: an Adventure-family card cast from hand asks which face. The spell half is
        // the `adventure_alt` / `omen_alt` modes; any other mode casts the creature face.
        WaitingFor::CastOffer { kind: CastOfferKind::Adventure { .. }, .. } => {
            let spell_half = matches!(cur.cast_mode, "adventure_alt" | "omen_alt");
            cur.honoured |= spell_half;
            offered(state, wf, GameAction::ChooseAdventureFace { creature: !spell_half }, cur)
        }

        // Modes.
        WaitingFor::ModeChoice { modal, .. } | WaitingFor::AbilityModeChoice { modal, .. } => {
            let chosen = modes(wf, &modal.mode_descriptions, cur)?;
            let a = chosen.map(|indices| {
                if indices.len() < modal.min_choices || indices.len() > modal.max_choices || indices.iter().any(|i| *i >= modal.mode_count) {
                    Err(miss(wf, cur, &format!("gorge chose {} mode(s), phase wants {}..={}", indices.len(), modal.min_choices, modal.max_choices)))
                } else {
                    // phase charges entwine when every mode is chosen (CR 702.42).
                    cur.honoured |= cur.cast_mode == "entwined" && indices.len() == modal.mode_count && modal.mode_count > 1;
                    ok(GameAction::SelectModes { indices })
                }
            });
            or_forced(state, wf, cur, a)
        }
        WaitingFor::ChooseOneOfBranch { branches, branch_descriptions, .. } => {
            let chosen = modes(wf, branch_descriptions, cur)?;
            let a = chosen.map(|indices| match indices.as_slice() {
                [i] if *i < branches.len() => offered(state, wf, GameAction::ChooseBranch { index: *i }, cur),
                _ => Err(miss(wf, cur, "gorge's branch pick is not one phase branch")),
            });
            or_forced(state, wf, cur, a)
        }

        // X.
        WaitingFor::ChooseXValue { min, max, .. } => {
            let a = take(cur, |d| x_value(d).is_some()).and_then(|d| x_value(&d)).map(|n| {
                match u32::try_from(n).ok().filter(|v| v >= min && v <= max) {
                    Some(value) => offered(state, wf, GameAction::ChooseX { value }, cur),
                    None => Err(miss(wf, cur, &format!("X = {n} is outside phase's {min}..={max}"))),
                }
            });
            or_forced(state, wf, cur, a)
        }

        // Yes/no.
        WaitingFor::OptionalEffectChoice { .. }
        | WaitingFor::OpponentMayChoice { .. }
        | WaitingFor::TributeChoice { .. }
        | WaitingFor::RevealUntilKeptChoice { .. }
        | WaitingFor::RepeatDecision { .. } => {
            let a = take(cur, is_yes_no_like)
                .map(|d| offered(state, wf, GameAction::DecideOptionalEffect { accept: accepts(&d) }, cur));
            or_forced(state, wf, cur, a)
        }
        WaitingFor::UnlessPayment { .. } => {
            let a = take(cur, is_unless_pay).map(|d| offered(state, wf, GameAction::PayUnlessCost { pay: pays_unless(&d) }, cur));
            or_forced(state, wf, cur, a)
        }
        WaitingFor::UnlessPaymentChooseCost { costs, .. } => {
            let a = take(cur, is_unless_pay).map(|d| {
                let choice = match (pays_unless(&d), costs.len()) {
                    (false, _) => UnlessCostBranch::Decline,
                    (true, 1) => UnlessCostBranch::Pay { index: 0 },
                    (true, _) => return Err(miss(wf, cur, "gorge paid but phase offers several costs")),
                };
                offered(state, wf, GameAction::ChooseUnlessCostBranch { choice }, cur)
            });
            or_forced(state, wf, cur, a)
        }

        // "Do A or pay B": gorge's `altaddcost` decision names the one it took; `pay` takes the first.
        WaitingFor::OptionalCostChoice { cost: AdditionalCost::Choice(first, second), .. } => {
            let a = take(cur, |d| d.pick_kinds.iter().any(|k| k == "altaddcost")).map(|d| {
                let label = d.picks.first().cloned().unwrap_or_default();
                match choice_index(first, second, &label) {
                    Some(i) => offered(state, wf, GameAction::DecideOptionalCost { pay: i == 0 }, cur),
                    None => Err(miss(wf, cur, &format!("gorge's {label:?} names neither of phase's two costs"))),
                }
            });
            or_forced(state, wf, cur, a)
        }
        // Optional costs: the cast step's cast_mode; none = decline.
        WaitingFor::OptionalCostChoice { cost, times_kicked, origin, .. } => {
            let pay = pays_optional_cost(cur.cast_mode, *origin, cost, *times_kicked);
            if pay && matches!(cur.cast_mode, "kicked1" | "kicked2" | "kickedboth") {
                if let AdditionalCost::Kicker { costs, .. } = cost {
                    if costs.len() > 1 {
                        cur.notes.push(format!("cast_mode {} paid as one kicker prompt at step {}", cur.cast_mode, cur.step));
                    }
                }
            }
            if pay && matches!(cur.cast_mode, "multikicked" | "replicated") {
                cur.notes.push(format!("cast_mode {} paid once at step {}", cur.cast_mode, cur.step));
            }
            cur.honoured |= pay;
            offered(state, wf, GameAction::DecideOptionalCost { pay }, cur)
        }
        WaitingFor::DefilerPayment { .. } => {
            cur.honoured |= cur.cast_mode == "optionalcost";
            offered(state, wf, GameAction::DecideOptionalCost { pay: cur.cast_mode == "optionalcost" }, cur)
        }

        // Cast variants.
        WaitingFor::CastingVariantChoice { options, .. } => {
            let Some(want) = wanted_variant(cur.cast_mode) else {
                return Err(miss(wf, cur, &format!("cast_mode {} names no casting variant", cur.cast_mode)));
            };
            let found: Vec<usize> = (0..options.len()).filter(|i| options[*i].variant == want).collect();
            match found.as_slice() {
                [index] => {
                    cur.honoured |= want != CastingVariant::Normal;
                    offered(state, wf, GameAction::ChooseCastingVariant { index: *index }, cur)
                }
                _ => Err(miss(wf, cur, &format!("cast_mode {:?} matches {} variant option(s)", cur.cast_mode, found.len()))),
            }
        }
        WaitingFor::ModalFaceChoice { .. } => {
            cur.honoured |= cur.cast_mode == "modal_spell";
            offered(state, wf, GameAction::ChooseModalFace { back_face: cur.cast_mode == "modal_spell" }, cur)
        }
        WaitingFor::AlternativeCastChoice { keyword, .. } => {
            let alt = matches!((cur.cast_mode, keyword), ("warped", AlternativeCastKeyword::Warp));
            let choice = if alt { AlternativeCastDecision::Alternative } else { AlternativeCastDecision::Normal };
            cur.honoured |= alt;
            offered(state, wf, GameAction::ChooseAlternativeCast { choice }, cur)
        }

        // Card picks.
        WaitingFor::DiscardChoice { cards, count, up_to, .. } => pick_cards(state, wf, cards, 0, *count, *up_to, false, cur),
        WaitingFor::DiscardToHandSize { cards, count, .. } => pick_cards(state, wf, cards, *count, *count, false, false, cur),
        WaitingFor::ConniveDiscard { cards, count, .. } => pick_cards(state, wf, cards, 0, *count, true, false, cur),
        WaitingFor::SearchChoice { cards, count, up_to, .. } => pick_cards(state, wf, cards, 0, *count, *up_to, false, cur),
        WaitingFor::DigChoice { selectable_cards, keep_count, up_to, .. } => {
            pick_cards(state, wf, selectable_cards, 0, *keep_count, *up_to, true, cur)
        }
        WaitingFor::ChooseFromZoneChoice { cards, count, up_to, .. } => pick_cards(state, wf, cards, 0, *count, *up_to, false, cur),
        WaitingFor::EffectZoneChoice { cards, count, min_count, up_to, .. } => {
            pick_cards(state, wf, cards, *min_count, *count, *up_to || min_count < count, false, cur)
        }
        WaitingFor::RevealChoice { cards, optional, .. } => pick_cards(state, wf, cards, 0, 1, *optional, false, cur),
        WaitingFor::PayCost { choices, count, min_count, .. } => {
            pick_cards(state, wf, choices, *min_count, *count, min_count < count, false, cur)
        }

        // Scry and surveil: the cards kept on top, in order; the rest go to the bottom (scry) or
        // the graveyard (surveil).
        WaitingFor::ScryChoice { cards, .. } | WaitingFor::SurveilChoice { cards, .. } => {
            let a = take(cur, is_arrange).map(|d| {
                let top = cards_of(cur, &d, cards);
                let bottomed = cards.len() - top.len();
                if matches!(wf, WaitingFor::ScryChoice { .. }) && bottomed > 1 {
                    cur.notes.push(format!(
                        "scry at step {}: phase puts the {bottomed} bottomed cards in looked-at order; gorge's bottom order is not scriptable",
                        cur.step
                    ));
                }
                ok(GameAction::SelectCards { cards: top })
            });
            or_forced(state, wf, cur, a)
        }

        // Colours and labels.
        WaitingFor::ChooseManaColor { choice, .. } => {
            let a = take(cur, is_colour_decision).map(|d| {
                let Some(t) = d.picks.first().and_then(|p| colour_of(p)) else {
                    return Err(miss(wf, cur, &format!("colour decision {:?} names no colour", d.picks)));
                };
                match choice {
                    ManaChoicePrompt::SingleColor { options } if options.contains(&t) => {
                        offered(state, wf, GameAction::ChooseManaColor { choice: ManaChoice::SingleColor(t), count: 1 }, cur)
                    }
                    ManaChoicePrompt::Combination { options } if options.iter().any(|o| o.len() == 1 && o[0] == t) => {
                        offered(state, wf, GameAction::ChooseManaColor { choice: ManaChoice::Combination(vec![t]), count: 1 }, cur)
                    }
                    _ => Err(miss(wf, cur, "gorge's colour is not one phase offers")),
                }
            });
            or_forced(state, wf, cur, a)
        }
        WaitingFor::NamedChoice { options, .. } | WaitingFor::CostTypeChoice { options, .. } => {
            let a = take(cur, |d| is_colour_decision(d) || is_label_decision(d)).map(|d| {
                let label = d.picks.first().cloned().unwrap_or_default();
                let alt = colour_of(&label).map(colour_word);
                let hit = options.iter().find(|o| {
                    o.eq_ignore_ascii_case(label.trim()) || alt.as_deref().is_some_and(|w| o.eq_ignore_ascii_case(w))
                });
                match hit {
                    Some(o) => offered(state, wf, GameAction::ChooseOption { choice: o.clone() }, cur),
                    None => Err(miss(wf, cur, &format!("gorge's {label:?} is not among phase's options"))),
                }
            });
            or_forced(state, wf, cur, a)
        }

        // Trigger order.
        WaitingFor::OrderTriggers { triggers, .. } => {
            let a = take(cur, is_trigger_order).map(|d| {
                match trigger_order(triggers, &trigger_order_stack_first(&d)) {
                    Ok(order) => ok(GameAction::OrderTriggers { order }),
                    Err(e) => Err(harness(format!("trigger order at step {}: {e}", cur.step))),
                }
            });
            or_forced(state, wf, cur, a)
        }

        // Divided damage: picks repeat a target once per point.
        WaitingFor::DistributeAmong { total, targets, .. } => {
            let board = cur.board;
            let a = take(cur, is_damage_split).map(|d| {
                let mut dist: Vec<(TargetRef, u32)> = Vec::new();
                for (i, label) in d.picks.iter().enumerate() {
                    let r = d.pick_refs.get(i).unwrap_or(label);
                    let Some(t) = Ref::parse(r).ok().and_then(|r| target_of(board, &r)).filter(|t| targets.contains(t)) else {
                        return Err(miss(wf, cur, &format!("split target {r:?} is not one of phase's")));
                    };
                    match dist.iter_mut().find(|(x, _)| *x == t) {
                        Some((_, n)) => *n += 1,
                        None => dist.push((t, 1)),
                    }
                }
                if dist.iter().map(|(_, n)| n).sum::<u32>() != *total {
                    return Err(miss(wf, cur, &format!("gorge split {} point(s), phase divides {total}", d.picks.len())));
                }
                ok(GameAction::DistributeAmong { distribution: dist })
            });
            or_forced(state, wf, cur, a)
        }

        // Combat. The declarations are the step machine's (`pass_to`/`attack`/`block` stop at the
        // prompt and apply the step's own lists), so the answerer is never asked.
        WaitingFor::DeclareAttackers { .. } | WaitingFor::DeclareBlockers { .. } => {
            Err(harness(format!("answerer asked about {} at step {}", variant_name(wf), cur.step)))
        }
        // CR 510.1a: a lone blocker takes all of the attacker's damage. Several blockers (or trample)
        // divide it, and gorge logs no such decision: never invent an order.
        WaitingFor::AssignCombatDamage { blockers, total_damage, trample, .. } => {
            if blockers.len() == 1 && trample.is_none() {
                cur.notes.push(format!("forced AssignCombatDamage at step {}", cur.step));
                ok(GameAction::AssignCombatDamage {
                    mode: Default::default(),
                    assignments: vec![(blockers[0].blocker_id, *total_damage)],
                    trample_damage: 0,
                    controller_damage: 0,
                })
            } else {
                Err(miss(wf, cur, ""))
            }
        }
        // CR 510.1d: a blocker only splits its damage when it blocks several attackers (banding).
        WaitingFor::AssignBlockerDamage { attackers, total_damage, .. } => {
            if attackers.len() == 1 {
                cur.notes.push(format!("forced AssignBlockerDamage at step {}", cur.step));
                ok(GameAction::AssignBlockerDamage { assignments: vec![(attackers[0], *total_damage)] })
            } else {
                Err(miss(wf, cur, ""))
            }
        }
        // Optional costs of untapping and attacking: gorge's yes/no (or object) decision, else forced.
        WaitingFor::UntapChoice { candidates, .. } => {
            let a = take(cur, is_yes_no_like).and_then(|d| {
                candidates.first().map(|id| ok(GameAction::ChooseUntap { object_id: *id, untap: accepts(&d) }))
            });
            or_forced(state, wf, cur, a)
        }
        WaitingFor::ExertChoice { .. } => {
            let a = take(cur, is_yes_no_like).map(|d| ok(GameAction::ChooseExert { exert: accepts(&d) }));
            or_forced(state, wf, cur, a)
        }
        WaitingFor::EnlistChoice { eligible, .. } => {
            let a = if let Some(d) = take(cur, is_object_choice) {
                let ids = cards_of(cur, &d, eligible);
                if ids.len() > 1 {
                    Some(Err(miss(wf, cur, &format!("gorge picked {} creature(s), enlist taps up to one", ids.len()))))
                } else {
                    Some(ok(GameAction::ChooseEnlist { target: ids.first().copied() }))
                }
            } else {
                take(cur, is_yes_no_like).map(|d| {
                    if accepts(&d) {
                        forced(state, wf, cur)
                    } else {
                        ok(GameAction::ChooseEnlist { target: None })
                    }
                })
            };
            or_forced(state, wf, cur, a)
        }

        // Everything else only ever forces: no level-A scenario scripts it.
        WaitingFor::MeldPairChoice { .. }
        | WaitingFor::MeldAttackTargetChoice { .. }
        | WaitingFor::EntryAttackTargetChoice { .. }
        | WaitingFor::MulliganDecision { .. }
        | WaitingFor::OpeningHandBottomCards { .. }
        | WaitingFor::ManaPayment { .. }
        | WaitingFor::ManaSourceSelection { .. }
        | WaitingFor::AssistChoosePlayer { .. }
        | WaitingFor::AssistPayment { .. }
        | WaitingFor::ChooseUntapSubset { .. }
        | WaitingFor::ReplacementChoice { .. }
        | WaitingFor::EntryControllerChoice { .. }
        | WaitingFor::CopyTargetChoice { .. }
        | WaitingFor::ExploreChoice { .. }
        | WaitingFor::ReturnAsAuraTarget { .. }
        | WaitingFor::ChooseTokenHost { .. }
        | WaitingFor::EquipTarget { .. }
        | WaitingFor::CrewVehicle { .. }
        | WaitingFor::StationTarget { .. }
        | WaitingFor::SaddleMount { .. }
        | WaitingFor::RippleRevealChoice { .. }
        | WaitingFor::RippleBottomOrder { .. }
        | WaitingFor::RevealUntilBottomOrder { .. }
        | WaitingFor::DigBottomOrder { .. }
        | WaitingFor::ArrangePlanarDeckTopChoice { .. }
        | WaitingFor::RedistributeLifeTotals { .. }
        | WaitingFor::CoinFlipKeepChoice { .. }
        | WaitingFor::DieKeepChoice { .. }
        | WaitingFor::DigRestSplitChoice { .. }
        | WaitingFor::SearchPartitionChoice { .. }
        | WaitingFor::OutsideGameChoice { .. }
        | WaitingFor::BeholdChoice { .. }
        | WaitingFor::EmpowerJaceChoice { .. }
        | WaitingFor::SpellCopyOrderChoice { .. }
        | WaitingFor::HeroEventChoice { .. }
        | WaitingFor::DrawnThisTurnTopdeckChoice { .. }
        | WaitingFor::LearnChoice { .. }
        | WaitingFor::ManifestDreadChoice { .. }
        | WaitingFor::BetweenGamesSideboard { .. }
        | WaitingFor::BetweenGamesChoosePlayDraw { .. }
        | WaitingFor::OpponentGuess { .. }
        | WaitingFor::SpellbookDraft { .. }
        | WaitingFor::DamageSourceChoice { .. }
        | WaitingFor::ChooseGiftRecipient { .. }
        | WaitingFor::SpliceOffer { .. }
        | WaitingFor::OrderCostReductions { .. }
        | WaitingFor::CastOffer { .. }
        | WaitingFor::MutateMergeChoice { .. }
        | WaitingFor::CipherEncodeChoice { .. }
        | WaitingFor::ChoosePermanentTypeSlot { .. }
        | WaitingFor::ResolutionOptionalPaymentChoice { .. }
        | WaitingFor::PairChoice { .. }
        | WaitingFor::MiracleReveal { .. }
        | WaitingFor::WardDiscardChoice { .. }
        | WaitingFor::WardSacrificeChoice { .. }
        | WaitingFor::UnlessBounceChoice { .. }
        | WaitingFor::ChooseRingBearer { .. }
        | WaitingFor::ChooseRoomDoor { .. }
        | WaitingFor::ChooseDungeon { .. }
        | WaitingFor::ChooseDungeonRoom { .. }
        | WaitingFor::SpecializeColor { .. }
        | WaitingFor::ActivationCostOneOfChoice { .. }
        | WaitingFor::BlightChoice { .. }
        | WaitingFor::PayManaAbilityMana { .. }
        | WaitingFor::ManaAbilityManaPayment { .. }
        | WaitingFor::CollectEvidenceChoice { .. }
        | WaitingFor::HarmonizeTapChoice { .. }
        | WaitingFor::TopOrBottomChoice { .. }
        | WaitingFor::PopulateChoice { .. }
        | WaitingFor::ClashChooseOpponent { .. }
        | WaitingFor::ChooseFromZoneOpponentChooser { .. }
        | WaitingFor::ChooseAnnouncingOpponent { .. }
        | WaitingFor::ClashCardPlacement { .. }
        | WaitingFor::PhysicalContest { .. }
        | WaitingFor::VoteChoice { .. }
        | WaitingFor::SeparatePilesChooseOpponent { .. }
        | WaitingFor::SeparatePilesPartition { .. }
        | WaitingFor::SeparatePilesChoice { .. }
        | WaitingFor::SecretAgendaChoice { .. }
        | WaitingFor::CompanionReveal { .. }
        | WaitingFor::ChooseLegend { .. }
        | WaitingFor::CommanderZoneChoice { .. }
        | WaitingFor::BattleProtectorChoice { .. }
        | WaitingFor::ProliferateChoice { .. }
        | WaitingFor::TimeTravelChoice { .. }
        | WaitingFor::ChooseObjectsSelection { .. }
        | WaitingFor::CategoryChoice { .. }
        | WaitingFor::EachPlayerCopyChosenSelection { .. }
        | WaitingFor::KeepWithinTotalPowerChoice { .. }
        | WaitingFor::KeepExactPermanentsChoice { .. }
        | WaitingFor::CopyRetarget { .. }
        | WaitingFor::MoveCountersDistribution { .. }
        | WaitingFor::RemoveCountersChoice { .. }
        | WaitingFor::PayAmountChoice { .. }
        | WaitingFor::RetargetChoice { .. }
        | WaitingFor::CombatTaxPayment { .. }
        | WaitingFor::PhyrexianPayment { .. }
        => forced(state, wf, cur),
    }
}

/// Which of the two costs of a "do A or pay B" choice gorge's label names: the one whose shape
/// carries the label's first word (`Sacrifice 1 creature`), else a `Pay ...` label is the mana cost.
fn choice_index(first: &engine::types::ability::AbilityCost, second: &engine::types::ability::AbilityCost, label: &str) -> Option<usize> {
    let word = fold(label.split_whitespace().next().unwrap_or(""));
    let shape = |c: &engine::types::ability::AbilityCost| fold(&format!("{c:?}"));
    if !word.is_empty() {
        match (shape(first).contains(&word), shape(second).contains(&word)) {
            (true, false) => return Some(0),
            (false, true) => return Some(1),
            _ => {}
        }
    }
    if word == "pay" {
        let mana = |c: &engine::types::ability::AbilityCost| format!("{c:?}").starts_with("Mana");
        return match (mana(first), mana(second)) {
            (true, false) => Some(0),
            (false, true) => Some(1),
            _ => None,
        };
    }
    None
}

/// phase's `OrderTriggers.order`: `order[k]` is the index (into the prompt's triggers) of the
/// trigger placed k-th on the stack, so index 0 goes on FIRST (the bottom of the controller's
/// group) and resolves LAST (CR 603.3b, CR 405.3). gorge's `picks[0]` is likewise stack-first, so
/// the picks line up with `order` one to one. A pick matches a trigger by its ability text, else
/// by its source's name; two triggers it cannot tell apart (same-named sources) are an error.
fn trigger_order(triggers: &[engine::types::game_state::PendingTriggerSummary], picks: &[String]) -> Result<Vec<usize>, String> {
    let mut order: Vec<usize> = Vec::new();
    for label in picks {
        let by_desc: Vec<usize> =
            (0..triggers.len()).filter(|i| !order.contains(i) && text_match(&triggers[*i].description, label)).collect();
        let by_name: Vec<usize> =
            (0..triggers.len()).filter(|i| !order.contains(i) && text_match(&triggers[*i].source_name, label)).collect();
        match (by_desc.as_slice(), by_name.as_slice()) {
            ([i], _) | (_, [i]) => order.push(*i),
            _ => {
                return Err(format!(
                    "{label:?} matches {} by text and {} by source name of phase's triggers (same-named sources cannot be told apart)",
                    by_desc.len(),
                    by_name.len()
                ))
            }
        }
    }
    if order.len() != triggers.len() {
        return Err(format!("gorge ordered {} of phase's {} triggers", order.len(), triggers.len()));
    }
    Ok(order)
}

/// gorge asks "pay the unless cost?" as a yes/no or, from the resolve fallback, as a one-pick mode
/// decision (`resume == "unless_pay"`, label `Don't pay` to decline).
fn is_unless_pay(d: &Decision) -> bool {
    is_yes_no_like(d) || d.resume == "unless_pay"
}

/// A declined optional trigger cost ("Do not pay") is a no, though the shared yes/no shapes do
/// not list its pick kind.
fn declines_trigger_cost(d: &Decision) -> bool {
    d.pick_kinds.iter().any(|k| k == "trigger_cost_decline")
}

fn is_yes_no_like(d: &Decision) -> bool {
    is_yes_no(d) || declines_trigger_cost(d)
}

fn accepts(d: &Decision) -> bool {
    !declines_trigger_cost(d) && yes(d)
}

fn pays_unless(d: &Decision) -> bool {
    if d.resume == "unless_pay" {
        let l = d.picks.first().map(|p| p.trim().to_ascii_lowercase()).unwrap_or_default();
        return !l.is_empty() && !l.starts_with("don't") && !l.starts_with("dont") && !l.starts_with("no");
    }
    accepts(d)
}

/// A colour pick: `choose_n` with a `color` pick kind or an `Add X` label.
fn is_colour_decision(d: &Decision) -> bool {
    d.kind == "choose_n" && (d.pick_kinds.iter().any(|k| k == "color") || d.picks.iter().any(|p| p.starts_with("Add ")))
}

/// A single label pick that is not any of the other decision shapes.
fn is_label_decision(d: &Decision) -> bool {
    d.picks.len() == 1
        && !is_yes_no(d)
        && !is_arrange(d)
        && !is_damage_split(d)
        && d.object_picks.is_empty()
        && !matches!(d.kind.as_str(), "target" | "order" | "attackers" | "blockers" | "mode")
}

/// The colour a label names: `Add R`, `R`, `Red`.
fn colour_of(label: &str) -> Option<ManaType> {
    let word = label.trim().rsplit(' ').next()?;
    match word.to_ascii_lowercase().as_str() {
        "white" | "w" => Some(ManaType::White),
        "blue" | "u" => Some(ManaType::Blue),
        "black" | "b" => Some(ManaType::Black),
        "red" | "r" => Some(ManaType::Red),
        "green" | "g" => Some(ManaType::Green),
        "colorless" | "c" => Some(ManaType::Colorless),
        _ => None,
    }
}

fn colour_word(t: ManaType) -> String {
    match t {
        ManaType::White => "White",
        ManaType::Blue => "Blue",
        ManaType::Black => "Black",
        ManaType::Red => "Red",
        ManaType::Green => "Green",
        ManaType::Colorless => "Colorless",
    }
    .to_string()
}

/// A card-picking prompt (`SelectCards`): gorge's object choice (refs first, then same name); a
/// pick that finds no card is dropped, so a search that finds nothing answers empty. `top` is
/// for prompts whose picks may also be an arrangement. `lo..=hi` is the count phase takes
/// (`up_to` lowers `lo` to 0).
#[allow(clippy::too_many_arguments)]
fn pick_cards(
    state: &GameState,
    wf: &WaitingFor,
    cards: &[ObjectId],
    lo: usize,
    hi: usize,
    up_to: bool,
    arrangement_too: bool,
    cur: &mut Cursor,
) -> Result<Answer, Stop> {
    let lo = if up_to { lo } else { hi.min(cards.len()) };
    let picked = take(cur, |d| is_object_choice(d) || (arrangement_too && is_arrange(d)));
    let picked = match picked {
        Some(d) => Some(d),
        // An explicit empty choice, when the prompt allows taking nothing.
        None if lo == 0 => take(cur, |d| {
            d.kind == "choose_n" && d.picks.is_empty() && d.pick_refs.is_empty() && d.object_picks.is_empty() && !is_damage_split(d)
        }),
        None => None,
    };
    let Some(d) = picked else { return forced(state, wf, cur) };
    let ids = cards_of(cur, &d, cards);
    if ids.len() > hi || ids.len() < lo {
        return Err(miss(wf, cur, &format!("gorge picked {} card(s), phase takes {lo}..={hi}", ids.len())));
    }
    ok(GameAction::SelectCards { cards: ids })
}

#[cfg(test)]
mod tests {
    use super::*;
    use engine::types::game_state::PendingTriggerSummary;
    use follower_proto::batch::RowBuilder;
    use follower_proto::row::Snapshot;
    use std::sync::OnceLock;

    fn db() -> &'static engine::database::card_db::CardDatabase {
        static DB: OnceLock<engine::database::card_db::CardDatabase> = OnceLock::new();
        DB.get_or_init(|| {
            let path = std::env::var("PHASE_CARD_DATA").expect("PHASE_CARD_DATA: run make phase-follower-test");
            crate::load_db(&path).expect("load card data")
        })
    }

    fn go(json: &str) -> (Result<(), Stop>, RowBuilder) {
        let req = Request::parse(json).expect("request");
        let mut rb = RowBuilder::default();
        let out = crate::setup::run(db(), &req, &mut rb).and_then(|board| crate::steps::run(&req, board, &mut rb));
        (out, rb)
    }

    fn last(json: &str) -> (Snapshot, RowBuilder) {
        let (out, rb) = go(json);
        out.unwrap_or_else(|e| panic!("stopped: {e:?}; notes {:?}", rb.notes));
        (rb.snapshots.last().expect("a snapshot").clone(), rb)
    }

    fn bf(s: &Snapshot, seat: i64, name: &str) -> usize {
        s.permanents.iter().filter(|p| p.controller == seat && p.name == name).count()
    }

    fn bolt(card: &str, mana: &str, mode: &str, decisions: &str) -> String {
        format!(
            r#"{{"id":"t","item":{{"id":"t","setup":{{"p0":{{"hand":["{card}"]}},"p1":{{"battlefield":["Grizzly Bears","Runeclaw Bear"]}}}},
              "steps":[{{"op":"cast","seat":0,"card":"p0:{card}","mana":"{mana}","cast_mode":"{mode}"}},{{"op":"resolve","seat":0}}]}},
              "gorge_decisions":[{decisions}]}}"#
        )
    }

    const PLAYER_TARGET: &str = r#"{"step":0,"seat":0,"kind":"target","picks":["p1"],"pick_refs":["p1"],"pick_kinds":["player"]}"#;
    const BEARS_TARGET: &str =
        r#"{"step":0,"seat":0,"kind":"target","picks":["Grizzly Bears"],"pick_refs":["p1:Grizzly Bears"],"pick_kinds":["permanent"]}"#;

    #[test]
    fn answer_target_player() {
        let (s, rb) = last(&bolt("Lightning Strike", "1R", "", PLAYER_TARGET));
        assert_eq!(s.players[1].life, 17);
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 1);
        assert_eq!(rb.leftover, None, "the decision was consumed");
    }

    #[test]
    fn answer_target_permanent_by_ref() {
        let (s, rb) = last(&bolt("Lightning Strike", "1R", "", BEARS_TARGET));
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 0);
        assert_eq!(s.players[1].graveyard, vec!["Grizzly Bears".to_string()]);
        assert_eq!(s.players[1].life, 20);
        assert_eq!(rb.leftover, None);
    }

    #[test]
    fn answer_up_to_left_empty() {
        // "up to one target": gorge's explicit empty target decision leaves the slot empty.
        let (s, rb) = last(&bolt("Impractical Joke", "R", "", r#"{"step":0,"seat":0,"kind":"target"}"#));
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 1, "no target: nothing was damaged");
        assert_eq!(rb.leftover, None);
        assert!(rb.notes.iter().any(|n| n.contains("left empty")), "{:?}", rb.notes);
        // With a target scripted, the same slot is filled.
        let (s, _) = last(&bolt("Impractical Joke", "R", "", BEARS_TARGET));
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 0);
        // No decision at all for the optional slot is a miss, not a guess.
        let (out, _) = go(&bolt("Impractical Joke", "R", "", ""));
        assert_eq!(out.err(), Some(Stop::StrictMiss(
            "Missing TargetSelection answer at step 0: no scripted target for an optional slot".to_string())));
    }

    #[test]
    fn answer_mode_by_neutral_ordinal() {
        // Abrade: mode 0 is the damage, mode 1 destroys an artifact. gorge's mode decision is index 0
        // of gorge_decisions and neutral.mode names its ordinal and text.
        let mode = r#"{"step":0,"seat":0,"kind":"mode","picks":["CARDNAME deals 3 damage to target creature."],"pick_kinds":["mode"],"resume":"cast_modes","gorge_kind":"modes"}"#;
        let json = bolt("Abrade", "1R", "", &format!("{mode},{BEARS_TARGET}"));
        let with = |ordinal: u32, text: &str| {
            let v: serde_json::Value = serde_json::from_str(&json).unwrap();
            let mut v = v;
            v["neutral"] = serde_json::json!({"mode": {"0": {"step": 0, "picks": [{"ordinal": ordinal, "text": text}]}}});
            v.to_string()
        };
        let (s, rb) = last(&with(0, "~ deals 3 damage to target creature."));
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 0);
        assert_eq!(rb.leftover, None);
        // The ordinal names the wrong mode for the text: a strict miss that says so.
        let (out, _) = go(&with(1, "~ deals 3 damage to target creature."));
        match out.err() {
            Some(Stop::StrictMiss(m)) => assert!(m.starts_with("Missing ModeChoice answer at step 0: mode 1 text differs"), "{m}"),
            other => panic!("{other:?}"),
        }
        // Without a neutral entry the label is matched against phase's mode text.
        let (s, _) = last(&json);
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 0);
    }

    #[test]
    fn answer_x_value() {
        let x = r#"{"step":0,"seat":0,"kind":"choose_n","picks":["X = 2"],"pick_kinds":["x"]}"#;
        let (s, rb) = last(&bolt("Blaze", "2R", "", &format!("{PLAYER_TARGET},{x}")));
        assert_eq!(s.players[1].life, 18, "{:?}", rb.notes);
        assert_eq!(rb.leftover, None);
    }

    #[test]
    fn answer_optional_cost_kicked() {
        // Burst Lightning: 2 damage, 4 when kicked ({4}).
        let (kicked, rb) = last(&bolt("Burst Lightning", "4R", "kicked", PLAYER_TARGET));
        assert_eq!(kicked.players[1].life, 16, "notes {:?}", rb.notes);
        // No cast_mode: the kicker is declined.
        let (plain, _) = last(&bolt("Burst Lightning", "R", "", PLAYER_TARGET));
        assert_eq!(plain.players[1].life, 18);
    }

    #[test]
    fn answer_discard_by_ref() {
        // Mind Rot: p1 discards two of three; gorge's pick names two of them by ref.
        let discard = r#"{"step":1,"seat":1,"kind":"choose_n","picks":["Island","Forest"],"pick_refs":["p1:Island","p1:Forest"],"pick_refs_inexact":[false,false],"object_picks":["p1:Island","p1:Forest"],"pick_kinds":["discard","discard"]}"#;
        let json = format!(
            r#"{{"id":"t","item":{{"id":"t","setup":{{"p0":{{"hand":["Mind Rot"]}},"p1":{{"hand":["Forest","Island","Mountain"]}}}},
              "steps":[{{"op":"cast","seat":0,"card":"p0:Mind Rot","mana":"2B"}},{{"op":"resolve","seat":0}}]}},
              "gorge_decisions":[{PLAYER_TARGET},{discard}]}}"#
        );
        let (s, rb) = last(&json);
        let mut gy = s.players[1].graveyard.clone();
        gy.sort();
        assert_eq!(gy, vec!["Forest".to_string(), "Island".to_string()]);
        assert_eq!(s.players[1].hand, vec!["Mountain".to_string()]);
        assert_eq!(rb.leftover, None);
    }

    #[test]
    fn answer_scry_keeps_picks_on_top() {
        // Opt: scry 1, then draw. gorge's arrange decision lists the cards kept on top.
        let run = |arrange: &str| {
            let json = format!(
                r#"{{"id":"t","item":{{"id":"t","setup":{{"p0":{{"hand":["Opt"],"library_top":["Forest","Island"]}}}},
                  "steps":[{{"op":"cast","seat":0,"card":"p0:Opt","mana":"U"}},{{"op":"resolve","seat":0}}]}},
                  "gorge_decisions":[{arrange}]}}"#
            );
            last(&json).0
        };
        let kept = run(r#"{"step":1,"seat":0,"kind":"order","gorge_kind":"arrange","picks":["Forest"],"pick_refs":["p0:Forest"],"pick_refs_inexact":[true],"object_picks":["p0:Forest"],"pick_kinds":["bottom"],"resume":"arrange"}"#);
        assert_eq!(kept.players[0].hand, vec!["Forest".to_string()], "the kept card is drawn");
        // An empty answer bottoms the looked-at card, so the next one is drawn.
        let bottomed = run(r#"{"step":1,"seat":0,"kind":"order","gorge_kind":"arrange","arrange_labels":["Forest"],"arrange_kind":"bottom","resume":"arrange"}"#);
        assert_eq!(bottomed.players[0].hand, vec!["Island".to_string()], "the bottomed card is not drawn");
    }

    fn summary(id: u64, source: &str, text: &str) -> PendingTriggerSummary {
        PendingTriggerSummary { source_id: ObjectId(id), source_name: source.to_string(), description: text.to_string() }
    }

    #[test]
    fn answer_trigger_order_direction() {
        // phase: order[0] is placed on the stack first and resolves last. gorge's picks[0] is
        // stack-first too, so the order lists the triggers in pick order.
        let triggers = [summary(1, "Alpha", "Whenever a creature dies, you gain 1 life."), summary(2, "Beta", "Whenever a creature dies, draw a card.")];
        assert_eq!(trigger_order(&triggers, &["Alpha".into(), "Beta".into()]), Ok(vec![0, 1]));
        assert_eq!(trigger_order(&triggers, &["Beta".into(), "Alpha".into()]), Ok(vec![1, 0]));
        // By ability text as well as by source name.
        assert_eq!(trigger_order(&triggers, &["draw a card".into(), "gain 1 life".into()]), Ok(vec![1, 0]));
        // Same-named sources with the same text cannot be told apart.
        let twins = [summary(1, "Twin", "Whenever X, do Y."), summary(2, "Twin", "Whenever X, do Y.")];
        assert!(trigger_order(&twins, &["Twin".into(), "Twin".into()]).is_err());
        // Fewer picks than triggers is an error, not a guess.
        assert!(trigger_order(&triggers, &["Alpha".into()]).is_err());
    }

    #[test]
    fn answer_sole_target_decision_is_consumed() {
        // One creature and no player target: phase targets it without a prompt, and gorge's target
        // decision for the same object is consumed rather than reported.
        let json = format!(
            r#"{{"id":"t","item":{{"id":"t","setup":{{"p0":{{"hand":["Disfigure"]}},"p1":{{"battlefield":["Grizzly Bears"]}}}},
              "steps":[{{"op":"cast","seat":0,"card":"p0:Disfigure","mana":"B"}},{{"op":"resolve","seat":0}}]}},
              "gorge_decisions":[{BEARS_TARGET}]}}"#
        );
        let (s, rb) = last(&json);
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 0);
        assert_eq!(rb.leftover, None, "{:?}", rb.notes);
        // A decision naming another object is not explained by phase's choice: it stays.
        let other = BEARS_TARGET.replace("Grizzly Bears", "Hill Giant");
        let json = json.replace(BEARS_TARGET, &other);
        let (_, rb) = last(&json);
        assert!(rb.leftover.is_some_and(|l| l.contains("Hill Giant")));
    }

    #[test]
    fn answer_multi_candidate_without_decision_is_strict_miss() {
        let (out, rb) = go(&bolt("Lightning Strike", "1R", "", ""));
        assert_eq!(out.err(), Some(Stop::StrictMiss("Missing TargetSelection answer at step 0".to_string())));
        assert_eq!(rb.snapshots.len(), 1, "only the setup snapshot");
        // A target that is not legal (a land no one controls) is a miss too, never a substitute.
        let bad = r#"{"step":0,"seat":0,"kind":"target","picks":["p1:Island"],"pick_refs":["p1:Island"],"pick_kinds":["permanent"]}"#;
        let (out, _) = go(&bolt("Lightning Strike", "1R", "", bad));
        assert_eq!(out.err(), Some(Stop::StrictMiss("Missing TargetSelection answer at step 0".to_string())));
    }

    #[test]
    fn answer_cast_mode_not_honoured_is_harness() {
        // A kicker the card does not have: the spell would be cast plain, so the row is the
        // follower's (harness), never a phase rules difference.
        let (out, _) = go(&bolt("Lightning Strike", "1R", "kicked", PLAYER_TARGET));
        match out.err() {
            Some(Stop::Harness(m)) => assert!(m.starts_with("phase follower: cast_mode kicked not honoured at step 0"), "{m}"),
            other => panic!("{other:?}"),
        }
        // plot is gorge's special action; phase's plot variant casts an already plotted card.
        let (out, _) = go(&bolt("Lightning Strike", "1R", "plot", PLAYER_TARGET));
        assert_eq!(out.err(), Some(Stop::Harness("phase follower: cast_mode plot unsupported".to_string())));
    }

    #[test]
    fn answer_adventure_alt_from_hand() {
        // Bonecrusher Giant from hand: phase asks which face (CastOffer). adventure_alt casts Stomp.
        let (s, rb) = last(&bolt("Bonecrusher Giant", "1R", "adventure_alt", PLAYER_TARGET));
        assert_eq!(s.players[1].life, 18, "notes {:?}", rb.notes);
        assert_eq!(bf(&s, 0, "Bonecrusher Giant"), 0);
        // No mode: the creature face.
        let (s, _) = last(&bolt("Bonecrusher Giant", "2R", "", ""));
        assert_eq!(bf(&s, 0, "Bonecrusher Giant"), 1);
    }

    #[test]
    fn answer_sweep_leaves_decisions_of_answered_targets() {
        // The spell's target came from the prompt, so the sweep must not take the second target
        // decision (a later prompt's) when the spell lands on the stack.
        let (s, rb) = last(&bolt("Lightning Strike", "1R", "", &format!("{BEARS_TARGET},{BEARS_TARGET}")));
        assert_eq!(bf(&s, 1, "Grizzly Bears"), 0);
        assert!(rb.leftover.as_deref().is_some_and(|l| l.contains("target")), "{:?}", rb.leftover);
    }
}
