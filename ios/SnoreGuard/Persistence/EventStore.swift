// EventStore.swift
//
// Domain API over Core Data. Hides NSManagedObjectContext from the
// view layer: callers say `await store.record(session)`, not
// `try await ctx.perform { ... }`. All writes run on a background
// context; reads use the view context so SwiftUI binds without
// extra hops.

import CoreData
import os.log

@MainActor
final class EventStore: ObservableObject {
    private let controller: PersistenceController
    private let log = Logger(subsystem: "com.snoreguard.app", category: "EventStore")

    init(_ controller: PersistenceController = .shared) {
        self.controller = controller
    }

    /// Persist a finished recording session and its events.
    func record(_ session: RecordingSession) async {
        let bg = controller.newBackgroundContext()
        await bg.perform {
            let entity = SessionEntity(context: bg)
            entity.id = session.id
            entity.startedAt = session.startedAt
            entity.endedAt = session.endedAt
            for ev in session.events {
                let e = EventEntity(context: bg)
                e.id = ev.id
                e.session = entity
                e.startedAt = session.startedAt.addingTimeInterval(TimeInterval(ev.startMs) / 1000)
                e.durationMS = Int32(ev.durationMs)
                e.avgDB = ev.avgDB
            }
            do {
                try bg.save()
            } catch {
                self.log.error("record session: \(error.localizedDescription)")
            }
        }
    }

    /// Fetch sessions starting within [interval], newest first.
    func fetchSessions(in interval: DateInterval) async throws -> [SessionEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
        req.predicate = NSPredicate(format: "startedAt >= %@ AND startedAt < %@",
                                    interval.start as NSDate, interval.end as NSDate)
        req.sortDescriptors = [NSSortDescriptor(key: "startedAt", ascending: false)]
        return try ctx.fetch(req)
    }

    /// All sessions, newest first. Used by the History tab.
    func allSessions() async throws -> [SessionEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
        req.sortDescriptors = [NSSortDescriptor(key: "startedAt", ascending: false)]
        return try ctx.fetch(req)
    }

    /// Events not yet pushed to the backend. Used by the post-Phase-2-B
    /// EventSync upload loop.
    func fetchUnsynced(limit: Int = 500) async throws -> [EventEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<EventEntity>(entityName: "EventEntity")
        req.predicate = NSPredicate(format: "syncedAt == nil")
        req.sortDescriptors = [NSSortDescriptor(key: "startedAt", ascending: true)]
        req.fetchLimit = limit
        return try ctx.fetch(req)
    }

    /// Mark a batch of events synced.
    func markSynced(_ eventIDs: [UUID]) async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let req = NSFetchRequest<EventEntity>(entityName: "EventEntity")
            req.predicate = NSPredicate(format: "id IN %@", eventIDs)
            let rows = try bg.fetch(req)
            let now = Date()
            for r in rows { r.syncedAt = now }
            try bg.save()
        }
    }
}
