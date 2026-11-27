//! Which cards phase can replay faithfully.
//!
//! A card is supported when every face (back faces included) has no parse warnings, no coverage
//! gaps and no confirmed misparse, and none of its abilities draws a random outcome from phase's
//! own RNG, which a scripted replay cannot force. Phase's own coverage result folds parse warnings
//! into its gap set; `card_face_gaps` is the lightweight entry point that does not, so the warnings
//! are checked here too.

use engine::database::card_db::CardDatabase;
use engine::game::coverage::card_face_gaps;
use engine::types::card::CardFace;
use serde_json::Value;

use crate::names::phase_faces;

/// Effect types whose resolution draws from phase's RNG with no scriptable alternative. Read from
/// phase's `crates/engine/src/game/effects/flip_coin.rs` (`Effect::FlipCoins`),
/// `roll_die.rs` (`Effect::RollDie`) and `physical_contest.rs` (`Effect::PhysicalContest`, a
/// real-world dexterity contest no engine can script), in the cargo git checkout at the pinned rev.
/// Random discard is not an effect type of its own: `discard.rs` calls `discard_at_random` when
/// `Effect::Discard`'s `selection` is `Random`, serialised as `"random": true` (see
/// `RANDOM_FLAG`).
pub const RANDOM_EFFECTS: &[&str] = &["FlipCoins", "RollDie", "PhysicalContest"];

/// The card-data key under which a random card selection (random discard, random choice from a
/// zone) is serialised: `CardSelectionMode::Random` is written as a bare `random: true`.
pub const RANDOM_FLAG: &str = "random";

/// First random-outcome source in `v`, as `"<effect>"` or `"<effect> at random"`. `parent` is the
/// nearest enclosing effect type.
fn random_source(v: &Value, parent: &str) -> Option<String> {
    match v {
        Value::Object(map) => {
            let ty = map.get("type").and_then(Value::as_str).unwrap_or(parent);
            if map.get("type").and_then(Value::as_str).is_some_and(|t| RANDOM_EFFECTS.contains(&t)) {
                return Some(ty.to_string());
            }
            if map.get(RANDOM_FLAG) == Some(&Value::Bool(true)) {
                return Some(format!("{ty} at random"));
            }
            map.values().find_map(|c| random_source(c, ty))
        }
        Value::Array(items) => items.iter().find_map(|c| random_source(c, parent)),
        _ => None,
    }
}

/// The banding rule phase lacks: CR 702.22c makes the active player choose which creatures attack
/// in a band, and phase has no prompt for it. The keyword reaches a face as a printed keyword, as a
/// static grant (`AddKeyword`), or through an activated ability's static payload, so the check
/// looks for the keyword value anywhere in the face rather than at a hand-written card list. The
/// next card that grants banding is covered without an edit here.
fn banding_source(v: &Value) -> Option<&'static str> {
    match v {
        // Exact keyword values only: oracle text that merely mentions banding is a sentence, not
        // this value.
        Value::String(s) if s == "Banding" => Some("banding"),
        Value::Object(map) => {
            if map.contains_key("BandsWithOther") {
                return Some("banding with other creatures");
            }
            map.values().find_map(banding_source)
        }
        Value::Array(items) => items.iter().find_map(banding_source),
        _ => None,
    }
}

fn face_reason(face: &CardFace) -> Option<String> {
    // A face that cannot serialise still gets the gap and misparse checks below; only the
    // keyword and random searches need the JSON.
    let json = serde_json::to_value(face).unwrap_or(Value::Null);
    // A missing combat rule is a hard refusal: phase cannot present the prompt at all.
    if let Some(kind) = banding_source(&json) {
        return Some(format!("phase lacks rule: {kind} cannot be chosen (CR 702.22c)"));
    }
    if let Some(class) = face.metadata.confirmed_misparse {
        let kind = serde_json::to_value(class).ok().and_then(|v| v.as_str().map(str::to_string));
        return Some(format!("confirmed misparse: {}", kind.unwrap_or_else(|| format!("{class:?}"))));
    }
    // A parse warning means phase dropped or misread printed text. The card data can still look
    // complete (The Bear Force Pilot's token loses the swallowed "and crew 2" inside an effect),
    // so `card_face_gaps` does not see it; a replay that reaches the dropped ability would die in
    // the engine instead. Fail closed here, naming the warning.
    if let Some(warning) = face.parse_warnings.first() {
        let kind = serde_json::to_value(warning)
            .ok()
            .and_then(|v| v.get("type").and_then(Value::as_str).map(str::to_string));
        return Some(format!("parse warning: {}", kind.unwrap_or_else(|| format!("{warning:?}"))));
    }
    let gaps = card_face_gaps(face);
    if !gaps.is_empty() {
        return Some(format!("gaps: {}", gaps.join(", ")));
    }
    random_source(&json, "").map(|s| format!("random outcome: {s}"))
}

/// `None` when phase supports `corpus_name`; otherwise the preflight reason.
pub fn lacks(db: &CardDatabase, corpus_name: &str) -> Option<String> {
    match phase_faces(db, corpus_name) {
        Err(reason) => Some(reason),
        Ok(faces) => faces.iter().find_map(|f| face_reason(f)),
    }
}
