//! Replay determinism: the same `game_secret` and the same answers give the same transcript, byte for
//! byte. Needs the engine's card data (`PHASE_CARD_DATA`, as `make phase-seat-test` sets it) and a
//! deck catalog (`PHASE_SEAT_DECKS`, a directory of gorge deck JSON); without both the test skips
//! (and says so on stderr).
#![cfg(feature = "phase")]

use engine::database::card_db::CardDatabase;
use phase_seat::game::{load_decks, PhaseBackend};
use phase_seat::json::canonical;
use phase_seat::server::{Backend, Outcome, Reset};

/// A fixed pseudo-random pick (xorshift) so the policy itself adds no nondeterminism.
struct Pick(u64);
impl Pick {
    fn next(&mut self, n: usize) -> u32 {
        self.0 ^= self.0 << 13;
        self.0 ^= self.0 >> 7;
        self.0 ^= self.0 << 17;
        (self.0 % n as u64) as u32
    }
}

fn line(o: &Outcome) -> String {
    match o {
        Outcome::Decision(v) | Outcome::Terminal(v) => canonical(v),
    }
}

/// Plays one game to its end (or `cap` steps) and returns the canonical transcript.
fn play(b: &mut PhaseBackend, r: &Reset, policy: u64, cap: usize) -> Vec<String> {
    let mut pick = Pick(policy);
    let mut out = Vec::new();
    let mut o = b.reset(r).expect("reset");
    for _ in 0..cap {
        out.push(line(&o));
        match &o {
            Outcome::Terminal(_) => break,
            Outcome::Decision(sd) => {
                let n = sd["candidates"].as_array().expect("candidates").len();
                o = b.choose(pick.next(n));
            }
        }
    }
    out
}

fn run() {
    let (Ok(data), Ok(decks)) = (std::env::var("PHASE_CARD_DATA"), std::env::var("PHASE_SEAT_DECKS")) else {
        eprintln!("replay: PHASE_CARD_DATA or PHASE_SEAT_DECKS unset: skipped");
        return;
    };
    let db = CardDatabase::from_export(std::path::Path::new(&data)).expect("card data");
    let catalog = load_decks(std::path::Path::new(&decks)).expect("decks");
    assert!(catalog.len() >= 2, "need two catalog decks");
    let mut backend = PhaseBackend::new(db, catalog.clone(), "test+test", None);
    let (mut longest, mut terminal, mut played) = (0, 0, 0);
    // PHASE_SEAT_SEEDS="2,3" narrows the seeds; PHASE_SEAT_ONCE=1 plays each once (no comparison).
    let only: Option<Vec<u8>> = std::env::var("PHASE_SEAT_SEEDS").ok().map(|v| v.split(',').filter_map(|x| x.trim().parse().ok()).collect());
    let once = std::env::var("PHASE_SEAT_ONCE").is_ok_and(|v| v == "1");
    for seed in 0u8..6 {
        if only.as_ref().is_some_and(|o| !o.contains(&seed)) {
            continue;
        }
        let mut secret = [0u8; 32];
        secret[0] = seed.wrapping_mul(37).wrapping_add(11);
        secret[1] = seed;
        let r = Reset {
            game_id: format!("g{seed}"),
            decks: [catalog[seed as usize % catalog.len()].catalog_id.clone(), catalog[(seed as usize + 1) % catalog.len()].catalog_id.clone()],
            mulligan: "london".into(),
            starting_seat: seed % 2,
            names: vec![],
            opponent_decklist_visible: true,
            game_secret: secret,
            max_decisions: 100_000,
            max_steps: 100_000,
        };
        let a = play(&mut backend, &r, 0x9e37_79b9_7f4a_7c15 ^ u64::from(seed), 4000);
        eprintln!("replay: seed {seed}: {} lines, last: {}", a.len(), a.last().map_or(String::new(), |l| l.chars().take(160).collect()));
        let b = if once { a.clone() } else { play(&mut backend, &r, 0x9e37_79b9_7f4a_7c15 ^ u64::from(seed), 4000) };
        played += 1;
        longest = longest.max(a.len());
        terminal += usize::from(a.last().is_some_and(|l| l.contains("\"response_type\"") || l.contains("\"outcome\"")));
        if a != b {
            let i = a.iter().zip(&b).position(|(x, y)| x != y).unwrap_or(a.len().min(b.len()));
            panic!("seed {seed}: transcripts diverge at line {i} of {} / {}\n--- first\n{}\n--- second\n{}", a.len(), b.len(), a.get(i).cloned().unwrap_or_default(), b.get(i).cloned().unwrap_or_default());
        }
    }
    eprintln!("replay: {played} seeds {}; longest {longest} lines; {terminal} ended", if once { "played once" } else { "identical twice over" });
}

#[test]
fn the_same_seed_and_answers_replay_byte_for_byte() {
    // The engine's stack needs are above a test thread's default.
    std::thread::Builder::new().stack_size(64 << 20).spawn(run).expect("thread").join().expect("replay thread");
}
