//! phase.rs follower for gorge-compliance.
//!
//! Stage 1 (this file): the pinned out-of-tree build only. `--version` prints
//! `phase-follower <engine sha12>+<card-data hash16>` and `--card-data FILE --count` loads the
//! card database and prints its face count. Every other mode exits 2 with `not implemented`.
//!
//! Later stages replay requests against the engine. Two runtime rules apply to all of them: the
//! engine assumes deep recursion, so every run sets `RUST_MIN_STACK=16777216` and engine work runs
//! on a thread with a 64 MiB stack.

use std::process::ExitCode;

use engine::database::card_db::CardDatabase;

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
        _ => {
            eprintln!("phase-follower: not implemented");
            ExitCode::from(2)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

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
