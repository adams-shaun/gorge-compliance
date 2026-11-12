//! Engine-neutral plumbing for gorge-compliance followers.
//!
//! A follower is a program `RUNNER IN.jsonl OUT.jsonl` that replays each request line against
//! some other rules engine and writes one result row per line, in input order. This crate holds
//! the parts every follower shares and nothing engine-specific:
//!
//! * [`request`]: the serde types for one input line,
//! * [`row`]: the serde types for one output row, plus the message-cap conventions,
//! * [`batch`]: the JSONL loop (malformed lines, panics, flushing, `request_sha`),
//! * [`preflight`]: the `RUNNER --preflight NAMES.txt` helper,
//! * [`refs`]: gorge's object-reference grammar and a ref-to-engine-id table,
//! * [`vocab`]: the snapshot vocabulary helpers (steps, colours, counters, zones).

pub mod batch;
pub mod preflight;
pub mod refs;
pub mod request;
pub mod row;
pub mod vocab;

pub use batch::{run_batch, BatchConfig, BatchStats, RowBuilder, Stop};
pub use preflight::run_preflight;
pub use refs::{ref_name, Ref, RefTable};
pub use request::Request;
pub use row::ResultRow;
