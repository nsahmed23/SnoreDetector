//! Integration tests covering the C-ABI surface.
//!
//! These exercise the same lifecycle Swift will use: create → push
//! frames → poll events → free.

use std::ptr;

use snoreguard_core::ffi::{
    sg_detector_free, sg_detector_new, sg_detector_poll_event, sg_detector_push_frame,
    sg_detector_set_sensitivity, sg_detector_set_threshold, sg_frame_size, SgEvent, SgSensitivity,
    SgStatus,
};
use snoreguard_core::FRAME_SIZE;

fn silence() -> Vec<f32> {
    vec![0.0; FRAME_SIZE]
}

fn low_freq_tone(amplitude: f32) -> Vec<f32> {
    let mut buf = Vec::with_capacity(FRAME_SIZE);
    let mut phase = 0.0f32;
    let step = std::f32::consts::TAU * 100.0 / 16_000.0;
    for _ in 0..FRAME_SIZE {
        buf.push(amplitude * phase.sin());
        phase += step;
    }
    buf
}

#[test]
fn ffi_lifecycle_sanity() {
    unsafe {
        let d = sg_detector_new(35.0, SgSensitivity::High, 16_000);
        assert!(!d.is_null());

        let frame = silence();
        let status = sg_detector_push_frame(d, frame.as_ptr(), frame.len());
        assert_eq!(status, SgStatus::Ok as i32);

        let mut ev = SgEvent::default();
        let polled = sg_detector_poll_event(d, &mut ev);
        assert_eq!(polled, 0);

        sg_detector_free(d);
    }
}

#[test]
fn ffi_rejects_null_pointers() {
    unsafe {
        let frame = silence();
        let s = sg_detector_push_frame(ptr::null_mut(), frame.as_ptr(), frame.len());
        assert_eq!(s, SgStatus::NullPointer as i32);

        let mut ev = SgEvent::default();
        let s = sg_detector_poll_event(ptr::null_mut(), &mut ev);
        assert_eq!(s, SgStatus::NullPointer as i32);

        // sg_detector_free(NULL) must be a no-op (not a crash).
        sg_detector_free(ptr::null_mut());
    }
}

#[test]
fn ffi_rejects_bad_frame_length() {
    unsafe {
        let d = sg_detector_new(60.0, SgSensitivity::Medium, 16_000);
        let too_short = vec![0.0f32; 64];
        let status = sg_detector_push_frame(d, too_short.as_ptr(), too_short.len());
        assert_eq!(status, SgStatus::InvalidLength as i32);
        sg_detector_free(d);
    }
}

#[test]
fn ffi_rejects_zero_sample_rate() {
    let d = sg_detector_new(60.0, SgSensitivity::Medium, 0);
    assert!(d.is_null());
}

#[test]
fn ffi_setters_validate_inputs() {
    unsafe {
        let d = sg_detector_new(60.0, SgSensitivity::Medium, 16_000);
        assert_eq!(sg_detector_set_threshold(d, 50.0), SgStatus::Ok as i32);
        assert_eq!(
            sg_detector_set_threshold(d, f32::NAN),
            SgStatus::InvalidArgument as i32,
        );
        assert_eq!(
            sg_detector_set_sensitivity(d, SgSensitivity::Low),
            SgStatus::Ok as i32,
        );
        assert_eq!(
            sg_detector_set_threshold(ptr::null_mut(), 50.0),
            SgStatus::NullPointer as i32,
        );
        sg_detector_free(d);
    }
}

#[test]
fn ffi_emits_event_for_sustained_low_freq_input() {
    unsafe {
        let d = sg_detector_new(35.0, SgSensitivity::High, 16_000);
        for _ in 0..100 {
            let frame = low_freq_tone(0.9);
            let s = sg_detector_push_frame(d, frame.as_ptr(), frame.len());
            assert!(s >= 0, "push returned error: {s}");
        }
        for _ in 0..5 {
            let frame = silence();
            sg_detector_push_frame(d, frame.as_ptr(), frame.len());
        }
        let mut ev = SgEvent::default();
        let polled = sg_detector_poll_event(d, &mut ev);
        assert_eq!(polled, 1, "expected an event");
        assert!(ev.duration_ms > 0);
        sg_detector_free(d);
    }
}

#[test]
fn frame_size_constant_matches() {
    assert_eq!(sg_frame_size(), FRAME_SIZE);
}
