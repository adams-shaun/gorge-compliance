//! The SpellBench v2 engine role over NDJSON: strict framing, exact request field sets, the
//! request-id cache, `hello` / `validate_deck` / `reset` / `step`, and the error codes, as
//! `docs/site/api/spellbench-v2.md` in gorge states them. The game itself sits behind [`Backend`], so
//! this layer is tested without the engine.

use std::collections::{BTreeMap, BTreeSet};
use std::io::{BufRead, Read, Write};

use serde_json::{json, Map, Value};
use sha2::{Digest, Sha256};
use unicode_normalization::is_nfc;

use crate::json::{canonical, deck_id, domain_id, parse_strict, MAX_SAFE};

pub const PROTOCOL: &str = "spellbench/v2";
/// The one format this engine plays. Decks are the engine catalog, as in gorge's env.
pub const FORMAT: &str = "pauper-bo1";
const MAX_LINE: usize = 8 * 1024 * 1024;

/// One catalog deck: `catalog_id`, display name, rows.
#[derive(Clone, Debug)]
pub struct CatalogDeck {
    pub catalog_id: String,
    pub name: String,
    pub rows: Vec<(String, u32)>,
}

impl CatalogDeck {
    pub fn deck_id(&self) -> String {
        deck_id(&self.rows)
    }
}

/// A decoded, shape-checked `reset`.
#[derive(Clone, Debug)]
pub struct Reset {
    pub game_id: String,
    /// `catalog_id` of each seat's deck, p0 then p1.
    pub decks: [String; 2],
    pub mulligan: String,
    pub starting_seat: u8,
    pub names: Vec<String>,
    pub opponent_decklist_visible: bool,
    pub game_secret: [u8; 32],
    pub max_decisions: u64,
    pub max_steps: u64,
}

/// What the game is waiting on after a reset or a step.
pub enum Outcome {
    /// A `seat_decision` object: `acting_seat`, `seat_step`, `group`, `context`, `observation`,
    /// `candidates`, `extensions`. The server adds the envelope.
    Decision(Value),
    /// The terminal fields (`outcome`, `classification`, `winner`, `reason`, `decision_count`, and any
    /// `x_` extra); the server adds `step_count`, `game_id` and the provenance.
    Terminal(Value),
}

/// The game host behind the protocol.
pub trait Backend {
    /// `hello_ok.engine`: `name`, `version`, `rules_snapshot_id`, `card_pool_identity`, `source_revision`.
    fn identity(&self) -> Value;
    /// Catalog decks the engine will play, in `catalog_id` order.
    fn catalog(&self) -> Vec<CatalogDeck>;
    /// Why `deck` is not playable, if it is not (names the cards).
    fn refuse_deck(&self, deck: &CatalogDeck) -> Option<String>;
    /// Start a game. `Err` is the `unsupported_deck` message ("engine could not start: ...").
    fn reset(&mut self, r: &Reset) -> Result<Outcome, String>;
    /// Answer the pending decision with `candidate_id` (already range- and echo-checked).
    fn choose(&mut self, candidate_id: u32) -> Outcome;
}

struct Pending {
    step: u64,
    candidates: Vec<Value>,
}

struct Game {
    id: String,
    step_count: u64,
    pending: Option<Pending>,
    done: bool,
}

pub struct Server<B: Backend> {
    pub backend: B,
    game: Option<Game>,
    game_ids: BTreeSet<String>,
    cache: BTreeMap<String, ([u8; 32], Vec<u8>)>,
}

type Fail = (&'static str, String);

fn fail<T>(code: &'static str, msg: impl Into<String>) -> Result<T, Fail> {
    Err((code, msg.into()))
}

impl<B: Backend> Server<B> {
    pub fn new(backend: B) -> Self {
        Server { backend, game: None, game_ids: BTreeSet::new(), cache: BTreeMap::new() }
    }

    /// Serve until `input` ends. A clean end is `Ok`; input that ends in the middle of a line is an error
    /// and gets no answer.
    pub fn serve(&mut self, input: &mut impl BufRead, out: &mut impl Write) -> std::io::Result<()> {
        loop {
            let mut line = Vec::new();
            let n = input.by_ref().take(MAX_LINE as u64 + 2).read_until(b'\n', &mut line)?;
            if n == 0 {
                return Ok(());
            }
            let resp = if line.last() == Some(&b'\n') {
                line.pop();
                if line.last() == Some(&b'\r') {
                    line.pop();
                }
                if line.len() > MAX_LINE {
                    error_line("", "malformed_json", "line exceeds 8 MiB")
                } else {
                    self.handle(&line)
                }
            } else if line.len() > MAX_LINE {
                // Read the rest of the long line through to its newline, then resume.
                let mut rest = Vec::new();
                loop {
                    rest.clear();
                    let m = input.by_ref().take(1 << 20).read_until(b'\n', &mut rest)?;
                    if m == 0 || rest.last() == Some(&b'\n') {
                        break;
                    }
                }
                error_line("", "malformed_json", "line exceeds 8 MiB")
            } else {
                return Err(std::io::Error::new(std::io::ErrorKind::UnexpectedEof, "input ended mid-line"));
            };
            out.write_all(&resp)?;
            out.write_all(b"\n")?;
            out.flush()?;
        }
    }

    /// Answer one request line.
    pub fn handle(&mut self, line: &[u8]) -> Vec<u8> {
        let value = match parse_strict(line) {
            Ok(v) => v,
            Err(m) => return error_line("", "malformed_json", &format!("malformed_json: {m}")),
        };
        let req = match decode(&value) {
            Ok(r) => r,
            Err((code, msg, id)) => return error_line(&id, code, &msg),
        };
        let id = req.id().to_string();
        let sum: [u8; 32] = Sha256::digest(line).into();
        if let Some((s, resp)) = self.cache.get(&id) {
            return if *s == sum {
                resp.clone()
            } else {
                error_line(&id, "request_id_reuse_mismatch", "request_id reused with a different payload")
            };
        }
        let before = self.game.as_ref().map(|g| g.id.clone());
        let out = match self.dispatch(&req) {
            Ok(v) => canonical(&v).into_bytes(),
            Err((code, msg)) => error_line(&id, code, &msg),
        };
        if self.game.as_ref().map(|g| g.id.clone()) != before {
            // An accepted reset clears the cache.
            self.cache.clear();
        }
        self.cache.insert(id, (sum, out.clone()));
        out
    }

    fn dispatch(&mut self, req: &Req) -> Result<Value, Fail> {
        match req {
            Req::Hello { id, .. } => Ok(self.hello(id)),
            Req::ValidateDeck { id, format, deck } => {
                if format != FORMAT {
                    return fail("unsupported_format", format.clone());
                }
                let d = match deck {
                    DeckRef::Catalog { catalog_id, .. } => self.catalog_deck(catalog_id),
                    DeckRef::Decklist(_) => None,
                };
                let Some(d) = d else { return fail("unsupported_deck", "only the engine catalog decks are playable") };
                if let Some(why) = self.backend.refuse_deck(&d) {
                    return fail("unsupported_deck", why);
                }
                Ok(json!({"response_type": "deck_ok", "protocol": PROTOCOL, "request_id": id}))
            }
            Req::Probe { .. } => fail("unsupported_request", "probe_resample is not implemented"),
            Req::Reset { id, r } => self.reset(id, r),
            Req::Step { id, game_id, expected_step, candidate_id, echo } => self.step(id, game_id, *expected_step, *candidate_id, echo),
        }
    }

    fn catalog_deck(&self, id: &str) -> Option<CatalogDeck> {
        self.backend.catalog().into_iter().find(|d| d.catalog_id == id)
    }

    fn hello(&self, id: &str) -> Value {
        let catalog: Vec<Value> = self
            .backend
            .catalog()
            .iter()
            .map(|d| {
                let rows: Vec<Value> = d.rows.iter().map(|(n, c)| json!({"name": n, "count": c})).collect();
                json!({"catalog_id": d.catalog_id, "name": d.name, "decklist": rows})
            })
            .collect();
        json!({
            "response_type": "hello_ok", "protocol": PROTOCOL, "request_id": id, "protocol_minor": 0,
            "engine": self.backend.identity(),
            "formats": [FORMAT], "deck_sources": ["catalog"], "catalog": catalog,
            "rules_supported": {"mulligan": ["london", "none"], "starting_player": ["host_assigned"]},
            "observation": crate::observe::flags(),
            "decision_kinds": crate::semantic::DECISION_KINDS,
            "engine_defaults": {"trigger_order": null, "replacement_order": null,
                "combat_damage_assignment": "engine_order", "mana_payment": null},
            "rewind": false, "fairness": {"noninterference_probe": false}, "extensions": [],
        })
    }

    fn provenance(&self) -> Value {
        let e = self.backend.identity();
        json!({
            "engine_name": e["name"], "engine_version": e["version"],
            "rules_snapshot_id": e["rules_snapshot_id"], "card_pool_identity": e["card_pool_identity"],
        })
    }

    fn reset(&mut self, id: &str, r: &ResetReq) -> Result<Value, Fail> {
        if self.game.as_ref().is_some_and(|g| !g.done) {
            return fail("game_already_active", "a game is active");
        }
        if self.game_ids.contains(&r.game_id) {
            return fail("malformed_request", "game_id reused");
        }
        if r.format != FORMAT {
            return fail("unsupported_format", r.format.clone());
        }
        let mut decks = [String::new(), String::new()];
        for (i, spec) in r.decks.iter().enumerate() {
            let DeckRef::Catalog { catalog_id, deck_id: did } = spec else {
                return fail("unsupported_deck", "only the engine catalog decks are playable");
            };
            let Some(d) = self.catalog_deck(catalog_id) else {
                return fail("unsupported_deck", "only the engine catalog decks are playable");
            };
            if d.deck_id() != *did {
                return fail("deck_id_mismatch", format!("deck_id does not match {catalog_id}"));
            }
            if let Some(why) = self.backend.refuse_deck(&d) {
                return fail("unsupported_deck", why);
            }
            decks[i] = catalog_id.clone();
        }
        if r.starting_player != "host_assigned" {
            return fail("unsupported_rule", "starting_player");
        }
        if r.probe {
            return fail("unsupported_rule", "probe");
        }
        if domain_id(&r.names) != r.domain_id {
            return fail("malformed_request", "card_name_domain.domain_id does not hash its distinct names");
        }
        if let Some(x) = r.extensions.first() {
            return fail("unsupported_rule", format!("extension {x}"));
        }
        let secret = parse_secret(&r.game_secret)?;
        let reset = Reset {
            game_id: r.game_id.clone(),
            decks,
            mulligan: r.mulligan.clone(),
            starting_seat: r.starting_seat.unwrap_or(0),
            names: r.names.clone(),
            opponent_decklist_visible: r.opponent_decklist == "visible",
            game_secret: secret,
            max_decisions: r.max_decisions,
            max_steps: r.max_steps,
        };
        let outcome = self.backend.reset(&reset).map_err(|m| ("unsupported_deck", format!("engine could not start: {m}")))?;
        self.game_ids.insert(r.game_id.clone());
        self.game = Some(Game { id: r.game_id.clone(), step_count: 0, pending: None, done: false });
        Ok(self.respond(id, outcome))
    }

    fn step(&mut self, id: &str, game_id: &str, expected: u64, cand: u32, echo: &Value) -> Result<Value, Fail> {
        let Some(game) = self.game.as_ref() else { return fail("step_before_reset", "no game") };
        if game.id != game_id {
            return fail("game_id_mismatch", "another game is active");
        }
        if game.done {
            return fail("game_already_terminal", "the game is over");
        }
        let pending = game.pending.as_ref().expect("an unfinished game has a pending decision");
        if pending.step != expected {
            return fail("expected_step_mismatch", format!("pending step is {}", pending.step));
        }
        let Some(c) = pending.candidates.get(cand as usize) else {
            return fail("candidate_id_out_of_range", format!("candidate {cand}"));
        };
        if c["semantic"] != *echo {
            return fail("semantic_echo_mismatch", "semantic_echo differs from the candidate's semantic");
        }
        let outcome = self.backend.choose(cand);
        if let Some(g) = self.game.as_mut() {
            g.step_count += 1;
        }
        Ok(self.respond(id, outcome))
    }

    /// Wrap a backend outcome in its envelope and record what is pending.
    fn respond(&mut self, id: &str, outcome: Outcome) -> Value {
        let prov = self.provenance();
        let game = self.game.as_mut().expect("a game");
        match outcome {
            Outcome::Decision(sd) => {
                let candidates = sd["candidates"].as_array().cloned().unwrap_or_default();
                game.pending = Some(Pending { step: game.step_count, candidates });
                json!({"response_type": "decision", "protocol": PROTOCOL, "request_id": id,
                    "game_id": game.id, "step": game.step_count, "seat_decision": sd, "provenance": prov})
            }
            Outcome::Terminal(t) => {
                game.pending = None;
                game.done = true;
                let mut m: Map<String, Value> = t.as_object().cloned().unwrap_or_default();
                m.insert("response_type".into(), "terminal".into());
                m.insert("protocol".into(), PROTOCOL.into());
                m.insert("request_id".into(), id.into());
                m.insert("game_id".into(), game.id.clone().into());
                m.insert("step_count".into(), game.step_count.into());
                m.insert("provenance".into(), prov);
                Value::Object(m)
            }
        }
    }
}

fn parse_secret(s: &str) -> Result<[u8; 32], Fail> {
    if s.len() != 64 || !s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')) {
        return fail("malformed_request", "game_secret is not 64 lowercase hex characters");
    }
    let mut out = [0u8; 32];
    for (i, o) in out.iter_mut().enumerate() {
        *o = u8::from_str_radix(&s[2 * i..2 * i + 2], 16).expect("hex checked");
    }
    Ok(out)
}

/// One error line (canonical JSON).
pub fn error_line(id: &str, code: &str, msg: &str) -> Vec<u8> {
    canonical(&json!({"response_type": "error", "protocol": PROTOCOL, "request_id": id,
        "error": {"code": code, "message": msg}}))
    .into_bytes()
}

// ---- request decoding -------------------------------------------------------------------------

enum DeckRef {
    Catalog { catalog_id: String, deck_id: String },
    Decklist(#[allow(dead_code)] Vec<(String, u32)>),
}

struct ResetReq {
    game_id: String,
    format: String,
    decks: [DeckRef; 2],
    opponent_decklist: String,
    mulligan: String,
    starting_player: String,
    starting_seat: Option<u8>,
    domain_id: String,
    names: Vec<String>,
    extensions: Vec<String>,
    probe: bool,
    game_secret: String,
    max_decisions: u64,
    max_steps: u64,
}

enum Req {
    Hello {
        id: String,
    },
    ValidateDeck {
        id: String,
        format: String,
        deck: DeckRef,
    },
    Probe {
        #[allow(dead_code)]
        id: String,
    },
    Reset {
        id: String,
        r: Box<ResetReq>,
    },
    Step {
        id: String,
        game_id: String,
        expected_step: u64,
        candidate_id: u32,
        echo: Value,
    },
}

impl Req {
    fn id(&self) -> &str {
        match self {
            Req::Hello { id } | Req::ValidateDeck { id, .. } | Req::Probe { id } | Req::Reset { id, .. } | Req::Step { id, .. } => id,
        }
    }
}

type DecodeErr = (&'static str, String, String);

fn bad(id: &str, msg: impl Into<String>) -> DecodeErr {
    ("malformed_request", msg.into(), id.to_string())
}

/// The object must carry exactly `names`.
fn exact<'a>(o: &'a Map<String, Value>, names: &[&str], id: &str, what: &str) -> Result<&'a Map<String, Value>, DecodeErr> {
    for n in names {
        if !o.contains_key(*n) {
            return Err(bad(id, format!("{what}: missing field {n}")));
        }
    }
    if let Some(extra) = o.keys().find(|k| !names.contains(&k.as_str())) {
        return Err(bad(id, format!("{what}: unexpected field {extra}")));
    }
    Ok(o)
}

fn string(o: &Map<String, Value>, k: &str, id: &str) -> Result<String, DecodeErr> {
    o[k].as_str().map(str::to_string).ok_or_else(|| bad(id, format!("{k} is not a string")))
}

fn nonempty(o: &Map<String, Value>, k: &str, id: &str) -> Result<String, DecodeErr> {
    let s = string(o, k, id)?;
    if s.is_empty() {
        return Err(bad(id, format!("{k} is empty")));
    }
    Ok(s)
}

fn uint(o: &Map<String, Value>, k: &str, max: u64, id: &str) -> Result<u64, DecodeErr> {
    match o[k].as_u64() {
        Some(n) if n <= max => Ok(n),
        _ => Err(bad(id, format!("{k} is not a u{}", if max == u64::from(u32::MAX) { "32" } else { "64" }))),
    }
}

fn boolean(o: &Map<String, Value>, k: &str, id: &str) -> Result<bool, DecodeErr> {
    o[k].as_bool().ok_or_else(|| bad(id, format!("{k} is not a boolean")))
}

fn object<'a>(v: &'a Value, what: &str, id: &str) -> Result<&'a Map<String, Value>, DecodeErr> {
    v.as_object().ok_or_else(|| bad(id, format!("{what} is not an object")))
}

fn array<'a>(v: &'a Value, what: &str, id: &str) -> Result<&'a Vec<Value>, DecodeErr> {
    v.as_array().ok_or_else(|| bad(id, format!("{what} is not an array")))
}

fn name_ok(s: &str, id: &str) -> Result<(), DecodeErr> {
    if is_nfc(s) {
        Ok(())
    } else {
        Err(bad(id, format!("card name {s:?} is not NFC")))
    }
}

fn rows(v: &Value, id: &str) -> Result<Vec<(String, u32)>, DecodeErr> {
    let mut out = Vec::new();
    for r in array(v, "decklist", id)? {
        let o = exact(object(r, "decklist row", id)?, &["name", "count"], id, "decklist row")?;
        let name = nonempty(o, "name", id)?;
        name_ok(&name, id)?;
        out.push((name, uint(o, "count", u64::from(u32::MAX), id)? as u32));
    }
    Ok(out)
}

fn deck_ref(v: &Value, with_id: bool, id: &str) -> Result<DeckRef, DecodeErr> {
    let o = object(v, "deck", id)?;
    if o.contains_key("catalog_id") {
        let names: &[&str] = if with_id { &["deck_id", "catalog_id"] } else { &["catalog_id"] };
        let o = exact(o, names, id, "deck")?;
        let did = if with_id { string(o, "deck_id", id)? } else { String::new() };
        return Ok(DeckRef::Catalog { catalog_id: nonempty(o, "catalog_id", id)?, deck_id: did });
    }
    let names: &[&str] = if with_id { &["deck_id", "decklist"] } else { &["decklist"] };
    let o = exact(o, names, id, "deck")?;
    Ok(DeckRef::Decklist(rows(&o["decklist"], id)?))
}

fn decode(v: &Value) -> Result<Req, DecodeErr> {
    let Some(o) = v.as_object() else { return Err(bad("", "request is not an object")) };
    let id = o.get("request_id").and_then(Value::as_str).unwrap_or("").to_string();
    let Some(rt) = o.get("request_type").and_then(Value::as_str) else {
        return Err(bad(&id, "request_type is missing or not a string"));
    };
    match o.get("protocol") {
        Some(Value::String(p)) if p == PROTOCOL => {}
        Some(Value::String(p)) => return Err(("protocol_mismatch", format!("protocol {p:?}"), id)),
        _ => return Err(bad(&id, "protocol is missing or not a string")),
    }
    if id.is_empty() {
        return Err(bad("", "request_id is missing or empty"));
    }
    let common = ["request_type", "protocol", "request_id"];
    let with = |extra: &[&'static str]| -> Vec<&str> { common.iter().chain(extra.iter()).copied().collect() };
    match rt {
        "hello" => {
            let o = exact(o, &with(&["protocol_minor"]), &id, "hello")?;
            uint(o, "protocol_minor", u64::from(u32::MAX), &id)?;
            Ok(Req::Hello { id })
        }
        "validate_deck" => {
            let o = exact(o, &with(&["format", "deck"]), &id, "validate_deck")?;
            Ok(Req::ValidateDeck { format: string(o, "format", &id)?, deck: deck_ref(&o["deck"], false, &id)?, id })
        }
        "probe_resample" => {
            let o = exact(o, &with(&["game_id", "samples"]), &id, "probe_resample")?;
            nonempty(o, "game_id", &id)?;
            uint(o, "samples", u64::from(u32::MAX), &id)?;
            Ok(Req::Probe { id })
        }
        "step" => {
            let o = exact(o, &with(&["game_id", "expected_step", "selection"]), &id, "step")?;
            let sel = exact(object(&o["selection"], "selection", &id)?, &["candidate_id", "semantic_echo"], &id, "selection")?;
            let echo = &sel["semantic_echo"];
            object(echo, "semantic_echo", &id)?;
            Ok(Req::Step {
                game_id: nonempty(o, "game_id", &id)?,
                expected_step: uint(o, "expected_step", MAX_SAFE as u64, &id)?,
                candidate_id: uint(sel, "candidate_id", u64::from(u32::MAX), &id)? as u32,
                echo: echo.clone(),
                id,
            })
        }
        "reset" => {
            let o = exact(o, &with(&["game_id", "format", "seats", "rules", "game_secret", "max_decisions", "max_steps"]), &id, "reset")?;
            let seats = array(&o["seats"], "seats", &id)?;
            if seats.len() != 2 {
                return Err(bad(&id, "seats is not exactly two"));
            }
            let mut decks = Vec::new();
            for (i, s) in seats.iter().enumerate() {
                let s = exact(object(s, "seat", &id)?, &["seat", "deck"], &id, "seat")?;
                if s["seat"].as_str() != Some(["p0", "p1"][i]) {
                    return Err(bad(&id, "seats are not p0 then p1"));
                }
                decks.push(deck_ref(&s["deck"], true, &id)?);
            }
            let rules = exact(
                object(&o["rules"], "rules", &id)?,
                &["opponent_decklist", "mulligan", "starting_player", "starting_seat", "card_name_domain", "extensions", "probe"],
                &id,
                "rules",
            )?;
            let opp = string(rules, "opponent_decklist", &id)?;
            let mulligan = string(rules, "mulligan", &id)?;
            let sp = string(rules, "starting_player", &id)?;
            if !matches!(opp.as_str(), "visible" | "hidden") || !matches!(mulligan.as_str(), "london" | "none") {
                return Err(bad(&id, "opponent_decklist or mulligan is not a known word"));
            }
            if !matches!(sp.as_str(), "host_assigned" | "toss_winner_chooses") {
                return Err(bad(&id, "starting_player is not a known word"));
            }
            let seat = match &rules["starting_seat"] {
                Value::Null => None,
                Value::String(s) if s == "p0" => Some(0),
                Value::String(s) if s == "p1" => Some(1),
                _ => return Err(bad(&id, "starting_seat is not p0, p1 or null")),
            };
            if seat.is_some() != (sp == "host_assigned") {
                return Err(bad(&id, "starting_seat is a seat exactly when starting_player is host_assigned"));
            }
            let dom =
                exact(object(&rules["card_name_domain"], "card_name_domain", &id)?, &["domain_id", "names"], &id, "card_name_domain")?;
            let mut names = Vec::new();
            for n in array(&dom["names"], "names", &id)? {
                let n = n.as_str().ok_or_else(|| bad(&id, "a name is not a string"))?.to_string();
                name_ok(&n, &id)?;
                if names.contains(&n) {
                    return Err(bad(&id, format!("repeated name {n:?}")));
                }
                names.push(n);
            }
            if names.is_empty() {
                return Err(bad(&id, "card_name_domain.names is empty"));
            }
            let mut extensions = Vec::new();
            for x in array(&rules["extensions"], "extensions", &id)? {
                extensions.push(x.as_str().ok_or_else(|| bad(&id, "an extension is not a string"))?.to_string());
            }
            let d1 = decks.pop().expect("two");
            let d0 = decks.pop().expect("two");
            Ok(Req::Reset {
                r: Box::new(ResetReq {
                    game_id: nonempty(o, "game_id", &id)?,
                    format: string(o, "format", &id)?,
                    decks: [d0, d1],
                    opponent_decklist: opp,
                    mulligan,
                    starting_player: sp,
                    starting_seat: seat,
                    domain_id: string(dom, "domain_id", &id)?,
                    names,
                    extensions,
                    probe: boolean(rules, "probe", &id)?,
                    game_secret: string(o, "game_secret", &id)?,
                    max_decisions: uint(o, "max_decisions", MAX_SAFE as u64, &id)?,
                    max_steps: uint(o, "max_steps", MAX_SAFE as u64, &id)?,
                }),
                id,
            })
        }
        other => Err(bad(&id, format!("unknown request_type {other:?}"))),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A backend with a two-decision game: pass, then the game ends.
    struct Stub {
        asked: u32,
    }

    impl Backend for Stub {
        fn identity(&self) -> Value {
            json!({"name": "stub", "version": "0", "rules_snapshot_id": "r", "card_pool_identity": "c", "source_revision": null})
        }
        fn catalog(&self) -> Vec<CatalogDeck> {
            vec![CatalogDeck { catalog_id: "d".into(), name: "d".into(), rows: vec![("Mountain".into(), 40)] }]
        }
        fn refuse_deck(&self, _: &CatalogDeck) -> Option<String> {
            None
        }
        fn reset(&mut self, _: &Reset) -> Result<Outcome, String> {
            self.asked = 0;
            Ok(self.ask())
        }
        fn choose(&mut self, _: u32) -> Outcome {
            self.asked += 1;
            if self.asked >= 2 {
                Outcome::Terminal(json!({"outcome": "draw", "classification": "natural", "winner": null,
                    "reason": "draw", "decision_count": 2}))
            } else {
                self.ask()
            }
        }
    }

    impl Stub {
        fn ask(&self) -> Outcome {
            Outcome::Decision(json!({"acting_seat": "p0", "seat_step": self.asked, "candidates": [
                {"candidate_id": 0, "display_text": null, "semantic": {"kind": "pass"}}]}))
        }
    }

    fn reset_line(id: &str, game: &str) -> String {
        let deck = deck_id(&[("Mountain".to_string(), 40)]);
        let dom = domain_id(&["Mountain".to_string()]);
        format!(
            r#"{{"request_type":"reset","protocol":"spellbench/v2","request_id":"{id}","game_id":"{game}","format":"pauper-bo1","seats":[{{"seat":"p0","deck":{{"deck_id":"{deck}","catalog_id":"d"}}}},{{"seat":"p1","deck":{{"deck_id":"{deck}","catalog_id":"d"}}}}],"rules":{{"opponent_decklist":"visible","mulligan":"london","starting_player":"host_assigned","starting_seat":"p0","card_name_domain":{{"domain_id":"{dom}","names":["Mountain"]}},"extensions":[],"probe":false}},"game_secret":"{}","max_decisions":10,"max_steps":100}}"#,
            "ab".repeat(32)
        )
    }

    fn code(resp: &[u8]) -> String {
        let v: Value = serde_json::from_slice(resp).unwrap();
        if v["response_type"] == "error" {
            v["error"]["code"].as_str().unwrap().to_string()
        } else {
            v["response_type"].as_str().unwrap().to_string()
        }
    }

    #[test]
    fn hello_names_the_profile() {
        let mut s = Server::new(Stub { asked: 0 });
        let r = s.handle(br#"{"request_type":"hello","protocol":"spellbench/v2","request_id":"h","protocol_minor":0}"#);
        let v: Value = serde_json::from_slice(&r).unwrap();
        assert_eq!(v["response_type"], "hello_ok");
        // The manual surface: no engine_autopay declaration (phase_v2's own default omits the value).
        assert!(v["engine_defaults"]["mana_payment"].is_null());
        assert_eq!(v["catalog"][0]["catalog_id"], "d");
        assert_eq!(v["decision_kinds"].as_array().unwrap().len(), crate::semantic::DECISION_KINDS.len());
    }

    #[test]
    fn a_game_plays_through_with_the_step_checks_in_order() {
        let mut s = Server::new(Stub { asked: 0 });
        assert_eq!(code(&s.handle(br#"{"request_type":"step","protocol":"spellbench/v2","request_id":"s0","game_id":"g","expected_step":0,"selection":{"candidate_id":0,"semantic_echo":{"kind":"pass"}}}"#)), "step_before_reset");
        let first = s.handle(reset_line("r1", "g").as_bytes());
        assert_eq!(code(&first), "decision");
        // A retransmission of the same bytes is the cached answer; a different line with the id is refused.
        assert_eq!(s.handle(reset_line("r1", "g").as_bytes()), first);
        assert_eq!(code(&s.handle(reset_line("r1", "g2").as_bytes())), "request_id_reuse_mismatch");
        assert_eq!(code(&s.handle(reset_line("r2", "g2").as_bytes())), "game_already_active");
        let step = |id: &str, game: &str, exp: u64, cand: u32, kind: &str| {
            format!(
                r#"{{"request_type":"step","protocol":"spellbench/v2","request_id":"{id}","game_id":"{game}","expected_step":{exp},"selection":{{"candidate_id":{cand},"semantic_echo":{{"kind":"{kind}"}}}}}}"#
            )
        };
        assert_eq!(code(&s.handle(step("a", "x", 0, 0, "pass").as_bytes())), "game_id_mismatch");
        assert_eq!(code(&s.handle(step("b", "g", 7, 0, "pass").as_bytes())), "expected_step_mismatch");
        assert_eq!(code(&s.handle(step("c", "g", 0, 5, "pass").as_bytes())), "candidate_id_out_of_range");
        assert_eq!(code(&s.handle(step("d", "g", 0, 0, "other").as_bytes())), "semantic_echo_mismatch");
        assert_eq!(code(&s.handle(step("e", "g", 0, 0, "pass").as_bytes())), "decision");
        let end = s.handle(step("f", "g", 1, 0, "pass").as_bytes());
        let v: Value = serde_json::from_slice(&end).unwrap();
        assert_eq!(v["response_type"], "terminal");
        assert_eq!(v["step_count"], 2);
        assert_eq!(code(&s.handle(step("g", "g", 2, 0, "pass").as_bytes())), "game_already_terminal");
        // The finished game id cannot be used again; a new id starts the next game.
        assert_eq!(code(&s.handle(reset_line("r3", "g").as_bytes())), "malformed_request");
        assert_eq!(code(&s.handle(reset_line("r4", "g3").as_bytes())), "decision");
    }

    #[test]
    fn malformed_lines_are_refused_with_the_right_codes() {
        let mut s = Server::new(Stub { asked: 0 });
        assert_eq!(code(&s.handle(b"not json")), "malformed_json");
        assert_eq!(code(&s.handle(b"[1]")), "malformed_request");
        assert_eq!(
            code(&s.handle(br#"{"request_type":"hello","protocol":"spellbench/v1","request_id":"m","protocol_minor":0}"#)),
            "protocol_mismatch"
        );
        assert_eq!(
            code(&s.handle(br#"{"request_type":"hello","protocol":"spellbench/v2","request_id":"m","protocol_minor":0,"x":1}"#)),
            "malformed_request"
        );
        assert_eq!(
            code(&s.handle(br#"{"request_type":"hello","protocol":"spellbench/v2","request_id":"m","protocol_minor":4294967296}"#)),
            "malformed_request"
        );
        assert_eq!(code(&s.handle(br#"{"request_type":"nope","protocol":"spellbench/v2","request_id":"m"}"#)), "malformed_request");
        assert_eq!(
            code(&s.handle(br#"{"request_type":"probe_resample","protocol":"spellbench/v2","request_id":"p","game_id":"g","samples":4}"#)),
            "unsupported_request"
        );
        assert_eq!(code(&s.handle(br#"{"request_type":"validate_deck","protocol":"spellbench/v2","request_id":"v","format":"modern","deck":{"catalog_id":"d"}}"#)), "unsupported_format");
        assert_eq!(code(&s.handle(br#"{"request_type":"validate_deck","protocol":"spellbench/v2","request_id":"v2","format":"pauper-bo1","deck":{"catalog_id":"d"}}"#)), "deck_ok");
        assert_eq!(code(&s.handle(br#"{"request_type":"validate_deck","protocol":"spellbench/v2","request_id":"v3","format":"pauper-bo1","deck":{"decklist":[{"name":"Mountain","count":4}]}}"#)), "unsupported_deck");
    }

    #[test]
    fn serve_reads_lines_and_ends_clean() {
        let mut s = Server::new(Stub { asked: 0 });
        let input = b"{\"request_type\":\"hello\",\"protocol\":\"spellbench/v2\",\"request_id\":\"h\",\"protocol_minor\":0}\r\nnot json\n";
        let mut out = Vec::new();
        s.serve(&mut &input[..], &mut out).unwrap();
        let lines: Vec<&[u8]> = out.split(|&b| b == b'\n').filter(|l| !l.is_empty()).collect();
        assert_eq!(lines.len(), 2);
        assert_eq!(code(lines[0]), "hello_ok");
        assert_eq!(code(lines[1]), "malformed_json");
        // Input ending mid-line is an error.
        let mut s = Server::new(Stub { asked: 0 });
        assert!(s.serve(&mut &b"{\"a\":"[..], &mut Vec::new()).is_err());
    }
}
