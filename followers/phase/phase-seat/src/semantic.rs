//! SpellBench v2 candidate `semantic` objects: `{"kind": K, ...}` with exactly the fields the spec
//! lists for `K`, all required, no others (`docs/site/api/spellbench-v2.md`, "Kinds").
//!
//! [`KINDS`] is the kind table; every builder here goes through [`make`], which refuses a field set
//! that is not the kind's, so a drifted builder fails its test instead of the host's validator.

use serde_json::{json, Map, Value};

/// Every kind the engine role may advertise, with its exact field set (spec order).
pub const KINDS: &[(&str, &[&str])] = &[
    ("pass", &[]),
    ("play_land", &["source", "face"]),
    ("cast_spell", &["source", "method"]),
    ("activate_mana_ability", &["source", "ability_index", "mana_choice", "cost_target"]),
    ("activate_ability", &["source", "ability_index"]),
    ("special_action", &["source", "action"]),
    ("choose_target", &["source", "slot", "target", "selected_count", "minimum", "maximum"]),
    ("finish_target_selection", &["source", "slot", "selected_count"]),
    ("choose_cost_target", &["source", "cost_kind", "candidate", "selected_count", "minimum", "maximum"]),
    ("choose_spell_mode", &["source", "mode_index", "mode_count", "selected_count", "minimum", "maximum"]),
    ("choose_color", &["source", "purpose", "color"]),
    ("choose_number", &["source", "purpose", "value", "minimum", "maximum"]),
    ("choose_boolean", &["source", "purpose", "value"]),
    ("choose_name", &["source", "purpose", "value"]),
    ("select_object", &["source", "purpose", "choice", "selected_count", "minimum", "maximum"]),
    ("finish_selection", &["source", "purpose", "selected_count"]),
    ("optional_cost", &["source", "cost", "pay"]),
    ("optional_cast", &["card", "method", "cast_it"]),
    ("mulligan", &["hand_size", "mulligans_taken", "keep"]),
    ("order_pick", &["source", "purpose", "item", "position", "count"]),
    ("arrange_card", &["source", "purpose", "card", "card_index", "card_count", "destination"]),
    ("choose_replacement", &["affected", "event", "replacement_source", "replacement_index", "replacement_count"]),
    ("declare_attack", &["attacker", "defender"]),
    ("declare_block", &["blocker", "attacker"]),
    ("choose_option", &["source", "purpose", "option_index", "option_count", "option_label"]),
];

/// The kinds this engine poses today and advertises in `hello_ok.decision_kinds`. Every other
/// [`KINDS`] row is valid protocol the engine never asks; a `WaitingFor` that would need one is an
/// `engine_error` (see `waiting`), never a silent fallback.
pub const DECISION_KINDS: &[&str] = &[
    "pass",
    "play_land",
    "cast_spell",
    "activate_ability",
    "choose_target",
    "finish_target_selection",
    "choose_boolean",
    "choose_number",
    "select_object",
    "optional_cost",
    "mulligan",
    "order_pick",
    "declare_attack",
    "declare_block",
    "choose_spell_mode",
    "choose_option",
    "finish_selection",
    "arrange_card",
];

/// Build one `semantic`. Panics on a kind or field set that is not in [`KINDS`]: that is a bug in the
/// builder, never an input error.
pub fn make(kind: &str, fields: Value) -> Value {
    let (_, want) = KINDS.iter().find(|(k, _)| *k == kind).unwrap_or_else(|| panic!("unknown semantic kind {kind}"));
    let Value::Object(mut m) = fields else { panic!("semantic fields of {kind} are not an object") };
    let mut keys: Vec<&String> = m.keys().collect();
    keys.sort();
    let mut want_sorted: Vec<&str> = want.to_vec();
    want_sorted.sort_unstable();
    assert!(keys.iter().map(|k| k.as_str()).eq(want_sorted.iter().copied()), "semantic {kind} has fields {keys:?}, want {want_sorted:?}");
    let mut out = Map::new();
    out.insert("kind".into(), kind.into());
    out.append(&mut m);
    Value::Object(out)
}

/// An ObjectRef: `{object_id, card_name, owner_seat, controller_seat, zone}`.
pub fn object_ref(object_id: &str, card_name: Option<&str>, owner: &str, controller: &str, zone: &str) -> Value {
    json!({"object_id": object_id, "card_name": card_name, "owner_seat": owner, "controller_seat": controller, "zone": zone})
}

pub fn target_player(seat: &str) -> Value {
    json!({"player": seat})
}

pub fn target_object(r: Value) -> Value {
    json!({"object": r})
}

pub fn pass() -> Value {
    make("pass", json!({}))
}

pub fn play_land(source: Value, face: u32) -> Value {
    make("play_land", json!({"source": source, "face": face}))
}

pub fn cast_spell(source: Value, method: Option<&str>) -> Value {
    make("cast_spell", json!({"source": source, "method": method}))
}

pub fn activate_ability(source: Value, ability_index: u32) -> Value {
    make("activate_ability", json!({"source": source, "ability_index": ability_index}))
}

pub fn mulligan(hand_size: u32, taken: u32, keep: bool) -> Value {
    make("mulligan", json!({"hand_size": hand_size, "mulligans_taken": taken, "keep": keep}))
}

pub fn declare_attack(attacker: Value, defender: Option<Value>) -> Value {
    make("declare_attack", json!({"attacker": attacker, "defender": defender}))
}

pub fn declare_block(blocker: Value, attacker: Option<Value>) -> Value {
    make("declare_block", json!({"blocker": blocker, "attacker": attacker}))
}

#[allow(clippy::too_many_arguments)]
pub fn choose_target(source: Value, slot: u32, target: Value, selected: u32, min: u32, max: u32) -> Value {
    make(
        "choose_target",
        json!({"source": source, "slot": slot, "target": target, "selected_count": selected, "minimum": min, "maximum": max}),
    )
}

pub fn finish_target_selection(source: Value, slot: u32, selected: u32) -> Value {
    make("finish_target_selection", json!({"source": source, "slot": slot, "selected_count": selected}))
}

pub fn choose_boolean(source: Option<Value>, purpose: &str, value: bool) -> Value {
    make("choose_boolean", json!({"source": source, "purpose": purpose, "value": value}))
}

pub fn choose_number(source: Option<Value>, purpose: &str, value: i64, min: i64, max: i64) -> Value {
    make("choose_number", json!({"source": source, "purpose": purpose, "value": value, "minimum": min, "maximum": max}))
}

pub fn choose_option(source: Option<Value>, purpose: &str, index: u32, count: u32, label: Option<&str>) -> Value {
    make(
        "choose_option",
        json!({"source": source, "purpose": purpose, "option_index": index, "option_count": count, "option_label": label}),
    )
}

pub fn choose_spell_mode(source: Value, mode_index: u32, mode_count: u32, selected: u32, min: u32, max: u32) -> Value {
    make(
        "choose_spell_mode",
        json!({"source": source, "mode_index": mode_index, "mode_count": mode_count, "selected_count": selected,
            "minimum": min, "maximum": max}),
    )
}

pub fn select_object(source: Option<Value>, purpose: &str, choice: Value, selected: u32, min: u32, max: u32) -> Value {
    make(
        "select_object",
        json!({"source": source, "purpose": purpose, "choice": choice, "selected_count": selected, "minimum": min, "maximum": max}),
    )
}

pub fn finish_selection(source: Option<Value>, purpose: &str, selected: u32) -> Value {
    make("finish_selection", json!({"source": source, "purpose": purpose, "selected_count": selected}))
}

pub fn order_pick(source: Option<Value>, purpose: &str, item: Value, position: u32, count: u32) -> Value {
    make("order_pick", json!({"source": source, "purpose": purpose, "item": item, "position": position, "count": count}))
}

#[allow(clippy::too_many_arguments)]
pub fn arrange_card(source: Option<Value>, purpose: &str, card: Value, index: u32, count: u32, destination: &str) -> Value {
    make(
        "arrange_card",
        json!({"source": source, "purpose": purpose, "card": card, "card_index": index, "card_count": count,
            "destination": destination}),
    )
}

pub fn optional_cost(source: Value, cost: &str, pay: bool) -> Value {
    make("optional_cost", json!({"source": source, "cost": cost, "pay": pay}))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_advertised_kind_is_in_the_table() {
        for k in DECISION_KINDS {
            assert!(KINDS.iter().any(|(n, _)| n == k), "{k} advertised but not in KINDS");
        }
        let mut seen = std::collections::BTreeSet::new();
        for (k, _) in KINDS {
            assert!(seen.insert(*k), "duplicate kind {k}");
        }
    }

    #[test]
    fn builders_carry_exactly_the_kind_fields() {
        let o = object_ref("o-1", Some("Mountain"), "p0", "p0", "hand");
        let all = [
            pass(),
            play_land(o.clone(), 0),
            cast_spell(o.clone(), Some("normal")),
            activate_ability(o.clone(), 0),
            mulligan(7, 0, true),
            declare_attack(o.clone(), Some(target_player("p1"))),
            declare_block(o.clone(), None),
            choose_target(o.clone(), 0, target_player("p1"), 0, 1, 1),
            finish_target_selection(o.clone(), 0, 1),
            choose_boolean(None, "optional_effect", true),
            choose_number(Some(o.clone()), "x_value", 1, 0, 3),
            choose_option(None, "named_choice", 0, 2, Some("a")),
            choose_spell_mode(o.clone(), 0, 2, 0, 1, 1),
            select_object(None, "discard", target_object(o.clone()), 0, 1, 1),
            finish_selection(None, "discard", 1),
            optional_cost(o.clone(), "kicker", true),
            order_pick(None, "mulligan_bottom", json!({"object": o}), 0, 1),
            arrange_card(None, "scry", o.clone(), 0, 2, "top"),
        ];
        // Every advertised kind has a builder that the table accepts.
        let built: std::collections::BTreeSet<&str> = all.iter().map(|v| v["kind"].as_str().unwrap()).collect();
        for k in DECISION_KINDS {
            assert!(built.contains(k), "{k} is advertised but no builder is tested");
        }
    }

    #[test]
    #[should_panic(expected = "has fields")]
    fn a_drifted_builder_is_refused() {
        make("pass", json!({"extra": 1}));
    }
}
