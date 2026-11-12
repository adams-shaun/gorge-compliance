//! Which cards phase can replay faithfully.
//!
//! A card is supported when every face (back faces included) has no coverage gaps and no confirmed
//! misparse, and none of its abilities draws a random outcome from phase's own RNG, which a
//! scripted replay cannot force.

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

fn face_reason(face: &CardFace) -> Option<String> {
    if let Some(class) = face.metadata.confirmed_misparse {
        let kind = serde_json::to_value(class).ok().and_then(|v| v.as_str().map(str::to_string));
        return Some(format!("confirmed misparse: {}", kind.unwrap_or_else(|| format!("{class:?}"))));
    }
    let gaps = card_face_gaps(face);
    if !gaps.is_empty() {
        return Some(format!("gaps: {}", gaps.join(", ")));
    }
    let json = serde_json::to_value(face).ok()?;
    random_source(&json, "").map(|s| format!("random outcome: {s}"))
}

/// `None` when phase supports `corpus_name`; otherwise the preflight reason.
pub fn lacks(db: &CardDatabase, corpus_name: &str) -> Option<String> {
    match phase_faces(db, corpus_name) {
        Err(reason) => Some(reason),
        Ok(faces) => faces.iter().find_map(|f| face_reason(f)),
    }
}
