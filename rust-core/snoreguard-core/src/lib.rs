//! SnoreGuard detection core.
//!
//! A pure heuristic detector — no machine-learning model. The algorithm
//! performs a real FFT on each pushed audio frame, then flags the frame
//! as "snore-like" when (a) the frame's volume crosses the configured
//! threshold and (b) the lower portion of the spectrum dominates the
//! upper portion by a sensitivity-controlled ratio. An event is emitted
//! once that condition holds for a sustained number of consecutive
//! frames.
//!
//! Mirrors the prototype's `useAudioMonitor` hook in `src/hooks/`.

#![allow(clippy::missing_safety_doc)]

mod detector;
pub mod ffi;

pub use detector::{Detector, DetectorConfig, Event, Sensitivity};

pub const FRAME_SIZE: usize = 256;

/// Default sustained-frame count before an event starts. The web
/// prototype uses 15 frames at ~60 fps (≈250 ms).
pub const DEFAULT_SUSTAIN_FRAMES: u32 = 15;
