//! The `--house-seat` / `--house-difficulty` command line, as a pure function (no engine): the binary
//! parses it before it loads anything, and it is tested under `--no-default-features`.

/// The difficulty names phase-ai accepts (`phase_ai::config::ACCEPTED_DIFFICULTY_LABELS`), lower case.
pub const DIFFICULTIES: [&str; 6] = ["veryeasy", "easy", "medium", "hard", "veryhard", "cedh"];

/// The default house difficulty.
pub const DEFAULT_DIFFICULTY: &str = "medium";

/// Reads `--house-seat p0|p1` and `--house-difficulty NAME` out of `args` (anything else is ignored).
/// `Ok(None)` when no seat is asked for (a difficulty alone is an error: it names nothing to answer).
/// The difficulty comes back lower-cased. An unknown seat or difficulty, a repeated flag or a flag with
/// no value is an `Err` naming it.
pub fn parse_house(args: &[String]) -> Result<Option<(u8, String)>, String> {
    let (mut seat, mut diff): (Option<u8>, Option<String>) = (None, None);
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--house-seat" => {
                let v = it.next().ok_or("--house-seat needs p0 or p1")?;
                let s = match v.as_str() {
                    "p0" => 0,
                    "p1" => 1,
                    _ => return Err(format!("--house-seat {v}: want p0 or p1")),
                };
                if seat.replace(s).is_some() {
                    return Err("--house-seat given twice".into());
                }
            }
            "--house-difficulty" => {
                let v = it.next().ok_or("--house-difficulty needs a name")?.trim().to_lowercase();
                if !DIFFICULTIES.contains(&v.as_str()) {
                    return Err(format!("--house-difficulty {v}: want one of {}", DIFFICULTIES.join(", ")));
                }
                if diff.replace(v).is_some() {
                    return Err("--house-difficulty given twice".into());
                }
            }
            _ => {}
        }
    }
    match (seat, diff) {
        (None, None) => Ok(None),
        (None, Some(_)) => Err("--house-difficulty needs --house-seat".into()),
        (Some(s), d) => Ok(Some((s, d.unwrap_or_else(|| DEFAULT_DIFFICULTY.to_string())))),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn v(a: &[&str]) -> Vec<String> {
        a.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn parse_house_accepts_p0_p1_and_rejects_p2() {
        assert_eq!(parse_house(&v(&["--card-data", "x"])), Ok(None));
        assert_eq!(parse_house(&v(&["--house-seat", "p0"])), Ok(Some((0, "medium".into()))));
        assert_eq!(parse_house(&v(&["--house-seat", "p1", "--house-difficulty", "Hard"])), Ok(Some((1, "hard".into()))));
        assert_eq!(parse_house(&v(&["--house-difficulty", "CEDH", "--house-seat", "p1"])), Ok(Some((1, "cedh".into()))));
        assert!(parse_house(&v(&["--house-seat", "p2"])).is_err());
        assert!(parse_house(&v(&["--house-seat"])).is_err());
        assert!(parse_house(&v(&["--house-seat", "p0", "--house-seat", "p1"])).is_err());
        assert!(parse_house(&v(&["--house-seat", "p0", "--house-difficulty", "godlike"])).is_err());
        assert!(parse_house(&v(&["--house-difficulty", "easy"])).is_err());
    }
}
