// EventStoreTests.swift
//
// XCTest cases for the Core Data persistence layer. Each test
// builds a fresh in-memory PersistenceController so cases stay
// isolated. Runs on macOS via `xcodebuild test`; cannot run on
// Linux (no CoreData on Swift-Linux without a different stack).

import XCTest
import CoreData
@testable import SnoreGuard

@MainActor
final class EventStoreTests: XCTestCase {
    private var controller: PersistenceController!
    private var store: EventStore!

    override func setUp() async throws {
        controller = PersistenceController(inMemory: true)
        store = EventStore(controller)
    }

    override func tearDown() async throws {
        controller = nil
        store = nil
    }

    func testRecord_PersistsSessionAndEvents() async throws {
        var session = RecordingSession()
        session.endedAt = session.startedAt.addingTimeInterval(3600)
        session.events = [
            SnoreEvent(startMs: 1000, durationMs: 500, avgDB: 60),
            SnoreEvent(startMs: 5000, durationMs: 1000, avgDB: 65),
        ]
        await store.record(session)

        let sessions = try await store.allSessions()
        XCTAssertEqual(sessions.count, 1)
        XCTAssertEqual((sessions[0].events as? Set<EventEntity>)?.count, 2)
    }

    func testFetchSessions_FilterByRange() async throws {
        let now = Date()
        var s1 = RecordingSession(startedAt: now.addingTimeInterval(-86400 * 2))
        s1.endedAt = s1.startedAt.addingTimeInterval(3600)
        var s2 = RecordingSession(startedAt: now)
        s2.endedAt = s2.startedAt.addingTimeInterval(3600)
        await store.record(s1)
        await store.record(s2)

        let interval = DateInterval(start: now.addingTimeInterval(-3600),
                                    end: now.addingTimeInterval(3600))
        let result = try await store.fetchSessions(in: interval)
        XCTAssertEqual(result.count, 1)
    }

    func testFetchUnsynced_AndMarkSynced() async throws {
        var session = RecordingSession()
        session.endedAt = session.startedAt.addingTimeInterval(60)
        session.events = (0..<3).map { SnoreEvent(startMs: UInt64($0 * 1000), durationMs: 500, avgDB: 60) }
        await store.record(session)

        let unsynced1 = try await store.fetchUnsynced(limit: 100)
        XCTAssertEqual(unsynced1.count, 3)

        let ids = unsynced1.compactMap { $0.id }
        try await store.markSynced(ids)

        let unsynced2 = try await store.fetchUnsynced(limit: 100)
        XCTAssertEqual(unsynced2.count, 0)
    }
}
