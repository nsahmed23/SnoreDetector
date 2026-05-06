// SettingsStore.swift
//
// UserDefaults-backed persistence for the user's detector settings.
// Phase 2 only persists the slider/toggle state; phase 5 will move
// recording history to Core Data / SwiftData.

import Foundation
import Combine

final class SettingsStore: ObservableObject {
    @Published var settings: DetectorSettings {
        didSet { persist() }
    }

    @Published var hasAcceptedDisclaimer: Bool {
        didSet { defaults.set(hasAcceptedDisclaimer, forKey: Keys.disclaimer) }
    }

    private let defaults: UserDefaults
    private enum Keys {
        static let threshold = "settings.thresholdDB"
        static let sensitivity = "settings.sensitivity"
        static let disclaimer = "settings.hasAcceptedDisclaimer"
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
    }

    private func persist() {
        defaults.set(settings.thresholdDB, forKey: Keys.threshold)
        defaults.set(Int(settings.sensitivity.rawValue), forKey: Keys.sensitivity)
    }
}
