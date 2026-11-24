//! phase `GameState` -> the SpellBench v2 observation of one seat.
//!
//! Information rule: the state is first filtered by phase's own `filter_state_for_viewer`, then
//! projected. The other seat's hand and every library are never listed (the other hand is `null`,
//! libraries are counts), a face-down object off the battlefield has no name or characteristics,
//! and an object id is `"o-" + hex(HMAC(id_key, viewer:object:incarnation:zone)[:8])`, new for each
//! viewer and for each incarnation phase gives an object (CR 400.7), so an id never links a card
//! across a zone change or a shuffle.
//!
//! Gaps (declared false in `observe::FLAGS` or noted here): no progress, `full_name`, `known`
//! (always `[]`), `passed_seats` or stack text; stack `targets`/`modes` are empty and `x_value`
//! is null. `pending_triggers` lists phase's one in-flight trigger (the one choosing targets);
//! `designations` are the monarch and the initiative.

use std::collections::{BTreeMap, BTreeSet};

use engine::game::filter_state_for_viewer;
use engine::types::game_state::{DayNight, GameState, StackEntryKind, WaitingFor};
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
    /// Which look the library cards in view belong to: a card looked at or searched for is a new object
    /// to the seat each time (V7), so its id carries the look's number.
    look: std::cell::Cell<u64>,
}

impl Ids {
    pub fn new(game_secret: &[u8; 32]) -> Self {
        Ids { key: hmac_sha256(game_secret, b"spellbench/v2/object-id"), look: std::cell::Cell::new(0) }
    }

    /// `zone` is the zone the observation shows the object in: a spell is on the stack while phase
    /// still lists its card in a hand, and the zone change is a new object to the validator (V7).
    /// A new look opens: the library cards it shows get ids no earlier look used.
    pub fn open_look(&self) {
        self.look.set(self.look.get() + 1);
    }

    /// A new id for an object the seat saw leave and come back; `old` is the id it was minted under, `n` the
    /// count of reissues so far in the game, so no two ids repeat.
    pub fn reissue(&self, viewer: PlayerId, old: &str, n: u64) -> String {
        let m = format!("reissue:{}:{}:{}", viewer.0, old, n);
        format!("o-{}", hex(&hmac_sha256(&self.key, m.as_bytes())[..8]))
    }

    pub fn object(&self, viewer: PlayerId, id: ObjectId, incarnation: u64, zone: &str) -> String {
        let look = if matches!(zone, "library" | "hand-revealed") { self.look.get() } else { 0 };
        let m = format!("{}:{}:{}:{}:{}", viewer.0, id.0, incarnation, zone, look);
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

pub fn mana_letter(t: ManaType) -> &'static str {
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

/// The stack entry of a spell whose card is `id`. Phase pushes the entry while the card is still in
/// its hand list (targets and costs are chosen after), so the stack decides where the card is.
pub(crate) fn spell_entry(state: &GameState, id: ObjectId) -> Option<&engine::types::game_state::StackEntry> {
    state.stack.iter().find(|e| e.id == id && matches!(e.kind, StackEntryKind::Spell { .. }))
}

/// An ObjectRef (`{object_id, card_name, owner_seat, controller_seat, zone}`), or `None` for an id the
/// state no longer holds.
pub fn object_ref(state: &GameState, ids: &Ids, viewer: PlayerId, id: ObjectId) -> Option<Value> {
    let o = state.objects.get(&id)?;
    let name = readable(state, viewer, id).then_some(o.name.as_str());
    let (controller, zone) = match spell_entry(state, id) {
        Some(e) => (e.controller, "stack"),
        None => (o.controller, zone_word(o.zone)),
    };
    Some(crate::semantic::object_ref(&ids.object(viewer, id, o.incarnation, zone), name, seat(o.owner), seat(controller), zone))
}

/// The ref of a card the seat is looking at (a library card looked at or searched, or a card of the
/// other hand it was shown): named, whatever zone it is in. Its `known` entry says the same.
pub fn looked_ref(state: &GameState, ids: &Ids, id: ObjectId, viewer: PlayerId) -> Option<Value> {
    let o = state.objects.get(&id)?;
    let zone = zone_word(o.zone);
    let tag = if o.zone == Zone::Hand { "hand-revealed" } else { zone };
    Some(crate::semantic::object_ref(&ids.object(viewer, id, o.incarnation, tag), Some(&o.name), seat(o.owner), seat(o.controller), zone))
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
    let exiler = (o.zone == Zone::Exile)
        .then(|| state.exile_links.iter().find(|l| l.exiled_id == id))
        .flatten()
        .and_then(|l| object_ref(state, ids, viewer, l.source_id))
        .unwrap_or(Value::Null);
    m.insert("exiled_by".into(), exiler);
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
    // An object is listed in the zone it says it is in, once: a zone vector that still holds an
    // object phase has moved on (a spell now on the stack) must not list it a second time.
    let in_zone = |z: Zone, id: &ObjectId| spell_entry(state, *id).is_none() && state.objects.get(id).is_some_and(|o| o.zone == z);
    let hand = (p.id == viewer).then(|| recs(&mut p.hand.iter().copied().filter(|id| in_zone(Zone::Hand, id))));
    // A card phase still lists in a hand while its spell is on the stack is not in the hand (V5).
    let hand_count = hand.as_ref().map_or_else(|| p.hand.iter().filter(|id| spell_entry(state, **id).is_none()).count(), Vec::len);
    let battlefield = recs(&mut state.battlefield.iter().copied().filter(|id| in_zone(Zone::Battlefield, id) && state.objects[id].controller == p.id));
    let graveyard = recs(&mut p.graveyard.iter().copied().filter(|id| in_zone(Zone::Graveyard, id)));
    let exile = recs(&mut state.exile.iter().copied().filter(|id| in_zone(Zone::Exile, id) && state.objects[id].owner == p.id));
    let command = recs(&mut state.command_zone.iter().copied().filter(|id| in_zone(Zone::Command, id) && state.objects[id].owner == p.id));
    json!({
        "seat": seat(p.id), "life": p.life, "poison": p.poison_counters, "counters": Value::Object(counters),
        "designations": designations(state, p.id), "progress": Value::Null, "mana_pool": Value::Object(pool),
        "lands_played_this_turn": p.lands_played_this_turn, "mulligans_taken": 0,
        "hand_count": hand_count, "library_count": p.library.len(),
        "hand": hand, "battlefield": battlefield, "graveyard": graveyard,
        "exile": exile, "command": command,
    })
}

/// The v2 designations of a seat, sorted: `initiative`, `monarch`.
fn designations(state: &GameState, p: PlayerId) -> Value {
    let mut d = Vec::new();
    if state.initiative == Some(p) {
        d.push("initiative");
    }
    if state.monarch == Some(p) {
        d.push("monarch");
    }
    json!(d)
}

/// The one trigger phase holds while it asks for its targets, unless its source is hidden.
fn pending_triggers(state: &GameState, ids: &Ids, viewer: PlayerId) -> Value {
    let Some(t) = &state.pending_trigger else { return json!([]) };
    if !readable(state, viewer, t.source_id) {
        return json!([]);
    }
    let name = state.objects.get(&t.source_id).map(|o| o.name.clone());
    json!([{"source": object_ref(state, ids, viewer, t.source_id), "source_name": name,
        "controller_seat": seat(t.controller), "label": Value::Null, "optional": false}])
}

fn stack_entry(state: &GameState, ids: &Ids, viewer: PlayerId, e: &engine::types::game_state::StackEntry) -> Value {
    let kind = match e.kind {
        StackEntryKind::Spell { .. } => "spell",
        StackEntryKind::ActivatedAbility { .. } => "activated_ability",
        _ => "triggered_ability",
    };
    let source = object_ref(state, ids, viewer, e.source_id);
    let is_spell = matches!(e.kind, StackEntryKind::Spell { .. });
    let chars = if is_spell { characteristics(state, viewer, e.id) } else { Value::Null };
    let base = if is_spell { object_ref(state, ids, viewer, e.id) } else { None };
    let (object_id, card_name, owner, controller) = match &base {
        Some(r) => (r["object_id"].clone(), r["card_name"].clone(), r["owner_seat"].clone(), r["controller_seat"].clone()),
        None => {
            let inc = state.objects.get(&e.id).map_or(0, |o| o.incarnation);
            (json!(ids.object(viewer, e.id, inc, "stack")), Value::Null, json!(seat(e.controller)), json!(seat(e.controller)))
        }
    };
    json!({
        "object_id": object_id, "card_name": card_name, "owner_seat": owner,
        "controller_seat": controller, "zone": "stack", "stack_kind": kind,
        "source": if kind == "spell" { Value::Null } else { source.unwrap_or(Value::Null) },
        "face_down": false, "copy": false, "characteristics": chars, "targets": [], "divided": Value::Null,
        "modes": [], "x_value": Value::Null, "text": Value::Null,
    })
}

/// A spell phase is resolving: its card is in the stack zone but its entry is already off the list
/// (a reflexive trigger, "when you do", names it as its source). It is the top of the stack for the
/// observation, so every ref to it is a record.
fn resolving_spells(state: &GameState) -> Vec<ObjectId> {
    let mut v: Vec<ObjectId> = state.objects.values().filter(|o| o.zone == Zone::Stack && spell_entry(state, o.id).is_none()).map(|o| o.id).collect();
    v.sort();
    v
}

fn resolving_entry(state: &GameState, ids: &Ids, viewer: PlayerId, id: ObjectId) -> Option<Value> {
    let r = object_ref(state, ids, viewer, id)?;
    Some(json!({
        "object_id": r["object_id"], "card_name": r["card_name"], "owner_seat": r["owner_seat"],
        "controller_seat": r["controller_seat"], "zone": "stack", "stack_kind": "spell",
        "source": Value::Null, "face_down": false, "copy": false, "characteristics": characteristics(state, viewer, id),
        "targets": [], "divided": Value::Null, "modes": [], "x_value": Value::Null, "text": Value::Null,
    }))
}

/// The observation `viewer` is allowed to see of `state`.
pub fn observation(state: &GameState, viewer: PlayerId, ids: &Ids, looks: &[(ObjectId, &'static str)]) -> Value {
    let known = known(state, ids, viewer, looks);
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
    let obs = json!({
        "viewer": seat(viewer), "turn": state.turn_number, "phase_step": phase_step(state.phase),
        "active_seat": seat(state.active_player), "priority_seat": priority,
        "passed_seats": Value::Null,
        "day_night": match state.day_night { None => "none", Some(DayNight::Day) => "day", Some(DayNight::Night) => "night" },
        "players": state.players.iter().map(|p| player(state, ids, viewer, p, &atk, &blk)).collect::<Vec<_>>(),
        "stack": resolving_spells(state).into_iter().filter_map(|id| resolving_entry(state, ids, viewer, id))
            .chain(state.stack.iter().rev().map(|e| stack_entry(state, ids, viewer, e))).collect::<Vec<_>>(),
        "pending_triggers": pending_triggers(state, ids, viewer), "known": known,
    });
    let obs = scrub_refs(obs);
    report_duplicate_ids(state, &obs);
    obs
}

/// V4: every object an observation points at (`exiled_by`, `attached_to`, a stack entry's `source`, a
/// pending trigger's source) must be one of its records. Phase can point at an object this viewer is
/// not shown (one in a library or a hidden hand) or no longer holds; such a pointer is `null`, and a
/// pending trigger without a shown source is left out.
fn scrub_refs(mut obs: Value) -> Value {
    let mut ids: BTreeSet<String> = BTreeSet::new();
    let zones = ["hand", "battlefield", "graveyard", "exile", "command"];
    for p in obs["players"].as_array().into_iter().flatten() {
        for z in zones {
            ids.extend(p[z].as_array().into_iter().flatten().filter_map(|r| r["object_id"].as_str().map(str::to_string)));
        }
    }
    ids.extend(obs["stack"].as_array().into_iter().flatten().filter_map(|r| r["object_id"].as_str().map(str::to_string)));
    ids.extend(obs["known"].as_array().into_iter().flatten().filter_map(|r| r["object_id"].as_str().map(str::to_string)));
    let shown = |r: &Value| r["object_id"].as_str().is_some_and(|id| ids.contains(id));
    for p in obs["players"].as_array_mut().into_iter().flatten() {
        for z in zones {
            for r in p[z].as_array_mut().into_iter().flatten() {
                if !r["exiled_by"].is_null() && !shown(&r["exiled_by"]) {
                    r["exiled_by"] = Value::Null;
                }
                let at = &r["permanent"]["attached_to"];
                if at.get("object").is_some() && !shown(&at["object"]) {
                    r["permanent"]["attached_to"] = Value::Null;
                }
            }
        }
    }
    for e in obs["stack"].as_array_mut().into_iter().flatten() {
        if !e["source"].is_null() && !shown(&e["source"]) {
            e["source"] = Value::Null;
        }
    }
    if let Some(list) = obs["pending_triggers"].as_array_mut() {
        list.retain(|t| shown(&t["source"]));
    }
    obs
}

/// The library cards this decision lets the viewer see (`looked_at`, `searching`), in the spec's order
/// (owner, zone, name, position, how, id). A card the viewer may not read is left out.
fn known(state: &GameState, ids: &Ids, viewer: PlayerId, looks: &[(ObjectId, &'static str)]) -> Vec<Value> {
    let mut rows: Vec<((String, String, String, Option<u64>, String, String), Value)> = Vec::new();
    for (id, how) in looks {
        let Some(o) = state.objects.get(id) else { continue };
        // A library card looked at or searched, or a card of another seat's hand shown to the viewer.
        let (zone, top) = match o.zone {
            Zone::Library if readable(state, viewer, *id) => (
                "library",
                (*how != "searching")
                    .then(|| state.players.iter().find(|p| p.id == o.owner).and_then(|p| p.library.iter().position(|x| x == id)).map(|n| n as u64))
                    .flatten(),
            ),
            Zone::Hand if o.owner != viewer => ("hand", None),
            _ => continue,
        };
        let oid = ids.object(viewer, *id, o.incarnation, if zone == "hand" { "hand-revealed" } else { zone });
        let key = (seat(o.owner).to_string(), zone.to_string(), o.name.clone(), top, (*how).to_string(), oid.clone());
        rows.push((
            key,
            json!({"owner_seat": seat(o.owner), "zone": zone, "card_name": o.name, "object_id": oid,
                "position_from_top": top, "position_from_bottom": Value::Null, "how": how}),
        ));
    }
    rows.sort_by(|a, b| a.0.cmp(&b.0));
    rows.into_iter().map(|(_, v)| v).collect()
}

/// A diagnostic, not a filter: every object id must appear once in an observation (V4). A repeat is
/// an engine bug; it is named on stderr (card, zone, kind) so the next run can fix the projection.
fn report_duplicate_ids(state: &GameState, obs: &Value) {
    let mut seen: BTreeMap<String, String> = BTreeMap::new();
    let mut each = |v: &Value, where_: &str| {
        let Some(id) = v["object_id"].as_str() else { return };
        if let Some(first) = seen.insert(id.to_string(), where_.to_string()) {
            eprintln!(
                "phase-seat: duplicate object id {id}: first in {first}, again in {where_}: {} zone {} turn {} waiting {}",
                v["card_name"],
                v["zone"],
                state.turn_number,
                crate::waiting::classify(&state.waiting_for).0
            );
        }
    };
    for p in obs["players"].as_array().into_iter().flatten() {
        for z in ["hand", "battlefield", "graveyard", "exile", "command"] {
            for r in p[z].as_array().into_iter().flatten() {
                each(r, z);
            }
        }
    }
    for r in obs["stack"].as_array().into_iter().flatten() {
        each(r, "stack");
    }
    for r in obs["known"].as_array().into_iter().flatten() {
        each(r, "known");
    }
}
