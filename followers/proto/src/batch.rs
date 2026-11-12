//! The JSONL batch loop: one output row per input line, in input order, flushed after every
//! row. A bad line becomes a harness row and never stops the batch.

use crate::request::Request;
use crate::row::{cap, harness_msg, ResultRow, Snapshot, MISS_CAP};
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
use std::io::{self, BufRead, Write};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::time::Instant;

/// Per-batch constants stamped on every row.
#[derive(Debug, Clone, Default)]
pub struct BatchConfig {
    /// Row `engine`, e.g. "phase".
    pub engine: String,
    /// Engine ref key, e.g. "phase_ref".
    pub ref_key: String,
    /// Engine ref value (the pinned engine commit).
    pub ref_value: String,
}

/// What a replay accumulates; the batch loop turns it into the row.
#[derive(Debug, Clone, Default)]
pub struct RowBuilder {
    pub snapshots: Vec<Snapshot>,
    pub notes: Vec<String>,
    /// A complete `unconsumed gorge decision(s): ...` message (see `row::leftover_msg`).
    pub leftover: Option<String>,
    /// Row `strict`: set by the replay once it knows the run was strict.
    pub strict: bool,
}

impl RowBuilder {
    pub fn push_snapshot(&mut self, s: Snapshot) {
        self.snapshots.push(s);
    }
    pub fn note(&mut self, n: impl Into<String>) {
        self.notes.push(n.into());
    }
}

/// Why a replay stopped early. Snapshots pushed before the stop are kept.
#[derive(Debug, Clone, PartialEq)]
pub enum Stop {
    /// The follower itself failed; the row's `harness` is `harness: <msg>`.
    Harness(String),
    /// The follower had no answer for a gorge decision; the message is the full
    /// `Missing <what> answer at step <i>[: detail]` (see `row::strict_miss_msg`).
    StrictMiss(String),
}

/// Counts over one batch.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BatchStats {
    pub rows: usize,
    pub harness: usize,
    pub strict_miss: usize,
    pub leftover: usize,
}

/// Replay every line of `input`, writing one row per line to `output`. Only I/O errors stop the
/// loop; everything else becomes a row.
pub fn run_batch(
    mut input: impl BufRead,
    mut output: impl Write,
    cfg: &BatchConfig,
    mut replay: impl FnMut(&Request, &mut RowBuilder) -> Result<(), Stop>,
) -> io::Result<BatchStats> {
    let mut stats = BatchStats::default();
    let mut buf = Vec::new();
    loop {
        buf.clear();
        if input.read_until(b'\n', &mut buf)? == 0 {
            break;
        }
        if buf.last() == Some(&b'\n') {
            buf.pop();
        }
        let row = line_row(&buf, cfg, &mut replay);
        stats.rows += 1;
        stats.harness += row.harness.is_some() as usize;
        stats.strict_miss += row.strict_miss.is_some() as usize;
        stats.leftover += row.leftover.is_some() as usize;
        serde_json::to_writer(&mut output, &row).map_err(io::Error::from)?;
        output.write_all(b"\n")?;
        output.flush()?;
    }
    Ok(stats)
}

fn line_row(
    bytes: &[u8],
    cfg: &BatchConfig,
    replay: &mut impl FnMut(&Request, &mut RowBuilder) -> Result<(), Stop>,
) -> ResultRow {
    let start = Instant::now();
    let mut row = ResultRow {
        engine: cfg.engine.clone(),
        request_sha: sha256_hex(bytes),
        refs: BTreeMap::from([(cfg.ref_key.clone(), cfg.ref_value.clone())]),
        ..ResultRow::default()
    };
    let text = String::from_utf8_lossy(bytes);
    let parsed = if std::str::from_utf8(bytes).is_err() {
        Err("line is not valid UTF-8".to_string())
    } else {
        Request::parse(&text)
    };
    match parsed {
        Err(e) => {
            row.id = first_id(&text);
            row.harness = Some(harness_msg(&format!("malformed request line: {e}")));
        }
        Ok(req) => {
            row.id = req.id.clone();
            row.name = req.item.name.clone();
            let mut rb = RowBuilder::default();
            let out = catch_unwind(AssertUnwindSafe(|| replay(&req, &mut rb)));
            match out {
                Ok(Ok(())) => {}
                Ok(Err(Stop::Harness(m))) => row.harness = Some(harness_msg(&m)),
                Ok(Err(Stop::StrictMiss(m))) => row.strict_miss = Some(cap(&m, MISS_CAP)),
                Err(p) => {
                    row.harness = Some(harness_msg(&format!("engine panic: {}", panic_text(&*p))))
                }
            }
            row.strict = rb.strict;
            row.snapshots = rb.snapshots;
            row.notes = rb.notes;
            row.leftover = rb.leftover.map(|l| cap(&l, MISS_CAP));
        }
    }
    row.ms = start.elapsed().as_millis() as u64;
    row
}

fn panic_text(p: &(dyn std::any::Any + Send)) -> String {
    if let Some(s) = p.downcast_ref::<&str>() {
        (*s).to_string()
    } else if let Some(s) = p.downcast_ref::<String>() {
        s.clone()
    } else {
        "unknown panic payload".to_string()
    }
}

/// Lower-case hex sha256.
pub fn sha256_hex(b: &[u8]) -> String {
    use std::fmt::Write as _;
    let mut s = String::with_capacity(64);
    for byte in Sha256::digest(b) {
        let _ = write!(s, "{byte:02x}");
    }
    s
}

/// The value of the first `"id":"..."` in a raw line, or "".
pub fn first_id(line: &str) -> String {
    let mut from = 0;
    while let Some(rel) = line[from..].find("\"id\"") {
        let after = from + rel + 4;
        let rest = line[after..].trim_start();
        if let Some(rest) = rest.strip_prefix(':') {
            let rest = rest.trim_start();
            if rest.starts_with('"') {
                if let Some(s) = json_string_prefix(rest) {
                    return s;
                }
            }
        }
        from = after;
    }
    String::new()
}

/// Decode the JSON string literal at the start of `s` (which begins with `"`).
fn json_string_prefix(s: &str) -> Option<String> {
    let mut esc = false;
    for (i, c) in s.char_indices().skip(1) {
        match c {
            _ if esc => esc = false,
            '\\' => esc = true,
            '"' => return serde_json::from_str::<String>(&s[..=i]).ok(),
            _ => {}
        }
    }
    None
}

