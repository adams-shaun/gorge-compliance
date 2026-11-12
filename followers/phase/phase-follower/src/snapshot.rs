//! phase `GameState` -> gorge snapshot vocabulary.
//!
//! The comparator diffs gorge's snapshots with the follower's field by field, so everything here
//! writes exactly [`follower_proto::row::Snapshot`] with the spellings of [`follower_proto::vocab`].
//!
//! phase finalises layers at the `apply` boundary: [`snapshot`] takes `&GameState` and reads the
//! post-layer `power`/`toughness`/`color`/`keywords`, so a caller that mutated the state directly
//! (debug actions, setup) MUST call `engine::game::layers::flush_layers(&mut state)` first.
//!
//! phase keeps `std::HashMap`s in its state (`counters`, `blocker_assignments`, ...): nothing here
//! lets their iteration order reach the output; every list is sorted or taken from an ordered
//! vector, and every map is rebuilt into a `BTreeMap`.

use std::collections::{BTreeMap, BTreeSet};

use engine::types::game_state::{GameState, StackEntryKind, WaitingFor};
use engine::types::identifiers::ObjectId;
use engine::types::mana::{ManaColor, ManaType};
use engine::types::phase::Phase;
use engine::types::player::{Player, PlayerCounterKind, PlayerId};
use engine::types::zones::Zone;
use engine::types::object_data::AttachTarget;
use engine::types::CoreType;
use follower_proto::row::{PermSnap, PlayerSnap, Snapshot, StackSnap};
use follower_proto::vocab;

use crate::names::fold;

/// The corpus spelling of each object, recorded by setup when the object is created.
#[derive(Debug, Clone, Default)]
pub struct NameMap {
    pub by_object: BTreeMap<ObjectId, String>,
}

/// gorge's step name for a phase step. Exhaustive on purpose: a variant phase adds later must be
/// classified here, not silently mislabelled.
pub fn step_name(phase: Phase) -> &'static str {
    let i = match phase {
        Phase::Untap => 0,
        Phase::Upkeep => 1,
        Phase::Draw => 2,
        Phase::PreCombatMain => 3,
        Phase::BeginCombat => 4,
        Phase::DeclareAttackers => 5,
        Phase::DeclareBlockers => 6,
        // First-strike and regular damage are both `combat-damage`.
        Phase::CombatDamage => 7,
        Phase::EndCombat => 8,
        Phase::PostCombatMain => 9,
        Phase::End => 10,
        Phase::Cleanup => 11,
    };
    vocab::STEP_NAMES[i]
}

fn seat(p: PlayerId) -> i64 {
    i64::from(p.0)
}

/// The front half of a (possibly `A // B`) name, accent-folded and lower-cased.
fn front_key(name: &str) -> String {
    fold(name.split(" // ").next().unwrap_or(name)).to_lowercase()
}

/// The name written for object `id` (see the module doc on corpus spelling): empty for a
/// face-down permanent, the corpus name while the object still shows the face it was created
/// from, phase's current face name otherwise (transformed, copied), and for a token the setup
/// token name or phase's name plus " Token".
fn display_name(state: &GameState, names: &NameMap, id: ObjectId) -> String {
    let Some(obj) = state.objects.get(&id) else { return String::new() };
    if obj.face_down && obj.zone == Zone::Battlefield {
        return String::new();
    }
    if obj.is_token {
        return match names.by_object.get(&id) {
            Some(n) => n.clone(),
            None if obj.name.ends_with(" Token") => obj.name.clone(),
            None => format!("{} Token", obj.name),
        };
    }
    match names.by_object.get(&id) {
        Some(corpus) if front_key(corpus) == front_key(&obj.name) => corpus.clone(),
        _ => obj.name.clone(),
    }
}

fn zone_names(state: &GameState, names: &NameMap, ids: impl IntoIterator<Item = ObjectId>) -> Vec<String> {
    ids.into_iter().map(|id| display_name(state, names, id)).collect()
}

fn mana_letter(t: ManaType) -> char {
    match t {
        ManaType::White => 'W',
        ManaType::Blue => 'U',
        ManaType::Black => 'B',
        ManaType::Red => 'R',
        ManaType::Green => 'G',
        ManaType::Colorless => 'C',
    }
}

fn color_letter(c: ManaColor) -> char {
    match c {
        ManaColor::White => 'W',
        ManaColor::Blue => 'U',
        ManaColor::Black => 'B',
        ManaColor::Red => 'R',
        ManaColor::Green => 'G',
    }
}

fn player_snap(state: &GameState, names: &NameMap, p: &Player) -> PlayerSnap {
    let exile = state.exile.iter().copied().filter(|id| state.objects.get(id).is_some_and(|o| o.owner == p.id));
    let command = state.command_zone.iter().copied().filter(|id| state.objects.get(id).is_some_and(|o| o.owner == p.id));
    let library = zone_names(state, names, p.library.iter().copied());
    let mut counters = BTreeMap::new();
    let mut put = |kind: &str, n: u32| {
        if n > 0 {
            counters.insert(kind.to_string(), i64::from(n));
        }
    };
    put("POISON", p.poison_counters);
    put("ENERGY", p.energy);
    for kind in [PlayerCounterKind::Experience, PlayerCounterKind::Rad, PlayerCounterKind::Ticket] {
        put(&vocab::counter_kind(&kind.to_string()), p.player_counter(&kind));
    }
    let pool: String = p.mana_pool.units().map(|u| mana_letter(u.color)).collect();
    PlayerSnap {
        seat: seat(p.id),
        life: i64::from(p.life),
        counters,
        hand: vocab::sort_zone(zone_names(state, names, p.hand.iter().copied())),
        // Oldest first: phase pushes to the back (`zones::add_to_zone`).
        graveyard: zone_names(state, names, p.graveyard.iter().copied()),
        exile: vocab::sort_zone(zone_names(state, names, exile)),
        command: vocab::sort_zone(zone_names(state, names, command)),
        library_count: library.len() as i64,
        library_top: vocab::library_top(&library),
        pool: vocab::pool(&pool),
    }
}

fn perm_snap(
    state: &GameState,
    names: &NameMap,
    id: ObjectId,
    attackers: &BTreeSet<ObjectId>,
    blockers: &BTreeSet<ObjectId>,
) -> Option<PermSnap> {
    let obj = state.objects.get(&id)?;
    let ct = &obj.card_types;
    let is_creature = ct.core_types.contains(&CoreType::Creature);
    let pt = match (is_creature, obj.power, obj.toughness) {
        (true, Some(p), Some(t)) => Some(vocab::pt(i64::from(p), i64::from(t))),
        _ => None,
    };
    let mut counters = BTreeMap::new();
    for (kind, n) in obj.counters.iter().filter(|(_, n)| **n > 0) {
        *counters.entry(vocab::counter_kind(&kind.as_str())).or_insert(0i64) += i64::from(*n);
    }
    let types: Vec<String> = ct
        .core_types
        .iter()
        .map(ToString::to_string)
        .chain(ct.supertypes.iter().map(ToString::to_string))
        .chain(ct.subtypes.iter().cloned())
        .collect();
    let colors: String = obj.color.iter().map(|c| color_letter(*c)).collect();
    let mut keywords: Vec<String> = obj.keywords.iter().map(ToString::to_string).collect();
    keywords.sort();
    keywords.dedup();
    let attached_to = obj.attached_to.map(|a| match a {
        AttachTarget::Object(host) => display_name(state, names, host),
        AttachTarget::Player(p) => format!("p{}", p.0),
    });
    Some(PermSnap {
        name: display_name(state, names, id),
        controller: seat(obj.controller),
        owner: seat(obj.owner),
        token: obj.is_token,
        tapped: obj.tapped,
        face_down: obj.face_down,
        pt,
        damage: i64::from(obj.damage_marked),
        counters,
        types: vocab::sorted_types(types),
        colors: vocab::colors(&colors),
        keywords,
        attached_to,
        attacking: attackers.contains(&id),
        blocking: blockers.contains(&id),
    })
}

/// The snapshot of `state` at `checkpoint`. The caller flushes layers first (module doc).
pub fn snapshot(state: &GameState, checkpoint: &str, names: &NameMap) -> Snapshot {
    let (mut attackers, mut blockers) = (BTreeSet::new(), BTreeSet::new());
    if let Some(c) = &state.combat {
        attackers.extend(c.attackers.iter().map(|a| a.object_id));
        for (blocker, attacked) in &c.blocker_to_attacker {
            if !attacked.is_empty() {
                blockers.insert(*blocker);
            }
        }
        for list in c.blocker_assignments.values() {
            blockers.extend(list.iter().copied());
        }
    }
    Snapshot {
        checkpoint: checkpoint.to_string(),
        turn: i64::from(state.turn_number),
        step: step_name(state.phase).to_string(),
        active: seat(state.active_player),
        priority: seat(state.priority_player),
        over: matches!(state.waiting_for, WaitingFor::GameOver { .. }),
        players: state.players.iter().map(|p| player_snap(state, names, p)).collect(),
        permanents: state
            .battlefield
            .iter()
            .filter_map(|id| perm_snap(state, names, *id, &attackers, &blockers))
            .collect(),
        stack: state
            .stack
            .iter()
            .rev()
            .map(|e| StackSnap {
                kind: match e.kind {
                    StackEntryKind::Spell { .. } => "spell",
                    StackEntryKind::ActivatedAbility { .. }
                    | StackEntryKind::TriggeredAbility { .. }
                    | StackEntryKind::KeywordAction { .. }
                    | StackEntryKind::CombatDamage { .. } => "ability",
                }
                .to_string(),
                source: display_name(state, names, e.source_id),
                controller: seat(e.controller),
            })
            .collect(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use engine::database::card_db::CardDatabase;
    use engine::game::apply;
    use engine::game::{create_debug_cards, debug_card_entry_source, DebugCardCreateRequest};
    use engine::game::layers::flush_layers;
    use engine::types::actions::{DebugAction, DebugCardCreationKind, GameAction};
    use engine::types::CounterType;
    use std::sync::OnceLock;

    fn db() -> &'static CardDatabase {
        static DB: OnceLock<CardDatabase> = OnceLock::new();
        DB.get_or_init(|| {
            let path = std::env::var("PHASE_CARD_DATA").expect("PHASE_CARD_DATA: run make phase-follower-test");
            crate::load_db(&path).expect("load card data")
        })
    }

    fn new_state() -> GameState {
        let mut state = GameState::new_two_player(7);
        state.debug_mode = true;
        state
    }

    /// One raw-placed card (no entry effects) of corpus name `name`.
    fn add(state: &mut GameState, name: &str, owner: u8, zone: Zone) -> ObjectId {
        let faces = crate::names::phase_faces(db(), name).expect(name);
        let first = state.next_object_id;
        create_debug_cards(
            state,
            DebugCardCreateRequest {
                actor: PlayerId(0),
                source: debug_card_entry_source(db(), faces[0]),
                owner: PlayerId(owner),
                zone,
                count: 1,
                attach_to: None,
                run_etb: false,
                nonlegendary: false,
                creation_kind: DebugCardCreationKind::Card,
            },
        )
        .expect("create card");
        assert_eq!(state.next_object_id, first + 1, "{name}: one object");
        ObjectId(first)
    }

    fn debug(state: &mut GameState, action: DebugAction) {
        apply(state, PlayerId(0), GameAction::Debug(action)).expect("debug action");
    }

    fn snap(state: &mut GameState) -> Snapshot {
        flush_layers(state);
        snapshot(state, "setup", &NameMap::default())
    }

    #[test]
    fn snapshot_battlefield_fields() {
        let mut state = new_state();
        let bears = add(&mut state, "Grizzly Bears", 0, Zone::Battlefield);
        debug(&mut state, DebugAction::SetTapped { object_id: bears, tapped: true });
        debug(
            &mut state,
            DebugAction::ModifyCounters { object_id: bears, counter_type: CounterType::Plus1Plus1, delta: 1 },
        );
        debug(&mut state, DebugAction::SetLife { player_id: PlayerId(1), life: 13 });
        let s = snap(&mut state);
        assert_eq!(s.checkpoint, "setup");
        assert_eq!(s.step, "untap");
        assert!(!s.over);
        assert_eq!(s.players[1].life, 13);
        assert_eq!(s.permanents.len(), 1);
        let p = &s.permanents[0];
        assert_eq!(p.name, "Grizzly Bears");
        assert_eq!((p.controller, p.owner), (0, 0));
        assert!(p.tapped && !p.token && !p.face_down && !p.attacking && !p.blocking);
        assert_eq!(p.pt.as_deref(), Some("3/3"));
        assert_eq!(p.counters, BTreeMap::from([("P1P1".to_string(), 1)]));
        assert_eq!(p.types, ["Bear", "Creature"]);
        assert_eq!(p.colors, "G");
        assert_eq!(p.damage, 0);
        assert_eq!(p.attached_to, None);
    }

    #[test]
    fn snapshot_zone_orders() {
        let mut state = new_state();
        for n in ["Llanowar Elves", "Grizzly Bears", "Forest"] {
            add(&mut state, n, 0, Zone::Hand);
        }
        // Oldest first: the first card created is the first in the list.
        for n in ["Grizzly Bears", "Llanowar Elves"] {
            add(&mut state, n, 0, Zone::Graveyard);
        }
        let lib = ["Forest", "Island", "Swamp", "Mountain", "Plains", "Llanowar Elves", "Grizzly Bears"];
        for n in lib {
            add(&mut state, n, 1, Zone::Library);
        }
        let s = snap(&mut state);
        assert_eq!(s.players[0].hand, ["Forest", "Grizzly Bears", "Llanowar Elves"]);
        assert_eq!(s.players[0].graveyard, ["Grizzly Bears", "Llanowar Elves"]);
        assert_eq!(s.players[1].library_count, 7);
        assert_eq!(s.players[1].library_top, &lib[..5]);
        assert!(s.players[0].library_top.is_empty());
        // phase appends to the back of the graveyard, so a later card sorts after an earlier one.
        add(&mut state, "Forest", 0, Zone::Graveyard);
        let s = snap(&mut state);
        assert_eq!(s.players[0].graveyard, ["Grizzly Bears", "Llanowar Elves", "Forest"]);
    }

    #[test]
    fn snapshot_exile_by_owner() {
        let mut state = new_state();
        add(&mut state, "Llanowar Elves", 1, Zone::Exile);
        add(&mut state, "Grizzly Bears", 0, Zone::Exile);
        add(&mut state, "Forest", 1, Zone::Exile);
        let s = snap(&mut state);
        assert_eq!(s.players[0].exile, ["Grizzly Bears"]);
        assert_eq!(s.players[1].exile, ["Forest", "Llanowar Elves"]);
    }

    #[test]
    fn snapshot_step_names() {
        let all = [
            Phase::Untap,
            Phase::Upkeep,
            Phase::Draw,
            Phase::PreCombatMain,
            Phase::BeginCombat,
            Phase::DeclareAttackers,
            Phase::DeclareBlockers,
            Phase::CombatDamage,
            Phase::EndCombat,
            Phase::PostCombatMain,
            Phase::End,
            Phase::Cleanup,
        ];
        let got: Vec<&str> = all.iter().map(|p| step_name(*p)).collect();
        assert_eq!(got, vocab::STEP_NAMES);
        // The map must stay exhaustive: no catch-all arm in `step_name`.
        let src = include_str!("snapshot.rs");
        let start = src.find(concat!("pub fn step_name", "(")).unwrap();
        let end = start + src[start..].find(concat!("fn seat", "(")).unwrap();
        assert!(!src[start..end].contains(concat!("_", " =>")), "step_name has a wildcard arm");
    }

    #[test]
    fn snapshot_output_is_deterministic() {
        let mut state = new_state();
        let bears = add(&mut state, "Grizzly Bears", 0, Zone::Battlefield);
        add(&mut state, "Llanowar Elves", 1, Zone::Battlefield);
        for (ct, delta) in [
            (CounterType::Plus1Plus1, 2),
            (CounterType::Loyalty, 3),
            (CounterType::Stun, 1),
            (CounterType::Lore, 4),
            (CounterType::Time, 5),
            (CounterType::Shield, 1),
        ] {
            debug(&mut state, DebugAction::ModifyCounters { object_id: bears, counter_type: ct, delta });
        }
        for n in ["Forest", "Island"] {
            add(&mut state, n, 0, Zone::Exile);
            add(&mut state, n, 1, Zone::Exile);
        }
        flush_layers(&mut state);
        let names = NameMap::default();
        let a = serde_json::to_string(&snapshot(&state, "setup", &names)).unwrap();
        let b = serde_json::to_string(&snapshot(&state, "setup", &names)).unwrap();
        assert_eq!(a, b);
        let c = serde_json::to_string(&snapshot(&state.clone(), "setup", &names)).unwrap();
        assert_eq!(a, c);
        let p = &snapshot(&state, "setup", &names).permanents[0];
        assert_eq!(p.counters.keys().map(String::as_str).collect::<Vec<_>>(), ["LORE", "LOYALTY", "P1P1", "SHIELD", "STUN", "TIME"]);
    }

    #[test]
    fn snapshot_corpus_names_and_tokens() {
        let mut state = new_state();
        let fire = add(&mut state, "Fire", 0, Zone::Hand);
        let mut names = NameMap::default();
        names.by_object.insert(fire, "Fire".to_string());
        let s = snapshot(&state, "setup", &names);
        assert_eq!(s.players[0].hand, ["Fire"]);
        // A name that does not match the object's face is not rewritten.
        names.by_object.insert(fire, "Something Else".to_string());
        let s = snapshot(&state, "setup", &names);
        assert_eq!(s.players[0].hand, [state.objects.get(&fire).unwrap().name.clone()]);
    }
}
