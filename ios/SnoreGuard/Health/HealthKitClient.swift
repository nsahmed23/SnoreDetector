// HealthKitClient.swift
//
// Protocol that the production HealthStore conforms to and that the
// test fakes in `HealthRecorderTests` satisfy. Splitting the surface
// into read + write halves lets the three Settings toggles request
// only the scope they need — read of sleep stages, write of session
// inBed samples, write of estimated sound-level samples — and lets
// the user revoke any combination from iOS Settings without having
// to touch the others.
//
// The legacy `HealthRecorder` protocol in HealthStore.swift is kept
// for compatibility with the existing test suite and gets a new
// adapter shape. New code (RecorderViewModel + SettingsTabView)
// uses `HealthKitClient`.

import Foundation
import HealthKit

/// Surface that hides the HKHealthStore implementation details from
/// the rest of the app. Tests substitute `FakeHealthKitClient`.
protocol HealthKitClient: AnyObject {
    /// `true` only on hardware that ships HealthKit — iPhone and iPad.
    var isAvailable: Bool { get }

    /// Whether the user has granted *read* permission for sleep
    /// stages. HealthKit deliberately doesn't report read state on
    /// most platforms; this is a best-effort flag the app sets after
    /// a successful read query.
    var isReadAuthorized: Bool { get }

    /// Whether the user has granted *write* permission for the two
    /// sample types we write (sleep-analysis inBed + audio-exposure).
    var isWriteAuthorized: Bool { get }

    /// Trigger the system permission sheet for read-only scopes.
    /// Resolves once the user dismisses it. Throws if HealthKit is
    /// unavailable.
    func requestReadAuthorization() async throws

    /// Trigger the system permission sheet for write-only scopes.
    /// Resolves once the user dismisses it. Throws if HealthKit is
    /// unavailable.
    func requestWriteAuthorization() async throws

    /// Sleep-stage samples in [start, end). Returns empty array if
    /// read isn't authorized.
    func sleepSamples(start: Date, end: Date) async throws -> [SleepSample]

    /// Write an HKCategorySample of type sleepAnalysis with value
    /// .inBed for the session window. Caller chooses to call this.
    /// SnoreGuard never infers REM/core/deep stages from microphone
    /// data — sessions always land as `.inBed`.
    func writeSessionSample(session: RecordingSession) async throws

    /// Write an HKQuantitySample of type environmentalAudioExposure
    /// for a single snore-like event window. Includes uncalibrated
    /// metadata.
    func writeEstimatedSoundSample(event: SnoreEvent, sessionStartedAt: Date) async throws
}

/// Domain mirror of `HKCategorySample` for sleep analysis. The app
/// works with this type so test code never has to construct an
/// HKSample directly.
struct SleepSample: Equatable {
    let start: Date
    let end: Date
    let stage: SleepStage
    let sourceBundleID: String?
}

/// Mirror of `HKCategoryValueSleepAnalysis` raw values. Adding the
/// enum case to switch on lets us pattern-match without leaking the
/// HealthKit type into UI / VM code.
enum SleepStage: String, CaseIterable, Equatable {
    case inBed
    case awake
    case asleepUnspecified
    case asleepCore
    case asleepDeep
    case asleepREM
    case unknown

    init(hkValue: Int) {
        switch hkValue {
        case HKCategoryValueSleepAnalysis.inBed.rawValue:               self = .inBed
        case HKCategoryValueSleepAnalysis.awake.rawValue:               self = .awake
        case HKCategoryValueSleepAnalysis.asleepUnspecified.rawValue:   self = .asleepUnspecified
        case HKCategoryValueSleepAnalysis.asleepCore.rawValue:          self = .asleepCore
        case HKCategoryValueSleepAnalysis.asleepDeep.rawValue:          self = .asleepDeep
        case HKCategoryValueSleepAnalysis.asleepREM.rawValue:           self = .asleepREM
        default:                                                        self = .unknown
        }
    }
}

/// Find the sleep stage that overlaps the given event window. If
/// multiple samples overlap, prefer the one whose midpoint is
/// closest to the event midpoint. Returns nil if no sample overlaps.
func correlate(eventStart: Date, eventDuration: TimeInterval, samples: [SleepSample]) -> SleepStage? {
    let eventEnd = eventStart.addingTimeInterval(eventDuration)
    let eventMid = eventStart.addingTimeInterval(eventDuration / 2)
    let overlapping = samples.filter { $0.start < eventEnd && $0.end > eventStart }
    guard !overlapping.isEmpty else { return nil }
    return overlapping.min { lhs, rhs in
        let lhsMid = lhs.start.addingTimeInterval(lhs.end.timeIntervalSince(lhs.start) / 2)
        let rhsMid = rhs.start.addingTimeInterval(rhs.end.timeIntervalSince(rhs.start) / 2)
        return abs(eventMid.timeIntervalSince(lhsMid)) < abs(eventMid.timeIntervalSince(rhsMid))
    }?.stage
}
