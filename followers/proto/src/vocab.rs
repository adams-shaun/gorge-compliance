//! Snapshot vocabulary: the spellings and orderings gorge's comparator normalises with.

/// Step names, in turn order. Both damage steps are `combat-damage`.
pub const STEP_NAMES: [&str; 12] = [
    "untap",
    "upkeep",
    "draw",
    "main1",
    "begin-combat",
    "declare-attackers",
    "declare-blockers",
    "combat-damage",
    "end-combat",
    "main2",
    "end",
    "cleanup",
];

fn letters_in_order(letters: &str, order: &str, dedupe: bool) -> String {
    let mut out = String::new();
    for want in order.chars() {
        let n = letters.chars().filter(|c| c.to_ascii_uppercase() == want).count();
        let n = if dedupe { n.min(1) } else { n };
        for _ in 0..n {
            out.push(want);
        }
    }
    out
}

/// Colours as WUBRG letters in that order, "" for colourless. Other characters are dropped.
pub fn colors(letters: &str) -> String {
    letters_in_order(letters, "WUBRG", true)
}

/// A mana pool as WUBRGC letters in that order, each repeated per unit.
pub fn pool(letters: &str) -> String {
    letters_in_order(letters, "WUBRGC", false)
}

/// Counter kind spelling: `+1/+1` is `P1P1`, `-1/-1` is `M1M1`, anything else is the display
/// name upper-cased with whitespace removed (`Loyalty` is `LOYALTY`).
pub fn counter_kind(raw: &str) -> String {
    match raw.trim() {
        "+1/+1" => "P1P1".to_string(),
        "-1/-1" => "M1M1".to_string(),
        s => s.chars().filter(|c| !c.is_whitespace()).flat_map(char::to_uppercase).collect(),
    }
}

/// Hand, exile and command zones are compared as sorted name lists.
pub fn sort_zone(mut names: Vec<String>) -> Vec<String> {
    names.sort();
    names
}

/// `library_top`: at most the first 5 cards, top first.
pub fn library_top(top_first: &[String]) -> Vec<String> {
    top_first.iter().take(5).cloned().collect()
}

/// Core + super + sub types, sorted.
pub fn sorted_types(mut types: Vec<String>) -> Vec<String> {
    types.sort();
    types
}

/// `"P/T"` for a creature's power and toughness.
pub fn pt(power: i64, toughness: i64) -> String {
    format!("{power}/{toughness}")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn v(xs: &[&str]) -> Vec<String> {
        xs.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn vocab_colors_wubrg_order() {
        assert_eq!(colors("GRBUW"), "WUBRG");
        assert_eq!(colors("RW"), "WR");
        assert_eq!(colors("GG"), "G");
        assert_eq!(colors(""), "");
        assert_eq!(STEP_NAMES.len(), 12);
        assert_eq!(STEP_NAMES[7], "combat-damage");
        assert_eq!(pt(2, -1), "2/-1");
    }

    #[test]
    fn vocab_pool_wubrgc() {
        assert_eq!(pool("CGRRW"), "WRRGC");
        assert_eq!(pool("GG"), "GG");
        assert_eq!(pool(""), "");
    }

    #[test]
    fn vocab_counter_kinds() {
        assert_eq!(counter_kind("+1/+1"), "P1P1");
        assert_eq!(counter_kind("-1/-1"), "M1M1");
        assert_eq!(counter_kind("Loyalty"), "LOYALTY");
        assert_eq!(counter_kind("charge"), "CHARGE");
        assert_eq!(counter_kind("Time Travel"), "TIMETRAVEL");
        assert_eq!(counter_kind("P1P1"), "P1P1");
    }

    #[test]
    fn vocab_library_top_five() {
        let lib = v(&["a", "b", "c", "d", "e", "f", "g"]);
        assert_eq!(library_top(&lib), v(&["a", "b", "c", "d", "e"]));
        assert_eq!(library_top(&lib[..2]), v(&["a", "b"]));
        assert_eq!(sort_zone(v(&["b", "a"])), v(&["a", "b"]));
        assert_eq!(sorted_types(v(&["Land", "Basic", "Mountain"])), v(&["Basic", "Land", "Mountain"]));
    }
}
