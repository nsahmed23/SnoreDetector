// SnoreCore.swift
//
// Swift wrapper around the C ABI exposed by `rust-core/snoreguard-core`.
// The xcframework ships a `module.modulemap` that names the module
// `SnoreGuardCore`, so all the `sg_*` symbols and `Sg*` types are
// available after `import SnoreGuardCore`.
//
// Threading: the Rust detector is NOT Sync. A single SnoreCore must
// be confined to one thread or one serial queue. The expected pattern
// is the AVAudioEngine tap thread pushing frames; the UI thread
// polling events on a 30 Hz timer.
//
// Memory: SnoreCore owns the Rust-allocated detector and frees it in
// `deinit` via `sg_detector_free`. Never touch `pointer` from outside
// this class.

import Foundation
import SnoreGuardCore

/// Errors thrown by `SnoreCore`. Mirrors the negative `SgStatus` codes
/// from the Rust ABI plus a Swift-side allocation-failure case.
enum SnoreCoreError: Error, Equatable {
    /// `sg_detector_new` returned NULL — the only documented cause is
    /// a zero `sample_rate_hz`.
    case allocationFailed
    /// FFI call received a NULL pointer where a valid one was required.
    case nullPointer
    /// Pushed a frame whose length wasn't `FRAME_SIZE`.
    case invalidLength(expected: Int, got: Int)
    /// Setter rejected an out-of-range argument.
    case invalidArgument
    /// FFI returned a status the Swift layer doesn't know about. Should
    /// never happen unless the Rust ABI gains new error codes.
    case unknown(code: Int32)
}

/// Mirrors `SgSensitivity` from the Rust ABI.
enum Sensitivity: UInt8, CaseIterable, Identifiable {
    case low = 0, medium = 1, high = 2

    var id: UInt8 { rawValue }

    /// Display label used by SettingsView. The trailing description
    /// matches the language used in `useAudioMonitor.ts` so behaviour
    /// stays consistent with what users learned in the web prototype.
    var label: String {
        switch self {
        case .low: return "Low (strictest)"
        case .medium: return "Medium"
        case .high: return "High (loosest)"
        }
    }
}

/// One snore-like event finalized by the detector. Mirrors the
/// `SgEvent` C struct field-for-field.
struct SnoreEvent: Identifiable, Equatable, Hashable {
    /// Locally-generated UUID — Apple's Identifiable contract; the
    /// detector itself doesn't assign IDs.
    let id: UUID = UUID()
    /// Milliseconds from detector start (`sg_detector_new`) to the
    /// frame that started this event.
    let startMs: UInt64
    /// Event duration in milliseconds.
    let durationMs: UInt32
    /// Average uncalibrated dB across the event window. NOT calibrated SPL.
    let avgDB: Float
}

/// Thin Swift façade around `sg_detector_*`. Construct it with the
/// configured threshold + sensitivity, push 256-sample mono Float32
/// frames at 16 kHz, and poll for events on the UI thread.
final class SnoreCore {
    private var pointer: OpaquePointer
    private(set) var sampleRate: UInt32

    /// Frame size required by `pushFrame(_:)`. Mirrors `FRAME_SIZE` in
    /// the Rust crate; exposed here so callers don't have to import
    /// the C constant directly.
    static var frameSize: Int { Int(sg_frame_size()) }

    init(thresholdDB: Float, sensitivity: Sensitivity, sampleRate: UInt32 = 16_000) throws {
        guard let p = sg_detector_new(thresholdDB, SgSensitivity(sensitivity.rawValue), sampleRate) else {
            throw SnoreCoreError.allocationFailed
        }
        self.pointer = p
        self.sampleRate = sampleRate
    }

    deinit {
        sg_detector_free(pointer)
    }

    /// Push exactly `SnoreCore.frameSize` samples. Returns `true` when
    /// an event was just finalized and should be drained via `pollEvent()`.
    @discardableResult
    func pushFrame(_ samples: [Float]) throws -> Bool {
        guard samples.count == SnoreCore.frameSize else {
            throw SnoreCoreError.invalidLength(expected: SnoreCore.frameSize, got: samples.count)
        }
        return try samples.withUnsafeBufferPointer { buf -> Bool in
            let status = sg_detector_push_frame(pointer, buf.baseAddress, buf.count)
            switch status {
            case Int32(SgStatus.Ok.rawValue):
                return false
            case Int32(SgStatus.EventReady.rawValue):
                return true
            case Int32(SgStatus.NullPointer.rawValue):
                throw SnoreCoreError.nullPointer
            case Int32(SgStatus.InvalidLength.rawValue):
                throw SnoreCoreError.invalidLength(expected: SnoreCore.frameSize, got: samples.count)
            case Int32(SgStatus.InvalidArgument.rawValue):
                throw SnoreCoreError.invalidArgument
            default:
                throw SnoreCoreError.unknown(code: status)
            }
        }
    }

    /// Drain at most one event. Call this after `pushFrame` returns
    /// `true`, or unconditionally on a UI timer.
    func pollEvent() -> SnoreEvent? {
        var raw = SgEvent()
        let polled = sg_detector_poll_event(pointer, &raw)
        guard polled == 1 else { return nil }
        return SnoreEvent(startMs: raw.start_ms, durationMs: raw.duration_ms, avgDB: raw.avg_db)
    }

    /// Update threshold without rebuilding the detector. Throws on NaN
    /// or infinity (the Rust side's only documented reject case).
    func setThreshold(_ db: Float) throws {
        let status = sg_detector_set_threshold(pointer, db)
        try Self.translateStatus(status)
    }

    /// Update sensitivity without rebuilding the detector.
    func setSensitivity(_ s: Sensitivity) throws {
        let status = sg_detector_set_sensitivity(pointer, SgSensitivity(s.rawValue))
        try Self.translateStatus(status)
    }

    private static func translateStatus(_ status: Int32) throws {
        switch status {
        case Int32(SgStatus.Ok.rawValue):
            return
        case Int32(SgStatus.NullPointer.rawValue):
            throw SnoreCoreError.nullPointer
        case Int32(SgStatus.InvalidArgument.rawValue):
            throw SnoreCoreError.invalidArgument
        default:
            throw SnoreCoreError.unknown(code: status)
        }
    }
}
