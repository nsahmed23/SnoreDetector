// SnoreCoreTests.swift
//
// XCTest cases for the Swift FFI wrapper. These run on-device or in
// the simulator, not on Linux — so this file is the sibling to the
// Rust crate's `tests/integration.rs` rather than a duplicate of it.
//
// What's covered:
//   - Construction with valid + invalid sample rate
//   - pushFrame round-trip with silence
//   - pushFrame rejects wrong-length input
//   - setThreshold rejects NaN
//   - Sustained-low-frequency input produces a poll-able event

import XCTest
@testable import SnoreGuard

final class SnoreCoreTests: XCTestCase {
    func testInit_RejectsZeroSampleRate() {
        XCTAssertThrowsError(
            try SnoreCore(thresholdDB: 60, sensitivity: .medium, sampleRate: 0)
        ) { error in
            XCTAssertEqual(error as? SnoreCoreError, .allocationFailed)
        }
    }

    func testPushSilence_NoEvent() throws {
        let core = try SnoreCore(thresholdDB: 60, sensitivity: .medium)
        let silent = [Float](repeating: 0, count: SnoreCore.frameSize)
        for _ in 0..<200 {
            let ev = try core.pushFrame(silent)
            XCTAssertFalse(ev)
        }
        XCTAssertNil(core.pollEvent())
    }

    func testPushFrame_RejectsWrongLength() throws {
        let core = try SnoreCore(thresholdDB: 60, sensitivity: .medium)
        let short = [Float](repeating: 0, count: 64)
        XCTAssertThrowsError(try core.pushFrame(short)) { error in
            switch error as? SnoreCoreError {
            case .invalidLength(let expected, let got):
                XCTAssertEqual(expected, SnoreCore.frameSize)
                XCTAssertEqual(got, 64)
            default:
                XCTFail("expected invalidLength, got \(error)")
            }
        }
    }

    func testSetThreshold_RejectsNaN() throws {
        let core = try SnoreCore(thresholdDB: 60, sensitivity: .medium)
        XCTAssertThrowsError(try core.setThreshold(.nan)) { error in
            XCTAssertEqual(error as? SnoreCoreError, .invalidArgument)
        }
    }

    func testSustainedLowFrequencyProducesEvent() throws {
        let core = try SnoreCore(thresholdDB: 35, sensitivity: .high)
        let frame = lowFreqFrame()
        for _ in 0..<100 {
            _ = try core.pushFrame(frame)
        }
        let silent = [Float](repeating: 0, count: SnoreCore.frameSize)
        for _ in 0..<5 {
            _ = try core.pushFrame(silent)
        }
        XCTAssertNotNil(core.pollEvent())
    }

    // MARK: - Helpers

    private func lowFreqFrame(freq: Float = 100, sampleRate: Float = 16_000) -> [Float] {
        let step = 2 * Float.pi * freq / sampleRate
        var phase: Float = 0
        return (0..<SnoreCore.frameSize).map { _ in
            let s = 0.9 * sinf(phase)
            phase += step
            return s
        }
    }
}
