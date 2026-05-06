//! C-ABI surface called by Swift via the bridging header.
//!
//! Conventions:
//! - Opaque pointer: `*mut SgDetector` is owned by the caller, freed
//!   exclusively via [`sg_detector_free`].
//! - Threading: a single detector is **not** `Sync`. The caller must
//!   confine all calls to one thread (or serialize via a queue/actor).
//! - Error codes: integer return codes — 0/positive means success,
//!   negative means error. See the `SG_STATUS_*` constants.

use std::ptr;
use std::slice;

use crate::detector::{Detector, DetectorConfig, Sensitivity};
use crate::FRAME_SIZE;

/// Opaque handle. Swift sees this as a forward-declared struct and
/// only ever holds it via pointer.
pub struct SgDetector {
    _private: [u8; 0],
}

#[inline]
unsafe fn as_detector<'a>(p: *mut SgDetector) -> &'a mut Detector {
    &mut *(p as *mut Detector)
}

#[repr(u8)]
#[derive(Clone, Copy)]
pub enum SgSensitivity {
    Low = 0,
    Medium = 1,
    High = 2,
}

impl From<SgSensitivity> for Sensitivity {
    fn from(s: SgSensitivity) -> Self {
        match s {
            SgSensitivity::Low => Sensitivity::Low,
            SgSensitivity::Medium => Sensitivity::Medium,
            SgSensitivity::High => Sensitivity::High,
        }
    }
}

#[repr(C)]
#[derive(Clone, Copy, Default)]
pub struct SgEvent {
    pub start_ms: u64,
    pub duration_ms: u32,
    pub avg_db: f32,
}

// Status surface. Exported as plain `i32` constants rather than a
// `#[repr(i32)]` Rust enum so cbindgen emits them as preprocessor
// `#define`s. That form imports cleanly into Swift as unambiguous
// `Int32` constants — a Rust enum becomes a Swift typealias plus
// top-level globals (`Ok`, `EventReady`, …) which collide with anything
// else in scope and don't support member access.
pub const SG_STATUS_OK: i32 = 0;
pub const SG_STATUS_EVENT_READY: i32 = 1;
pub const SG_STATUS_NULL_POINTER: i32 = -1;
pub const SG_STATUS_INVALID_LENGTH: i32 = -2;
pub const SG_STATUS_INVALID_ARGUMENT: i32 = -3;

/// Construct a detector. Returns NULL on invalid arguments
/// (`sample_rate_hz == 0`, or `threshold_db` non-finite) or if the
/// underlying allocation fails.
#[no_mangle]
pub extern "C" fn sg_detector_new(
    threshold_db: f32,
    sensitivity: SgSensitivity,
    sample_rate_hz: u32,
) -> *mut SgDetector {
    if sample_rate_hz == 0 {
        return ptr::null_mut();
    }
    if !threshold_db.is_finite() {
        return ptr::null_mut();
    }
    let cfg = DetectorConfig {
        threshold_db,
        sensitivity: sensitivity.into(),
        sample_rate_hz,
    };
    Box::into_raw(Box::new(Detector::new(cfg))) as *mut SgDetector
}

/// Free a detector previously returned by [`sg_detector_new`].
/// Calling with NULL is a no-op.
#[no_mangle]
pub unsafe extern "C" fn sg_detector_free(d: *mut SgDetector) {
    if d.is_null() {
        return;
    }
    drop(Box::from_raw(d as *mut Detector));
}

/// Push one audio frame. `samples` must point to exactly
/// [`crate::FRAME_SIZE`] (256) `f32` values.
///
/// Returns:
/// - `SG_STATUS_EVENT_READY` (1) if a new event was finalized — the
///   caller should immediately call [`sg_detector_poll_event`] to read
///   it before pushing more frames.
/// - `SG_STATUS_OK` (0) on a successful push without an event.
/// - Negative `SG_STATUS_*` on input error.
#[no_mangle]
pub unsafe extern "C" fn sg_detector_push_frame(
    d: *mut SgDetector,
    samples: *const f32,
    len: usize,
) -> i32 {
    if d.is_null() || samples.is_null() {
        return SG_STATUS_NULL_POINTER;
    }
    if len != FRAME_SIZE {
        return SG_STATUS_INVALID_LENGTH;
    }
    let det = as_detector(d);
    let buf = slice::from_raw_parts(samples, len);
    if det.push_frame(buf) {
        SG_STATUS_EVENT_READY
    } else {
        SG_STATUS_OK
    }
}

/// Drain the most-recent finalized event, if any.
///
/// Returns 1 and writes into `*out` if an event was available, 0 if
/// none, negative on error.
#[no_mangle]
pub unsafe extern "C" fn sg_detector_poll_event(d: *mut SgDetector, out: *mut SgEvent) -> i32 {
    if d.is_null() || out.is_null() {
        return SG_STATUS_NULL_POINTER;
    }
    let det = as_detector(d);
    match det.take_event() {
        Some(ev) => {
            *out = SgEvent {
                start_ms: ev.start_ms,
                duration_ms: ev.duration_ms,
                avg_db: ev.avg_db,
            };
            1
        }
        None => 0,
    }
}

/// Flush any in-flight event. Call when the caller stops pushing
/// frames (e.g. user stopped recording mid-snore) so the active event
/// isn't silently dropped.
///
/// Returns 1 and writes the event into `*out` when one was in flight;
/// 0 when the detector was idle; negative `SG_STATUS_*` on error.
/// Mirrors the convention of [`sg_detector_poll_event`].
#[no_mangle]
pub unsafe extern "C" fn sg_detector_finish(d: *mut SgDetector, out: *mut SgEvent) -> i32 {
    if d.is_null() || out.is_null() {
        return SG_STATUS_NULL_POINTER;
    }
    let det = as_detector(d);
    match det.finish() {
        Some(ev) => {
            *out = SgEvent {
                start_ms: ev.start_ms,
                duration_ms: ev.duration_ms,
                avg_db: ev.avg_db,
            };
            1
        }
        None => 0,
    }
}

#[no_mangle]
pub unsafe extern "C" fn sg_detector_set_threshold(d: *mut SgDetector, threshold_db: f32) -> i32 {
    if d.is_null() {
        return SG_STATUS_NULL_POINTER;
    }
    if !threshold_db.is_finite() {
        return SG_STATUS_INVALID_ARGUMENT;
    }
    as_detector(d).set_threshold_db(threshold_db);
    SG_STATUS_OK
}

#[no_mangle]
pub unsafe extern "C" fn sg_detector_set_sensitivity(
    d: *mut SgDetector,
    sensitivity: SgSensitivity,
) -> i32 {
    if d.is_null() {
        return SG_STATUS_NULL_POINTER;
    }
    as_detector(d).set_sensitivity(sensitivity.into());
    SG_STATUS_OK
}

/// Frame size required by [`sg_detector_push_frame`].
#[no_mangle]
pub extern "C" fn sg_frame_size() -> usize {
    FRAME_SIZE
}
