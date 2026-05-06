// SettingsStore.swift
//
// UserDefaults-backed persistence for the user's detector settings
// and the three independent Apple Health toggles.
//
// Phase C splits the legacy single `syncToAppleHealth` toggle into
// three opt-in-per-direction toggles:
//   1. readSleepFromHealth        — read sleep stages
//   2. writeSessionsToHealth      — write inBed sleep samples
//   3. writeSoundLevelsToHealth   — write audio-exposure samples
//
// All three default OFF. Migration from PR #5: if the old
// `settings.syncToAppleHealth` key was `true`, we set
// `writeSoundLevelsToHealth = true` (the only behaviour the old
// toggle actually had) and leave the other two OFF. After migration
// the legacy key is cleared so this only runs once.

import Foundation
import Combine

final class SettingsStore: ObservableObject {
    @Published var settings: DetectorSettings {
        didSet { persist() }
    }

    @Published var hasAcceptedDisclaimer: Bool {
        didSet { defaults.set(hasAcceptedDisclaimer, forKey: Keys.disclaimer) }
    }

    /// Legacy single toggle. Kept as a *computed* alias of
    /// `writeSoundLevelsToHealth` so older test code and the legacy
    /// RecorderViewModel forwarding path continue to compile. New
    /// callers should use the three explicit toggles below.
    var syncToAppleHealth: Bool {
        get { writeSoundLevelsToHealth }
        set { writeSoundLevelsToHealth = newValue }
    }

    /// Read sleep-stage samples from Apple Health to correlate with
    /// detected snore-like events. Read access is independent from
    /// writes.
    @Published var readSleepFromHealth: Bool {
        didSet { defaults.set(readSleepFromHealth, forKey: Keys.healthReadSleep) }
    }

    /// Write each completed recording session as an `HKCategorySample`
    /// of type sleepAnalysis with value `.inBed`. SnoreGuard never
    /// infers REM/core/deep stages from microphone data.
    @Published var writeSessionsToHealth: Bool {
        didSet { defaults.set(writeSessionsToHealth, forKey: Keys.healthWriteSessions) }
    }

    /// Write each detected snore-like event window as an
    /// `HKQuantitySample` of type environmentalAudioExposure. Values
    /// are uncalibrated relative magnitudes — the metadata makes
    /// that explicit.
    @Published var writeSoundLevelsToHealth: Bool {
        didSet { defaults.set(writeSoundLevelsToHealth, forKey: Keys.healthWriteSoundLevels) }
    }

    private let defaults: UserDefaults
    private enum Keys {
        static let threshold = "settings.thresholdDB"
        static let sensitivity = "settings.sensitivity"
        static let disclaimer = "settings.hasAcceptedDisclaimer"
        // Legacy — migrated forward to `healthWriteSoundLevels`.
        static let legacyHealthSync = "settings.syncToAppleHealth"
        // New Phase C keys.
        static let healthReadSleep        = "settings.health.readSleepFromHealth"
        static let healthWriteSessions    = "settings.health.writeSessionsToHealth"
        static let healthWriteSoundLevels = "settings.health.writeSoundLevelsToHealth"
        static let healthMigrationDone    = "settings.health.migrationV1Done"
    }

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults

        let threshold: Float
        if defaults.object(forKey: Keys.threshold) != nil {
            threshold = DetectorSettings.snapped(defaults.float(forKey: Keys.threshold))
        } else {
            threshold = DetectorSettings.default.thresholdDB
        }

        let sensitivity = Sensitivity(
            rawValue: UInt8(defaults.integer(forKey: Keys.sensitivity))
        ) ?? DetectorSettings.default.sensitivity

        self.settings = DetectorSettings(thresholdDB: threshold, sensitivity: sensitivity)
        self.hasAcceptedDisclaimer = defaults.bool(forKey: Keys.disclaimer)

        // Health toggles — apply migration first, THEN seed @Published
        // values from the (now-canonical) new keys.
        Self.migrateLegacyHealthToggleIfNeeded(defaults: defaults)
        self.readSleepFromHealth      = defaults.bool(forKey: Keys.healthReadSleep)
        self.writeSessionsToHealth    = defaults.bool(forKey: Keys.healthWriteSessions)
        self.writeSoundLevelsToHealth = defaults.bool(forKey: Keys.healthWriteSoundLevels)
    }

    private func persist() {
        defaults.set(settings.thresholdDB, forKey: Keys.threshold)
        defaults.set(Int(settings.sensitivity.rawValue), forKey: Keys.sensitivity)
    }

    /// One-time migration of the PR #5 single `syncToAppleHealth`
    /// toggle into the new `writeSoundLevelsToHealth` toggle. Runs
    /// once per install; the migration flag in defaults guards
    /// re-application. Idempotent.
    private static func migrateLegacyHealthToggleIfNeeded(defaults: UserDefaults) {
        guard !defaults.bool(forKey: Keys.healthMigrationDone) else { return }
        if defaults.object(forKey: Keys.legacyHealthSync) != nil {
            let legacyOn = defaults.bool(forKey: Keys.legacyHealthSync)
            if legacyOn {
                // Old toggle was ON — preserve the only behaviour it
                // actually had (per-event sound-level writes). The
                // other two stay OFF; the user must opt into them
                // explicitly under the Phase C model.
                defaults.set(true, forKey: Keys.healthWriteSoundLevels)
            }
            // Clear the legacy key so future reads can't drift.
            defaults.removeObject(forKey: Keys.legacyHealthSync)
        }
        defaults.set(true, forKey: Keys.healthMigrationDone)
    }
}
