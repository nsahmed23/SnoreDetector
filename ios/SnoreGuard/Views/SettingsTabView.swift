// SettingsTabView.swift
//
// Mirrors the React `SettingsTab`: threshold slider, sensitivity
// picker, and the disclaimer reminder. HealthKit toggle lands in
// phase 3.

import SwiftUI

struct SettingsTabView: View {
    @EnvironmentObject var store: SettingsStore

    var body: some View {
        Form {
            Section {
                DisclaimerBanner()
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
            }

            Section("Detection") {
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text("Threshold")
                        Spacer()
                        Text("\(Int(store.settings.thresholdDB)) dB")
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                    Slider(
                        value: Binding(
                            get: { store.settings.thresholdDB },
                            set: { store.settings.thresholdDB = DetectorSettings.snapped($0) }
                        ),
                        in: DetectorSettings.thresholdRange,
                        step: DetectorSettings.thresholdStep
                    )
                    Text("Lower = more sensitive (more events). Range: \(Int(DetectorSettings.thresholdRange.lowerBound))–\(Int(DetectorSettings.thresholdRange.upperBound)) dB.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Picker("Sensitivity", selection: $store.settings.sensitivity) {
                    ForEach(Sensitivity.allCases) { s in
                        Text(s.label).tag(s)
                    }
                }
                .pickerStyle(.menu)
            }

            Section("About") {
                LabeledContent("Version", value: Bundle.main.shortVersionString)
                LabeledContent("Build", value: Bundle.main.buildNumber)
                Link("Read the disclaimer in full",
                     destination: URL(string: "https://github.com/nsahmed23/SnoreDetector/blob/main/docs/DISCLAIMER.md")!)
            }
        }
        .navigationTitle("Settings")
    }
}

private extension Bundle {
    var shortVersionString: String {
        infoDictionary?["CFBundleShortVersionString"] as? String ?? "—"
    }
    var buildNumber: String {
        infoDictionary?["CFBundleVersion"] as? String ?? "—"
    }
}
