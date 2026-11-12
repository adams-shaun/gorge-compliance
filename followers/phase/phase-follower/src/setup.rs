//! Board setup: place the two-seat board of `item.setup` exactly as gorge's runner does, run turn 1
//! up to p0's first main-1 priority and take the `setup` snapshot.
//!
//! Cards are placed with phase's production debug actions, never `GameScenario`/`add_real_card`
//! (those run the real entry pipeline, so an enters-tapped land would enter tapped and
//! enters-with-counters would add counters). Raw placement means: no ETB triggers, no entry
//! replacements, no summoning sickness; a planeswalker or battle still arrives with its printed
//! loyalty/defense. Life is written directly (`DebugAction::SetLife` is a real life-change event
//! and would leave "life gained/lost this turn" behind).
//!
//! Turn 1 is started with phase's own `start_game_skip_mulligan` (turn 1, p0 active, untap step,
//! the engine auto-advances), so p0's permanents untap and p0 draws no card the same way they do
//! in a real game; the upkeep/draw priority windows are passed without a checkpoint.

use std::collections::BTreeMap;
use std::str::FromStr;

use engine::database::card_db::CardDatabase;
use engine::game::layers::flush_layers;
use engine::game::{apply, create_debug_cards, debug_card_entry_source, start_game_skip_mulligan, DebugCardCreateRequest};
use engine::types::actions::{DebugAction, DebugCardCreationKind, DebugTokenRequest, GameAction};
use engine::types::game_state::{GameState, WaitingFor};
use engine::types::identifiers::ObjectId;
use engine::types::mana::ManaColor;
use engine::types::object_data::AttachTarget;
use engine::types::phase::Phase;
use engine::types::player::PlayerId;
use engine::types::proposed_event::TokenCharacteristics;
use engine::types::zones::Zone;
use engine::types::{parse_counter_type, CoreType, Keyword, Supertype};
use follower_proto::batch::{RowBuilder, Stop};
use follower_proto::refs::{Ref, RefTable};
use follower_proto::request::{Counters, Request, Seat};
use follower_proto::row::strict_miss_msg;

use crate::names::phase_faces;
use crate::snapshot::{snapshot, NameMap};

/// The filler card of a short library (gorge's runner pads each seat's library to 40 with it).
const FILLER: &str = "Wastes";
/// Library size the filler pads up to.
const LIBRARY_SIZE: usize = 40;
/// Cap on the priority passes between turn 1's untap step and p0's first main-1 priority.
const MAX_PASSES: usize = 64;

/// A placed object: its id plus the incarnation it had when placed (CR 400.7: the id survives a
/// zone change, the incarnation marks the new object).
pub type Placed = (ObjectId, u64);

/// The result of setup: the game at p0's first main-1 priority and the tables later stages use.
pub struct Board {
    pub state: GameState,
    pub refs: RefTable<Placed>,
    pub names: NameMap,
}

impl Board {
    /// The live object a ref names: a bound card ref whose incarnation still matches, or the k-th
    /// battlefield token controlled by the ref's seat whose name contains the ref's substring.
    pub fn resolve(&self, r: &Ref) -> Option<ObjectId> {
        match r {
            Ref::Card { .. } => {
                let (id, inc) = self.refs.bound(r)?;
                self.state.objects.get(&id).filter(|o| o.incarnation == inc).map(|_| id)
            }
            Ref::Token { seat, sub, ord } => {
                let sub = sub.to_lowercase();
                self.state
                    .battlefield
                    .iter()
                    .copied()
                    .filter(|id| {
                        self.state.objects.get(id).is_some_and(|o| {
                            o.is_token
                                && o.controller.0 == *seat
                                && (o.name.to_lowercase().contains(&sub)
                                    || self.names.by_object.get(id).is_some_and(|n| n.to_lowercase().contains(&sub)))
                        })
                    })
                    .nth((*ord as usize).checked_sub(1)?)
            }
            Ref::Player(_) | Ref::Ability { .. } => None,
        }
    }
}

fn harness(msg: impl Into<String>) -> Stop {
    Stop::Harness(format!("phase follower: {}", msg.into()))
}

/// `true` for a JSON value that carries no setup (null, `{}`, `[]`, `""`, `0`, `false`).
fn is_blank(v: &serde_json::Value) -> bool {
    use serde_json::Value;
    match v {
        Value::Null => true,
        Value::Bool(b) => !b,
        Value::Number(n) => n.as_f64() == Some(0.0),
        Value::String(s) => s.is_empty(),
        Value::Array(a) => a.is_empty(),
        Value::Object(o) => o.is_empty(),
    }
}

/// Refuse every setup shape phase cannot reproduce, with the message classify reads.
fn check_supported(req: &Request) -> Result<(), Stop> {
    let item = &req.item;
    if item.turn() != 1 {
        return Err(harness(format!("scenario turn {} unsupported", item.turn())));
    }
    if item.players() != 2 {
        return Err(harness(format!("scenario players {} unsupported", item.players())));
    }
    for i in 0..2 {
        let seat = item.seat(i).expect("seat 0 and 1");
        if seat.speed != 0 {
            return Err(harness("speed setup unsupported"));
        }
        if !seat.command.is_empty() {
            return Err(harness("command setup unsupported"));
        }
        if !seat.sideboard.is_empty() {
            return Err(harness("sideboard setup unsupported"));
        }
        if !is_blank(&seat.mana) {
            return Err(harness("mana setup unsupported"));
        }
    }
    Ok(())
}

struct Builder<'a> {
    db: &'a CardDatabase,
    board: Board,
    /// Per (seat, corpus name): placements so far, the `#k` of the next ref.
    placed: BTreeMap<(u8, String), u32>,
}

impl Builder<'_> {
    fn debug(&mut self, action: DebugAction) -> Result<(), Stop> {
        apply(&mut self.board.state, PlayerId(0), GameAction::Debug(action))
            .map(|_| ())
            .map_err(|e| harness(format!("setup debug action failed: {e:?}")))
    }

    /// Create one card named `name` (corpus spelling) owned by `seat` in `zone` (raw: no entry
    /// effects) and record its corpus name. Returns the new object.
    fn create(&mut self, seat: u8, name: &str, zone: Zone) -> Result<ObjectId, Stop> {
        let faces = phase_faces(self.db, name).map_err(|_| Stop::Harness(format!("phase lacks card: {name}")))?;
        let state = &mut self.board.state;
        let first = state.next_object_id;
        create_debug_cards(
            state,
            DebugCardCreateRequest {
                actor: PlayerId(0),
                source: debug_card_entry_source(self.db, faces[0]),
                owner: PlayerId(seat),
                zone,
                count: 1,
                attach_to: None,
                run_etb: false,
                nonlegendary: false,
                creation_kind: DebugCardCreationKind::Card,
            },
        )
        .map_err(|e| harness(format!("cannot place {name}: {e:?}")))?;
        if state.next_object_id != first + 1 {
            return Err(harness(format!("placing {name} created {} objects", state.next_object_id - first)));
        }
        let id = ObjectId(first);
        self.board.names.by_object.insert(id, name.to_string());
        Ok(id)
    }

    /// Bind `pN:Name` (first) / `pN:Name#k` to `id`.
    fn bind(&mut self, seat: u8, name: &str, id: ObjectId) {
        let k = self.placed.entry((seat, name.to_string())).or_insert(0);
        *k += 1;
        let r = Ref::Card { seat, name: name.to_string(), ord: *k };
        let inc = self.board.state.objects[&id].incarnation;
        self.board.refs.bind(&r, (id, inc));
    }

    fn place(&mut self, seat: u8, name: &str, zone: Zone) -> Result<ObjectId, Stop> {
        let id = self.create(seat, name, zone)?;
        self.bind(seat, name, id);
        Ok(id)
    }

    /// ADD `counters` to `id` (`ModifyCounters` bypasses replacements).
    fn add_counters(&mut self, id: ObjectId, counters: &Counters) -> Result<(), Stop> {
        for (kind, n) in counters {
            let delta = i32::try_from(*n).map_err(|_| harness(format!("counter count {n} out of range")))?;
            self.debug(DebugAction::ModifyCounters { object_id: id, counter_type: parse_counter_type(kind), delta })?;
        }
        Ok(())
    }

    fn battlefield(&mut self, seat: u8, s: &Seat) -> Result<(), Stop> {
        for name in &s.battlefield {
            let id = self.place(seat, name, Zone::Battlefield)?;
            if let Some(counters) = s.counters.get(name) {
                self.add_counters(id, counters)?;
            }
            if s.tapped.contains(name) {
                self.debug(DebugAction::SetTapped { object_id: id, tapped: true })?;
            }
            if s.back_face.contains(name) {
                self.debug(DebugAction::SetFaceState {
                    object_id: id,
                    face_down: None,
                    transformed: Some(true),
                    flipped: None,
                })?;
            }
        }
        Ok(())
    }

    /// The seat's library, top first: `library_top`, the filler, then `library`.
    fn library(&mut self, seat: u8, s: &Seat, named: usize) -> Result<(), Stop> {
        let mut named_lib = Vec::new();
        for name in &s.library {
            named_lib.push(self.place(seat, name, Zone::Library)?);
        }
        for name in &s.exile {
            self.place(seat, name, Zone::Exile)?;
        }
        let mut top = Vec::new();
        for name in &s.library_top {
            top.push(self.place(seat, name, Zone::Library)?);
        }
        let mut filler = Vec::new();
        for _ in 0..LIBRARY_SIZE.saturating_sub(named) {
            filler.push(self.create(seat, FILLER, Zone::Library)?);
        }
        // Creation appended everything in creation order; the order that counts is top first.
        let ordered: Vec<ObjectId> = top.into_iter().chain(filler).chain(named_lib).collect();
        let player = &mut self.board.state.players[usize::from(seat)];
        player.library = ordered.into_iter().collect();
        Ok(())
    }

    fn tokens(&mut self, seat: u8, req: &Request, s: &Seat) -> Result<(), Stop> {
        for spec in &s.tokens {
            let t = req
                .neutral
                .token
                .get(&spec.script)
                .ok_or_else(|| Stop::Harness(format!("phase lacks card: token {}", spec.script)))?;
            let characteristics = token_characteristics(&spec.script, t)?;
            let first = self.board.state.next_object_id;
            self.debug(DebugAction::CreateToken {
                request: DebugTokenRequest::Custom {
                    owner: PlayerId(seat),
                    characteristics,
                    enter_with_counters: Vec::new(),
                },
                count: 1,
                run_etb: false,
            })?;
            let state = &self.board.state;
            if state.next_object_id != first + 1 {
                return Err(harness(format!(
                    "token {} created {} objects",
                    spec.script,
                    state.next_object_id - first
                )));
            }
            let id = ObjectId(first);
            self.board.names.by_object.insert(id, t.name.clone());
            // The listed counters are the complete set; add them through the replacement-free
            // debug action, never the token's own entry.
            self.add_counters(id, &spec.counters)?;
        }
        Ok(())
    }

    fn attached(&mut self, seat: u8, s: &Seat) -> Result<(), Stop> {
        for a in &s.attached {
            let card = Ref::parse(&format!("p{seat}:{}", a.card)).map_err(|e| harness(format!("setup attached: {e}")))?;
            let to = Ref::parse(&a.to).map_err(|e| harness(format!("setup attached: {e}")))?;
            let aura = self.board.resolve(&card).ok_or_else(|| harness(format!("setup attached names {card}, which is not placed")))?;
            let host = self.board.resolve(&to).ok_or_else(|| harness(format!("setup attached names {to}, which is not on the battlefield")))?;
            self.debug(DebugAction::Attach { object_id: aura, target: AttachTarget::Object(host) })?;
        }
        Ok(())
    }
}

/// A token's characteristics from `neutral.token[stem]`.
fn token_characteristics(
    stem: &str,
    t: &follower_proto::request::NeutralToken,
) -> Result<TokenCharacteristics, Stop> {
    let mut core_types = Vec::new();
    for ty in &t.types {
        core_types.push(CoreType::from_str(ty).map_err(|_| harness(format!("token {stem}: unknown type {ty}")))?);
    }
    let mut supertypes = Vec::new();
    for ty in &t.supertypes {
        supertypes.push(Supertype::from_str(ty).map_err(|_| harness(format!("token {stem}: unknown supertype {ty}")))?);
    }
    let mut colors = Vec::new();
    for c in t.colours.as_deref().unwrap_or("").chars() {
        colors.push(match c {
            'W' => ManaColor::White,
            'U' => ManaColor::Blue,
            'B' => ManaColor::Black,
            'R' => ManaColor::Red,
            'G' => ManaColor::Green,
            other => return Err(harness(format!("token {stem}: unknown colour {other}"))),
        });
    }
    // Keyword::from_str is infallible.
    let keywords = t.keywords.iter().filter_map(|k| Keyword::from_str(k).ok()).collect();
    Ok(TokenCharacteristics {
        display_name: t.name.clone(),
        power: t.power.as_deref().and_then(|p| p.parse().ok()),
        toughness: t.toughness.as_deref().and_then(|p| p.parse().ok()),
        loyalty: None,
        core_types,
        subtypes: t.subtypes.clone(),
        supertypes,
        colors,
        keywords,
    })
}

/// Place the board of `req`, run turn 1 to p0's first main-1 priority, apply `tapped_after_untap`
/// and push the `setup` snapshot to `rb`.
pub fn run(db: &CardDatabase, req: &Request, rb: &mut RowBuilder) -> Result<Board, Stop> {
    check_supported(req)?;
    let mut state = GameState::new_two_player(u64::from(crc32fast::hash(req.id.as_bytes())));
    state.debug_mode = true;
    // Before placing: summoning sickness compares the entry turn with this.
    state.turn_number = 1;
    let mut b = Builder {
        db,
        board: Board { state, refs: RefTable::new(), names: NameMap::default() },
        placed: BTreeMap::new(),
    };
    for seat in 0..2u8 {
        let s = req.item.seat(usize::from(seat)).expect("seat 0 and 1");
        b.battlefield(seat, s)?;
        for name in &s.hand {
            b.place(seat, name, Zone::Hand)?;
        }
        for name in &s.graveyard {
            b.place(seat, name, Zone::Graveyard)?;
        }
        // Named cards of the seat: what the filler pads up from.
        let named = s.battlefield.len() + s.hand.len() + s.graveyard.len() + s.library.len() + s.exile.len() + s.library_top.len();
        b.library(seat, s, named)?;
        b.tokens(seat, req, s)?;
    }
    for seat in 0..2u8 {
        let s = req.item.seat(usize::from(seat)).expect("seat 0 and 1");
        b.attached(seat, s)?;
    }
    let Board { state, .. } = &mut b.board;
    for seat in 0..2usize {
        let life = req.item.seat(seat).expect("seat 0 and 1").life();
        let life = i32::try_from(life).map_err(|_| harness(format!("life {life} out of range")))?;
        // Direct write: no life-gained/lost history.
        state.players[seat].life = life;
    }
    // Raw placement leaves no history: no "entered this turn", no summoning sickness.
    for id in state.battlefield.iter().copied().collect::<Vec<_>>() {
        if let Some(o) = state.objects.get_mut(&id) {
            o.summoning_sick = false;
            o.entered_battlefield_turn = None;
        }
    }
    state.debug_mode = false;
    flush_layers(state);
    start_game_skip_mulligan(state);
    advance_to_main1(state)?;
    let mut board = b.board;
    tap_after_untap(&mut board, req, rb)?;
    flush_layers(&mut board.state);
    rb.push_snapshot(snapshot(&board.state, "setup", &board.names));
    Ok(board)
}

/// Pass priority (no checkpoint) until p0 holds priority in the first main phase.
fn advance_to_main1(state: &mut GameState) -> Result<(), Stop> {
    for _ in 0..MAX_PASSES {
        match &state.waiting_for {
            WaitingFor::Priority { player } => {
                let p = *player;
                if state.phase == Phase::PreCombatMain && p == PlayerId(0) {
                    return Ok(());
                }
                apply(state, p, GameAction::PassPriority)
                    .map_err(|e| harness(format!("passing priority during setup failed: {e:?}")))?;
            }
            other => {
                // A prompt during setup would be answered from the step -1 gorge decisions; no
                // setup prompt is answered yet, so it is a strict miss.
                return Err(Stop::StrictMiss(strict_miss_msg("setup", -1, &format!("{other:?}"))));
            }
        }
    }
    Err(harness("setup did not reach main-1 priority"))
}

/// `tapped_after_untap`: seat-relative refs tapped at p0's first main-1 priority, one object each,
/// no trigger. The debug action needs `debug_mode`, switched on for these taps only.
fn tap_after_untap(board: &mut Board, req: &Request, rb: &mut RowBuilder) -> Result<(), Stop> {
    let mut targets = Vec::new();
    for seat in 0..2u8 {
        let s = req.item.seat(usize::from(seat)).expect("seat 0 and 1");
        for r in &s.tapped_after_untap {
            let parsed = Ref::parse(&format!("p{seat}:{r}")).map_err(|e| harness(format!("tapped_after_untap: {e}")))?;
            let id = board
                .resolve(&parsed)
                .ok_or_else(|| harness(format!("tapped_after_untap names {parsed}, which is not on the battlefield")))?;
            targets.push(id);
        }
    }
    if targets.is_empty() {
        return Ok(());
    }
    board.state.debug_mode = true;
    for id in targets {
        let r = apply(&mut board.state, PlayerId(0), GameAction::Debug(DebugAction::SetTapped { object_id: id, tapped: true }));
        if let Err(e) = r {
            board.state.debug_mode = false;
            return Err(harness(format!("tapped_after_untap failed: {e:?}")));
        }
    }
    board.state.debug_mode = false;
    rb.note("tapped_after_untap applied by a debug SetTapped at main-1 priority (phase has no untap-step hook)");
    Ok(())
}


#[cfg(test)]
mod tests {
    use super::*;
    use follower_proto::row::{PermSnap, Snapshot};
    use std::sync::OnceLock;

    fn db() -> &'static CardDatabase {
        static DB: OnceLock<CardDatabase> = OnceLock::new();
        DB.get_or_init(|| {
            let path = std::env::var("PHASE_CARD_DATA").expect("PHASE_CARD_DATA: run make phase-follower-test");
            crate::load_db(&path).expect("load card data")
        })
    }

    /// Run setup for a hand-written request; the board (or the stop) and the row builder.
    fn go(json: &str) -> (Result<Board, Stop>, RowBuilder) {
        let req = Request::parse(json).expect("request");
        let mut rb = RowBuilder::default();
        let out = run(db(), &req, &mut rb);
        (out, rb)
    }

    fn setup(json: &str) -> (Board, Snapshot, RowBuilder) {
        let (out, rb) = go(json);
        let board = out.unwrap_or_else(|e| panic!("setup stopped: {e:?}"));
        assert_eq!(rb.snapshots.len(), 1);
        let snap = rb.snapshots[0].clone();
        (board, snap, rb)
    }

    fn perms<'a>(s: &'a Snapshot, controller: i64, name: &str) -> Vec<&'a PermSnap> {
        s.permanents.iter().filter(|p| p.controller == controller && p.name == name).collect()
    }

    #[test]
    fn setup_places_without_etb() {
        // Elvish Visionary: "When this creature enters, draw a card."
        let (_, s, _) = setup(r#"{"id":"t","setup":{"p0":{"battlefield":["Elvish Visionary"]}}}"#);
        assert_eq!(s.checkpoint, "setup");
        assert_eq!((s.turn, s.step.as_str(), s.active, s.priority), (1, "main1", 0, 0));
        assert!(s.stack.is_empty(), "{:?}", s.stack);
        assert_eq!(perms(&s, 0, "Elvish Visionary").len(), 1);
        // No ETB draw and no turn-1 draw: the 39 filler cards are all still in the library.
        assert!(s.players[0].hand.is_empty());
        assert_eq!(s.players[0].library_count, 39);
        assert_eq!(s.players[1].library_count, 40);
        assert_eq!((s.players[0].life, s.players[1].life), (20, 20));
    }

    #[test]
    fn setup_entry_replacement_skipped() {
        // Tranquil Cove enters tapped; placed raw it is untapped (and gains no life).
        let (_, s, _) = setup(r#"{"id":"t","setup":{"p1":{"battlefield":["Tranquil Cove"]}}}"#);
        let cove = perms(&s, 1, "Tranquil Cove");
        assert_eq!(cove.len(), 1);
        assert!(!cove[0].tapped);
        assert_eq!(s.players[1].life, 20);
    }

    #[test]
    fn setup_library_order_and_filler() {
        let (_, s, _) = setup(
            r#"{"id":"t","setup":{"p0":{"hand":["Forest"],"library":["Island","Swamp"],
                "library_top":["Mountain","Plains","Llanowar Elves"]}}}"#,
        );
        // 6 named cards: filler 34; library = top 3 + filler 34 + named 2 = 39 (the hand card is not in it).
        assert_eq!(s.players[0].library_count, 39);
        assert_eq!(s.players[0].library_top, ["Mountain", "Plains", "Llanowar Elves", "Wastes", "Wastes"]);
        assert_eq!(s.players[0].hand, ["Forest"]);
        assert_eq!(s.players[1].library_count, 40);
        assert_eq!(s.players[1].library_top, ["Wastes"; 5]);
    }

    #[test]
    fn setup_library_bottom_is_the_named_library_cards() {
        let (b, _, _) = setup(r#"{"id":"t","setup":{"p0":{"library":["Island","Swamp"]}}}"#);
        let lib: Vec<&str> = b.state.players[0].library.iter().map(|id| b.names.by_object[id].as_str()).collect();
        assert_eq!(lib.len(), 40);
        assert_eq!(&lib[..38], &["Wastes"; 38]);
        assert_eq!(&lib[38..], &["Island", "Swamp"]);
    }

    #[test]
    fn setup_counters_added_to_entry() {
        // Kalonian Hydra enters with four +1/+1 counters; raw placement adds none, the listed ones
        // are added on top, on every placement of the name.
        let (_, s, _) = setup(
            r#"{"id":"t","setup":{"p0":{"battlefield":["Kalonian Hydra"],"counters":{"Kalonian Hydra":{"P1P1":2}}},
                "p1":{"battlefield":["Grizzly Bears","Grizzly Bears"],"counters":{"Grizzly Bears":{"P1P1":1,"LOYALTY":3}}}}}"#,
        );
        let hydra = perms(&s, 0, "Kalonian Hydra");
        assert_eq!(hydra[0].counters.get("P1P1"), Some(&2));
        let bears = perms(&s, 1, "Grizzly Bears");
        assert_eq!(bears.len(), 2);
        for b in bears {
            assert_eq!(b.counters.get("P1P1"), Some(&1));
            assert_eq!(b.counters.get("LOYALTY"), Some(&3));
            assert_eq!(b.pt.as_deref(), Some("3/3"));
        }
    }

    #[test]
    fn setup_tokens_from_neutral() {
        let json = r#"{"id":"t","item":{"id":"t","setup":{"p1":{"tokens":[{"script":"w_1_1_soldier","counters":{"P1P1":1}}]}}},
            "neutral":{"token":{"w_1_1_soldier":{"name":"Soldier","types":["Creature"],"subtypes":["Soldier"],
            "colours":"W","power":"1","toughness":"1","keywords":["Vigilance"]}}}}"#;
        let (b, s, _) = setup(json);
        let tok = perms(&s, 1, "Soldier");
        assert_eq!(tok.len(), 1, "{:?}", s.permanents);
        let t = tok[0];
        assert!(t.token && !t.tapped);
        assert_eq!(t.pt.as_deref(), Some("2/2"));
        assert_eq!(t.colors, "W");
        assert_eq!(t.counters.get("P1P1"), Some(&1));
        assert_eq!(t.keywords, ["Vigilance"]);
        assert_eq!(t.types, ["Creature", "Soldier"]);
        assert!(s.stack.is_empty());
        // A token ref resolves by name substring.
        assert!(b.resolve(&Ref::parse("p1:token:soldier").unwrap()).is_some());
        assert!(b.resolve(&Ref::parse("p0:token:soldier").unwrap()).is_none());
        // A stem missing from neutral.token is a harness row.
        let (out, _) = go(r#"{"id":"t","setup":{"p0":{"tokens":[{"script":"nope"}]}}}"#);
        assert_eq!(out.err(), Some(Stop::Harness("phase lacks card: token nope".to_string())));
    }

    #[test]
    fn setup_attached_aura() {
        let (_, s, _) = setup(
            r#"{"id":"t","setup":{"p0":{"battlefield":["Pacifism"],"attached":[{"card":"Pacifism","to":"p1:Grizzly Bears"}]},
                "p1":{"battlefield":["Grizzly Bears"]}}}"#,
        );
        let aura = perms(&s, 0, "Pacifism");
        assert_eq!(aura.len(), 1, "Pacifism left the battlefield: {:?}", s.permanents);
        assert_eq!(aura[0].attached_to.as_deref(), Some("Grizzly Bears"));
        assert!(s.stack.is_empty());
    }

    #[test]
    fn setup_tapped_after_untap() {
        let (_, s, rb) = setup(
            r#"{"id":"t","setup":{"p0":{"battlefield":["Forest","Grizzly Bears","Grizzly Bears"],
                "tapped_after_untap":["Forest","Grizzly Bears#2"]}}}"#,
        );
        assert!(perms(&s, 0, "Forest")[0].tapped);
        let bears = perms(&s, 0, "Grizzly Bears");
        assert_eq!(bears.iter().map(|b| b.tapped).collect::<Vec<_>>(), [false, true]);
        assert_eq!(rb.notes.len(), 1);
        assert!(rb.notes[0].starts_with("tapped_after_untap"));
        // A ref that names nothing is a harness row.
        let (out, _) = go(r#"{"id":"t","setup":{"p0":{"tapped_after_untap":["Forest"]}}}"#);
        assert!(matches!(out, Err(Stop::Harness(m)) if m.contains("tapped_after_untap names")));
    }

    #[test]
    fn setup_p0_permanents_untap_on_turn_1() {
        let (_, s, rb) = setup(
            r#"{"id":"t","setup":{"p0":{"battlefield":["Forest","Grizzly Bears"],"tapped":["Forest","Grizzly Bears"]},
                "p1":{"battlefield":["Forest"],"tapped":["Forest"]}}}"#,
        );
        assert!(!perms(&s, 0, "Forest")[0].tapped);
        assert!(!perms(&s, 0, "Grizzly Bears")[0].tapped);
        assert!(perms(&s, 1, "Forest")[0].tapped, "p1's tapped permanent stays tapped");
        assert!(rb.notes.is_empty());
        assert!(s.players[0].hand.is_empty());
    }

    #[test]
    fn setup_unsupported_shapes_are_harness_rows() {
        for (json, msg) in [
            (r#"{"id":"t","turn":3}"#, "phase follower: scenario turn 3 unsupported"),
            (r#"{"id":"t","players":4}"#, "phase follower: scenario players 4 unsupported"),
            (r#"{"id":"t","setup":{"p0":{"speed":1}}}"#, "phase follower: speed setup unsupported"),
            (r#"{"id":"t","setup":{"p1":{"command":["X"]}}}"#, "phase follower: command setup unsupported"),
            (r#"{"id":"t","setup":{"p1":{"sideboard":["X"]}}}"#, "phase follower: sideboard setup unsupported"),
            (r#"{"id":"t","setup":{"p1":{"mana":{"G":2}}}}"#, "phase follower: mana setup unsupported"),
            (r#"{"id":"t","setup":{"p1":{"hand":["No Such Card"]}}}"#, "phase lacks card: No Such Card"),
        ] {
            let (out, rb) = go(json);
            assert_eq!(out.err(), Some(Stop::Harness(msg.to_string())), "{json}");
            assert!(rb.snapshots.is_empty());
        }
    }

    #[test]
    fn setup_is_deterministic_and_life_has_no_history() {
        let json = r#"{"id":"same","setup":{"p0":{"life":7,"battlefield":["Forest"]},"p1":{"life":30}}}"#;
        let (a, sa, _) = setup(json);
        let (_, sb, _) = setup(json);
        assert_eq!(serde_json::to_string(&sa).unwrap(), serde_json::to_string(&sb).unwrap());
        assert_eq!((sa.players[0].life, sa.players[1].life), (7, 30));
        for p in &a.state.players {
            assert_eq!((p.life_gained_this_turn, p.life_lost_this_turn), (0, 0));
        }
    }
}
