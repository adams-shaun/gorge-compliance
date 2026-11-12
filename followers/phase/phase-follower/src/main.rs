//! phase.rs follower for gorge-compliance.
//!
//! Modes so far: `--version` prints `phase-follower <engine sha12>+<card-data hash16>`;
//! `--card-data FILE --count` loads the card database and prints its face count;
//! `--preflight NAMES|- --card-data FILE` prints `NAME<TAB>REASON` for each card phase lacks (NAMES
//! `-` is stdin). Every other mode exits 2 with `not implemented`.
//!
//! Later stages replay requests against the engine. Two runtime rules apply to all of them: the
//! engine assumes deep recursion, so every run sets `RUST_MIN_STACK=16777216` and engine work runs
//! on a thread with a 64 MiB stack.

use std::io::{self, BufRead};
use std::process::ExitCode;

use engine::database::card_db::CardDatabase;

mod names;
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
    fn version_line_names_engine_and_data() {
        let (sha, hash16) = pin_parts();
        assert_eq!(version_line(), format!("phase-follower {}+{}", &sha[..12], hash16));
        assert_eq!(hash16.len(), 16);
    }
}
