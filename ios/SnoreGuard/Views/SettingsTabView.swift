// SettingsTabView.swift
//
// Mirrors the React `SettingsTab`: threshold slider, sensitivity
// picker, HealthKit toggle, and the disclaimer reminder.

import SwiftUI

struct SettingsTabView: View {
    @EnvironmentObject var store: SettingsStore
    @StateObject private var health = HealthAuthorizationModel()

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

            Section {
                Toggle("Sync to Apple Health", isOn: Binding(
                    get: { store.syncToAppleHealth },
                    set: { newValue in
                        if newValue && !health.isAuthorized {
                            // Optimistically toggle on; if the user
                            // denies, we'll flip it back.
                            store.syncToAppleHealth = true
                            Task {
                                let granted = await health.requestAuthorization()
                                if !granted {
                                    store.syncToAppleHealth = false
                                }
                            }
                        } else {
                            store.syncToAppleHealth = newValue
                        }
                    }
                ))
                .disabled(!health.isAvailable)

                Text(health.statusText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } header: {
                Text("Apple Health")
            } footer: {
                if store.syncToAppleHealth {
                    Text("Snore events are written as audio-exposure samples. **Important:** the dB values are uncalibrated relative magnitudes from a heuristic detector — not SPL meter readings. The Health app will display them in dB(A) SPL but the calibration disclaimer is in each sample's metadata. Disable any time to stop new writes; existing samples stay in Apple Health unless you delete them there.")
                }
            }

            Section("About") {
                LabeledContent("Version", value: Bundle.main.shortVersionString)
                LabeledContent("Build", value: Bundle.main.buildNumber)
                Link("Read the disclaimer in full",
                     destination: URL(string: "https://github.com/nsahmed23/SnoreDetector/blob/main/docs/DISCLAIMER.md")!)
            }
        }
        .navigationTitle("Settings")
        .task { await health.refresh() }
    }
}

/// Wraps HealthRecorder so the SwiftUI view can react to permission
/// changes without owning HKHealthStore directly.
@MainActor
final class HealthAuthorizationModel: ObservableObject {
    @Published private(set) var isAvailable = false
    @Published private(set) var isAuthorized = false

    private let recorder: HealthRecorder

    init(recorder: HealthRecorder = HealthStore()) {
        self.recorder = recorder
    }

    var statusText: String {
        if !isAvailable {
            return "HealthKit isn't available on this device."
        }
        return isAuthorized
            ? "Write permission granted."
            : "Toggle on to grant write permission."
    }

    func refresh() async {
        isAvailable = recorder.isAvailable
        isAuthorized = recorder.isWriteAuthorized
    }

    /// Returns `true` if the user granted (or had previously granted)
    /// write permission.
    func requestAuthorization() async -> Bool {
        do {
            try await recorder.requestAuthorization()
        } catch {
            isAuthorized = false
            return false
        }
        isAuthorized = recorder.isWriteAuthorized
        return isAuthorized
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
