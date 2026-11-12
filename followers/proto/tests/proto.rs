use follower_proto::batch::{first_id, sha256_hex};
use follower_proto::row::{cap, harness_msg, strict_miss_msg};
use follower_proto::{run_batch, run_preflight, BatchConfig, Request, RowBuilder, Stop};
use serde_json::Value;

// Hand-written fixtures (adapted from the contract's examples; `abilities` is always empty).
const CAST_RESOLVE: &str = r#"{"abilities":{},"gorge_decisions":[{"step":0,"seat":0,"kind":"target","options":1,"picks":["b"],"pick_idx":[0],"pick_refs":["p1"],"pick_kinds":["player"],"via":"target","gorge_kind":"target","first":"b","min":1,"max":1}],"id":"Ruthless Negotiation/cast-resolve/v1","item":{"id":"Ruthless Negotiation/cast-resolve/v1","card":"Ruthless Negotiation","template":"cast-resolve","name":"gen1-cast-resolve","cr":["601.2"],"why":"generated level-A scenario","setup":{"p0":{"hand":["Ruthless Negotiation"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Ruthless Negotiation","mana":"B","targets":["p1"]},{"op":"resolve","seat":0}]},"scenario_sha":"08bac7","neutral":{"ability":{"0":{"class":"mana","cost":"T","produces":"any"}},"token":{"soldier":{"name":"Soldier","supertypes":[],"types":["Creature"],"subtypes":["Soldier"],"colours":["W"],"power":1,"toughness":"*","keywords":[],"text":""}},"mode":{"0":{"step":2,"picks":[{"ordinal":1,"text":"x"}]}}}}"#;

fn cfg() -> BatchConfig {
    BatchConfig {
        engine: "test".into(),
        ref_key: "test_ref".into(),
        ref_value: "abc123".into(),
    }
}

fn run(input: &str, mut f: impl FnMut(&Request, &mut RowBuilder) -> Result<(), Stop>) -> (Vec<Value>, String) {
    let mut out = Vec::new();
    run_batch(input.as_bytes(), &mut out, &cfg(), &mut f).unwrap();
    let text = String::from_utf8(out).unwrap();
    let rows = text.lines().map(|l| serde_json::from_str(l).unwrap()).collect();
    (rows, text)
}

#[test]
fn request_parses_cast_resolve_line() {
    let r = Request::parse(CAST_RESOLVE).unwrap();
    assert_eq!(r.id, "Ruthless Negotiation/cast-resolve/v1");
    assert_eq!(r.scenario_sha, "08bac7");
    assert_eq!(r.item.name, "gen1-cast-resolve");
    assert_eq!(r.item.turn(), 1);
    assert_eq!(r.item.players(), 2);
    assert_eq!(r.item.setup.p0.hand, ["Ruthless Negotiation"]);
    assert_eq!(r.item.setup.p0.life(), 20);
    assert_eq!(r.item.seat(1).unwrap().battlefield, ["Grizzly Bears"]);
    assert_eq!(r.item.steps.len(), 2);
    assert_eq!(r.item.steps[0].op, "cast");
    assert_eq!(r.item.steps[0].targets, ["p1"]);
    assert_eq!(r.item.steps[0].ability_index, None);
    assert!(r.abilities.is_empty());
    let d = &r.gorge_decisions[0];
    assert_eq!((d.step, d.kind.as_str(), d.via.as_str()), (0, "target", "target"));
    assert_eq!((d.pick_idx.clone(), d.min, d.max, d.divided), (vec![0], 1, 1, false));
    assert_eq!(d.pick_refs, ["p1"]);
    assert_eq!(r.neutral.ability["0"].class, "mana");
    assert_eq!(r.neutral.ability["0"].produces.as_deref(), Some("any"));
    assert_eq!(r.neutral.token["soldier"].power.as_deref(), Some("1"));
    assert_eq!(r.neutral.token["soldier"].toughness.as_deref(), Some("*"));
    assert_eq!(r.neutral.mode["0"].picks[0].ordinal, 1);
}

#[test]
fn request_accepts_bare_item_line() {
    let r = Request::parse(
        r#"{"id":"hand/one","name":"n","turn":null,"players":0,"setup":{"p0":{"life":7}},"steps":[{"op":"pass","seat":1,"blocks":[["a","b"]]}]}"#,
    )
    .unwrap();
    assert_eq!(r.id, "hand/one");
    assert_eq!(r.item.id, "hand/one");
    assert_eq!(r.item.turn(), 1);
    assert_eq!(r.item.players(), 2);
    assert_eq!(r.item.setup.p0.life(), 7);
    assert_eq!(r.item.steps[0].blocks, [("a".to_string(), "b".to_string())]);
    // A wrapped line with an empty id falls back to item.id.
    let r = Request::parse(r#"{"item":{"id":"inner"}}"#).unwrap();
    assert_eq!(r.id, "inner");
}

#[test]
fn request_ignores_unknown_fields() {
    let r = Request::parse(
        r#"{"id":"x","future":1,"item":{"id":"x","xmage_name":"N","xmage_answers":[null,null],"xmage_ability":["a",""],"setup_answers":[1],"setup":{"p0":{"newthing":{"a":1}}},"steps":[{"op":"cast","answers":[{"kind":"modes"}],"expect":{},"observe":[1],"zzz":true}]},"gorge_decisions":[{"step":-1,"extra":"y"}]}"#,
    )
    .unwrap();
    assert_eq!(r.item.steps[0].op, "cast");
    assert_eq!(r.gorge_decisions[0].step, -1);
    assert!(Request::parse("[1,2]").is_err());
    assert!(Request::parse("{nope").is_err());
}

#[test]
fn batch_rows_in_input_order() {
    let input = format!("{CAST_RESOLVE}\n{{\"id\":\"b\",\"name\":\"nb\"}}\n{{\"id\":\"c\"}}\n");
    let mut seen = Vec::new();
    let (rows, text) = run(&input, |r, rb| {
        seen.push(r.id.clone());
        rb.strict = true;
        rb.note("hello");
        if r.id == "c" {
            return Err(Stop::StrictMiss(strict_miss_msg("target", 3, "")));
        }
        Ok(())
    });
    assert_eq!(rows.len(), 3);
    assert_eq!(seen, ["Ruthless Negotiation/cast-resolve/v1", "b", "c"]);
    assert!(text.ends_with('\n'));
    assert_eq!(rows[0]["name"], "gen1-cast-resolve");
    assert_eq!(rows[1]["id"], "b");
    assert_eq!(rows[1]["name"], "nb");
    assert_eq!(rows[1]["engine"], "test");
    assert_eq!(rows[1]["test_ref"], "abc123");
    assert_eq!(rows[1]["strict"], true);
    assert_eq!(rows[1]["notes"][0], "hello");
    assert!(rows[1].get("harness").is_none() && rows[1].get("leftover").is_none());
    assert_eq!(rows[1]["snapshots"], serde_json::json!([]));
    assert_eq!(rows[2]["strict_miss"], "Missing target answer at step 3");
}

#[test]
fn batch_malformed_line_is_harness_row_with_id() {
    let input = "{\"id\": \"keep\\\"me\", broken\nnot json at all\n\n{\"id\":\"ok\"}\n\u{0}\n";
    let mut calls = 0;
    let (rows, _) = run(input, |_, _| {
        calls += 1;
        Ok(())
    });
    assert_eq!(rows.len(), 5);
    assert_eq!(calls, 1);
    assert_eq!(rows[0]["id"], "keep\"me");
    assert_eq!(rows[1]["id"], "");
    assert_eq!(rows[2]["id"], "");
    for i in [0, 1, 2, 4] {
        assert!(rows[i]["harness"].as_str().unwrap().starts_with("harness: "), "row {i}");
        assert_eq!(rows[i]["strict"], false);
    }
    assert!(rows[3].get("harness").is_none());
    assert_eq!(first_id(r#"{"xid":"no","id" : "yes"}"#), "yes");
    // Non-UTF-8 bytes never stop the batch either.
    let mut out = Vec::new();
    let bytes: &[u8] = b"\xff\xfe{\"id\":\"z\"}\n{\"id\":\"after\"}\n";
    let st = run_batch(bytes, &mut out, &cfg(), |_, _| Ok(())).unwrap();
    assert_eq!((st.rows, st.harness), (2, 1));
}

#[test]
fn batch_panic_keeps_snapshots() {
    let input = "{\"id\":\"p\"}\n{\"id\":\"q\"}\n";
    let (rows, _) = run(input, |r, rb| {
        rb.push_snapshot(follower_proto::row::Snapshot {
            checkpoint: "setup".into(),
            ..Default::default()
        });
        if r.id == "p" {
            panic!("boom {}", 7);
        }
        Ok(())
    });
    assert_eq!(rows[0]["harness"], "harness: engine panic: boom 7");
    assert_eq!(rows[0]["snapshots"][0]["checkpoint"], "setup");
    assert!(rows[1].get("harness").is_none());
    assert_eq!(rows[1]["snapshots"].as_array().unwrap().len(), 1);
}

#[test]
fn batch_request_sha_matches() {
    let line = r#"{"id":"s",  "name":"x"}"#;
    let (rows, _) = run(&format!("{line}\nbad\n"), |_, _| Ok(()));
    assert_eq!(rows[0]["request_sha"], sha256_hex(line.as_bytes()));
    assert_eq!(rows[1]["request_sha"], sha256_hex(b"bad"));
    // sha256("abc")
    assert_eq!(
        sha256_hex(b"abc"),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
}

#[test]
fn row_caps_on_char_boundary() {
    assert_eq!(cap("héllo", 2), "hé");
    assert_eq!(cap("héllo", 5), "héllo");
    let long = "é".repeat(1000);
    let h = harness_msg(&long);
    assert!(h.starts_with("harness: é"));
    assert_eq!(h.chars().count(), 800);
    assert_eq!(harness_msg("harness: already"), "harness: already");
    let m = strict_miss_msg("target", 2, &"日".repeat(500));
    assert_eq!(m.chars().count(), 300);
    assert!(m.starts_with("Missing target answer at step 2: 日"));
    // Stop messages are capped by the batch loop too.
    let (rows, _) = run("{\"id\":\"a\"}\n", |_, rb| {
        rb.leftover = Some("日".repeat(400));
        Err(Stop::StrictMiss("é".repeat(400)))
    });
    assert_eq!(rows[0]["strict_miss"].as_str().unwrap().chars().count(), 300);
    assert_eq!(rows[0]["leftover"].as_str().unwrap().chars().count(), 300);
}

#[test]
fn preflight_prints_name_tab_reason() {
    let names = "Lightning Bolt\n  Odd Name \r\n\nMystery\tCard\nGrizzly Bears";
    let mut out = Vec::new();
    let n = run_preflight(names.as_bytes(), &mut out, |name| match name {
        "Lightning Bolt" => None,
        "Grizzly Bears" => None,
        other => Some(format!("no\tsupport\nfor {}", other.len())),
    })
    .unwrap();
    assert_eq!(n, 2);
    assert_eq!(
        String::from_utf8(out).unwrap(),
        "  Odd Name \tno support for 11\nMystery\tCard\tno support for 12\n"
    );
}
