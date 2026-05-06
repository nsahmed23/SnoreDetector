// DisclaimerBanner.swift
//
// Persistent banner shown above any tab that exposes detection
// results. Mirrors the wording from `docs/DISCLAIMER.md`. Phase 0
// landed the language; this is the iOS rendering of it.

import SwiftUI

struct DisclaimerBanner: View {
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "info.circle.fill")
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text("Heuristic detector — not a medical device.")
                    .font(.footnote.weight(.semibold))
                Text("Results are an aid, not a diagnosis. Talk to a clinician about persistent snoring.")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(10)
        .background(Color.yellow.opacity(0.18), in: RoundedRectangle(cornerRadius: 10))
        .accessibilityElement(children: .combine)
    }
}
