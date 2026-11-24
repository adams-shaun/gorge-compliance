//! phase `GameState` -> the SpellBench v2 observation of one seat.
//!
//! Information rule: the state is first filtered by phase's own `filter_state_for_viewer`, then
//! projected. The other seat's hand and every library are never listed (the other hand is `null`,
//! libraries are counts), a face-down object off the battlefield has no name or characteristics,
//! and an object id is `"o-" + hex(HMAC(id_key, viewer:object:incarnation)[:8])`, new for each
//! viewer and for each incarnation phase gives an object (CR 400.7), so an id never links a card
//! across a zone change or a shuffle.
//!
//! Gaps (declared false in `observe::FLAGS` or noted here): no day/night, designations, progress,
//! `exiled_by`, `full_name`, `known` (always `[]`), pending triggers or stack text; stack
//! `targets`/`modes` are empty and `x_value` is null.

use std::collections::BTreeSet;

use engine::game::filter_state_for_viewer;
use engine::types::game_state::{GameState, StackEntryKind, WaitingFor};
use engine::types::identifiers::ObjectId;
use engine::types::mana::{ManaColor, ManaType};
use engine::types::object_data::AttachTarget;
use engine::types::phase::Phase;
use engine::types::player::{Player, PlayerId};
use engine::types::zones::Zone;
use engine::types::CoreType;
use serde_json::{json, Map, Value};

use crate::json::{hex, hmac_sha256};

/// Derives the per-viewer object ids of one game from its `game_secret`.
pub struct Ids {
    key: [u8; 32],
}

impl Ids {
    pub fn new(game_secret: &[u8; 32]) -> Self {
        Ids { key: hmac_sha256(game_secret, b"spellbench/v2/object-id") }
    }

    pub fn object(&self, viewer: PlayerId, id: ObjectId, incarnation: u64) -> String {
        let m = format!("{}:{}:{}", viewer.0, id.0, incarnation);
        format!("o-{}", hex(&hmac_sha256(&self.key, m.as_bytes())[..8]))
    }
}

pub fn seat(p: PlayerId) -> &'static str {
    if p.0 == 0 {
        "p0"
    } else {
        "p1"
    }
}

/// The v2 `phase_step` word. Exhaustive on purpose: a step phase adds later must be classified.
pub fn phase_step(p: Phase) -> &'static str {
    match p {
        Phase::Untap => "untap",
        Phase::Upkeep => "upkeep",
        Phase::Draw => "draw",
        Phase::PreCombatMain => "precombat_main",
        Phase::BeginCombat => "beginning_of_combat",
        Phase::DeclareAttackers => "declare_attackers",
        Phase::DeclareBlockers => "declare_blockers",
        Phase::CombatDamage => "combat_damage",
        Phase::EndCombat => "end_of_combat",
        Phase::PostCombatMain => "postcombat_main",
        Phase::End => "end_step",
        Phase::Cleanup => "cleanup",
    }
}

fn zone_word(z: Zone) -> &'static str {
    match z {
        Zone::Library => "library",
        Zone::Hand => "hand",
        Zone::Battlefield => "battlefield",
        Zone::Graveyard => "graveyard",
        Zone::Stack => "stack",
        Zone::Exile => "exile",
        Zone::Command => "command",
    }
}

/// Lowercase, apostrophes removed, spaces and hyphens as `_` (the v2 open-word rule).
fn word(s: &str) -> String {
    s.chars().filter(|c| !matches!(c, '\'' | '\u{2019}')).map(|c| if c == ' ' || c == '-' { '_' } else { c.to_ascii_lowercase() }).collect()
}

fn mana_letter(t: ManaType) -> &'static str {
    match t {
        ManaType::White => "W",
        ManaType::Blue => "U",
        ManaType::Black => "B",
        ManaType::Red => "R",
        ManaType::Green => "G",
        ManaType::Colorless => "C",
    }
}

fn color_word(c: ManaColor) -> &'static str {
    match c {
        ManaColor::White => "white",
        ManaColor::Blue => "blue",
        ManaColor::Black => "black",
        ManaColor::Red => "red",
        ManaColor::Green => "green",
    }
}

/// Whether `viewer` may read this object's face: not face-down unless on the battlefield (where the
/// 2/2 shows) and not in a hidden zone of the other seat.
fn readable(state: &GameState, viewer: PlayerId, id: ObjectId) -> bool {
    let Some(o) = state.objects.get(&id) else { return false };
    match o.zone {
        Zone::Hand | Zone::Library => o.owner == viewer,
        _ => !o.face_down || o.controller == viewer,
    }
}

/// An ObjectRef (`{object_id, card_name, owner_seat, controller_seat, zone}`), or `None` for an id the
/// state no longer holds.
pub fn object_ref(state: &GameState, ids: &Ids, viewer: PlayerId, id: ObjectId) -> Option<Value> {
    let o = state.objects.get(&id)?;
    let name = readable(state, viewer, id).then_some(o.name.as_str());
    Some(crate::semantic::object_ref(&ids.object(viewer, id, o.incarnation), name, seat(o.owner), seat(o.controller), zone_word(o.zone)))
}

fn characteristics(state: &GameState, viewer: PlayerId, id: ObjectId) -> Value {
    let Some(o) = state.objects.get(&id) else { return Value::Null };
    if o.face_down && o.zone == Zone::Battlefield && o.controller != viewer {
        // A face-down permanent shows a colourless 2/2 creature.
        return json!({"supertypes": [], "types": ["creature"], "subtypes": [], "colors": [],
            "mana_value": 0, "power": 2, "toughness": 2, "keywords": []});
    }
    if !readable(state, viewer, id) {
        return Value::Null;
    }
    let ct = &o.card_types;
    let is_creature = ct.core_types.contains(&CoreType::Creature);
    let mut keywords: Vec<String> = o
        .keywords
        .iter()
        .map(|k| {
            let s = k.to_string();
            word(s.split([':', ' ', '(']).next().unwrap_or(&s))
        })
        .collect();
    keywords.sort();
    keywords.dedup();
    json!({
        "supertypes": ct.supertypes.iter().map(|t| word(&t.to_string())).collect::<Vec<_>>(),
        "types": ct.core_types.iter().map(|t| word(&t.to_string())).collect::<Vec<_>>(),
        "subtypes": ct.subtypes.iter().map(|t| word(t)).collect::<Vec<_>>(),
        "colors": o.color.iter().map(|c| color_word(*c)).collect::<Vec<_>>(),
        "mana_value": o.mana_cost.mana_value(),
        "power": if is_creature { o.power.map(Value::from).unwrap_or(Value::Null) } else { Value::Null },
        "toughness": if is_creature { o.toughness.map(Value::from).unwrap_or(Value::Null) } else { Value::Null },
        "keywords": keywords,
    })
}

fn permanent(state: &GameState, ids: &Ids, viewer: PlayerId, id: ObjectId, atk: &BTreeSet<ObjectId>, blk: &BTreeSet<ObjectId>) -> Value {
    let Some(o) = state.objects.get(&id) else { return Value::Null };
    let mut counters = Map::new();
    for (kind, n) in o.counters.iter().filter(|(_, n)| **n > 0) {
        let k = word(&kind.as_str());
        let cur = counters.get(&k).and_then(Value::as_u64).unwrap_or(0);
        counters.insert(k, json!(cur + u64::from(*n)));
    }
    let attached = o.attached_to.map(|a| match a {
        AttachTarget::Object(host) => object_ref(state, ids, viewer, host).map(|r| json!({"object": r})).unwrap_or(Value::Null),
        AttachTarget::Player(p) => json!({"player": seat(p)}),
    });
    json!({
        "tapped": o.tapped, "summoning_sick": o.summoning_sick, "damage": o.damage_marked,
        "counters": Value::Object(counters), "attached_to": attached.unwrap_or(Value::Null),
        "attacking": atk.contains(&id), "attack_target": Value::Null,
        "blocking": blk.contains(&id), "blocked_attackers": [],
        "phased_out": false, "statuses": [], "class_level": Value::Null, "chosen": [],
    })
}

fn record(
    state: &GameState,
    ids: &Ids,
    viewer: PlayerId,
    id: ObjectId,
    atk: &BTreeSet<ObjectId>,
    blk: &BTreeSet<ObjectId>,
) -> Option<Value> {
    let o = state.objects.get(&id)?;
    let mut r = object_ref(state, ids, viewer, id)?;
    let m = r.as_object_mut().expect("object");
    m.insert("full_name".into(), Value::Null);
    m.insert("face_down".into(), json!(o.face_down && !readable(state, viewer, id)));
    m.insert("token".into(), json!(o.is_token));
    m.insert("copy".into(), json!(false));
    m.insert("characteristics".into(), characteristics(state, viewer, id));
    m.insert("permanent".into(), if o.zone == Zone::Battlefield { permanent(state, ids, viewer, id, atk, blk) } else { Value::Null });
    m.insert("exiled_by".into(), Value::Null);
    Some(r)
}

fn player(state: &GameState, ids: &Ids, viewer: PlayerId, p: &Player, atk: &BTreeSet<ObjectId>, blk: &BTreeSet<ObjectId>) -> Value {
    let recs =
        |it: &mut dyn Iterator<Item = ObjectId>| -> Vec<Value> { it.filter_map(|id| record(state, ids, viewer, id, atk, blk)).collect() };
    let mut pool = Map::new();
    for t in [ManaType::White, ManaType::Blue, ManaType::Black, ManaType::Red, ManaType::Green, ManaType::Colorless] {
        pool.insert(mana_letter(t).to_string(), json!(0));
    }
    for u in p.mana_pool.units() {
        let k = mana_letter(u.color);
        let n = pool[k].as_u64().unwrap_or(0) + 1;
        pool.insert(k.to_string(), json!(n));
    }
    let mut counters = Map::new();
    if p.poison_counters > 0 {
        counters.insert("poison".into(), json!(p.poison_counters));
    }
    let hand = (p.id == viewer).then(|| recs(&mut p.hand.iter().copied()));
    let battlefield = recs(&mut state.battlefield.iter().copied().filter(|id| state.objects.get(id).is_some_and(|o| o.controller == p.id)));
    let exile = recs(&mut state.exile.iter().copied().filter(|id| state.objects.get(id).is_some_and(|o| o.owner == p.id)));
    let command = recs(&mut state.command_zone.iter().copied().filter(|id| state.objects.get(id).is_some_and(|o| o.owner == p.id)));
    json!({
        "seat": seat(p.id), "life": p.life, "poison": p.poison_counters, "counters": Value::Object(counters),
        "designations": Value::Null, "progress": Value::Null, "mana_pool": Value::Object(pool),
        "lands_played_this_turn": p.lands_played_this_turn, "mulligans_taken": 0,
        "hand_count": p.hand.len(), "library_count": p.library.len(),
        "hand": hand, "battlefield": battlefield, "graveyard": recs(&mut p.graveyard.iter().copied()),
        "exile": exile, "command": command,
    })
}

fn stack_entry(state: &GameState, ids: &Ids, viewer: PlayerId, e: &engine::types::game_state::StackEntry) -> Value {
    let kind = match e.kind {
        StackEntryKind::Spell { .. } => "spell",
        StackEntryKind::ActivatedAbility { .. } => "activated_ability",
        _ => "triggered_ability",
    };
    let source = object_ref(state, ids, viewer, e.source_id);
    let (name, chars) = if matches!(e.kind, StackEntryKind::Spell { .. }) {
        (state.objects.get(&e.id).filter(|_| readable(state, viewer, e.id)).map(|o| o.name.clone()), characteristics(state, viewer, e.id))
    } else {
        (None, Value::Null)
    };
    let inc = state.objects.get(&e.id).map_or(0, |o| o.incarnation);
    json!({
        "object_id": ids.object(viewer, e.id, inc), "card_name": name, "owner_seat": seat(e.controller),
        "controller_seat": seat(e.controller), "zone": "stack", "stack_kind": kind,
        "source": if kind == "spell" { Value::Null } else { source.unwrap_or(Value::Null) },
        "face_down": false, "copy": false, "characteristics": chars, "targets": [], "divided": Value::Null,
        "modes": [], "x_value": Value::Null, "text": Value::Null,
    })
}

/// The observation `viewer` is allowed to see of `state`.
pub fn observation(state: &GameState, viewer: PlayerId, ids: &Ids) -> Value {
    let filtered = filter_state_for_viewer(state, viewer);
    let state = &filtered;
    let (mut atk, mut blk) = (BTreeSet::new(), BTreeSet::new());
    if let Some(c) = &state.combat {
        atk.extend(c.attackers.iter().map(|a| a.object_id));
        blk.extend(c.blocker_to_attacker.keys().copied());
        for list in c.blocker_assignments.values() {
            blk.extend(list.iter().copied());
        }
    }
    let priority = match state.waiting_for {
        WaitingFor::Priority { player } if player == viewer => json!(seat(viewer)),
        _ => Value::Null,
    };
    json!({
        "viewer": seat(viewer), "turn": state.turn_number, "phase_step": phase_step(state.phase),
        "active_seat": seat(state.active_player), "priority_seat": priority,
        "passed_seats": Value::Null, "day_night": Value::Null,
        "players": state.players.iter().map(|p| player(state, ids, viewer, p, &atk, &blk)).collect::<Vec<_>>(),
        "stack": state.stack.iter().rev().map(|e| stack_entry(state, ids, viewer, e)).collect::<Vec<_>>(),
        "pending_triggers": Value::Null, "known": [],
    })
}
