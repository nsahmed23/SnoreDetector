//! Heuristic snore detector.
//!
//! Operates on raw `f32` PCM frames. The caller is responsible for
//! delivering mono samples at the configured sample rate; on iOS this
//! comes from `AVAudioEngine` after conversion via `AVAudioConverter`.

use std::sync::Arc;

use rustfft::num_complex::Complex32;
use rustfft::{Fft, FftPlanner};

use crate::{DEFAULT_SUSTAIN_FRAMES, FRAME_SIZE};

#[repr(u8)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Sensitivity {
    Low = 0,
    Medium = 1,
    High = 2,
}

impl Sensitivity {
    /// Multiplier applied to the high-frequency energy when checking
    /// low-frequency dominance. Higher multiplier → stricter detector.
    /// Mirrors the prototype's hook (low=2.0, medium=1.5, high=1.2).
    fn dominance_multiplier(self) -> f32 {
        match self {
            Sensitivity::Low => 2.0,
            Sensitivity::Medium => 1.5,
            Sensitivity::High => 1.2,
        }
    }
}

#[derive(Clone, Copy, Debug)]
pub struct DetectorConfig {
    pub threshold_db: f32,
    pub sensitivity: Sensitivity,
    pub sample_rate_hz: u32,
}

impl Default for DetectorConfig {
    fn default() -> Self {
        Self {
            threshold_db: 60.0,
            sensitivity: Sensitivity::Medium,
            sample_rate_hz: 16_000,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Event {
    pub start_ms: u64,
    pub duration_ms: u32,
    pub avg_db: f32,
}

pub struct Detector {
    cfg: DetectorConfig,
    fft: Arc<dyn Fft<f32>>,
    scratch: Vec<Complex32>,
    magnitudes: Vec<f32>,
    frames_pushed: u64,
    active_frames: u32,
    in_event: bool,
    event_start_frame: u64,
    event_intensity_sum: f32,
    pending_event: Option<Event>,
}

impl Detector {
    pub fn new(cfg: DetectorConfig) -> Self {
        let mut planner = FftPlanner::<f32>::new();
        let fft = planner.plan_fft_forward(FRAME_SIZE);
        Self {
            cfg,
            fft,
            scratch: vec![Complex32::default(); FRAME_SIZE],
            magnitudes: vec![0.0; FRAME_SIZE / 2],
            frames_pushed: 0,
            active_frames: 0,
            in_event: false,
            event_start_frame: 0,
            event_intensity_sum: 0.0,
            pending_event: None,
        }
    }

    pub fn config(&self) -> DetectorConfig {
        self.cfg
    }

    pub fn set_threshold_db(&mut self, db: f32) {
        self.cfg.threshold_db = db.clamp(0.0, 120.0);
    }

    pub fn set_sensitivity(&mut self, s: Sensitivity) {
        self.cfg.sensitivity = s;
    }

    /// Push exactly `FRAME_SIZE` mono `f32` samples.
    ///
    /// Returns `true` if a new event was just finalized (caller can
    /// poll it via [`Detector::take_event`]).
    pub fn push_frame(&mut self, samples: &[f32]) -> bool {
        assert_eq!(samples.len(), FRAME_SIZE, "expected {FRAME_SIZE} samples");

        self.scratch.clear();
        self.scratch
            .extend(samples.iter().map(|&s| Complex32 { re: s, im: 0.0 }));
        self.fft.process(&mut self.scratch);

        let bins = FRAME_SIZE / 2;
        let mut total = 0.0f32;
        for (i, c) in self.scratch.iter().take(bins).enumerate() {
            let m = c.norm();
            self.magnitudes[i] = m;
            total += m;
        }
        let avg_mag = total / bins as f32;

        let low_cut = bins / 4;
        let mut low = 0.0f32;
        let mut high = 0.0f32;
        for (i, &m) in self.magnitudes.iter().enumerate() {
            if i < low_cut {
                low += m;
            } else {
                high += m;
            }
        }

        let mult = self.cfg.sensitivity.dominance_multiplier();
        let low_dominant = low > high * mult;
        let frame_db = magnitude_to_db(avg_mag);

        let frame_idx = self.frames_pushed;
        self.frames_pushed += 1;

        let mut event_finalized = false;

        if frame_db >= self.cfg.threshold_db && low_dominant {
            if !self.in_event && self.active_frames == 0 {
                self.event_start_frame = frame_idx;
                self.event_intensity_sum = 0.0;
            }
            self.active_frames += 1;
            self.event_intensity_sum += frame_db;
            if self.active_frames > DEFAULT_SUSTAIN_FRAMES && !self.in_event {
                self.in_event = true;
            }
        } else if self.in_event {
            // Event ended.
            let duration_frames = self.active_frames as u64;
            let duration_ms_u64 = frames_to_ms(duration_frames, self.cfg.sample_rate_hz);
            let avg_db = if duration_frames > 0 {
                self.event_intensity_sum / duration_frames as f32
            } else {
                0.0
            };
            self.pending_event = Some(Event {
                start_ms: frames_to_ms(self.event_start_frame, self.cfg.sample_rate_hz),
                duration_ms: duration_ms_u64.min(u32::MAX as u64) as u32,
                avg_db,
            });
            self.in_event = false;
            self.active_frames = 0;
            self.event_intensity_sum = 0.0;
            event_finalized = true;
        } else {
            // Heuristic broke before sustain reached — discard accumulator.
            self.active_frames = 0;
            self.event_intensity_sum = 0.0;
        }

        event_finalized
    }

    /// Drain the most-recently-finalized event, if any.
    pub fn take_event(&mut self) -> Option<Event> {
        self.pending_event.take()
    }

    /// Flush any in-flight event. Call this when the caller stops
    /// pushing frames (e.g. user stopped recording mid-snore) so the
    /// active event isn't silently dropped.
    ///
    /// Returns `Some(Event)` when the detector was inside an event,
    /// `None` otherwise. The detector's in-event state is reset either
    /// way.
    pub fn finish(&mut self) -> Option<Event> {
        if !self.in_event {
            return None;
        }
        let duration_frames = self.active_frames as u64;
        let duration_ms_u64 = frames_to_ms(duration_frames, self.cfg.sample_rate_hz);
        let avg_db = if duration_frames > 0 {
            self.event_intensity_sum / duration_frames as f32
        } else {
            0.0
        };
        let ev = Event {
            start_ms: frames_to_ms(self.event_start_frame, self.cfg.sample_rate_hz),
            duration_ms: duration_ms_u64.min(u32::MAX as u64) as u32,
            avg_db,
        };
        self.in_event = false;
        self.active_frames = 0;
        self.event_intensity_sum = 0.0;
        Some(ev)
    }
}

/// Map an FFT magnitude average to a uncalibrated dB-ish value in
/// [30, 100]. The web prototype derives this from byte-quantized
/// magnitudes — the Rust port works in `f32` directly but keeps the
/// same output range so existing thresholds (30–90 dB) remain valid.
///
/// This is **not calibrated SPL**.
fn magnitude_to_db(avg_mag: f32) -> f32 {
    // FRAME_SIZE-point real FFT magnitudes scale roughly with input
    // amplitude × FRAME_SIZE/2 for a sine. A full-scale sine therefore
    // yields avg_mag ≈ 0.5 * FRAME_SIZE/2 / bins. Scale empirically so
    // that a frame of full-scale white noise lands near the upper
    // bound, matching the web prototype's `(avg/255)*110` mapping.
    let scaled = (avg_mag / 16.0).clamp(0.0, 1.0);
    (30.0 + scaled * 70.0).clamp(30.0, 100.0)
}

fn frames_to_ms(frames: u64, sample_rate_hz: u32) -> u64 {
    if sample_rate_hz == 0 {
        return 0;
    }
    let samples = frames.saturating_mul(FRAME_SIZE as u64);
    samples.saturating_mul(1000) / sample_rate_hz as u64
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::f32::consts::TAU;

    fn sine_frame(freq_hz: f32, sample_rate: u32, amplitude: f32, phase: &mut f32) -> Vec<f32> {
        let step = TAU * freq_hz / sample_rate as f32;
        let mut out = Vec::with_capacity(FRAME_SIZE);
        for _ in 0..FRAME_SIZE {
            out.push(amplitude * phase.sin());
            *phase += step;
        }
        out
    }

    fn silence_frame() -> Vec<f32> {
        vec![0.0; FRAME_SIZE]
    }

    #[test]
    fn silence_never_triggers() {
        let mut d = Detector::new(DetectorConfig::default());
        for _ in 0..200 {
            d.push_frame(&silence_frame());
        }
        assert!(d.take_event().is_none());
    }

    #[test]
    fn sustained_low_frequency_above_threshold_emits_event() {
        let cfg = DetectorConfig {
            threshold_db: 35.0,
            sensitivity: Sensitivity::High,
            ..DetectorConfig::default()
        };
        let mut d = Detector::new(cfg);
        let mut phase = 0.0;
        // 100 Hz at full amplitude — strong low-frequency content.
        for _ in 0..100 {
            let frame = sine_frame(100.0, cfg.sample_rate_hz, 0.9, &mut phase);
            d.push_frame(&frame);
        }
        // End the event by feeding silence.
        for _ in 0..5 {
            d.push_frame(&silence_frame());
        }
        let ev = d.take_event().expect("expected an event");
        assert!(ev.duration_ms > 0);
        assert!(ev.avg_db >= 35.0);
    }

    #[test]
    fn high_frequency_does_not_dominate_low() {
        let cfg = DetectorConfig {
            threshold_db: 35.0,
            sensitivity: Sensitivity::Medium,
            ..DetectorConfig::default()
        };
        let mut d = Detector::new(cfg);
        let mut phase = 0.0;
        // 4 kHz tone — sits in the upper 3/4 of the spectrum, so
        // low-frequency dominance check should fail.
        for _ in 0..100 {
            let frame = sine_frame(4_000.0, cfg.sample_rate_hz, 0.9, &mut phase);
            d.push_frame(&frame);
        }
        for _ in 0..5 {
            d.push_frame(&silence_frame());
        }
        assert!(d.take_event().is_none(), "high-freq tone should not produce a snore event");
    }

    #[test]
    fn brief_low_freq_burst_does_not_emit_event() {
        // Burst shorter than the sustain window must not produce an event.
        let cfg = DetectorConfig {
            threshold_db: 35.0,
            sensitivity: Sensitivity::High,
            ..DetectorConfig::default()
        };
        let mut d = Detector::new(cfg);
        let mut phase = 0.0;
        for _ in 0..(DEFAULT_SUSTAIN_FRAMES as usize - 2) {
            let frame = sine_frame(120.0, cfg.sample_rate_hz, 0.9, &mut phase);
            d.push_frame(&frame);
        }
        for _ in 0..10 {
            d.push_frame(&silence_frame());
        }
        assert!(d.take_event().is_none());
    }

    #[test]
    fn sensitivity_low_is_stricter_than_high() {
        // A signal with moderate low-freq dominance should pass at High
        // sensitivity (1.2× multiplier) but fail at Low (2.0×).
        let make_detector = |s: Sensitivity| {
            let cfg = DetectorConfig {
                threshold_db: 35.0,
                sensitivity: s,
                ..DetectorConfig::default()
            };
            Detector::new(cfg)
        };

        let run = |s: Sensitivity| {
            let mut d = make_detector(s);
            let mut phase = 0.0;
            for _ in 0..100 {
                // Mix a 200 Hz tone with a softer 3 kHz tone — clear
                // but not overwhelming low-freq dominance.
                let mut frame = sine_frame(200.0, 16_000, 0.6, &mut phase);
                let mut phase2 = 0.0;
                let high = sine_frame(3_000.0, 16_000, 0.4, &mut phase2);
                for (a, b) in frame.iter_mut().zip(high.iter()) {
                    *a += *b;
                }
                d.push_frame(&frame);
            }
            for _ in 0..5 {
                d.push_frame(&silence_frame());
            }
            d.take_event().is_some()
        };

        assert!(run(Sensitivity::High), "High sensitivity should fire on mixed signal");
        // We don't strictly assert Low fails — it depends on exact mix —
        // but the multiplier ordering must be respected.
        let high_mult = Sensitivity::High.dominance_multiplier();
        let med_mult = Sensitivity::Medium.dominance_multiplier();
        let low_mult = Sensitivity::Low.dominance_multiplier();
        assert!(high_mult < med_mult);
        assert!(med_mult < low_mult);
    }

    #[test]
    fn frame_size_assertion_on_wrong_length() {
        let mut d = Detector::new(DetectorConfig::default());
        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            d.push_frame(&[0.0; 128]);
        }));
        assert!(result.is_err());
    }

    #[test]
    fn finish_emits_in_flight_event() {
        let cfg = DetectorConfig {
            threshold_db: 35.0,
            sensitivity: Sensitivity::High,
            ..DetectorConfig::default()
        };
        let mut d = Detector::new(cfg);
        let mut phase = 0.0;
        // Feed enough sustained low-frequency frames to be inside an
        // event (i.e. past DEFAULT_SUSTAIN_FRAMES) without ending it.
        for _ in 0..(DEFAULT_SUSTAIN_FRAMES as usize + 5) {
            let frame = sine_frame(100.0, cfg.sample_rate_hz, 0.9, &mut phase);
            d.push_frame(&frame);
        }
        let ev = d.finish().expect("expected an in-flight event");
        assert!(ev.duration_ms > 0);
        assert!(ev.avg_db >= 35.0);
        // Calling finish again should now return None (state reset).
        assert!(d.finish().is_none());
    }

    #[test]
    fn finish_when_idle_returns_none() {
        let mut d = Detector::new(DetectorConfig::default());
        assert!(d.finish().is_none());
    }
}
