//! C-ABI surface called by Swift via the bridging header.
//!
//! Conventions:
//! - Opaque pointer: `*mut SgDetector` is owned by the caller, freed
//!   exclusively via [`sg_detector_free`].
//! - Threading: a single detector is **not** `Sync`. The caller must
//!   confine all calls to one thread (or serialize via a queue/actor).
//! - Error codes: integer return codes — 0/positive means success,
//!   negative means error. See [`SgStatus`].

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

#[repr(i32)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SgStatus {
    Ok = 0,
    EventReady = 1,
    NullPointer = -1,
    InvalidLength = -2,
    InvalidArgument = -3,
}

/// Construct a detector. Returns NULL only if the allocation fails
/// (rustfft planner construction is the only fallible step).
#[no_mangle]
pub extern "C" fn sg_detector_new(
    threshold_db: f32,
    sensitivity: SgSensitivity,
    sample_rate_hz: u32,
) -> *mut SgDetector {
    if sample_rate_hz == 0 {
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
/// - [`SgStatus::EventReady`] (1) if a new event was finalized — the
///   caller should immediately call [`sg_detector_poll_event`] to read
///   it before pushing more frames.
/// - [`SgStatus::Ok`] (0) on a successful push without an event.
/// - Negative [`SgStatus`] on input error.
#[no_mangle]
pub unsafe extern "C" fn sg_detector_push_frame(
    d: *mut SgDetector,
    samples: *const f32,
    len: usize,
) -> i32 {
    if d.is_null() || samples.is_null() {
        return SgStatus::NullPointer as i32;
    }
    if len != FRAME_SIZE {
        return SgStatus::InvalidLength as i32;
    }
    let det = as_detector(d);
    let buf = slice::from_raw_parts(samples, len);
    if det.push_frame(buf) {
        SgStatus::EventReady as i32
    } else {
        SgStatus::Ok as i32
    }
}

/// Drain the most-recent finalized event, if any.
///
/// Returns 1 and writes into `*out` if an event was available, 0 if
/// none, negative on error.
#[no_mangle]
pub unsafe extern "C" fn sg_detector_poll_event(d: *mut SgDetector, out: *mut SgEvent) -> i32 {
    if d.is_null() || out.is_null() {
        return SgStatus::NullPointer as i32;
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

#[no_mangle]
pub unsafe extern "C" fn sg_detector_set_threshold(d: *mut SgDetector, threshold_db: f32) -> i32 {
    if d.is_null() {
        return SgStatus::NullPointer as i32;
    }
    if !threshold_db.is_finite() {
        return SgStatus::InvalidArgument as i32;
    }
    as_detector(d).set_threshold_db(threshold_db);
    SgStatus::Ok as i32
}

#[no_mangle]
pub unsafe extern "C" fn sg_detector_set_sensitivity(
    d: *mut SgDetector,
    sensitivity: SgSensitivity,
) -> i32 {
    if d.is_null() {
        return SgStatus::NullPointer as i32;
    }
    as_detector(d).set_sensitivity(sensitivity.into());
    SgStatus::Ok as i32
}

/// Frame size required by [`sg_detector_push_frame`].
#[no_mangle]
pub extern "C" fn sg_frame_size() -> usize {
    FRAME_SIZE
}
