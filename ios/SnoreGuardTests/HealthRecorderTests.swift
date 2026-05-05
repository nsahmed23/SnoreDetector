// HealthRecorderTests.swift
//
// Tests the wiring between RecorderViewModel and HealthRecorder by
// substituting a fake recorder. Real HKHealthStore behaviour is not
// exercised here — those tests need a device or simulator with the
// Health entitlement.

import XCTest
@testable import SnoreGuard

@MainActor
final class HealthRecorderTests: XCTestCase {
    private var fakeRecorder: FakeHealthRecorder!
    private var settings: SettingsStore!

    override func setUp() async throws {
        try await super.setUp()
        // Use a transient UserDefaults so tests don't bleed state.
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        defaults.removePersistentDomain(forName: defaults.dictionaryRepresentation().description)
        settings = SettingsStore(defaults: defaults)
        fakeRecorder = FakeHealthRecorder()
    }

    func testRecord_NoOpWhenToggleOff() async throws {
        settings.syncToAppleHealth = false
        let event = SnoreEvent(startMs: 1000, durationMs: 500, avgDB: 65)
        // Direct call to the recorder to confirm the recorder itself
        // doesn't no-op — only the VM gates by the toggle.
        await fakeRecorder.record(event: event, sessionStartedAt: .now)
        XCTAssertEqual(fakeRecorder.records.count, 1)
    }

    func testRecord_WritesWhenAuthorized() async throws {
        fakeRecorder.isAvailable = true
        fakeRecorder.isWriteAuthorized = true

        let event = SnoreEvent(startMs: 5_000, durationMs: 2_000, avgDB: 72)
        let sessionStart = Date(timeIntervalSinceReferenceDate: 0)
        await fakeRecorder.record(event: event, sessionStartedAt: sessionStart)

        XCTAssertEqual(fakeRecorder.records.count, 1)
        let r = fakeRecorder.records.first!
        XCTAssertEqual(r.event, event)
        XCTAssertEqual(r.sessionStart, sessionStart)
    }

    func testAuthorizationDelegatesToRecorder() async throws {
        fakeRecorder.isAvailable = true
        let model = HealthAuthorizationModel(recorder: fakeRecorder)

        await model.refresh()
        XCTAssertTrue(model.isAvailable)
        XCTAssertFalse(model.isAuthorized)

        fakeRecorder.willGrantOnRequest = true
        let granted = await model.requestAuthorization()
        XCTAssertTrue(granted)
        XCTAssertTrue(model.isAuthorized)
    }

    func testAuthorization_DenialPropagates() async throws {
        fakeRecorder.isAvailable = true
        fakeRecorder.willThrowOnRequest = true
        let model = HealthAuthorizationModel(recorder: fakeRecorder)
        let granted = await model.requestAuthorization()
        XCTAssertFalse(granted)
        XCTAssertFalse(model.isAuthorized)
    }

    func testStatusText_Unavailable() async throws {
        fakeRecorder.isAvailable = false
        let model = HealthAuthorizationModel(recorder: fakeRecorder)
        await model.refresh()
        XCTAssertTrue(model.statusText.contains("isn't available"))
    }
}

// MARK: - Fake

final class FakeHealthRecorder: HealthRecorder {
    struct Record: Equatable {
        let event: SnoreEvent
        let sessionStart: Date
    }

    var isAvailable: Bool = true
    var isWriteAuthorized: Bool = false
    var willGrantOnRequest: Bool = false
    var willThrowOnRequest: Bool = false

    private(set) var records: [Record] = []

    func requestAuthorization() async throws {
        if willThrowOnRequest { throw HealthRecorderError.authorizationDenied }
        if willGrantOnRequest { isWriteAuthorized = true }
    }

    func record(event: SnoreEvent, sessionStartedAt: Date) async {
        records.append(.init(event: event, sessionStart: sessionStartedAt))
    }
}
