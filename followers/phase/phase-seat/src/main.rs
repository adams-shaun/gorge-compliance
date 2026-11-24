//! `phase-seat`: phase.rs as a SpellBench v2 engine on stdin/stdout.
//!
//! ```text
//! phase-seat --card-data FILE [--decks DIR] [--source-revision REV]
//! phase-seat --version
//! ```
//!
//! `PHASE_REF` (`<sha12>+<hash16>`, as `phaseoracle/run.sh` exports it) names the build in `hello_ok`.
//! Exit: 0 on a clean end of input, 1 when the stream fails or ends mid-line, 2 on bad usage or setup.
//! The engine runs on a 64 MiB stack (it assumes deep recursion). Diagnostics and the end-of-run
//! census go to stderr; stdout carries protocol lines only.

use std::io::{BufReader, Write};
use std::process::ExitCode;

use engine::database::card_db::CardDatabase;
use phase_seat::game::{load_decks, PhaseBackend};
use phase_seat::server::Server;

fn usage() -> ExitCode {
    eprintln!("usage: phase-seat --card-data FILE [--decks DIR] [--source-revision REV] | --version");
    ExitCode::from(2)
}

fn run() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let pin = std::env::var("PHASE_REF").unwrap_or_else(|_| "unknown".to_string());
    if args == ["--version"] {
        println!("phase-seat {pin}");
        return ExitCode::SUCCESS;
    }
    let (mut data, mut decks, mut rev) = (None, None, None);
    let mut it = args.iter();
    while let Some(a) = it.next() {
        let slot = match a.as_str() {
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
    let mut server = Server::new(PhaseBackend::new(db, catalog, &pin, rev.as_deref()));
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
