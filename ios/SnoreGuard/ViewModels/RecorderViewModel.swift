// RecorderViewModel.swift
//
// The view-model that joins AudioEngine and SnoreCore. The engine
// pushes 256-sample frames on its render thread; this VM hops to the
// main actor to update the published state. Polling for events
// happens both reactively (when pushFrame returns true) and on a 30 Hz
// timer as a safety net.

import Foundation
import Combine
import OSLog

@MainActor
final class RecorderViewModel: ObservableObject {
    enum State: Equatable {
        case idle
        case starting
        case recording
        case interrupted
        case stopped
        case error(String)
    }

    @Published private(set) var state: State = .idle
    @Published private(set) var session: RecordingSession?
    @Published private(set) var liveLevelDB: Float = 0
    /// Most recent finalized event count, exposed as a separate
    /// publisher for the live snore counter on the Record tab.
    @Published private(set) var eventCount: Int = 0

    private let log = Logger(subsystem: "com.snoreguard.app", category: "Recorder")
    private let settingsStore: SettingsStore
    private let engine: AudioEngine
    private let healthRecorder: HealthRecorder
    private var core: SnoreCore?
    private var pollTimer: AnyCancellable?
    private var settingsCancellable: AnyCancellable?
    private var stateCancellable: AnyCancellable?

    /// Strong reference to the proxy that bridges the audio render
    /// thread to the main actor. `AudioEngine.delegate` is `weak`
    /// (correct shape for a delegate), so the *owner* — this VM —
    /// must hold the proxy alive for the duration of recording. A
    /// previous version used `weak var` here, which let the proxy
    /// deallocate immediately after `start()` returned, silently
    /// dropping every audio frame.
    private var engineDelegateProxy: AudioEngineProxy?

    init(settingsStore: SettingsStore,
         engine: AudioEngine = AudioEngine(frameSize: SnoreCore.frameSize),
         healthRecorder: HealthRecorder = HealthStore()) {
        self.settingsStore = settingsStore
        self.engine = engine
        self.healthRecorder = healthRecorder

        // Forward live settings changes into the running detector.
        self.settingsCancellable = settingsStore.$settings
            .removeDuplicates()
            .sink { [weak self] new in
                self?.applySettings(new)
            }

        // Translate engine state into our enum so SwiftUI can bind.
        self.stateCancellable = engine.$state
            .receive(on: DispatchQueue.main)
            .sink { [weak self] s in
                self?.translate(engineState: s)
            }
    }

    // MARK: - Public API

    func start() {
        guard state == .idle || state == .stopped || state == .interrupted else { return }
        state = .starting

        do {
            self.core = try SnoreCore(
                thresholdDB: settingsStore.settings.thresholdDB,
                sensitivity: settingsStore.settings.sensitivity
            )
        } catch {
            state = .error("Failed to init detector: \(error.localizedDescription)")
            return
        }

        let proxy = AudioEngineProxy { [weak self] frame in
            Task { @MainActor in self?.handleFrame(frame) }
        }
        self.engineDelegateProxy = proxy
        engine.delegate = proxy

        Task {
            do {
                try await engine.start()
                self.session = RecordingSession()
                self.eventCount = 0
                startPollTimer()
                self.state = .recording
            } catch {
                self.state = .error(error.localizedDescription)
            }
        }
    }

    func stop() {
        engine.stop()
        engine.delegate = nil
        engineDelegateProxy = nil
        stopPollTimer()

        // Drain any queued events first.
        drainEvents()

        // Then flush the in-flight event (if any) so a stop mid-snore
        // doesn't drop the event the detector was building.
        if let inFlight = core?.finish() {
            session?.events.append(inFlight)
            eventCount += 1
            forwardToHealthIfEnabled(inFlight)
        }

        if var s = session {
            s.endedAt = .now
            session = s
        }
        core = nil
        state = .stopped

        // Phase C: optional one-shot session write to Apple Health
        // (HKCategorySample, sleepAnalysis = inBed). Best-effort —
        // we don't surface failures to the UI.
        forwardSessionToHealthIfEnabled(session)
    }

    // MARK: - Frame processing

    private func handleFrame(_ samples: [Float]) {
        guard let core = core else { return }
        // Crude live level for the meter — uses RMS of the raw samples
        // mapped into the same 30..100 range the detector uses.
        liveLevelDB = Self.rmsToDB(samples)

        do {
            if try core.pushFrame(samples) {
                drainEvents()
            }
        } catch {
            log.error("pushFrame: \(error.localizedDescription)")
        }
    }

    private func drainEvents() {
        guard let core = core else { return }
        while let ev = core.pollEvent() {
            session?.events.append(ev)
            eventCount += 1
            forwardToHealthIfEnabled(ev)
        }
    }

    private func forwardToHealthIfEnabled(_ ev: SnoreEvent) {
        guard settingsStore.writeSoundLevelsToHealth,
              let session = session else { return }
        Task.detached { [healthRecorder, session] in
            await healthRecorder.record(event: ev, sessionStartedAt: session.startedAt)
        }
    }

    /// Phase C addition: write the entire session window as a single
    /// `HKCategorySample` of type sleepAnalysis with value `.inBed`
    /// when the user has the corresponding toggle on. Always inBed —
    /// SnoreGuard never infers REM/core/deep stages from microphone
    /// data.
    private func forwardSessionToHealthIfEnabled(_ s: RecordingSession?) {
        guard settingsStore.writeSessionsToHealth, let s = s else { return }
        // The legacy HealthRecorder protocol used by tests doesn't
        // know about session writes; the production HealthStore
        // also conforms to HealthKitClient where the method exists.
        guard let client = healthRecorder as? HealthKitClient else { return }
        Task.detached { [client, s] in
            do {
                try await client.writeSessionSample(session: s)
            } catch {
                // Best-effort; HK writes are not surfaced to the UI.
            }
        }
    }

    private func startPollTimer() {
        pollTimer = Timer
            .publish(every: 1.0 / 30.0, on: .main, in: .common)
            .autoconnect()
            .sink { [weak self] _ in
                self?.drainEvents()
            }
    }

    private func stopPollTimer() {
        pollTimer?.cancel()
        pollTimer = nil
    }

    private func applySettings(_ new: DetectorSettings) {
        guard let core = core else { return }
        do {
            try core.setThreshold(new.thresholdDB)
            try core.setSensitivity(new.sensitivity)
        } catch {
            log.error("apply settings: \(error.localizedDescription)")
        }
    }

    private func translate(engineState: AudioEngineState) {
        switch engineState {
        case .idle:
            // Don't overwrite a richer state set by `start()`.
            if state == .stopped { state = .idle }
        case .starting:
            state = .starting
        case .running:
            state = .recording
        case .interrupted:
            state = .interrupted
        case .stopped:
            state = .stopped
        case .failed(let msg):
            state = .error(msg)
        }
    }

    /// Map RMS of the most recent 256-sample frame to the detector's
    /// 30..100 dB band so the meter and threshold slider line up.
    /// NOT calibrated SPL — see DISCLAIMER.md.
    private static func rmsToDB(_ samples: [Float]) -> Float {
        var sumSq: Float = 0
        for s in samples { sumSq += s * s }
        let rms = (samples.isEmpty ? 0 : sqrt(sumSq / Float(samples.count)))
        // Empirical mapping: full-scale (rms ≈ 0.707) → 100, silence → 30.
        let linear = min(max(rms / 0.707, 0), 1)
        return 30 + linear * 70
    }
}

/// A tiny indirect delegate so the audio render thread hop happens
/// in one place (here) rather than scattered through the VM.
final class AudioEngineProxy: AudioEngineDelegate {
    private let onFrame: ([Float]) -> Void
    init(onFrame: @escaping ([Float]) -> Void) {
        self.onFrame = onFrame
    }
    func audioEngine(_ engine: AudioEngine, didProduceFrame samples: [Float]) {
        onFrame(samples)
    }
}
