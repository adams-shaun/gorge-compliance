//! One output row of a follower batch, and the message conventions that classify reads.

use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

/// Harness messages are cut to this many chars.
pub const HARNESS_CAP: usize = 800;
/// `strict_miss` and `leftover` messages are cut to this many chars.
pub const MISS_CAP: usize = 300;

/// Cut `s` to at most `n` chars, always on a char boundary.
pub fn cap(s: &str, n: usize) -> String {
    match s.char_indices().nth(n) {
        Some((i, _)) => s[..i].to_string(),
        None => s.to_string(),
    }
}

/// `harness: <msg>`, cut to 800 chars. A message already starting `harness: ` is not re-prefixed.
pub fn harness_msg(msg: &str) -> String {
    let m = if msg.starts_with("harness: ") {
        msg.to_string()
    } else {
        format!("harness: {msg}")
    };
    cap(&m, HARNESS_CAP)
}

/// `Missing <what> answer at step <step>[: detail]`, cut to 300 chars. An empty `detail` is
/// omitted.
pub fn strict_miss_msg(what: &str, step: i64, detail: &str) -> String {
    let m = if detail.is_empty() {
        format!("Missing {what} answer at step {step}")
    } else {
        format!("Missing {what} answer at step {step}: {detail}")
    };
    cap(&m, MISS_CAP)
}

/// `unconsumed gorge decision(s): <detail>`, cut to 300 chars.
pub fn leftover_msg(detail: &str) -> String {
    cap(&format!("unconsumed gorge decision(s): {detail}"), MISS_CAP)
}

/// One result row. Serialises with deterministic key order; the engine ref key (`phase_ref`,
/// `manabrew_ref`, ...) rides in `refs`.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct ResultRow {
    pub name: String,
    pub id: String,
    pub engine: String,
    pub strict: bool,
    pub ms: u64,
    pub snapshots: Vec<Snapshot>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub harness: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub strict_miss: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub leftover: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub notes: Vec<String>,
    /// sha256 (lower-case hex) of the request line's bytes without the newline.
    pub request_sha: String,
    #[serde(flatten)]
    pub refs: BTreeMap<String, String>,
}

/// The game state at one checkpoint. Optional keys are omitted when empty.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Snapshot {
    /// "setup" | "step <i> (<op>)"
    pub checkpoint: String,
    pub turn: i64,
    pub step: String,
    pub active: i64,
    pub priority: i64,
    pub over: bool,
    pub players: Vec<PlayerSnap>,
    pub permanents: Vec<PermSnap>,
    pub stack: Vec<StackSnap>,
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct PlayerSnap {
    pub seat: i64,
    pub life: i64,
    #[serde(skip_serializing_if = "BTreeMap::is_empty", default)]
    pub counters: BTreeMap<String, i64>,
    pub hand: Vec<String>,
    pub graveyard: Vec<String>,
    pub exile: Vec<String>,
    pub command: Vec<String>,
    pub library_count: i64,
    pub library_top: Vec<String>,
    pub pool: String,
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct PermSnap {
    pub name: String,
    pub controller: i64,
    pub owner: i64,
    pub token: bool,
    pub tapped: bool,
    pub face_down: bool,
    /// "P/T"
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub pt: Option<String>,
    pub damage: i64,
    #[serde(skip_serializing_if = "BTreeMap::is_empty", default)]
    pub counters: BTreeMap<String, i64>,
    pub types: Vec<String>,
    /// WUBRG letters in that order, "" for colourless (a string on the wire, like the Java driver).
    pub colors: String,
    pub keywords: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub attached_to: Option<String>,
    pub attacking: bool,
    pub blocking: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct StackSnap {
    /// "spell" | "ability"
    pub kind: String,
    pub source: String,
    pub controller: i64,
}
