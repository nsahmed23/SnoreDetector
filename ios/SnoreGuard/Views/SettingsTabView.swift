// SettingsTabView.swift
//
// Mirrors the React `SettingsTab`: threshold slider, sensitivity
// picker, three independent Apple Health toggles, and the
// disclaimer reminder.
//
// Phase C upgrade: replaces the single "Sync to Apple Health"
// toggle with three separately-toggleable opt-ins:
//   1. Read sleep stages
//   2. Write sessions (as inBed sleep samples)
//   3. Write estimated sound levels (as audio-exposure samples)
//
// Each toggle, when flipped on, triggers the corresponding
// HealthKit authorization sheet. If the user denies, the toggle
// flips back automatically so it can't lie about whether reads /
// writes will land.

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

            Section("Apple Health") {
                Toggle("Read sleep stages", isOn: Binding(
                    get: { store.readSleepFromHealth },
                    set: { newValue in
                        if newValue {
                            store.readSleepFromHealth = true
                            Task {
                                let granted = await health.requestRead()
                                if !granted { store.readSleepFromHealth = false }
                            }
                        } else {
                            store.readSleepFromHealth = false
                        }
                    }
                ))
                .disabled(!health.isAvailable)
                Text("Allow SnoreGuard to read your sleep stages from Apple Health to correlate with detected snore-like events. Read access is never combined with write — these are independent permissions.")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Toggle("Write sessions to Apple Health", isOn: Binding(
                    get: { store.writeSessionsToHealth },
                    set: { newValue in
                        if newValue {
                            store.writeSessionsToHealth = true
                            Task {
                                let granted = await health.requestWrite()
                                if !granted { store.writeSessionsToHealth = false }
                            }
                        } else {
                            store.writeSessionsToHealth = false
                        }
                    }
                ))
                .disabled(!health.isAvailable)
                Text("Sessions are written as inBed sleep samples for your recording window. SnoreGuard never infers REM, core, or deep stages from microphone data.")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Toggle("Write estimated sound levels to Apple Health", isOn: Binding(
                    get: { store.writeSoundLevelsToHealth },
                    set: { newValue in
                        if newValue {
                            store.writeSoundLevelsToHealth = true
                            Task {
                                let granted = await health.requestWrite()
                                if !granted { store.writeSoundLevelsToHealth = false }
                            }
                        } else {
                            store.writeSoundLevelsToHealth = false
                        }
                    }
                ))
                .disabled(!health.isAvailable)
                Text("Estimated sound levels are written as audio-exposure samples around each detected snore-like event. Values are uncalibrated relative magnitudes — not SPL meter readings — and the metadata on each sample makes that explicit.")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                if !health.isAvailable {
                    Text("HealthKit isn't available on this device.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
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

/// Wraps HealthKitClient so the SwiftUI view can react to permission
/// changes without owning HKHealthStore directly. Phase C splits the
/// single `requestAuthorization` into separate read + write paths.
@MainActor
final class HealthAuthorizationModel: ObservableObject {
    @Published private(set) var isAvailable = false
    @Published private(set) var isReadAuthorized = false
    @Published private(set) var isWriteAuthorized = false

    /// Back-compat alias used by the existing test suite —
    /// `HealthRecorderTests.testAuthorizationDelegatesToRecorder`
    /// still asserts on `.isAuthorized`.
    var isAuthorized: Bool { isWriteAuthorized }

    private let client: HealthKitClient
    /// Back-compat: the previous shape took a `HealthRecorder`. We
    /// keep that init alive for tests. Production code uses the
    /// `HealthKitClient` form.
    private let recorder: HealthRecorder

    init(client: HealthKitClient = HealthStore()) {
        self.client = client
        // HealthStore conforms to both protocols, so when callers pass
        // the production type the recorder slot is the same instance.
        // For test fakes that only implement HealthKitClient, expose a
        // tiny adapter so the legacy `requestAuthorization` entry
        // point still routes through the same `client`.
        self.recorder = (client as? HealthRecorder) ?? HealthKitClientAsRecorder(client: client)
    }

    /// Legacy initializer kept for `HealthRecorderTests` which
    /// substitutes a `FakeHealthRecorder`. The fake doesn't satisfy
    /// the new `HealthKitClient` protocol, so the model degrades to
    /// the old single-grant flow when called this way.
    init(recorder: HealthRecorder) {
        self.recorder = recorder
        // Best-effort: if the recorder also happens to be a
        // HealthKitClient (e.g. production HealthStore), use it; else
        // fall back to a shim that delegates the legacy methods.
        if let asClient = recorder as? HealthKitClient {
            self.client = asClient
        } else {
            self.client = LegacyClientShim(recorder: recorder)
        }
    }

    var statusText: String {
        if !isAvailable {
            return "HealthKit isn't available on this device."
        }
        return isWriteAuthorized
            ? "Write permission granted."
            : "Toggle on to grant write permission."
    }

    func refresh() async {
        isAvailable = client.isAvailable
        isReadAuthorized = client.isReadAuthorized
        isWriteAuthorized = client.isWriteAuthorized
    }

    /// Request only the read scope (sleep stages). Returns `true` if
    /// the request resolved without error; HealthKit hides whether
    /// reads were actually granted.
    func requestRead() async -> Bool {
        do {
            try await client.requestReadAuthorization()
        } catch {
            isReadAuthorized = false
            return false
        }
        isReadAuthorized = client.isReadAuthorized
        return isReadAuthorized
    }

    /// Request only the write scope (sleepAnalysis + audio-exposure).
    func requestWrite() async -> Bool {
        do {
            try await client.requestWriteAuthorization()
        } catch {
            isWriteAuthorized = false
            return false
        }
        isWriteAuthorized = client.isWriteAuthorized
        return isWriteAuthorized
    }

    /// Legacy combined-grant entry point used by the existing test
    /// suite. Delegates to the legacy recorder's `requestAuthorization`
    /// which is the only call older fakes implement.
    func requestAuthorization() async -> Bool {
        do {
            try await recorder.requestAuthorization()
        } catch {
            isWriteAuthorized = false
            return false
        }
        isWriteAuthorized = recorder.isWriteAuthorized
        return isWriteAuthorized
    }
}

/// Mirror image of `LegacyClientShim`: adapts a `HealthKitClient`
/// (Phase C surface) into the legacy `HealthRecorder` so the
/// model's `recorder` slot doesn't have to construct a real
/// `HealthStore` when callers only supply a `HealthKitClient` fake.
private final class HealthKitClientAsRecorder: HealthRecorder {
    private let client: HealthKitClient
    init(client: HealthKitClient) { self.client = client }
    var isAvailable: Bool { client.isAvailable }
    var isWriteAuthorized: Bool { client.isWriteAuthorized }
    func requestAuthorization() async throws {
        try await client.requestWriteAuthorization()
        try? await client.requestReadAuthorization()
    }
    func record(event: SnoreEvent, sessionStartedAt: Date) async {
        try? await client.writeEstimatedSoundSample(event: event, sessionStartedAt: sessionStartedAt)
    }
}

/// Adapts a `HealthRecorder` (legacy) into the `HealthKitClient`
/// surface so `HealthAuthorizationModel` can be constructed against
/// older fakes without forcing every test to update.
private final class LegacyClientShim: HealthKitClient {
    private let recorder: HealthRecorder
    init(recorder: HealthRecorder) { self.recorder = recorder }

    var isAvailable: Bool { recorder.isAvailable }
    var isReadAuthorized: Bool { false }
    var isWriteAuthorized: Bool { recorder.isWriteAuthorized }

    func requestReadAuthorization() async throws { try await recorder.requestAuthorization() }
    func requestWriteAuthorization() async throws { try await recorder.requestAuthorization() }
    func sleepSamples(start: Date, end: Date) async throws -> [SleepSample] { [] }
    func writeSessionSample(session: RecordingSession) async throws {}
    func writeEstimatedSoundSample(event: SnoreEvent, sessionStartedAt: Date) async throws {
        await recorder.record(event: event, sessionStartedAt: sessionStartedAt)
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
