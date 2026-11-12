//! phase.rs follower for gorge-compliance.
//!
//! Modes so far: `--version` prints `phase-follower <engine sha12>+<card-data hash16>`;
//! `--card-data FILE --count` loads the card database and prints its face count;
//! `--preflight NAMES|- --card-data FILE` prints `NAME<TAB>REASON` for each card phase lacks (NAMES
//! `-` is stdin); `scenarios IN OUT --card-data FILE` replays a JSONL batch (one row per line): it builds the
//! board of `item.setup`, takes the `setup` snapshot and replays the steps (`steps.rs`: `mana play
//! activate cast pass resolve`; the other ops are harness rows until the combat ticket). Every other mode exits 2 with `not implemented`.
//!
//! Later stages replay requests against the engine. Two runtime rules apply to all of them: the
//! engine assumes deep recursion, so every run sets `RUST_MIN_STACK=16777216` and engine work runs
//! on a thread with a 64 MiB stack.

use std::io::{self, BufRead};
use std::process::ExitCode;

use engine::database::card_db::CardDatabase;
use follower_proto::{run_batch, BatchConfig, BatchStats};

mod answer;
mod names;
mod setup;
mod snapshot;
mod steps;
mod support;

/// `<engine sha> <hash16> <sha256>` from `../PHASE_PIN`, embedded at compile time.
const PIN: &str = include_str!("../../PHASE_PIN");

fn pin_parts() -> (&'static str, &'static str) {
    let mut it = PIN.split_whitespace();
    let sha = it.next().expect("PHASE_PIN: engine sha");
    let hash16 = it.next().expect("PHASE_PIN: hash16");
    (sha, hash16)
}

fn version_line() -> String {
    let (sha, hash16) = pin_parts();
    format!("phase-follower {}+{}", &sha[..12], hash16)
}

fn load_db(file: &str) -> Result<CardDatabase, String> {
    CardDatabase::from_export(std::path::Path::new(file)).map_err(|e| format!("cannot load {file}: {e}"))
}

/// `--preflight NAMES --card-data FILE`: the protocol's "which cards do you lack" answer.
fn preflight(names: &str, card_data: &str) -> Result<(), String> {
    let db = load_db(card_data)?;
    let reader: Box<dyn BufRead> = if names == "-" {
        Box::new(io::BufReader::new(io::stdin()))
    } else {
        let f = std::fs::File::open(names).map_err(|e| format!("cannot read {names}: {e}"))?;
        Box::new(io::BufReader::new(f))
    };
    follower_proto::preflight::run_preflight(reader, io::stdout().lock(), |n| support::lacks(&db, n))
        .map(|_| ())
        .map_err(|e| format!("preflight: {e}"))
}

/// The batch constants: `PHASE_REF` (`<engine sha>+<hash16>`, set by the runner) is the row's
/// `phase_ref`.
fn batch_config(phase_ref: &str) -> BatchConfig {
    BatchConfig { engine: "phase".into(), ref_key: "phase_ref".into(), ref_value: phase_ref.into() }
}

/// Replay `input` into `output`. The replay runs on a 64 MiB-stack worker thread (phase assumes at
/// least 16 MiB), joined before returning; a panic in the replay is a row, never a crash.
fn run_scenarios(
    db: &CardDatabase,
    input: impl BufRead + Send,
    output: impl io::Write + Send,
    cfg: &BatchConfig,
) -> io::Result<BatchStats> {
    std::thread::scope(|s| {
        std::thread::Builder::new()
            .stack_size(64 << 20)
            .spawn_scoped(s, || {
                run_batch(input, output, cfg, |req, rb| {
                    let board = setup::run(db, req, rb)?;
                    steps::run(req, board, rb)
                })
            })?
            .join()
            .map_err(|_| io::Error::new(io::ErrorKind::Other, "replay thread panicked"))?
    })
}

/// `scenarios IN OUT --card-data FILE`: one row per input line.
fn scenarios(input: &str, output: &str, card_data: &str) -> Result<(), String> {
    // A bad card-data file is a setup error (exit 1), not a batch of identical rows.
    let db = load_db(card_data)?;
    let phase_ref = std::env::var("PHASE_REF").map_err(|_| "PHASE_REF is not set".to_string())?;
    let cfg = batch_config(&phase_ref);
    let reader = io::BufReader::new(std::fs::File::open(input).map_err(|e| format!("cannot read {input}: {e}"))?);
    let writer = io::BufWriter::new(std::fs::File::create(output).map_err(|e| format!("cannot write {output}: {e}"))?);
    let stats = run_scenarios(&db, reader, writer, &cfg).map_err(|e| format!("scenarios: {e}"))?;
    eprintln!("phase-follower: {} rows, {} harness", stats.rows, stats.harness);
    Ok(())
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.iter().map(String::as_str).collect::<Vec<_>>().as_slice() {
        ["--version"] => {
            println!("{}", version_line());
            ExitCode::SUCCESS
        }
        ["--card-data", file, "--count"] => match CardDatabase::from_export(std::path::Path::new(file)) {
            Ok(db) => {
                println!("{}", db.face_iter().count());
                ExitCode::SUCCESS
            }
            Err(e) => {
                eprintln!("phase-follower: cannot load {file}: {e}");
                ExitCode::from(1)
            }
        },
        ["--preflight", names, "--card-data", file] | ["--card-data", file, "--preflight", names] => {
            match preflight(names, file) {
                Ok(()) => ExitCode::SUCCESS,
                Err(e) => {
                    eprintln!("phase-follower: {e}");
                    ExitCode::from(1)
                }
            }
        }
        ["scenarios", input, output, "--card-data", file] => match scenarios(input, output, file) {
            Ok(()) => ExitCode::SUCCESS,
            Err(e) => {
                eprintln!("phase-follower: {e}");
                ExitCode::from(1)
            }
        },
        _ => {
            eprintln!("phase-follower: not implemented");
            ExitCode::from(2)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::OnceLock;

    /// The card data, loaded once per test binary from `PHASE_CARD_DATA`.
    fn db() -> &'static CardDatabase {
        static DB: OnceLock<CardDatabase> = OnceLock::new();
        DB.get_or_init(|| {
            let path = std::env::var("PHASE_CARD_DATA").expect("PHASE_CARD_DATA: run make phase-follower-test");
            load_db(&path).expect("load card data")
        })
    }

    fn run(names: &str) -> String {
        let mut out = Vec::new();
        follower_proto::preflight::run_preflight(names.as_bytes(), &mut out, |n| support::lacks(db(), n)).unwrap();
        String::from_utf8(out).unwrap()
    }

    #[test]
    fn preflight_refuses_unknown_name() {
        assert_eq!(run("Not A Real Card\n"), "Not A Real Card\tnot in card-data\n");
    }

    #[test]
    fn preflight_accepts_llanowar_elves() {
        assert_eq!(run("Llanowar Elves\nGrizzly Bears\n"), "");
    }

    #[test]
    fn preflight_refuses_confirmed_misparse() {
        let out = run("Cat Collector\n");
        assert!(out.starts_with("Cat Collector\tconfirmed misparse: "), "{out}");
    }

    #[test]
    fn preflight_resolves_split_card_by_corpus_name() {
        for name in ["Fire // Ice", "Fire", "Ice"] {
            let faces = names::phase_faces(db(), name).expect(name);
            let got: Vec<&str> = faces.iter().map(|f| f.name.as_str()).collect();
            assert_eq!(got, ["Fire", "Ice"], "{name}");
        }
        assert_eq!(names::phase_faces(db(), "Nope // Nada").err().as_deref(), Some("not in card-data"));
    }

    #[test]
    fn preflight_refuses_random_outcome_and_folds_accents() {
        let out = run("Frenetic Efreet\nHymn to Tourach\nPixie Guide\n\u{c6}ther Vial\nJuz\u{e1}m Djinn\n");
        assert_eq!(
            out,
            "Frenetic Efreet\trandom outcome: FlipCoins\nHymn to Tourach\trandom outcome: Discard at random\n\
             Pixie Guide\trandom outcome: RollDie\n"
        );
    }

    #[test]
    fn preflight_output_is_name_tab_reason() {
        let out = run("Llanowar Elves\nCat Collector\nNot A Real Card\n");
        let lines: Vec<&str> = out.lines().collect();
        assert_eq!(lines.len(), 2, "{out}");
        for (line, name) in lines.iter().zip(["Cat Collector", "Not A Real Card"]) {
            let (n, reason) = line.split_once('\t').expect("tab");
            assert_eq!(n, name);
            assert!(!reason.is_empty() && !reason.contains('\t'));
        }
    }

    /// Loads `PHASE_CARD_DATA` (set by `make phase-follower-test`) once and checks that two
    /// Foundations cards resolve by lower-case face name.
    #[test]
    fn card_data_resolves_fdn_names() {
        let path = std::env::var("PHASE_CARD_DATA").expect("PHASE_CARD_DATA: run make phase-follower-test");
        let db = CardDatabase::from_export(std::path::Path::new(&path)).expect("load card data");
        for name in ["llanowar elves", "grizzly bears"] {
            assert!(db.get_face_by_name(name).is_some(), "{name} does not resolve");
        }
    }

    #[test]
    fn batch_every_line_gets_a_phase_row() {
        let input = "{\"id\":\"a\",\"item\":{\"id\":\"a\",\"card\":\"Llanowar Elves\",\"name\":\"n\",\"steps\":[{\"op\":\"pass\"}]}}\nnot json\n";
        let mut out = Vec::new();
        let stats = run_scenarios(db(), input.as_bytes(), &mut out, &batch_config("abc+0123456789abcdef")).unwrap();
        assert_eq!((stats.rows, stats.harness), (2, 2));
        let text = String::from_utf8(out).unwrap();
        let rows: Vec<serde_json::Value> = text.lines().map(|l| serde_json::from_str(l).unwrap()).collect();
        assert_eq!(rows.len(), 2);
        for r in &rows {
            assert_eq!(r["engine"], "phase");
            assert_eq!(r["phase_ref"], "abc+0123456789abcdef");
        }
        assert_eq!(rows[0]["id"], "a");
        assert_eq!(rows[0]["harness"], "harness: phase follower: op attack not implemented");
        // The setup snapshot is kept when the steps stop the replay.
        assert_eq!(rows[0]["snapshots"][0]["checkpoint"], "setup");
        assert!(rows[1]["harness"].as_str().unwrap().starts_with("harness: malformed request line"));
    }

    #[test]
    fn version_line_names_engine_and_data() {
        let (sha, hash16) = pin_parts();
        assert_eq!(version_line(), format!("phase-follower {}+{}", &sha[..12], hash16));
        assert_eq!(hash16.len(), 16);
    }
}
