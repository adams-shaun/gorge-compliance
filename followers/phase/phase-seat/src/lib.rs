//! phase-seat: phase.rs (github.com/phase-rs/phase, MIT OR Apache-2.0, pinned by `../PHASE_PIN`) as a
//! SpellBench v2 engine. The protocol layer ([`json`], [`server`], [`semantic`], [`observe`]) does not
//! link the engine and is tested without it ([`house`] parses `--house-seat`); the game host ([`waiting`], [`view`], [`game`]) is the
//! `phase` feature (on by default).

pub mod json;
pub mod observe;
pub mod semantic;
pub mod server;

#[cfg(feature = "phase")]
#[path = "../../phase-follower/src/names.rs"]
pub mod names;
#[cfg(feature = "phase")]
#[path = "../../phase-follower/src/support.rs"]
pub mod support;
#[cfg(feature = "phase")]
pub mod game;
#[cfg(feature = "phase")]
pub mod view;
#[cfg(feature = "phase")]
pub mod waiting;

pub mod house;
