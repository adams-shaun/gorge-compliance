//! `phase-seat`: phase.rs as a SpellBench v2 engine on stdin/stdout.
//!
//! ```text
//! phase-seat --card-data FILE [--decks DIR] [--source-revision REV] [--house-seat p0|p1 [--house-difficulty NAME]]
//! phase-seat --version
//! ```
//!
//! `--house-seat` (a build with the `house-ai` feature) answers that seat in-process with phase-ai at
//! `--house-difficulty` (veryeasy, easy, medium, hard, veryhard, cedh; default medium); `hello_ok`'s
//! `engine.version` ends `/house-<seat>-<difficulty>`. The client is then asked the other seat only.
//!
//! `PHASE_REF` (`<sha12>+<hash16>`, as `phaseoracle/run.sh` exports it) names the build in `hello_ok`.
//! Exit: 0 on a clean end of input, 1 when the stream fails or ends mid-line, 2 on bad usage or setup.
//! The engine runs on a 64 MiB stack (it assumes deep recursion). Diagnostics and the end-of-run
//! census go to stderr; stdout carries protocol lines only.

use std::io::{BufReader, Write};
use std::process::ExitCode;

use engine::database::card_db::CardDatabase;
use phase_seat::game::{load_decks, PhaseBackend};
use phase_seat::house::parse_house;
use phase_seat::server::Server;

fn usage() -> ExitCode {
    eprintln!(
        "usage: phase-seat --card-data FILE [--decks DIR] [--source-revision REV] [--house-seat p0|p1 [--house-difficulty veryeasy|easy|medium|hard|veryhard|cedh]] | --version"
    );
    ExitCode::from(2)
}

fn run() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let pin = std::env::var("PHASE_REF").unwrap_or_else(|_| "unknown".to_string());
    if args == ["--version"] {
        println!("phase-seat {pin}");
        return ExitCode::SUCCESS;
    }
    let house = match parse_house(&args) {
        Ok(h) => h,
        Err(e) => {
            eprintln!("phase-seat: {e}");
            return usage();
        }
    };
    let (mut data, mut decks, mut rev) = (None, None, None);
    let mut house_value = None;
    let mut it = args.iter();
    while let Some(a) = it.next() {
        let slot = match a.as_str() {
            // Read by `parse_house` above; here only skipped (with their values).
            "--house-seat" | "--house-difficulty" => &mut house_value,
            "--card-data" => &mut data,
            "--decks" => &mut decks,
            "--source-revision" => &mut rev,
            _ => return usage(),
        };
        match it.next() {
            Some(v) => *slot = Some(v.clone()),
            None => return usage(),
        }
    }
    let Some(data) = data else { return usage() };
    let db = match CardDatabase::from_export(std::path::Path::new(&data)) {
        Ok(db) => db,
        Err(e) => {
            eprintln!("phase-seat: cannot load {data}: {e}");
            return ExitCode::from(2);
        }
    };
    // No `--decks`: an empty catalog (xclient then writes its bundled decks and relaunches with `-decks`).
    let catalog = match decks.as_deref().map(|d| load_decks(std::path::Path::new(d))).unwrap_or(Ok(vec![])) {
        Ok(d) => d,
        Err(e) => {
            eprintln!("phase-seat: {e}");
            return ExitCode::from(2);
        }
    };
    let backend = PhaseBackend::new(db, catalog, &pin, rev.as_deref());
    #[cfg(feature = "house-ai")]
    let backend = match &house {
        Some((seat, difficulty)) => backend.with_house(*seat, difficulty),
        None => backend,
    };
    #[cfg(not(feature = "house-ai"))]
    if house.is_some() {
        eprintln!("phase-seat: --house-seat needs a build with the house-ai feature (cargo build -p phase-seat --features house-ai)");
        return ExitCode::from(2);
    }
    let mut server = Server::new(backend);
    let stdin = std::io::stdin();
    let mut input = BufReader::new(stdin.lock());
    let stdout = std::io::stdout();
    let mut out = stdout.lock();
    let r = server.serve(&mut input, &mut out);
    let _ = out.flush();
    eprint!("{}", server.backend.census.report());
    match r {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("phase-seat: {e}");
            ExitCode::from(1)
        }
    }
}

fn main() -> ExitCode {
    match std::thread::Builder::new().stack_size(64 << 20).spawn(run) {
        Ok(h) => h.join().unwrap_or(ExitCode::from(1)),
        Err(e) => {
            eprintln!("phase-seat: cannot start the engine thread: {e}");
            ExitCode::from(2)
        }
    }
}
