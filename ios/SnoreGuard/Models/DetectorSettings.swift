// DetectorSettings.swift
//
// Mirrors the React prototype's `Settings` shape. The threshold range
// and step (30..90 dB, step 5) match `useAudioMonitor`'s slider
// constants so that user-facing behaviour stays predictable across
// platforms.

import Foundation

struct DetectorSettings: Equatable, Codable {
    var thresholdDB: Float
    var sensitivity: Sensitivity

    static let `default` = DetectorSettings(thresholdDB: 60, sensitivity: .medium)

    static let thresholdRange: ClosedRange<Float> = 30...90
    static let thresholdStep: Float = 5

    /// Clamp + snap the threshold onto the slider grid. Mirrors the
    /// behaviour of the React `<input type="range">`.
    static func snapped(_ value: Float) -> Float {
        let clamped = min(max(value, thresholdRange.lowerBound), thresholdRange.upperBound)
        return (clamped / thresholdStep).rounded() * thresholdStep
    }
}
