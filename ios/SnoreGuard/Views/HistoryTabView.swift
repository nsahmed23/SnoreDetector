// HistoryTabView.swift
//
// Phase-2 placeholder. The real history view (with persistence,
// charts, and per-night drilldown) lands in phase 5 alongside the
// SwiftData/Core Data layer that backs it.

import SwiftUI

struct HistoryTabView: View {
    var body: some View {
        VStack(spacing: 16) {
            DisclaimerBanner()
            Spacer()
            Image(systemName: "moon.zzz")
                .font(.system(size: 56))
                .foregroundStyle(.secondary)
            Text("History coming soon")
                .font(.headline)
            Text("Recordings live only in memory in this build. Phase 5 of the port plan adds persistent history.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)
            Spacer()
        }
        .padding()
        .navigationTitle("History")
    }
}
