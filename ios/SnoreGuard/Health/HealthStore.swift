// HealthStore.swift
//
// Wraps HKHealthStore behind a small, mockable surface. Conforms to
// `HealthKitClient` (preferred new surface, used by RecorderViewModel
// + SettingsTabView) and to the legacy `HealthRecorder` protocol so
// the existing test fakes continue to work.
//
// The Phase C upgrade splits authorization into independent read +
// write requests so each of the three Settings toggles only asks for
// the scope it needs. Per the production-readiness scope, SnoreGuard
// never infers REM/core/deep sleep stages from microphone data —
// session writes always go to Apple Health as `.inBed`.
//
// Sample metadata (every write):
//   estimated         = "true"
//   uncalibrated      = "true"
//   not_diagnostic    = "true"
//   source            = "snoreguard"
//   HKMetadataKeyExternalUUID  (idempotency key)
// Plus, for the sound-level sample only:
//   detector_version  (Bundle short version string)
//   event_type        = "snore_like_event"

import Foundation
import HealthKit
import OSLog

/// Legacy surface from PR #5 — kept for back-compat with existing
/// fakes. New consumers should use `HealthKitClient` instead.
protocol HealthRecorder: AnyObject {
    var isAvailable: Bool { get }
    var isWriteAuthorized: Bool { get }
    func requestAuthorization() async throws
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
            return "Apple Health permission was denied. " +
                   "Enable it in Settings → Health → Data Access & Devices → SnoreGuard."
        case .underlying(let err):
            return err.localizedDescription
        }
    }
}

/// Shared metadata keys used by the two write paths. Strings are
/// duplicated in the DISCLAIMER and in the test assertions, so they
/// live as constants here.
enum HealthMetadataKey {
    static let estimated      = "estimated"
    static let uncalibrated   = "uncalibrated"
    static let notDiagnostic  = "not_diagnostic"
    static let source         = "source"
    static let detectorVersion = "detector_version"
    static let eventType      = "event_type"
}

/// Production HKHealthStore-backed client.
final class HealthStore: HealthKitClient, HealthRecorder {
    private let log = Logger(subsystem: "com.snoreguard.app", category: "HealthStore")
    private let store = HKHealthStore()

    /// Type written for per-event sound-level samples. Apple's
    /// `.environmentalAudioExposure` is a quantity measured in
    /// `dBASPL`; the metadata flag below makes the lack of
    /// calibration explicit so anyone parsing this data downstream
    /// doesn't mistake it for a real SPL meter reading.
    private static let audioExposureType: HKQuantityType = {
        HKQuantityType(.environmentalAudioExposure)
    }()

    /// Type written for full session windows. Always written with
    /// value `.inBed` — SnoreGuard does not infer sleep stages from
    /// microphone data.
    private static let sleepType: HKCategoryType = {
        HKCategoryType(.sleepAnalysis)
    }()

    // MARK: HealthKitClient

    var isAvailable: Bool { HKHealthStore.isHealthDataAvailable() }

    var isReadAuthorized: Bool {
        // HealthKit doesn't expose read state directly. We track it
        // best-effort via a successful authorization round-trip.
        // Until we've completed a read auth request, assume nothing.
        readAuthorizationGranted
    }

    var isWriteAuthorized: Bool {
        guard isAvailable else { return false }
        // Both write types must be authorized; we use the
        // audio-exposure flag as the canonical check since it's the
        // older / more restrictive path.
        return store.authorizationStatus(for: Self.audioExposureType) == .sharingAuthorized
            && store.authorizationStatus(for: Self.sleepType) == .sharingAuthorized
    }

    /// Best-effort read-authorization flag. HealthKit does not
    /// surface read state; we set this to `true` once
    /// `requestReadAuthorization` completes without throwing.
    private var readAuthorizationGranted: Bool = false

    func requestReadAuthorization() async throws {
        guard isAvailable else { throw HealthRecorderError.unavailable }
        let reads: Set<HKObjectType> = [Self.sleepType]
        do {
            try await store.requestAuthorization(toShare: [], read: reads)
            readAuthorizationGranted = true
        } catch {
            throw HealthRecorderError.underlying(error)
        }
    }

    func requestWriteAuthorization() async throws {
        guard isAvailable else { throw HealthRecorderError.unavailable }
        let writes: Set<HKSampleType> = [Self.sleepType, Self.audioExposureType]
        do {
            try await store.requestAuthorization(toShare: writes, read: [])
        } catch {
            throw HealthRecorderError.underlying(error)
        }
    }

    func sleepSamples(start: Date, end: Date) async throws -> [SleepSample] {
        guard isAvailable, isReadAuthorized else { return [] }
        let predicate = HKQuery.predicateForSamples(withStart: start, end: end, options: [])
        let samples: [HKCategorySample] = try await withCheckedThrowingContinuation { cont in
            let q = HKSampleQuery(
                sampleType: Self.sleepType,
                predicate: predicate,
                limit: HKObjectQueryNoLimit,
                sortDescriptors: nil
            ) { _, results, error in
                if let error = error {
                    cont.resume(throwing: error)
                    return
                }
                cont.resume(returning: (results as? [HKCategorySample]) ?? [])
            }
            store.execute(q)
        }
        return samples.map {
            SleepSample(
                start: $0.startDate,
                end: $0.endDate,
                stage: SleepStage(hkValue: $0.value),
                sourceBundleID: $0.sourceRevision.source.bundleIdentifier
            )
        }
    }

    func writeSessionSample(session: RecordingSession) async throws {
        guard isAvailable else { throw HealthRecorderError.unavailable }
        let end = session.endedAt ?? session.startedAt.addingTimeInterval(session.duration)
        let metadata: [String: Any] = [
            HKMetadataKeyExternalUUID:        session.id.uuidString,
            HealthMetadataKey.estimated:      "true",
            HealthMetadataKey.uncalibrated:   "true",
            HealthMetadataKey.notDiagnostic:  "true",
            HealthMetadataKey.source:         "snoreguard",
        ]
        let sample = HKCategorySample(
            type: Self.sleepType,
            value: HKCategoryValueSleepAnalysis.inBed.rawValue,
            start: session.startedAt,
            end: end,
            metadata: metadata
        )
        do {
            try await store.save(sample)
        } catch {
            log.error("save session sample: \(error.localizedDescription)")
            throw HealthRecorderError.underlying(error)
        }
    }

    func writeEstimatedSoundSample(event: SnoreEvent, sessionStartedAt: Date) async throws {
        guard isAvailable else { throw HealthRecorderError.unavailable }
        let start = sessionStartedAt.addingTimeInterval(TimeInterval(event.startMs) / 1000)
        let end = start.addingTimeInterval(TimeInterval(event.durationMs) / 1000)
        let unit = HKUnit(from: "dBASPL")
        let quantity = HKQuantity(unit: unit, doubleValue: Double(event.avgDB))

        let metadata: [String: Any] = [
            HKMetadataKeyExternalUUID:           event.id.uuidString,
            HealthMetadataKey.estimated:         "true",
            HealthMetadataKey.uncalibrated:      "true",
            HealthMetadataKey.notDiagnostic:     "true",
            HealthMetadataKey.eventType:         "snore_like_event",
            HealthMetadataKey.detectorVersion:   Self.detectorVersion(),
            HealthMetadataKey.source:            "snoreguard",
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
            throw HealthRecorderError.underlying(error)
        }
    }

    private static func detectorVersion() -> String {
        (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String) ?? "unknown"
    }

    // MARK: Legacy HealthRecorder back-compat

    /// Legacy combined authorization. Calls both halves so the older
    /// test paths (`HealthAuthorizationModel`) still pass.
    func requestAuthorization() async throws {
        try await requestWriteAuthorization()
        try? await requestReadAuthorization()
    }

    /// Legacy single-event write. Forwards to the new method; keeps
    /// the older signature alive for `RecorderViewModel`-style
    /// callers that haven't migrated.
    func record(event: SnoreEvent, sessionStartedAt: Date) async {
        guard isAvailable, isWriteAuthorized else { return }
        do {
            try await writeEstimatedSoundSample(event: event, sessionStartedAt: sessionStartedAt)
        } catch {
            log.error("legacy record(event:): \(error.localizedDescription)")
        }
    }
}
