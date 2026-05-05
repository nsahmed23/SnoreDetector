// RecordingSession.swift
//
// In-memory model for a single Record-tab session. Lives only as long
// as the user has the engine running; persistence (Core Data /
// SwiftData / Apple Health) is out of scope for phase 2.

import Foundation

struct RecordingSession: Identifiable, Equatable {
    let id: UUID
    let startedAt: Date
    var endedAt: Date?
    var events: [SnoreEvent]

    init(id: UUID = UUID(), startedAt: Date = .now) {
        self.id = id
        self.startedAt = startedAt
        self.endedAt = nil
        self.events = []
    }

    var isActive: Bool { endedAt == nil }

    var duration: TimeInterval {
        (endedAt ?? .now).timeIntervalSince(startedAt)
    }

    /// Total snore-like time, in seconds, summed across this session.
    var totalSnoreSeconds: TimeInterval {
        events.reduce(0) { $0 + Double($1.durationMs) / 1000.0 }
    }
}
