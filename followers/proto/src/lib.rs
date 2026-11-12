//! Engine-neutral plumbing for gorge-compliance followers.
//!
//! A follower is a program `RUNNER IN.jsonl OUT.jsonl` that replays each request line against
//! some other rules engine and writes one result row per line, in input order. This crate holds
//! the parts every follower shares and nothing engine-specific:
//!
//! * [`request`]: the serde types for one input line,
//! * [`row`]: the serde types for one output row, plus the message-cap conventions,
//! * [`batch`]: the JSONL loop (malformed lines, panics, flushing, `request_sha`),
//! * [`preflight`]: the `RUNNER --preflight NAMES.txt` helper.

pub mod batch;
pub mod preflight;
pub mod request;
pub mod row;

pub use batch::{run_batch, BatchConfig, BatchStats, RowBuilder, Stop};
pub use preflight::run_preflight;
pub use request::Request;
pub use row::ResultRow;
