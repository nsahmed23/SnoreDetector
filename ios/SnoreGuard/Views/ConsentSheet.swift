// ConsentSheet.swift
//
// First-launch sheet that explains what the app does and asks the
// user to acknowledge the disclaimer before recording. Mirrors the
// in-app banner from the React prototype but framed as a one-time
// modal so we can persist a "user has read this" bit.

import SwiftUI

struct ConsentSheet: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject var store: SettingsStore

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text("Welcome to SnoreGuard")
                        .font(.largeTitle.bold())

                    Text("Before you start, a few important things:")
                        .font(.headline)

                    bullet("This is a heuristic detector — it flags audio that *sounds* like snoring. It is not a medical device and it cannot diagnose sleep apnea or any other condition.")
                    bullet("All audio processing happens on your device. Audio is never recorded or uploaded.")
                    bullet("False positives and false negatives are expected. Persistent loud snoring, gasping, or pauses in breathing should be discussed with a clinician.")
                    bullet("To listen continuously, the app needs microphone access.")
                    bullet("Reading sleep stages from Apple Health is optional and off by default.")
                    bullet("Writing SnoreGuard sessions as inBed samples is optional and off by default.")
                    bullet("Writing estimated sound levels (uncalibrated audio-exposure samples) is optional and off by default.")
                    Text("All three toggles can be changed at any time in Settings; SnoreGuard never infers REM, core, or deep stages from microphone data.")
                        .font(.callout)
                        .foregroundStyle(.secondary)

                    Spacer(minLength: 24)

                    Button {
                        store.hasAcceptedDisclaimer = true
                        dismiss()
                    } label: {
                        Text("I understand")
                            .fontWeight(.semibold)
                            .frame(maxWidth: .infinity, minHeight: 50)
                            .background(Color.accentColor, in: Capsule())
                            .foregroundStyle(.white)
                    }
                    .accessibilityIdentifier("acceptDisclaimerButton")
                }
                .padding()
            }
            .navigationBarTitleDisplayMode(.inline)
        }
        .interactiveDismissDisabled(true)
    }

    private func bullet(_ text: String) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "circle.fill")
                .font(.system(size: 6))
                .foregroundStyle(.secondary)
                .padding(.top, 7)
            Text(text)
        }
    }
}
