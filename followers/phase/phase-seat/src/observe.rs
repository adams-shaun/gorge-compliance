//! The observation flags this engine declares in `hello_ok.observation`, and the thirteen-key
//! schema they gate. A field behind a false flag is `null` in every observation.

use serde_json::{json, Value};

/// The thirteen `ObservationFlags` (all required by the schema). What the projection in `view`
/// fills is true; the rest are false and read `null`.
pub const FLAGS: &[(&str, bool)] = &[
    ("day_night", false),
    ("designations", false),
    ("exiled_by", false),
    ("full_name", false),
    ("keywords", true),
    ("known_cards", false),
    ("passed_seats", false),
    ("pending_triggers", false),
    ("permanent_details", true),
    ("player_counters", true),
    ("player_progress", false),
    ("poison", true),
    ("stack_text", false),
];

pub fn flags() -> Value {
    let mut m = serde_json::Map::new();
    for (k, v) in FLAGS {
        m.insert((*k).to_string(), json!(v));
    }
    Value::Object(m)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn all_thirteen_flags_are_declared_once() {
        let f = flags();
        assert_eq!(f.as_object().unwrap().len(), 13);
        assert_eq!(f["keywords"], true);
        assert_eq!(f["stack_text"], false);
    }
}
