//! One input line of a follower batch (what `oraclediff follower-export` writes).
//!
//! Unknown fields are ignored everywhere (never `deny_unknown_fields`): items carry XMage-only
//! fields (`xmage_name`, `xmage_answers`, ...) a follower has no use for. JSON `null` object
//! members are treated as absent, so every field below simply defaults.

use serde::de::{self, Deserializer};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::BTreeMap;

/// A `{KIND: n}` counter map.
pub type Counters = BTreeMap<String, i64>;

/// One request line.
#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Request {
    pub id: String,
    pub scenario_sha: String,
    pub item: Item,
    pub gorge_decisions: Vec<Decision>,
    /// Forge script line per step index (kept as strings; level-B only).
    pub abilities: BTreeMap<String, String>,
    pub neutral: Neutral,
}

impl Request {
    /// Parse one request line. A line without an `item` key is a bare item (hand fixtures): the
    /// whole object is the item. `id` falls back to `item.id`.
    pub fn parse(line: &str) -> Result<Request, String> {
        let mut v: Value = serde_json::from_str(line).map_err(|e| format!("bad JSON: {e}"))?;
        if !v.is_object() {
            return Err("request line is not a JSON object".to_string());
        }
        strip_nulls(&mut v);
        let wrapped = v.as_object().map_or(false, |o| o.contains_key("item"));
        let mut req = if wrapped {
            serde_json::from_value::<Request>(v)
        } else {
            serde_json::from_value::<Item>(v).map(|item| Request {
                item,
                ..Request::default()
            })
        }
        .map_err(|e| format!("bad request: {e}"))?;
        if req.id.is_empty() {
            req.id = req.item.id.clone();
        }
        Ok(req)
    }
}

/// Drop `null` object members everywhere (array elements are kept: `[null, null]` is data).
fn strip_nulls(v: &mut Value) {
    match v {
        Value::Object(m) => {
            m.retain(|_, x| !x.is_null());
            m.values_mut().for_each(strip_nulls);
        }
        Value::Array(a) => a.iter_mut().for_each(strip_nulls),
        _ => {}
    }
}

/// A scenario item. `setup_answers`, and the XMage-only fields, are ignored.
#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Item {
    pub id: String,
    pub card: String,
    pub template: String,
    pub name: String,
    /// Absent/null = 1; see [`Item::turn`].
    pub turn: Option<i64>,
    /// 0/absent = 2; see [`Item::players`].
    pub players: Option<i64>,
    pub setup: Setup,
    pub steps: Vec<Step>,
    /// Parallel to `steps`: WUBRGC letters gorge spent from the pool on step i, "" = none.
    pub payment: Vec<String>,
    pub ignore: Vec<String>,
    pub compare: Vec<String>,
    pub cr: Vec<String>,
    pub why: String,
}

impl Item {
    pub fn turn(&self) -> i64 {
        self.turn.unwrap_or(1)
    }
    pub fn players(&self) -> i64 {
        match self.players {
            None | Some(0) => 2,
            Some(n) => n,
        }
    }
    /// The seat setup for seat index 0 or 1.
    pub fn seat(&self, i: usize) -> Option<&Seat> {
        match i {
            0 => Some(&self.setup.p0),
            1 => Some(&self.setup.p1),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Setup {
    pub p0: Seat,
    pub p1: Seat,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Seat {
    pub battlefield: Vec<String>,
    pub tapped: Vec<String>,
    pub tapped_after_untap: Vec<String>,
    pub back_face: Vec<String>,
    pub hand: Vec<String>,
    pub graveyard: Vec<String>,
    pub exile: Vec<String>,
    pub library: Vec<String>,
    pub library_top: Vec<String>,
    /// card name -> {KIND: n}
    pub counters: BTreeMap<String, Counters>,
    pub tokens: Vec<TokenSpec>,
    pub attached: Vec<Attach>,
    /// Starting life; absent = 20, see [`Seat::life`].
    pub life: Option<i64>,
    /// 0..4.
    pub speed: i64,
    pub command: Vec<String>,
    pub sideboard: Vec<String>,
    pub mana: Value,
}

impl Seat {
    pub fn life(&self) -> i64 {
        self.life.unwrap_or(20)
    }
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct TokenSpec {
    /// Token script stem (a key of [`Neutral::token`]).
    pub script: String,
    pub counters: Counters,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Attach {
    pub card: String,
    pub to: String,
}

/// One scripted step. `answers`, `expect` and `observe` are ignored.
#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Step {
    /// mana cast activate play resolve attack block pass pass_to move life attach
    pub op: String,
    pub seat: i64,
    pub card: String,
    pub mana: String,
    pub targets: Vec<String>,
    pub cast_mode: String,
    pub ability_index: Option<i64>,
    pub ability: String,
    pub attackers: Vec<String>,
    pub defender: String,
    /// `[blocker, attacker]` pairs.
    pub blocks: Vec<(String, String)>,
    pub step: String,
    /// Shape differs per op, so it is kept as raw JSON.
    pub active: Value,
    /// Shape differs per op, so it is kept as raw JSON.
    pub decision: Value,
    pub to: String,
    pub attached_to: String,
    pub amount: i64,
}

/// A decision gorge made while running the scenario (`rules.OracleDecision`).
#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Decision {
    /// -1 = setup.
    pub step: i32,
    pub seat: i64,
    /// target yesno mode choose_n order attackers blockers
    pub kind: String,
    pub gorge_kind: String,
    pub via: String,
    pub resume: String,
    pub options: i64,
    pub picks: Vec<String>,
    pub pick_idx: Vec<i64>,
    pub pick_refs: Vec<String>,
    pub pick_refs_inexact: Vec<bool>,
    pub object_picks: Vec<String>,
    pub option_refs: Vec<String>,
    pub pick_kinds: Vec<String>,
    pub first: String,
    pub min: i64,
    pub max: i64,
    pub divided: bool,
}

/// Engine-neutral facts derived from the Forge-side data. Every map is present from
/// `follower-export`; a field that could not be derived is absent.
#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct Neutral {
    /// step index -> ability description
    pub ability: BTreeMap<String, NeutralAbility>,
    /// token script stem -> token description
    pub token: BTreeMap<String, NeutralToken>,
    /// decision index -> mode choices
    pub mode: BTreeMap<String, NeutralMode>,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct NeutralAbility {
    /// mana | activated | loyalty | intrinsic_mana
    pub class: String,
    pub ordinal: Option<i64>,
    /// e.g. "Equip:2"
    pub keyword: Option<String>,
    pub cost: String,
    pub text: Option<String>,
    /// WUBRGC letters or "any"
    pub produces: Option<String>,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct NeutralToken {
    pub name: String,
    pub supertypes: Vec<String>,
    pub types: Vec<String>,
    pub subtypes: Vec<String>,
    pub colours: Vec<String>,
    /// Kept as text: a token's power can be `*`.
    #[serde(deserialize_with = "de_opt_text")]
    pub power: Option<String>,
    #[serde(deserialize_with = "de_opt_text")]
    pub toughness: Option<String>,
    pub keywords: Vec<String>,
    pub text: String,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct NeutralMode {
    pub step: i32,
    pub picks: Vec<ModePick>,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize, Serialize)]
#[serde(default)]
pub struct ModePick {
    pub ordinal: i64,
    pub text: String,
}

/// A string or an integer, kept as text.
fn de_opt_text<'de, D: Deserializer<'de>>(d: D) -> Result<Option<String>, D::Error> {
    match Value::deserialize(d)? {
        Value::Null => Ok(None),
        Value::String(s) => Ok(Some(s)),
        Value::Number(n) => Ok(Some(n.to_string())),
        other => Err(de::Error::custom(format!("expected string or number, got {other}"))),
    }
}
