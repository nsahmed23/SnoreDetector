// AudioEngine.swift
//
// AVAudioEngine wrapper that:
//   1. Configures `AVAudioSession` for `.playAndRecord` with
//      `.mixWithOthers + .allowBluetooth`, so the user can still
//      hear their alarm and we don't kick out other audio.
//   2. Taps the input node at the device's preferred format, converts
//      the buffer to mono Float32 @ 16 kHz via `AVAudioConverter`, and
//      forwards 256-sample frames to a callback.
//   3. Survives audio-session interruptions (phone calls, Siri) by
//      pausing the engine on `.began` and restarting on `.ended`.
//
// The engine intentionally does NOT own the SnoreCore — RecorderViewModel
// wires the two together. That keeps AudioEngine pure (just samples in,
// samples out) and easy to swap for a mock in tests.

import AVFoundation
import Combine
import OSLog

/// One-line summary of the engine's lifecycle state, suitable for UI.
enum AudioEngineState: Equatable {
    case idle
    case starting
    case running
    case interrupted
    case stopped
    case failed(String)
}

protocol AudioEngineDelegate: AnyObject {
    /// Called on the audio render thread. Implementations must NOT
    /// touch UIKit/SwiftUI state directly — bounce to MainActor.
    func audioEngine(_ engine: AudioEngine, didProduceFrame samples: [Float])
}

final class AudioEngine {
    weak var delegate: AudioEngineDelegate?

    private let log = Logger(subsystem: "com.snoreguard.app", category: "AudioEngine")
    private let engine = AVAudioEngine()
    private let session = AVAudioSession.sharedInstance()
    private let frameSize: AVAudioFrameCount
    private let targetSampleRate: Double = 16_000

    /// Buffer of converted samples that haven't yet been chunked into
    /// `frameSize`-length frames. Flushed by `forwardFrames`.
    private var residual: [Float] = []
    private var converter: AVAudioConverter?

    @Published private(set) var state: AudioEngineState = .idle

    private var notificationTokens: [NSObjectProtocol] = []

    init(frameSize: Int) {
        self.frameSize = AVAudioFrameCount(frameSize)
    }

    deinit {
        for t in notificationTokens { NotificationCenter.default.removeObserver(t) }
        if engine.isRunning { engine.stop() }
    }

    /// Request microphone permission, configure the session, and start
    /// the engine. Throws on any failure; UI should surface the message
    /// from `state == .failed(...)`.
    func start() async throws {
        state = .starting
        try await ensureMicrophonePermission()
        try configureSession()
        try installTap()
        try engine.start()
        registerInterruptionObservers()
        state = .running
    }

    /// Stop the tap, the engine, and deactivate the session. Idempotent.
    func stop() {
        if engine.isRunning {
            engine.inputNode.removeTap(onBus: 0)
            engine.stop()
        }
        try? session.setActive(false, options: [.notifyOthersOnDeactivation])
        residual.removeAll(keepingCapacity: false)
        for t in notificationTokens { NotificationCenter.default.removeObserver(t) }
        notificationTokens.removeAll()
        state = .stopped
    }

    // MARK: - Setup

    private func ensureMicrophonePermission() async throws {
        switch AVAudioApplication.shared.recordPermission {
        case .granted:
            return
        case .denied:
            throw AudioEngineError.microphoneDenied
        case .undetermined:
            let granted = await AVAudioApplication.requestRecordPermission()
            guard granted else { throw AudioEngineError.microphoneDenied }
        @unknown default:
            throw AudioEngineError.microphoneDenied
        }
    }

    private func configureSession() throws {
        try session.setCategory(.playAndRecord,
                                mode: .measurement,
                                options: [.mixWithOthers, .allowBluetooth, .defaultToSpeaker])
        try session.setPreferredSampleRate(targetSampleRate)
        try session.setPreferredIOBufferDuration(0.02) // ~320 samples @ 16 kHz
        try session.setActive(true, options: [])
    }

    private func installTap() throws {
        let input = engine.inputNode
        let inputFormat = input.outputFormat(forBus: 0)

        guard inputFormat.sampleRate > 0 else {
            throw AudioEngineError.noInput
        }

        // Target: mono Float32 @ 16 kHz, non-interleaved.
        guard let target = AVAudioFormat(
            commonFormat: .pcmFormatFloat32,
            sampleRate: targetSampleRate,
            channels: 1,
            interleaved: false
        ) else {
            throw AudioEngineError.formatUnsupported
        }

        guard let converter = AVAudioConverter(from: inputFormat, to: target) else {
            throw AudioEngineError.formatUnsupported
        }
        self.converter = converter

        // Tap with a generous buffer size; we rebuffer ourselves.
        input.installTap(onBus: 0, bufferSize: 4096, format: inputFormat) { [weak self] buffer, _ in
            self?.process(buffer: buffer, target: target, converter: converter)
        }
    }

    private func process(buffer: AVAudioPCMBuffer,
                         target: AVAudioFormat,
                         converter: AVAudioConverter) {
        guard let out = AVAudioPCMBuffer(
            pcmFormat: target,
            frameCapacity: AVAudioFrameCount(Double(buffer.frameLength)
                                             * target.sampleRate
                                             / buffer.format.sampleRate
                                             + 16)
        ) else { return }

        var supplied = false
        let inputBlock: AVAudioConverterInputBlock = { _, outStatus in
            if supplied {
                outStatus.pointee = .noDataNow
                return nil
            }
            supplied = true
            outStatus.pointee = .haveData
            return buffer
        }

        var error: NSError?
        let status = converter.convert(to: out, error: &error, withInputFrom: inputBlock)
        if status == .error || error != nil {
            log.error("converter error: \(error?.localizedDescription ?? "unknown")")
            return
        }
        guard let channels = out.floatChannelData else { return }
        let samples = Array(UnsafeBufferPointer(start: channels[0], count: Int(out.frameLength)))
        rebuffer(samples)
    }

    /// Combine new samples with the residual buffer and emit
    /// `frameSize`-length chunks via `forwardFrames`. The residual
    /// holds whatever's left after the last full frame so we don't
    /// drop tail samples.
    private func rebuffer(_ samples: [Float]) {
        residual.append(contentsOf: samples)
        let frame = Int(frameSize)
        guard residual.count >= frame else { return }

        var idx = 0
        let upper = (residual.count / frame) * frame
        while idx + frame <= upper {
            let chunk = Array(residual[idx..<(idx + frame)])
            delegate?.audioEngine(self, didProduceFrame: chunk)
            idx += frame
        }
        residual.removeFirst(upper)
    }

    // MARK: - Interruption handling

    private func registerInterruptionObservers() {
        let nc = NotificationCenter.default

        let interruption = nc.addObserver(
            forName: AVAudioSession.interruptionNotification,
            object: session,
            queue: .main
        ) { [weak self] note in
            self?.handleInterruption(note)
        }

        let routeChange = nc.addObserver(
            forName: AVAudioSession.routeChangeNotification,
            object: session,
            queue: .main
        ) { [weak self] _ in
            // Mic was unplugged or the route otherwise changed — log
            // and let AVAudioEngine adapt; we don't tear down.
            self?.log.info("audio route changed")
        }

        notificationTokens.append(contentsOf: [interruption, routeChange])
    }

    private func handleInterruption(_ note: Notification) {
        guard
            let info = note.userInfo,
            let typeRaw = info[AVAudioSessionInterruptionTypeKey] as? UInt,
            let type = AVAudioSession.InterruptionType(rawValue: typeRaw)
        else { return }

        switch type {
        case .began:
            engine.pause()
            state = .interrupted
        case .ended:
            let optsRaw = info[AVAudioSessionInterruptionOptionKey] as? UInt ?? 0
            let opts = AVAudioSession.InterruptionOptions(rawValue: optsRaw)
            if opts.contains(.shouldResume) {
                do {
                    try session.setActive(true)
                    try engine.start()
                    state = .running
                } catch {
                    state = .failed("resume after interruption failed: \(error.localizedDescription)")
                }
            } else {
                state = .stopped
            }
        @unknown default:
            break
        }
    }
}

enum AudioEngineError: LocalizedError {
    case microphoneDenied
    case noInput
    case formatUnsupported

    var errorDescription: String? {
        switch self {
        case .microphoneDenied:
            return "Microphone access is required for snore detection. " +
                   "Enable it in Settings → SnoreGuard → Microphone."
        case .noInput:
            return "No audio input device available."
        case .formatUnsupported:
            return "Audio format conversion is unavailable on this device."
        }
    }
}
