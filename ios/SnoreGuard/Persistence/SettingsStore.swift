// SettingsStore.swift
//
// UserDefaults-backed persistence for the user's detector + cloud-
// sync settings. Phase D adds three opt-in cloud-sync toggles
// (cloudSyncEnabled, uploadAudioClipsEnabled, wifiOnlyUpload). All
// three default to OFF (or, in wifiOnlyUpload's case, the safe-cost
// default of ON) so a fresh install doesn't silently begin uploading.
// Phase 5 will move recording history to Core Data / SwiftData.

import Foundation
import Combine

final class SettingsStore: ObservableObject {
    @Published var settings: DetectorSettings {
        didSet { persist() }
    }

    @Published var hasAcceptedDisclaimer: Bool {
        didSet { defaults.set(hasAcceptedDisclaimer, forKey: Keys.disclaimer) }
    }

    /// Master cloud-sync switch. When `false`, `SyncManager.sync()`
    /// is a no-op — no events, sessions, or clips leave the device.
    /// Defaults OFF; user must affirmatively opt in via Settings.
    @Published var cloudSyncEnabled: Bool {
        didSet { defaults.set(cloudSyncEnabled, forKey: Keys.cloudSyncEnabled) }
    }

    /// Sub-toggle gating audio-clip upload. Has no effect when
    /// `cloudSyncEnabled = false`. Defaults OFF — users who want
    /// event metadata in the cloud might NOT want raw audio bytes
    /// there, so the choice is decoupled.
    @Published var uploadAudioClipsEnabled: Bool {
        didSet { defaults.set(uploadAudioClipsEnabled, forKey: Keys.uploadAudioClipsEnabled) }
    }

    /// When true, uploads pause on cellular networks and resume
    /// automatically on Wi-Fi. Defaults ON to be data-friendly.
    /// NOTE: actual gating via `NWPathMonitor` is a Phase-E follow-
    /// up; this branch wires the toggle but doesn't yet enforce it.
    @Published var wifiOnlyUpload: Bool {
        didSet { defaults.set(wifiOnlyUpload, forKey: Keys.wifiOnlyUpload) }
    }

    private let defaults: UserDefaults
    private enum Keys {
        static let threshold = "settings.thresholdDB"
        static let sensitivity = "settings.sensitivity"
        static let disclaimer = "settings.hasAcceptedDisclaimer"
        // Cloud-sync settings, namespaced under "settings.cloud.*"
        // so future cloud-related toggles can be grepped + cleared
        // as a group during a "reset cloud preferences" UX.
        static let cloudSyncEnabled = "settings.cloud.syncEnabled"
        static let uploadAudioClipsEnabled = "settings.cloud.uploadAudioClipsEnabled"
        static let wifiOnlyUpload = "settings.cloud.wifiOnlyUpload"
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
        // Cloud-sync defaults: cloudSyncEnabled / uploadAudioClipsEnabled
        // both default to OFF; wifiOnlyUpload defaults to ON. Reading
        // a missing key as `false` is fine for the first two; the
        // third needs the explicit "absent → true" check so a fresh
        // install gets the safer Wi-Fi-only behaviour.
        self.cloudSyncEnabled = defaults.bool(forKey: Keys.cloudSyncEnabled)
        self.uploadAudioClipsEnabled = defaults.bool(forKey: Keys.uploadAudioClipsEnabled)
        if defaults.object(forKey: Keys.wifiOnlyUpload) == nil {
            self.wifiOnlyUpload = true
        } else {
            self.wifiOnlyUpload = defaults.bool(forKey: Keys.wifiOnlyUpload)
        }
    }

    private func persist() {
        defaults.set(settings.thresholdDB, forKey: Keys.threshold)
        defaults.set(Int(settings.sensitivity.rawValue), forKey: Keys.sensitivity)
    }
}
