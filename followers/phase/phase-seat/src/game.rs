//! The phase-hosted game behind the SpellBench v2 engine role: [`PhaseBackend`] builds a two-player
//! game from two catalog decks, poses each acting seat's `WaitingFor` as a seat decision, and applies
//! the picked candidate with phase's `apply`.
//!
//! Both seats are agent seats: the host (four-yay's `cmd/xclient -engine sbv2`) answers each, with
//! its own policy on either seat. phase-ai is not linked (it is a separate, larger build; see the
//! ticket's report), so there is no in-process house seat.
//!
//! Every `WaitingFor` goes through [`classify`](crate::waiting::classify). A variant it does not pose
//! halts the game with `engine_contract_failure:unmapped_decision` and is counted in
//! [`Census::engine_errors`] under its name; nothing is answered on an agent's behalf. A legal
//! priority action the mapper has no `semantic` for (an equip, a crew, ...) is left out of the
//! candidates and counted in [`Census::unmapped_actions`].

use std::collections::{BTreeMap, HashMap};
use std::path::Path;

use engine::ai_support::AiDecisionContract;
use engine::database::card_db::CardDatabase;
use engine::game::combat::AttackTarget;
use engine::game::deck_loading::{load_and_hydrate_decks, resolve_deck_list, DeckList, PlayerDeckList};
use engine::game::{apply, start_game_skip_mulligan, start_game_with_starting_player};
use engine::types::ability::TargetRef;
use engine::types::actions::{GameAction, MulliganChoice};
use engine::types::game_state::{GameState, MulliganDecisionPhase, WaitingFor};
use engine::types::identifiers::ObjectId;
use engine::types::player::PlayerId;
use serde_json::{json, Value};

use crate::json::canonical;
use crate::semantic as sem;
use crate::server::{Backend, CatalogDeck, Outcome, Reset};
use crate::view::{object_ref, observation, seat, Ids};
use crate::waiting::{classify, Family, Pose};

/// Counts of what the host could not pose. Printed to stderr when the process ends.
#[derive(Default, Debug)]
pub struct Census {
    /// `WaitingFor` variants (and sub-states) with no mapping: the game halted on each.
    pub engine_errors: BTreeMap<String, u64>,
    /// Legal `GameAction` variants at a posed prompt with no `semantic`: left out of the candidates.
    pub unmapped_actions: BTreeMap<String, u64>,
    /// Games halted for any other `engine_contract_failure` cause.
    pub halts: BTreeMap<String, u64>,
    pub decisions: u64,
    pub games: u64,
}

impl Census {
    pub fn report(&self) -> String {
        let mut s = format!("phase-seat census: games={} decisions={}\n", self.games, self.decisions);
        for (title, m) in [("engine_error", &self.engine_errors), ("unmapped_action", &self.unmapped_actions), ("halt", &self.halts)] {
            for (k, n) in m {
                s.push_str(&format!("  {title} {k} {n}\n"));
            }
        }
        s
    }
}

/// How a picked candidate acts.
#[derive(Clone, Debug)]
enum Effect {
    /// Apply this action now.
    Act(GameAction),
    /// One substep of the open group.
    Sub(Sub),
}

#[derive(Clone, Debug)]
enum Sub {
    Attack(Option<AttackTarget>),
    Block(Option<ObjectId>),
    Card(ObjectId),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum CardsFor {
    Discard,
    MulliganBottom,
}

enum Plan {
    Attack {
        creatures: Vec<ObjectId>,
        all: Vec<AttackTarget>,
        by_attacker: Option<HashMap<ObjectId, Vec<AttackTarget>>>,
        picks: Vec<(ObjectId, AttackTarget)>,
    },
    Block {
        blockers: Vec<ObjectId>,
        targets: HashMap<ObjectId, Vec<ObjectId>>,
        picks: Vec<(ObjectId, ObjectId)>,
    },
    Cards {
        pool: Vec<ObjectId>,
        picks: Vec<ObjectId>,
        count: usize,
        what: CardsFor,
    },
}

/// A decision asked as several `choose` lines: `index` of `total` substeps are done.
struct Group {
    total: usize,
    index: usize,
    plan: Plan,
}

struct Cand {
    semantic: Value,
    effect: Effect,
}

struct Live {
    state: GameState,
    ids: Ids,
    cands: Vec<Cand>,
    acting: PlayerId,
    /// Who submits the action: the acting seat, or whoever controls it (CR 722).
    actor: PlayerId,
    group: Option<Group>,
    /// Per-seat counters: groups completed and decisions answered.
    groups: [u64; 2],
    seat_steps: [u64; 2],
    decisions: u64,
    steps: u64,
    max_decisions: u64,
    max_steps: u64,
}

pub struct PhaseBackend {
    db: CardDatabase,
    decks: Vec<CatalogDeck>,
    identity: Value,
    live: Option<Live>,
    pub census: Census,
}

/// A gorge deck JSON (`{"name": .., "cards": [{"name": .., "count": ..}]}`) as a catalog deck whose id
/// is the file stem: the same `-decks DIR` convention `sbv2dirac` documents and `xclient` writes.
pub fn load_decks(dir: &Path) -> Result<Vec<CatalogDeck>, String> {
    let mut out = Vec::new();
    let entries = std::fs::read_dir(dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    for e in entries {
        let path = e.map_err(|e| e.to_string())?.path();
        if path.extension().and_then(|x| x.to_str()) != Some("json") {
            continue;
        }
        let stem = path.file_stem().and_then(|s| s.to_str()).ok_or("bad deck file name")?.to_string();
        let raw = std::fs::read(&path).map_err(|e| format!("{}: {e}", path.display()))?;
        let v: Value = serde_json::from_slice(&raw).map_err(|e| format!("{}: {e}", path.display()))?;
        let mut rows = Vec::new();
        for c in v["cards"].as_array().ok_or_else(|| format!("{}: no cards", path.display()))? {
            let name = c["name"].as_str().ok_or("card without a name")?.to_string();
            let count = c["count"].as_u64().ok_or("card without a count")?;
            rows.push((name, u32::try_from(count).map_err(|_| "count out of range")?));
        }
        let name = v["name"].as_str().filter(|n| !n.is_empty()).unwrap_or(&stem).to_string();
        out.push(CatalogDeck { catalog_id: stem, name, rows });
    }
    out.sort_by(|a, b| a.catalog_id.cmp(&b.catalog_id));
    Ok(out)
}

impl PhaseBackend {
    /// `pin` is `<sha12>+<hash16>` (the follower's `PHASE_REF`); `source_revision` is reported as is.
    pub fn new(db: CardDatabase, decks: Vec<CatalogDeck>, pin: &str, source_revision: Option<&str>) -> Self {
        let (sha, data) = pin.split_once('+').unwrap_or((pin, "unknown"));
        let identity = json!({
            "name": "phase",
            "version": format!("phase-{pin}/phase-seat-0.1.0/autopay"),
            "rules_snapshot_id": format!("phase/{sha}"),
            "card_pool_identity": format!("phase-card-data/{data}"),
            "source_revision": source_revision,
        });
        PhaseBackend { db, decks, identity, live: None, census: Census::default() }
    }

    fn halt(&mut self, cause: &str, detail: &str) -> Outcome {
        *self.census.halts.entry(cause.to_string()).or_insert(0) += 1;
        eprintln!("phase-seat: halted engine_contract_failure:{cause} {detail}");
        let n = self.live.as_ref().map_or(0, |l| l.decisions);
        Outcome::Terminal(json!({"outcome": "halted", "classification": "halted", "winner": null,
            "reason": format!("engine_contract_failure:{cause}"), "decision_count": n}))
    }

    /// Pose whatever the game waits on now.
    fn pose(&mut self) -> Outcome {
        loop {
            let live = self.live.as_mut().expect("a live game");
            if live.group.is_none() {
                if live.decisions >= live.max_decisions {
                    return truncated("max_decisions", live.decisions);
                }
                if live.steps >= live.max_steps {
                    return truncated("max_steps", live.decisions);
                }
            }
            let (name, pose) = classify(&live.state.waiting_for);
            let family = match pose {
                Pose::Terminal => return self.terminal(),
                Pose::Unposed => {
                    *self.census.engine_errors.entry(name.to_string()).or_insert(0) += 1;
                    eprintln!("phase-seat: engine_error unmapped_decision {name}");
                    return self.halt("unmapped_decision", name);
                }
                Pose::Posed(f) => f,
            };
            match build(live, family, &mut self.census) {
                Ok(Built::Decision) => {
                    self.census.decisions += 1;
                    return self.decision(family);
                }
                Ok(Built::Applied) => continue,
                Err(Stop::Halt(cause, detail)) => return self.halt(cause, &detail),
            }
        }
    }

    fn decision(&mut self, family: Family) -> Outcome {
        let live = self.live.as_mut().expect("a live game");
        let who = live.acting;
        let s = who.0 as usize;
        let (index, total) = live.group.as_ref().map_or((0, 1), |g| (g.index, g.total));
        let cands: Vec<Value> =
            live.cands.iter().enumerate().map(|(i, c)| json!({"candidate_id": i, "semantic": c.semantic, "display_text": null})).collect();
        let source = match &live.state.waiting_for {
            WaitingFor::TargetSelection { pending_cast, .. }
            | WaitingFor::OptionalCostChoice { pending_cast, .. }
            | WaitingFor::ChooseXValue { pending_cast, .. } => object_ref(&live.state, &live.ids, who, pending_cast.object_id),
            _ => None,
        };
        Outcome::Decision(json!({
            "acting_seat": seat(who), "seat_step": live.seat_steps[s],
            "group": {"group_id": live.groups[s], "substep_index": index, "substep_count": total},
            "context": {"kind": if family == Family::Priority { "priority" } else { "choice" },
                "source": source, "purpose": null, "text": null, "rewind": false},
            "observation": observation(&live.state, who, &live.ids),
            "candidates": cands, "extensions": {},
        }))
    }

    fn terminal(&mut self) -> Outcome {
        let live = self.live.as_ref().expect("a live game");
        let WaitingFor::GameOver { winner } = live.state.waiting_for else { unreachable!("terminal() on a live prompt") };
        let n = live.decisions;
        match winner {
            None => Outcome::Terminal(json!({"outcome": "draw", "classification": "natural", "winner": null,
                "reason": "draw", "decision_count": n})),
            Some(w) => {
                let loser = &live.state.players[usize::from(1 - w.0)];
                let cause = if loser.life <= 0 {
                    "life_zero"
                } else if loser.poison_counters >= 10 {
                    "poison"
                } else if loser.library.is_empty() {
                    "decked"
                } else {
                    "lost"
                };
                Outcome::Terminal(json!({"outcome": format!("{}_win", seat(w)), "classification": "natural",
                    "winner": seat(w), "reason": format!("{}_{cause}", seat(PlayerId(1 - w.0))), "decision_count": n}))
            }
        }
    }
}

fn truncated(reason: &str, decisions: u64) -> Outcome {
    Outcome::Terminal(json!({"outcome": "truncated", "classification": "truncated", "winner": null,
        "reason": reason, "decision_count": decisions}))
}

enum Built {
    /// `live.cands` holds the pending decision.
    Decision,
    /// The prompt was settled without asking (nothing to choose); look at the next one.
    Applied,
}

enum Stop {
    Halt(&'static str, String),
}

fn apply_now(live: &mut Live, actor: PlayerId, action: GameAction) -> Result<(), Stop> {
    apply(&mut live.state, actor, action).map(|_| ()).map_err(|e| Stop::Halt("submit_rejected", format!("{e:?}")))
}

/// Fill `live.cands` for `family`, or settle the prompt when there is nothing to choose.
fn build(live: &mut Live, family: Family, census: &mut Census) -> Result<Built, Stop> {
    let viewer = |live: &Live, p: PlayerId, id: ObjectId| object_ref(&live.state, &live.ids, p, id);
    let mut cands: Vec<Cand> = Vec::new();
    let mut actor = None;
    let w = live.state.waiting_for.clone();
    match (&w, family) {
        (WaitingFor::Priority { player }, Family::Priority) => {
            let who = *player;
            live.acting = who;
            let contract = AiDecisionContract::issue(&live.state, who);
            actor = Some(contract.authorized_actor);
            let mut seen = std::collections::BTreeSet::new();
            for c in &contract.candidates {
                let sem = match &c.action {
                    GameAction::PassPriority => Some(sem::pass()),
                    GameAction::PlayLand { object_id, .. } => viewer(live, who, *object_id).map(|r| sem::play_land(r, 0)),
                    GameAction::CastSpell { object_id, .. } => viewer(live, who, *object_id).map(|r| sem::cast_spell(r, Some("normal"))),
                    GameAction::ActivateAbility { source_id, ability_index, .. } => {
                        viewer(live, who, *source_id).map(|r| sem::activate_ability(r, *ability_index as u32))
                    }
                    other => {
                        let d = format!("{other:?}");
                        let name = d.split(|c: char| !c.is_alphanumeric()).next().unwrap_or("?").to_string();
                        *census.unmapped_actions.entry(name).or_insert(0) += 1;
                        None
                    }
                };
                if let Some(s) = sem {
                    if seen.insert(canonical(&s)) {
                        cands.push(Cand { semantic: s, effect: Effect::Act(c.action.clone()) });
                    }
                }
            }
            // `pass` first, the rest in canonical order: nothing in the order says anything about hidden zones.
            cands.sort_by_key(|c| (c.semantic["kind"] != "pass", canonical(&c.semantic)));
        }
        (WaitingFor::MulliganDecision { pending, .. }, Family::Mulligan) => {
            let Some(e) = pending.first() else { return Err(Stop::Halt("dead_end", "empty mulligan".into())) };
            live.acting = e.player;
            if let MulliganDecisionPhase::BottomCards { count, .. } = e.phase {
                let hand: Vec<ObjectId> = live.state.players[usize::from(e.player.0)].hand.iter().copied().collect();
                return cards_group(live, e.player, hand, usize::from(count), CardsFor::MulliganBottom);
            }
            let hand = live.state.players[usize::from(e.player.0)].hand.len() as u32;
            for (keep, choice) in [(true, MulliganChoice::Keep), (false, MulliganChoice::Mulligan)] {
                cands.push(Cand {
                    semantic: sem::mulligan(hand, u32::from(e.mulligan_count), keep),
                    effect: Effect::Act(GameAction::MulliganDecision { choice }),
                });
            }
        }
        (
            WaitingFor::DeclareAttackers { player, valid_attacker_ids, valid_attack_targets, valid_attack_targets_by_attacker, .. },
            Family::DeclareAttackers,
        ) => {
            live.acting = *player;
            if live.group.is_none() {
                let mut creatures = valid_attacker_ids.clone();
                creatures.sort();
                if creatures.is_empty() {
                    let a = GameAction::DeclareAttackers { attacks: vec![], bands: vec![] };
                    apply_now(live, *player, a)?;
                    return Ok(Built::Applied);
                }
                live.group = Some(Group {
                    total: creatures.len(),
                    index: 0,
                    plan: Plan::Attack {
                        creatures,
                        all: valid_attack_targets.clone(),
                        by_attacker: valid_attack_targets_by_attacker.clone(),
                        picks: vec![],
                    },
                });
            }
            let g = live.group.as_ref().expect("group");
            let Plan::Attack { creatures, all, by_attacker, .. } = &g.plan else {
                return Err(Stop::Halt("followup_mismatch", "attack group".into()));
            };
            let a = creatures[g.index];
            let Some(aref) = viewer(live, *player, a) else { return Err(Stop::Halt("unresolvable_source", format!("attacker {}", a.0))) };
            let targets = by_attacker.as_ref().and_then(|m| m.get(&a)).unwrap_or(all);
            cands.push(Cand { semantic: sem::declare_attack(aref.clone(), None), effect: Effect::Sub(Sub::Attack(None)) });
            for t in targets {
                let def = match t {
                    AttackTarget::Player(p) => Some(sem::target_player(seat(*p))),
                    AttackTarget::Planeswalker(o) | AttackTarget::Battle(o) => viewer(live, *player, *o).map(sem::target_object),
                };
                if let Some(d) = def {
                    cands.push(Cand { semantic: sem::declare_attack(aref.clone(), Some(d)), effect: Effect::Sub(Sub::Attack(Some(*t))) });
                }
            }
        }
        (WaitingFor::DeclareBlockers { player, valid_blocker_ids, valid_block_targets, .. }, Family::DeclareBlockers) => {
            live.acting = *player;
            if live.group.is_none() {
                let mut blockers = valid_blocker_ids.clone();
                blockers.sort();
                if blockers.is_empty() {
                    let a = GameAction::DeclareBlockers { assignments: vec![] };
                    apply_now(live, *player, a)?;
                    return Ok(Built::Applied);
                }
                live.group = Some(Group {
                    total: blockers.len(),
                    index: 0,
                    plan: Plan::Block { blockers, targets: valid_block_targets.clone(), picks: vec![] },
                });
            }
            let g = live.group.as_ref().expect("group");
            let Plan::Block { blockers, targets, .. } = &g.plan else { return Err(Stop::Halt("followup_mismatch", "block group".into())) };
            let b = blockers[g.index];
            let Some(bref) = viewer(live, *player, b) else { return Err(Stop::Halt("unresolvable_source", format!("blocker {}", b.0))) };
            cands.push(Cand { semantic: sem::declare_block(bref.clone(), None), effect: Effect::Sub(Sub::Block(None)) });
            let mut atks = targets.get(&b).cloned().unwrap_or_default();
            atks.sort();
            for a in atks {
                if let Some(r) = viewer(live, *player, a) {
                    cands.push(Cand { semantic: sem::declare_block(bref.clone(), Some(r)), effect: Effect::Sub(Sub::Block(Some(a))) });
                }
            }
        }
        (WaitingFor::TargetSelection { player, pending_cast, target_slots, selection, .. }, Family::Targets) => {
            live.acting = *player;
            targets(live, *player, Some(pending_cast.object_id), target_slots, selection, &mut cands)?;
        }
        (WaitingFor::TriggerTargetSelection { player, source_id, target_slots, selection, .. }, Family::Targets) => {
            live.acting = *player;
            targets(live, *player, *source_id, target_slots, selection, &mut cands)?;
        }
        (
            WaitingFor::OptionalEffectChoice { player, source_id, .. } | WaitingFor::OpponentMayChoice { player, source_id, .. },
            Family::OptionalEffect,
        ) => {
            live.acting = *player;
            let src = viewer(live, *player, *source_id);
            for accept in [true, false] {
                cands.push(Cand {
                    semantic: sem::choose_boolean(src.clone(), "may_ability", accept),
                    effect: Effect::Act(GameAction::DecideOptionalEffect { accept }),
                });
            }
        }
        (WaitingFor::OptionalCostChoice { player, pending_cast, .. }, Family::OptionalCost) => {
            live.acting = *player;
            let Some(src) = viewer(live, *player, pending_cast.object_id) else {
                return Err(Stop::Halt("unresolvable_source", "optional cost source".into()));
            };
            for pay in [true, false] {
                cands.push(Cand {
                    semantic: sem::optional_cost(src.clone(), "other", pay),
                    effect: Effect::Act(GameAction::DecideOptionalCost { pay }),
                });
            }
        }
        (WaitingFor::ChooseXValue { player, min, max, pending_cast, .. }, Family::ChooseX) => {
            live.acting = *player;
            let src = viewer(live, *player, pending_cast.object_id);
            // At most 21 values are offered: min..=min+20.
            let top = (*max).min(min.saturating_add(20));
            for v in *min..=top {
                cands.push(Cand {
                    semantic: sem::choose_number(src.clone(), "x_value", i64::from(v), i64::from(*min), i64::from(top)),
                    effect: Effect::Act(GameAction::ChooseX { value: v }),
                });
            }
        }
        (WaitingFor::DiscardToHandSize { player, count, cards }, Family::DiscardToHand) => {
            live.acting = *player;
            let mut pool = cards.clone();
            pool.sort();
            return cards_group(live, *player, pool, *count, CardsFor::Discard);
        }
        _ => return Err(Stop::Halt("followup_mismatch", format!("{:?} for {family:?}", classify(&w).0))),
    }
    if cands.is_empty() {
        return Err(Stop::Halt("dead_end", format!("{} has no candidates", classify(&w).0)));
    }
    live.actor = actor.unwrap_or(live.acting);
    live.cands = cands;
    Ok(Built::Decision)
}

/// `count` picks over `pool`, one `select_object` (discard) or `order_pick` (mulligan bottom) per substep.
fn cards_group(live: &mut Live, who: PlayerId, pool: Vec<ObjectId>, count: usize, what: CardsFor) -> Result<Built, Stop> {
    if count == 0 || pool.is_empty() {
        let action = match what {
            CardsFor::Discard | CardsFor::MulliganBottom => GameAction::SelectCards { cards: vec![] },
        };
        apply_now(live, who, action)?;
        return Ok(Built::Applied);
    }
    let count = count.min(pool.len());
    if live.group.is_none() {
        live.group = Some(Group { total: count, index: 0, plan: Plan::Cards { pool, picks: vec![], count, what } });
    }
    let g = live.group.as_ref().expect("group");
    let Plan::Cards { pool, picks, count, what } = &g.plan else { return Err(Stop::Halt("followup_mismatch", "card group".into())) };
    let mut cands = Vec::new();
    let mut rest: Vec<ObjectId> = pool.iter().copied().filter(|c| !picks.contains(c)).collect();
    rest.sort();
    for c in rest {
        let Some(r) = object_ref(&live.state, &live.ids, who, c) else { continue };
        let semantic = match what {
            CardsFor::Discard => {
                sem::select_object(None, "discard", sem::target_object(r), picks.len() as u32, *count as u32, *count as u32)
            }
            CardsFor::MulliganBottom => sem::order_pick(None, "mulligan_bottom", json!({"object": r}), picks.len() as u32, *count as u32),
        };
        cands.push(Cand { semantic, effect: Effect::Sub(Sub::Card(c)) });
    }
    cands.sort_by_key(|c| canonical(&c.semantic));
    if cands.is_empty() {
        return Err(Stop::Halt("dead_end", "no cards left to pick".into()));
    }
    live.actor = who;
    live.cands = cands;
    Ok(Built::Decision)
}

fn targets(
    live: &Live,
    who: PlayerId,
    source: Option<ObjectId>,
    slots: &[engine::types::game_state::TargetSelectionSlot],
    selection: &engine::types::game_state::TargetSelectionProgress,
    cands: &mut Vec<Cand>,
) -> Result<(), Stop> {
    let slot = selection.current_slot;
    let Some(s) = slots.get(slot) else { return Err(Stop::Halt("dead_end", "target slot out of range".into())) };
    let Some(src) = source.and_then(|id| object_ref(&live.state, &live.ids, who, id)) else {
        return Err(Stop::Halt("unresolvable_source", "target source".into()));
    };
    let legal = if selection.current_legal_targets.is_empty() { &s.legal_targets } else { &selection.current_legal_targets };
    let chosen = selection.selected_slots.iter().filter(|t| t.is_some()).count() as u32;
    let min = u32::from(!s.optional);
    for t in legal {
        let target = match t {
            TargetRef::Player(p) => sem::target_player(seat(*p)),
            TargetRef::Object(o) => match object_ref(&live.state, &live.ids, who, *o) {
                Some(r) => sem::target_object(r),
                None => continue,
            },
        };
        cands.push(Cand {
            semantic: sem::choose_target(src.clone(), slot as u32, target, chosen, min, 1),
            effect: Effect::Act(GameAction::ChooseTarget { target: Some(t.clone()) }),
        });
    }
    if s.optional {
        cands.push(Cand {
            semantic: sem::finish_target_selection(src, slot as u32, chosen),
            effect: Effect::Act(GameAction::ChooseTarget { target: None }),
        });
    }
    cands.sort_by_key(|c| canonical(&c.semantic));
    Ok(())
}

impl Backend for PhaseBackend {
    fn identity(&self) -> Value {
        self.identity.clone()
    }

    fn catalog(&self) -> Vec<CatalogDeck> {
        self.decks.clone()
    }

    fn refuse_deck(&self, deck: &CatalogDeck) -> Option<String> {
        let mut bad: Vec<String> = Vec::new();
        for (name, _) in &deck.rows {
            if let Some(why) = crate::support::lacks(&self.db, name) {
                bad.push(format!("{name} ({why})"));
            }
        }
        bad.sort();
        bad.dedup();
        (!bad.is_empty()).then(|| format!("cards phase does not replay: {}", bad.join(", ")))
    }

    fn reset(&mut self, r: &Reset) -> Result<Outcome, String> {
        let list = |id: &str| -> Result<PlayerDeckList, String> {
            let d = self.decks.iter().find(|d| d.catalog_id == id).ok_or_else(|| format!("unknown deck {id}"))?;
            let main_deck = d.rows.iter().flat_map(|(n, c)| std::iter::repeat_n(n.clone(), *c as usize)).collect();
            Ok(PlayerDeckList { main_deck, ..Default::default() })
        };
        let payload =
            resolve_deck_list(&self.db, &DeckList { player: list(&r.decks[0])?, opponent: list(&r.decks[1])?, ..Default::default() });
        let seed = u64::from_be_bytes(r.game_secret[..8].try_into().expect("8 bytes"));
        let mut state = GameState::new_two_player(seed);
        load_and_hydrate_decks(&mut state, &payload, Some(&self.db));
        if r.mulligan == "none" {
            start_game_skip_mulligan(&mut state);
        } else {
            start_game_with_starting_player(&mut state, PlayerId(r.starting_seat));
        }
        self.census.games += 1;
        self.live = Some(Live {
            state,
            ids: Ids::new(&r.game_secret),
            cands: vec![],
            acting: PlayerId(0),
            actor: PlayerId(0),
            group: None,
            groups: [0; 2],
            seat_steps: [0; 2],
            decisions: 0,
            steps: 0,
            max_decisions: r.max_decisions,
            max_steps: r.max_steps,
        });
        Ok(self.pose())
    }

    fn choose(&mut self, candidate_id: u32) -> Outcome {
        let live = self.live.as_mut().expect("a live game");
        let cand = &live.cands[candidate_id as usize];
        let effect = cand.effect.clone();
        let who = live.acting;
        let actor = live.actor;
        live.seat_steps[who.0 as usize] += 1;
        live.steps += 1;
        let applied = match effect {
            Effect::Act(a) => {
                live.group = None;
                live.groups[who.0 as usize] += 1;
                live.decisions += 1;
                apply_now(live, actor, a)
            }
            Effect::Sub(sub) => step_group(live, who, actor, sub),
        };
        match applied {
            Ok(()) => self.pose(),
            Err(Stop::Halt(c, d)) => self.halt(c, &d),
        }
    }
}

/// Record one substep; when the group is complete, apply the whole declaration.
fn step_group(live: &mut Live, who: PlayerId, actor: PlayerId, sub: Sub) -> Result<(), Stop> {
    let g = live.group.as_mut().ok_or_else(|| Stop::Halt("followup_mismatch", "substep with no group".into()))?;
    match (&mut g.plan, sub) {
        (Plan::Attack { creatures, picks, .. }, Sub::Attack(t)) => {
            if let Some(t) = t {
                picks.push((creatures[g.index], t));
            }
        }
        (Plan::Block { blockers, picks, .. }, Sub::Block(a)) => {
            if let Some(a) = a {
                picks.push((blockers[g.index], a));
            }
        }
        (Plan::Cards { picks, .. }, Sub::Card(c)) => picks.push(c),
        _ => return Err(Stop::Halt("followup_mismatch", "substep does not fit its group".into())),
    }
    g.index += 1;
    if g.index < g.total {
        return Ok(());
    }
    let g = live.group.take().expect("group");
    let action = match g.plan {
        Plan::Attack { picks, .. } => GameAction::DeclareAttackers { attacks: picks, bands: vec![] },
        Plan::Block { picks, .. } => GameAction::DeclareBlockers { assignments: picks },
        Plan::Cards { picks, .. } => GameAction::SelectCards { cards: picks },
    };
    live.groups[who.0 as usize] += 1;
    live.decisions += 1;
    apply_now(live, actor, action)
}
