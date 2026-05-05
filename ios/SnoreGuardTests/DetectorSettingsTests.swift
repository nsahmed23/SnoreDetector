// DetectorSettingsTests.swift
//
// Pure-Swift, no FFI involvement — these tests would be the first to
// catch regressions in the slider snap behaviour the React prototype
// relies on.

import XCTest
@testable import SnoreGuard

final class DetectorSettingsTests: XCTestCase {
    func testSnap_ClampsBelowRange() {
        XCTAssertEqual(DetectorSettings.snapped(0), DetectorSettings.thresholdRange.lowerBound)
        XCTAssertEqual(DetectorSettings.snapped(-50), DetectorSettings.thresholdRange.lowerBound)
    }

    func testSnap_ClampsAboveRange() {
        XCTAssertEqual(DetectorSettings.snapped(150), DetectorSettings.thresholdRange.upperBound)
    }

    func testSnap_RoundsToStep() {
        XCTAssertEqual(DetectorSettings.snapped(62), 60)
        XCTAssertEqual(DetectorSettings.snapped(63), 65)
        XCTAssertEqual(DetectorSettings.snapped(67.5), 70)
    }

    func testSensitivity_AllCases() {
        XCTAssertEqual(Sensitivity.allCases.count, 3)
        XCTAssertEqual(Sensitivity(rawValue: 0), .low)
        XCTAssertEqual(Sensitivity(rawValue: 1), .medium)
        XCTAssertEqual(Sensitivity(rawValue: 2), .high)
    }
}
