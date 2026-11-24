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
use crate::view::{looked_ref, mana_letter, object_ref, observation, seat, spell_entry, Ids};
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
    /// An item (a card, or a mode index) of the open selection.
    Pick(usize),
    /// Stop picking (a variable selection that has met its minimum).
    Finish,
    /// Scry/surveil: the next card goes to this destination.
    Dest(&'static str),
    /// Scry/surveil placement, or trigger order: this item (card index, trigger index) is next.
    Place(usize),
}

/// What a selection's picks mean to phase.
#[derive(Clone, Debug)]
enum SelKind {
    /// `select_object` of cards under this purpose; answered with `SelectCards`.
    Cards(&'static str),
    /// London bottom: `order_pick` `mulligan_bottom`; answered with `SelectCards`.
    MulliganBottom,
    /// `choose_spell_mode` over `count` modes; answered with `SelectModes`.
    Modes(u32),
    /// `select_object` `legend_rule`; answered with `ChooseLegend`.
    Legend,
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
    /// `min..=max` picks of `items`. A fixed selection (`min == max`) is one group of `max` substeps;
    /// a variable one is a group of one substep per pick (the spec's rule), the picks so far kept in
    /// `Live::carry` between groups.
    Select {
        kind: SelKind,
        source: Option<ObjectId>,
        items: Vec<usize>,
        picks: Vec<usize>,
        max: usize,
    },
    /// Scry or surveil: one `arrange_card` per card (top-down), then `n - 1` `order_pick`s.
    Arrange {
        purpose: &'static str,
        other: &'static str,
        cards: Vec<ObjectId>,
        chosen: Vec<&'static str>,
        placed: Vec<usize>,
    },
    /// Trigger order: one `order_pick` per position but the last, which is implied.
    Order {
        items: Vec<Value>,
        placed: Vec<usize>,
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
    /// Picks made so far in the open variable selection (one group per pick).
    carry: Vec<usize>,
    /// Library cards this decision lets the seat look at or search (the observation's `known`).
    looks: Vec<(ObjectId, &'static str)>,
    /// The context purpose of the substep being asked, when it has one.
    purpose: Option<&'static str>,
    /// Diagnostics only: per seat, the ids last shown and the ids that have left (V7).
    shown: [(std::collections::BTreeMap<String, String>, std::collections::BTreeSet<String>); 2],
    /// Per seat: the id an object that left and came back is shown under now (V7: an id never returns).
    alias: [std::collections::BTreeMap<String, String>; 2],
    /// Reissued ids so far; each new id is derived from the old one and this count.
    reissued: u64,
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
                    let detail = format!("{:?} pending_cast_cost={:?} pool={:?}", live.state.waiting_for, live.state.pending_cast.as_ref().map(|c| &c.cost), live.state.players.iter().map(|p| p.mana_pool.units().count()).collect::<Vec<_>>());
                    eprintln!("phase-seat: detail {}", detail.chars().take(700).collect::<String>());
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
            | WaitingFor::ChooseXValue { pending_cast, .. } => shown_ref(&live.state, &live.ids, who, pending_cast.object_id),
            _ => None,
        };
        let observation = observation(&live.state, who, &live.ids, &live.looks);
        diagnose(live, who, &observation, &cands);
        let mut decision = json!({
            "acting_seat": seat(who), "seat_step": live.seat_steps[s],
            "group": {"group_id": live.groups[s], "substep_index": index, "substep_count": total},
            "context": {"kind": if family == Family::Priority { "priority" } else { "choice" },
                "source": source, "purpose": live.purpose, "text": null, "rewind": false},
            "observation": observation,
            "candidates": cands, "extensions": {},
        });
        reissue(live, who, &mut decision);
        Outcome::Decision(decision)
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

/// The ids an observation shows, each with a short description (`zone` is the key the object sits under).
fn shown_ids(v: &Value, out: &mut std::collections::BTreeMap<String, String>, zone: &str) {
    match v {
        Value::Object(m) => {
            if let Some(id) = m.get("object_id").and_then(Value::as_str) {
                out.insert(id.to_string(), format!("{} {zone}", m.get("card_name").map_or("?".to_string(), |n| n.to_string())));
                return;
            }
            for (k, x) in m {
                shown_ids(x, out, k);
            }
        }
        Value::Array(a) => a.iter().for_each(|x| shown_ids(x, out, zone)),
        _ => {}
    }
}

/// The ids the seat's observation lists: the zones a viewer sees, the stack and the looked-at cards.
fn observed_ids(obs: &Value) -> std::collections::BTreeMap<String, String> {
    let mut now = std::collections::BTreeMap::new();
    for p in obs["players"].as_array().into_iter().flatten() {
        for z in ["hand", "battlefield", "graveyard", "exile", "command"] {
            shown_ids(&p[z], &mut now, z);
        }
    }
    shown_ids(&obs["stack"], &mut now, "stack");
    shown_ids(&obs["known"], &mut now, "known");
    now
}

/// Diagnostics on stderr, never a filter: a candidate that names an object the observation does not list (V4).
fn diagnose(live: &mut Live, _who: PlayerId, obs: &Value, cands: &[Value]) {
    let now = observed_ids(obs);
    let waiting = classify(&live.state.waiting_for).0;
    for c in cands {
        let mut refs = std::collections::BTreeMap::new();
        shown_ids(&c["semantic"], &mut refs, "candidate");
        for (id, what) in refs {
            if !now.contains_key(&id) {
                eprintln!("phase-seat: diagnose V4 {waiting}: candidate {} names {id} ({what}), not in the observation", c["semantic"]["kind"]);
            }
        }
    }
}

/// V7: an object id that left the seat's view never comes back. A cancelled cast puts the card back in
/// the hand it left (phase rolls the cast back), and any other rollback or return does the same; the
/// object is then a new one to the seat, so it is shown under a fresh id from here on. `decision` is the
/// whole decision (observation, candidates and context source), rewritten in place; the seat's ids are
/// tracked by their first-minted form, so an alias is stable for as long as the object stays in view.
fn reissue(live: &mut Live, who: PlayerId, decision: &mut Value) {
    let s = who.0 as usize;
    let now = observed_ids(&decision["observation"]);
    let (prev, departed) = &mut live.shown[s];
    for id in now.keys() {
        if departed.remove(id) {
            live.reissued += 1;
            let fresh = live.ids.reissue(who, id, live.reissued);
            live.alias[s].insert(id.clone(), fresh);
        }
    }
    for id in prev.keys() {
        if !now.contains_key(id) {
            departed.insert(id.clone());
        }
    }
    *prev = now;
    let alias = &live.alias[s];
    if !alias.is_empty() {
        rewrite_ids(decision, alias);
    }
}

/// Replaces every string equal to an aliased id (ids sit under `object_id`).
fn rewrite_ids(v: &mut Value, alias: &std::collections::BTreeMap<String, String>) {
    match v {
        Value::String(x) => {
            if let Some(a) = alias.get(x.as_str()) {
                *x = a.clone();
            }
        }
        Value::Array(a) => a.iter_mut().for_each(|x| rewrite_ids(x, alias)),
        Value::Object(m) => m.values_mut().for_each(|x| rewrite_ids(x, alias)),
        _ => {}
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
    let viewer = |live: &Live, p: PlayerId, id: ObjectId| shown_ref(&live.state, &live.ids, p, id);
    let mut cands: Vec<Cand> = Vec::new();
    let mut actor = None;
    let w = live.state.waiting_for.clone();
    live.looks.clear();
    live.purpose = None;
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
                let n = usize::from(count);
                let spec = PoolSpec::cards(e.player, hand, n, n, SelKind::MulliganBottom, None);
                return select_group(live, spec);
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
        (WaitingFor::AssignCombatDamage { player, total_damage, blockers, assignment_modes, trample, pw_loyalty, pw_controller, .. }, Family::CombatDamage) => {
            // The host divides combat damage (`combat_damage_assignment: "engine_order"`): lethal to each
            // blocker in phase's order and the rest to the last (to the defender with trample).
            use engine::types::game_state::CombatDamageAssignmentMode as Mode;
            if !assignment_modes.is_empty() && !assignment_modes.contains(&Mode::Normal) {
                return Err(Stop::Halt("unsupported_damage_mode", format!("{assignment_modes:?}")));
            }
            // Spillover past a planeswalker (CR 702.19c) is only asked of a trampler.
            if trample.is_some() && (pw_loyalty.is_some() || pw_controller.is_some()) {
                return Err(Stop::Halt("unsupported_damage_mode", "trample over a planeswalker".into()));
            }
            let mut left = *total_damage;
            let mut assignments = Vec::new();
            for (i, b) in blockers.iter().enumerate() {
                let give = if i + 1 == blockers.len() && trample.is_none() { left } else { b.lethal_minimum.min(left) };
                assignments.push((b.blocker_id, give));
                left -= give;
            }
            let trample_damage = if trample.is_some() { left } else { 0 };
            let a = GameAction::AssignCombatDamage { mode: Mode::Normal, assignments, trample_damage, controller_damage: 0 };
            apply_now(live, *player, a)?;
            return Ok(Built::Applied);
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
        (
            WaitingFor::DiscardToHandSize { .. }
            | WaitingFor::DiscardChoice { .. }
            | WaitingFor::RevealChoice { .. }
            | WaitingFor::SearchChoice { .. }
            | WaitingFor::EffectZoneChoice { .. }
            | WaitingFor::DigChoice { .. }
            | WaitingFor::PayCost { .. }
            | WaitingFor::ChooseLegend { .. },
            Family::Pool,
        ) => {
            let spec = pool_spec(&w)?;
            live.acting = spec.who;
            return select_group(live, spec);
        }
        (
            WaitingFor::ModeChoice { player, modal, unavailable_modes, .. }
            | WaitingFor::AbilityModeChoice { player, modal, unavailable_modes, .. },
            Family::Modes,
        ) => {
            live.acting = *player;
            let source = match &w {
                WaitingFor::ModeChoice { pending_cast, .. } => pending_cast.object_id,
                WaitingFor::AbilityModeChoice { source_id, .. } => *source_id,
                _ => unreachable!("matched above"),
            };
            if modal.allow_repeat_modes {
                return Err(Stop::Halt("unsupported_modal", "a mode that may be chosen more than once".into()));
            }
            let items: Vec<usize> = (0..modal.mode_count).filter(|i| !unavailable_modes.contains(i)).collect();
            let spec = PoolSpec {
                who: *player,
                items,
                min: modal.min_choices,
                max: modal.max_choices,
                kind: SelKind::Modes(modal.mode_count as u32),
                source: Some(source),
            };
            return select_group(live, spec);
        }
        (WaitingFor::OrderTriggers { player, triggers }, Family::OrderTriggers) => {
            live.acting = *player;
            let n = triggers.len();
            if n <= 1 {
                apply_now(live, *player, GameAction::OrderTriggers { order: (0..n).collect() })?;
                return Ok(Built::Applied);
            }
            if live.group.is_none() {
                let mut seen: BTreeMap<String, u32> = BTreeMap::new();
                let items = triggers
                    .iter()
                    .map(|t| {
                        let source = shown_ref(&live.state, &live.ids, *player, t.source_id);
                        let mut item = json!({"source": source, "source_name": t.source_name, "ability_index": null,
                            "event_objects": [], "instance": 0, "label": null});
                        let n = seen.entry(canonical(&item)).or_insert(0);
                        item["instance"] = json!(*n);
                        *n += 1;
                        json!({"trigger": item})
                    })
                    .collect();
                live.group = Some(Group { total: n - 1, index: 0, plan: Plan::Order { items, placed: vec![] } });
            }
            let g = live.group.as_ref().expect("group");
            let Plan::Order { items, placed } = &g.plan else { return Err(Stop::Halt("followup_mismatch", "order group".into())) };
            for (i, item) in items.iter().enumerate().filter(|(i, _)| !placed.contains(i)) {
                cands.push(Cand {
                    semantic: sem::order_pick(None, "triggers", item.clone(), placed.len() as u32, n as u32),
                    effect: Effect::Sub(Sub::Place(i)),
                });
            }
            cands.sort_by_key(|c| canonical(&c.semantic["item"]));
        }
        (WaitingFor::ScryChoice { player, cards } | WaitingFor::SurveilChoice { player, cards }, Family::Arrange) => {
            live.acting = *player;
            let (purpose, other) = if matches!(w, WaitingFor::ScryChoice { .. }) { ("scry", "bottom") } else { ("surveil", "graveyard") };
            let n = cards.len();
            if n == 0 {
                apply_now(live, *player, GameAction::SelectCards { cards: vec![] })?;
                return Ok(Built::Applied);
            }
            if live.group.is_none() {
                live.ids.open_look();
                live.group = Some(Group {
                    total: 2 * n - 1,
                    index: 0,
                    plan: Plan::Arrange { purpose, other, cards: cards.clone(), chosen: vec![], placed: vec![] },
                });
            }
            live.looks = cards.iter().map(|c| (*c, "looked_at")).collect();
            let g = live.group.as_ref().expect("group");
            let Plan::Arrange { purpose, other, cards, chosen, placed } = &g.plan else {
                return Err(Stop::Halt("followup_mismatch", "arrange group".into()));
            };
            let refs: Vec<Option<Value>> = cards.iter().map(|c| looked_ref(&live.state, &live.ids, *c, *player)).collect();
            if refs.iter().any(Option::is_none) {
                return Err(Stop::Halt("unresolvable_source", "a looked-at card".into()));
            }
            let refs: Vec<Value> = refs.into_iter().flatten().collect();
            if chosen.len() < n {
                let i = chosen.len();
                for dst in ["top", *other] {
                    cands.push(Cand {
                        semantic: sem::arrange_card(None, purpose, refs[i].clone(), i as u32, n as u32, dst),
                        effect: Effect::Sub(Sub::Dest(dst)),
                    });
                }
            } else {
                live.purpose = Some("arrangement");
                // The cards sent to `top` are placed first, then the rest; the rest's order is phase's own
                // (the offered order), so one candidate is posed for each of its picks.
                let (dst, rest_only) = if (0..n).any(|i| chosen[i] == "top" && !placed.contains(&i)) { ("top", false) } else { (*other, true) };
                let mut unplaced: Vec<usize> = (0..n).filter(|i| chosen[*i] == dst && !placed.contains(i)).collect();
                if rest_only {
                    unplaced.truncate(1);
                }
                for i in unplaced {
                    cands.push(Cand {
                        semantic: sem::order_pick(None, "arrangement", json!({"object": refs[i]}), placed.len() as u32, n as u32),
                        effect: Effect::Sub(Sub::Place(i)),
                    });
                }
                cands.sort_by_key(|c| hidden_key(&c.semantic["item"]["object"]));
            }
        }
        (WaitingFor::ManaPayment { player, .. }, Family::ManaPayment) => {
            let who = *player;
            live.acting = who;
            // The profile declares `engine_autopay`: a payment the pool already covers is finished by the
            // engine (phase's own confirm), not asked. Probed on a copy, since a short pool is refused.
            let mut probe = live.state.clone();
            if apply(&mut probe, who, GameAction::PassPriority).is_ok() {
                live.state = probe;
                return Ok(Built::Applied);
            }
            live.purpose = Some("mana_payment");
            let spell = live.state.pending_cast.as_ref().and_then(|c| shown_ref(&live.state, &live.ids, who, c.object_id));
            let Some(spell) = spell else { return Err(Stop::Halt("unresolvable_source", "the spell being paid for".into())) };
            let contract = AiDecisionContract::issue(&live.state, who);
            let mut seen = std::collections::BTreeSet::new();
            for c in &contract.candidates {
                // A mana ability that is not a land tap (a Treasure, a creature that taps for mana): the same
                // `activate_mana_ability`, with no colour to name (the source's own ability makes it).
                if let GameAction::ActivateAbility { source_id, ability_index, .. } = &c.action {
                    if let Some(src) = shown_ref(&live.state, &live.ids, who, *source_id) {
                        let semantic = sem::activate_mana_ability(src, *ability_index as u32, None, None);
                        if seen.insert(canonical(&semantic)) {
                            cands.push(Cand { semantic, effect: Effect::Act(c.action.clone()) });
                        }
                    }
                    continue;
                }
                let GameAction::TapLandForMana { selection } = &c.action else {
                    let d = format!("{:?}", c.action);
                    let name = d.split(|ch: char| !ch.is_alphanumeric()).next().unwrap_or("?").to_string();
                    if name != "PassPriority" && name != "CancelCast" {
                        *census.unmapped_actions.entry(name).or_insert(0) += 1;
                    }
                    continue;
                };
                let Some(src) = shown_ref(&live.state, &live.ids, who, selection.source.object_id) else { continue };
                let semantic = sem::activate_mana_ability(src, selection.ability_index.unwrap_or(0) as u32, Some(mana_letter(selection.mana_type)), None);
                if seen.insert(canonical(&semantic)) {
                    cands.push(Cand { semantic, effect: Effect::Act(c.action.clone()) });
                }
            }
            cands.sort_by_key(|c| canonical(&c.semantic));
            actor = Some(contract.authorized_actor);
            // Declining to pay is always offered (V9): it withdraws the cast.
            cands.push(Cand { semantic: sem::optional_cost(spell, "other", false), effect: Effect::Act(GameAction::CancelCast) });
        }
        (WaitingFor::ManaSourceSelection { player, options, .. }, Family::ManaSource) => {
            live.acting = *player;
            live.purpose = Some("mana_payment");
            let spell = live.state.pending_cast.as_ref().and_then(|c| shown_ref(&live.state, &live.ids, *player, c.object_id));
            let Some(spell) = spell else { return Err(Stop::Halt("unresolvable_source", "the spell being paid for".into())) };
            let mut seen = std::collections::BTreeSet::new();
            for o in options {
                let Some(src) = shown_ref(&live.state, &live.ids, *player, o.source.object_id) else { continue };
                let semantic = sem::activate_mana_ability(src, o.ability_index.unwrap_or(0) as u32, Some(mana_letter(o.mana_type)), None);
                if seen.insert(canonical(&semantic)) {
                    cands.push(Cand { semantic, effect: Effect::Act(GameAction::ActivateManaSource { selection: o.clone() }) });
                }
            }
            cands.sort_by_key(|c| canonical(&c.semantic));
            // Declining to choose a source is always offered (V9).
            cands.push(Cand { semantic: sem::optional_cost(spell, "other", false), effect: Effect::Act(GameAction::BackToManaPayment) });
        }
        (WaitingFor::TopOrBottomChoice { player, .. }, Family::TopOrBottom) => {
            live.acting = *player;
            for (i, (label, top)) in [("top", true), ("bottom", false)].into_iter().enumerate() {
                cands.push(Cand {
                    semantic: sem::choose_option(None, "top_or_bottom", i as u32, 2, Some(label)),
                    effect: Effect::Act(GameAction::ChooseTopOrBottom { top }),
                });
            }
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

/// What a pool selection asks: who picks, from which items, how many, and what the picks mean.
struct PoolSpec {
    who: PlayerId,
    /// Object ids (as numbers), or mode indices for [`SelKind::Modes`].
    items: Vec<usize>,
    min: usize,
    max: usize,
    kind: SelKind,
    source: Option<ObjectId>,
}

impl PoolSpec {
    fn cards(who: PlayerId, pool: Vec<ObjectId>, min: usize, max: usize, kind: SelKind, source: Option<ObjectId>) -> Self {
        PoolSpec { who, items: pool.into_iter().map(|c| c.0 as usize).collect(), min, max, kind, source }
    }
}

/// The selection a `WaitingFor` of the pool family asks.
fn pool_spec(w: &WaitingFor) -> Result<PoolSpec, Stop> {
    use engine::types::game_state::PayCostKind;
    let cards = |who: &PlayerId, pool: &[ObjectId], min: usize, max: usize, purpose: &'static str, src: Option<ObjectId>| {
        Ok(PoolSpec::cards(*who, pool.to_vec(), min, max, SelKind::Cards(purpose), src))
    };
    match w {
        WaitingFor::DiscardToHandSize { player, count, cards: pool } => cards(player, pool, *count, *count, "discard", None),
        WaitingFor::DiscardChoice { player, count, cards: pool, source_id, up_to, .. } => {
            cards(player, pool, if *up_to { 0 } else { *count }, *count, "discard", Some(*source_id))
        }
        WaitingFor::RevealChoice { player, cards: pool, optional, .. } => cards(player, pool, usize::from(!*optional), 1, "reveal", None),
        WaitingFor::SearchChoice { player, cards: pool, count, up_to, allows_partial_find, constraint, .. } => {
            let partial = *up_to || *allows_partial_find || constraint.permits_partial_find();
            cards(player, pool, if partial { 0 } else { *count }, *count, "search", None)
        }
        WaitingFor::EffectZoneChoice { player, cards: pool, count, min_count, up_to, source_id, effect_kind, .. } => {
            let purpose = match format!("{effect_kind:?}").as_str() {
                "Sacrifice" => "sacrifice",
                "Discard" => "discard",
                "Destroy" => "destroy",
                _ => "other",
            };
            cards(player, pool, if *up_to { *min_count } else { *count }, *count, purpose, Some(*source_id))
        }
        WaitingFor::PayCost { player, kind, choices, count, min_count, resume } => {
            let manaability = matches!(resume, engine::types::game_state::CostResume::ManaAbility { .. });
            match kind {
                PayCostKind::Discard => cards(player, choices, *count, *count, "discard", None),
                PayCostKind::ReturnToHand => cards(player, choices, *count, *count, "return_to_hand", None),
                PayCostKind::Reveal { shared_quality: None } => cards(player, choices, *count, *count, "reveal", None),
                PayCostKind::Sacrifice => cards(player, choices, if manaability { *count } else { *min_count }, *count, "sacrifice", None),
                PayCostKind::ExileFromZone { .. } => cards(player, choices, *min_count, *count, "exile", None),
                other => Err(Stop::Halt("unsupported_pay_cost", format!("{other:?}").split([' ', '{', '(']).next().unwrap_or("?").to_string())),
            }
        }
        WaitingFor::DigChoice { player, cards: looked, keep_count, up_to, selectable_cards, source_id, .. } => {
            // The dig keeps as many as it can when fewer cards pass the filter than `keep_count`.
            let max = (*keep_count).min(selectable_cards.len());
            let _ = looked;
            cards(player, selectable_cards, if *up_to { 0 } else { max }, max, "other", *source_id)
        }
        WaitingFor::ChooseLegend { player, candidates, .. } => Ok(PoolSpec::cards(*player, candidates.clone(), 1, 1, SelKind::Legend, None)),
        other => Err(Stop::Halt("followup_mismatch", format!("{} is not a pool", classify(other).0))),
    }
}

/// A ref the observation lists as a record, or `None` (V4: every referenced id is a record).
fn shown_ref(state: &GameState, ids: &Ids, viewer: PlayerId, id: ObjectId) -> Option<Value> {
    let r = object_ref(state, ids, viewer, id)?;
    let listed = match r["zone"].as_str()? {
        "battlefield" | "graveyard" | "exile" | "command" => true,
        // The stack lists the spells on it, and the spell phase is resolving (see `view::observation`).
        "stack" => true,
        "hand" => r["owner_seat"] == seat(viewer),
        _ => false,
    };
    listed.then_some(r)
}

/// V5: candidates that name a hidden-zone card come in (card_name, object_id) order.
fn hidden_key(r: &Value) -> (String, String) {
    (r["card_name"].as_str().unwrap_or("").to_string(), r["object_id"].as_str().unwrap_or("").to_string())
}

fn selection_action(kind: &SelKind, picks: &[usize]) -> GameAction {
    match kind {
        SelKind::Cards(_) | SelKind::MulliganBottom => GameAction::SelectCards { cards: picks.iter().map(|i| ObjectId(*i as u64)).collect() },
        SelKind::Modes(_) => GameAction::SelectModes { indices: picks.to_vec() },
        SelKind::Legend => GameAction::ChooseLegend { keep: ObjectId(picks[0] as u64) },
    }
}

/// Open (or continue) a `min..=max` selection over `spec.items`.
fn select_group(live: &mut Live, spec: PoolSpec) -> Result<Built, Stop> {
    let max = spec.max.min(spec.items.len());
    let min = spec.min.min(max);
    if max == 0 || spec.items.len() <= min {
        // Nothing to choose: no pick can be made, or every item must be picked.
        let all = if max == 0 { vec![] } else { spec.items.clone() };
        if matches!(spec.kind, SelKind::Legend) && all.is_empty() {
            return Err(Stop::Halt("dead_end", "legend rule with no candidate".into()));
        }
        live.carry.clear();
        apply_now(live, spec.who, selection_action(&spec.kind, &all))?;
        return Ok(Built::Applied);
    }
    let cards = !matches!(spec.kind, SelKind::Modes(_));
    if cards {
        live.looks = spec
            .items
            .iter()
            .map(|i| ObjectId(*i as u64))
            .filter_map(|id| {
                let o = live.state.objects.get(&id)?;
                match o.zone {
                    engine::types::zones::Zone::Library => Some((id, "searching")),
                    engine::types::zones::Zone::Hand if o.owner != spec.who => Some((id, "revealed")),
                    _ => None,
                }
            })
            .collect();
    }
    let fixed = min == max;
    if live.group.is_none() {
        if !live.looks.is_empty() {
            live.ids.open_look();
            // The refs below are minted after the new look opens; `looks` stays the same list.
        }
        let picks = if fixed { vec![] } else { live.carry.clone() };
        live.group = Some(Group {
            total: if fixed { max } else { 1 },
            index: 0,
            plan: Plan::Select { kind: spec.kind.clone(), source: spec.source, items: spec.items.clone(), picks, max },
        });
    }
    let g = live.group.as_ref().expect("group");
    let Plan::Select { kind, source, items, picks, .. } = &g.plan else {
        return Err(Stop::Halt("followup_mismatch", "selection group".into()));
    };
    let who = spec.who;
    let src = source.and_then(|id| shown_ref(&live.state, &live.ids, who, id));
    if matches!(kind, SelKind::Modes(_)) && src.is_none() {
        return Err(Stop::Halt("unresolvable_source", "mode source".into()));
    }
    let mut rows: Vec<((String, String), Cand)> = Vec::new();
    for &it in items.iter().filter(|i| !picks.contains(i)) {
        let (key, semantic) = if let SelKind::Modes(count) = kind {
            let s = sem::choose_spell_mode(src.clone().expect("mode source"), it as u32, *count, picks.len() as u32, min as u32, max as u32);
            ((String::new(), format!("{it:08}")), s)
        } else {
            // A card in a library is only in view while it is looked at (a `known` entry); anything else
            // must be one of the observation's records.
            let id = ObjectId(it as u64);
            let looked = live.looks.iter().any(|(l, _)| *l == id);
            let r = if looked { looked_ref(&live.state, &live.ids, id, who) } else { shown_ref(&live.state, &live.ids, who, id) };
            let Some(r) = r else { continue };
            let key = hidden_key(&r);
            let s = match kind {
                SelKind::Cards(p) => sem::select_object(src.clone(), p, sem::target_object(r), picks.len() as u32, min as u32, max as u32),
                SelKind::Legend => sem::select_object(None, "legend_rule", sem::target_object(r), picks.len() as u32, 1, 1),
                SelKind::MulliganBottom => sem::order_pick(None, "mulligan_bottom", json!({"object": r}), picks.len() as u32, max as u32),
                SelKind::Modes(_) => unreachable!("handled above"),
            };
            (key, s)
        };
        rows.push((key, Cand { semantic, effect: Effect::Sub(Sub::Pick(it)) }));
    }
    rows.sort_by(|a, b| a.0.cmp(&b.0));
    let mut cands: Vec<Cand> = rows.into_iter().map(|(_, c)| c).collect();
    if !fixed && picks.len() >= min {
        let purpose = match kind {
            SelKind::Cards(p) => p,
            SelKind::Modes(_) => "modes",
            SelKind::Legend | SelKind::MulliganBottom => "other",
        };
        cands.push(Cand { semantic: sem::finish_selection(src.clone(), purpose, picks.len() as u32), effect: Effect::Sub(Sub::Finish) });
    }
    if cands.is_empty() {
        let zones: Vec<String> = items
            .iter()
            .take(6)
            .map(|i| live.state.objects.get(&ObjectId(*i as u64)).map_or("gone".to_string(), |o| format!("{} {:?} owner {}", o.name, o.zone, o.owner.0)))
            .collect();
        return Err(Stop::Halt("dead_end", format!("no item left to pick in {}: {} items, picks {:?}, who {}: {zones:?}", classify(&live.state.waiting_for).0, items.len(), picks, who.0)));
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
    let Some(src) = source.and_then(|id| shown_ref(&live.state, &live.ids, who, id)) else {
        let what = source.map_or("no source".to_string(), |id| {
            live.state.objects.get(&id).map_or(format!("{} gone", id.0), |o| format!("{} {} {:?} owner {} controller {} on stack {}", id.0, o.name, o.zone, o.owner.0, o.controller.0, spell_entry(&live.state, id).is_some()))
        });
        return Err(Stop::Halt("unresolvable_source", format!("target source: {what} in {}", classify(&live.state.waiting_for).0)));
    };
    let legal = if selection.current_legal_targets.is_empty() { &s.legal_targets } else { &selection.current_legal_targets };
    // Every slot takes one target: the count within the slot (V1: below `maximum`) is 0.
    let chosen = 0u32;
    let min = u32::from(!s.optional);
    for t in legal {
        let target = match t {
            TargetRef::Player(p) => sem::target_player(seat(*p)),
            TargetRef::Object(o) => match shown_ref(&live.state, &live.ids, who, *o) {
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
            carry: vec![],
            looks: vec![],
            purpose: None,
            shown: Default::default(),
            alias: Default::default(),
            reissued: 0,
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
                live.carry.clear();
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

/// Record one substep; when the group is complete, apply the whole declaration (a variable selection
/// that is not full yet is kept in `carry` instead, and its next pick is a new group).
fn step_group(live: &mut Live, who: PlayerId, actor: PlayerId, sub: Sub) -> Result<(), Stop> {
    let g = live.group.as_mut().ok_or_else(|| Stop::Halt("followup_mismatch", "substep with no group".into()))?;
    let mut finished = false;
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
        (Plan::Select { picks, .. }, Sub::Pick(it)) => picks.push(it),
        (Plan::Select { .. }, Sub::Finish) => {
            finished = true;
            g.index = g.total.saturating_sub(1);
        }
        (Plan::Arrange { chosen, .. }, Sub::Dest(d)) => chosen.push(d),
        (Plan::Arrange { cards, placed, .. }, Sub::Place(i)) => {
            placed.push(i);
            if placed.len() + 1 == cards.len() {
                // The last card of the arrangement is implied: top's, then the rest's.
                let rest: Vec<usize> = (0..cards.len()).filter(|c| !placed.contains(c)).collect();
                debug_assert_eq!(rest.len(), 1);
                placed.extend(rest);
            }
        }
        (Plan::Order { items, placed }, Sub::Place(i)) => {
            placed.push(i);
            if placed.len() + 1 == items.len() {
                let rest: Vec<usize> = (0..items.len()).filter(|c| !placed.contains(c)).collect();
                placed.extend(rest);
            }
        }
        _ => return Err(Stop::Halt("followup_mismatch", "substep does not fit its group".into())),
    }
    // A one-card arrangement has no ordering substep: the single card is placed by its destination.
    if let Plan::Arrange { cards, chosen, placed, .. } = &mut g.plan {
        if cards.len() == 1 && chosen.len() == 1 && placed.is_empty() {
            placed.push(0);
        }
    }
    g.index += 1;
    if g.index < g.total {
        return Ok(());
    }
    let g = live.group.take().expect("group");
    live.groups[who.0 as usize] += 1;
    live.decisions += 1;
    let action = match g.plan {
        Plan::Attack { picks, .. } => GameAction::DeclareAttackers { attacks: picks, bands: vec![] },
        Plan::Block { picks, .. } => GameAction::DeclareBlockers { assignments: picks },
        Plan::Select { kind, picks, max, .. } => {
            if !finished && picks.len() < max {
                live.carry = picks;
                return Ok(());
            }
            live.carry.clear();
            selection_action(&kind, &picks)
        }
        Plan::Arrange { cards, chosen, placed, .. } => {
            let top: Vec<ObjectId> = placed.iter().filter(|i| chosen[**i] == "top").map(|i| cards[*i]).collect();
            GameAction::SelectCards { cards: top }
        }
        Plan::Order { placed, .. } => GameAction::OrderTriggers { order: placed },
    };
    apply_now(live, actor, action)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// V7: an aliased id is replaced wherever it sits (observation, candidates, context source), and an id
    /// that has no alias is left alone.
    #[test]
    fn rewrite_ids_replaces_every_occurrence_of_an_aliased_id() {
        let mut alias = std::collections::BTreeMap::new();
        alias.insert("o-old".to_string(), "o-new".to_string());
        let mut v = json!({
            "context": {"source": {"object_id": "o-old"}},
            "observation": {"players": [{"hand": [{"object_id": "o-old"}, {"object_id": "o-keep"}]}]},
            "candidates": [{"semantic": {"source": {"object_id": "o-old"}}}],
        });
        rewrite_ids(&mut v, &alias);
        let s = canonical(&v);
        assert!(!s.contains("o-old"), "{s}");
        assert_eq!(s.matches("o-new").count(), 3);
        assert_eq!(s.matches("o-keep").count(), 1);
    }

    #[test]
    fn observed_ids_lists_the_viewers_zones_and_the_stack() {
        let obs = json!({"players": [{"hand": [{"object_id": "a", "card_name": "X"}], "library": [{"object_id": "hidden"}]}],
            "stack": [{"object_id": "s"}], "known": []});
        let ids: Vec<String> = observed_ids(&obs).into_keys().collect();
        assert_eq!(ids, vec!["a".to_string(), "s".to_string()]);
    }
}
