// HealthStore.swift
//
// Wraps HKHealthStore behind a small, mockable surface. The detector
// pipeline calls `record(event:)` for every finalized SnoreEvent —
// when the user has the toggle on, the event is written as an
// `environmentalAudioExposure` quantity sample.
//
// Caveat called out in the consent copy AND in the sample metadata:
// the dB values from the Rust core are *uncalibrated*. We document
// that in the sample's metadata so a curious user can find it in the
// Health app's "Show All Data" view.
//
// This file deliberately defines its own protocol so RecorderViewModel
// depends on `HealthRecorder`, not on HKHealthStore directly. Tests
// inject a fake recorder.

import Foundation
import HealthKit
import OSLog

/// Surface RecorderViewModel + the Settings tab depend on. The
/// real implementation talks to HealthKit; the test fake just
/// records what was asked of it.
protocol HealthRecorder: AnyObject {
    /// `true` only on hardware that ships HealthKit — iPhone and
    /// iPad. Always `false` in tests, the simulator handles it.
    var isAvailable: Bool { get }

    /// Whether the user has granted *write* permission for our
    /// audio-exposure samples. Reads use a separate authorization;
    /// HealthKit deliberately doesn't tell apps the read state.
    var isWriteAuthorized: Bool { get }

    /// Trigger the system permission sheet. Resolves once the user
    /// dismisses it. Throws if HealthKit is unavailable.
    func requestAuthorization() async throws

    /// Record a single finalized snore event. No-op if HealthKit is
    /// unavailable or the user hasn't granted write permission.
    func record(event: SnoreEvent, sessionStartedAt: Date) async
}

enum HealthRecorderError: LocalizedError {
    case unavailable
    case authorizationDenied
    case underlying(Error)

    var errorDescription: String? {
        switch self {
        case .unavailable:
            return "HealthKit is not available on this device."
        case .authorizationDenied:
            return "Apple Health write permission was denied. " +
                   "Enable it in Settings → Health → Data Access & Devices → SnoreGuard."
        case .underlying(let err):
            return err.localizedDescription
        }
    }
}

/// Production HKHealthStore-backed recorder.
final class HealthStore: HealthRecorder {
    private let log = Logger(subsystem: "com.snoreguard.app", category: "HealthStore")
    private let store = HKHealthStore()

    /// Type written: noise during the event window in dB(A) SPL-equiv.
    /// Apple's `.environmentalAudioExposure` is a quantity measured
    /// in `dBASPL`, which is the closest match for our uncalibrated
    /// magnitude → dB output. The metadata flag below makes the lack
    /// of calibration explicit so anyone parsing this data downstream
    /// doesn't mistake it for a real SPL meter reading.
    private static let audioExposureType: HKQuantityType = {
        HKQuantityType(.environmentalAudioExposure)
    }()

    /// Type read: sleep stages, used by phase-5 analytics to correlate
    /// snore events with sleep state. Read access doesn't gate
    /// anything in this phase but we ask once during onboarding so
    /// the user isn't re-prompted later.
    private static let sleepType: HKCategoryType = {
        HKCategoryType(.sleepAnalysis)
    }()

    var isAvailable: Bool { HKHealthStore.isHealthDataAvailable() }

    var isWriteAuthorized: Bool {
        guard isAvailable else { return false }
        return store.authorizationStatus(for: Self.audioExposureType) == .sharingAuthorized
    }

    func requestAuthorization() async throws {
        guard isAvailable else { throw HealthRecorderError.unavailable }
        let writes: Set<HKSampleType> = [Self.audioExposureType]
        let reads: Set<HKObjectType> = [Self.sleepType]
        do {
            try await store.requestAuthorization(toShare: writes, read: reads)
        } catch {
            throw HealthRecorderError.underlying(error)
        }
    }

    func record(event: SnoreEvent, sessionStartedAt: Date) async {
        guard isAvailable, isWriteAuthorized else { return }
        // Reconstruct the absolute timestamps from the offsets the
        // detector reports.
        let start = sessionStartedAt.addingTimeInterval(TimeInterval(event.startMs) / 1000)
        let end = start.addingTimeInterval(TimeInterval(event.durationMs) / 1000)
        let unit = HKUnit(from: "dBASPL")
        let quantity = HKQuantity(unit: unit, doubleValue: Double(event.avgDB))

        let metadata: [String: Any] = [
            HKMetadataKeyExternalUUID: event.id.uuidString,
            "com.snoreguard.calibration": "uncalibrated-relative-db",
            "com.snoreguard.detector": "heuristic-fft-low-freq-dominance",
            "com.snoreguard.detector_version": "0.1.0",
        ]

        let sample = HKQuantitySample(
            type: Self.audioExposureType,
            quantity: quantity,
            start: start,
            end: end,
            metadata: metadata
        )

        do {
            try await store.save(sample)
        } catch {
            log.error("save audio-exposure sample: \(error.localizedDescription)")
        }
    }
}
