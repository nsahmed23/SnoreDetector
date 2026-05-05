// LevelMeterView.swift
//
// Horizontal level meter that renders both the live RMS-derived dB
// value and a marker for the configured threshold. Mirrors the visual
// idea from `useAudioMonitor`'s prototype meter.

import SwiftUI

struct LevelMeterView: View {
    let liveDB: Float
    let thresholdDB: Float
    var range: ClosedRange<Float> = 30...100

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule()
                    .fill(Color.gray.opacity(0.15))
                Capsule()
                    .fill(barColor)
                    .frame(width: geo.size.width * fraction(for: liveDB))
                    .animation(.linear(duration: 0.05), value: liveDB)

                // Threshold marker
                Rectangle()
                    .fill(Color.orange)
                    .frame(width: 2, height: geo.size.height + 10)
                    .offset(x: geo.size.width * fraction(for: thresholdDB) - 1, y: -5)
                    .accessibilityLabel("Threshold marker")
            }
        }
        .frame(height: 18)
    }

    private var barColor: Color {
        liveDB >= thresholdDB ? .red : .green
    }

    private func fraction(for db: Float) -> CGFloat {
        let span = range.upperBound - range.lowerBound
        guard span > 0 else { return 0 }
        let clamped = min(max(db, range.lowerBound), range.upperBound)
        return CGFloat((clamped - range.lowerBound) / span)
    }
}
