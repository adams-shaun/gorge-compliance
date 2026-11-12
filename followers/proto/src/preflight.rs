//! `RUNNER --preflight NAMES.txt`: report the cards a follower lacks.

use std::io::{self, BufRead, Write};

/// Read one card name per line from `names`; for each the follower lacks (`lacks` returns a
/// reason) print `NAME<TAB>REASON`, NAME exactly as given and REASON with tabs and newlines
/// replaced by spaces. Blank lines are skipped. Returns how many cards were reported.
pub fn run_preflight(
    names: impl BufRead,
    mut out: impl Write,
    mut lacks: impl FnMut(&str) -> Option<String>,
) -> io::Result<usize> {
    let mut n = 0;
    for line in names.lines() {
        let line = line?;
        let name = line.strip_suffix('\r').unwrap_or(&line);
        if name.is_empty() {
            continue;
        }
        if let Some(reason) = lacks(name) {
            let reason: String = reason
                .chars()
                .map(|c| if matches!(c, '\t' | '\n' | '\r') { ' ' } else { c })
                .collect();
            writeln!(out, "{name}\t{reason}")?;
            n += 1;
        }
    }
    out.flush()?;
    Ok(n)
}
