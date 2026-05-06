// EventStore.swift
//
// Domain API over Core Data. Hides NSManagedObjectContext from the
// view layer: callers say `await store.record(session)`, not
// `try await ctx.perform { ... }`. All writes run on a background
// context; reads use the view context so SwiftUI binds without
// extra hops.
//
// Phase E adds the cloud-sync surface on top of the v1 record /
// fetch / markSynced API: per-row sync state, audio-clip lifecycle,
// and remote upserts for the pull side. The CD schema bumped to v2
// (additive only, lightweight-migration-friendly) — see
// SessionEntity.xcdatamodeld/SessionEntity 2.xcdatamodel/contents.

import CoreData
import os.log

// MARK: - Wire types (temporary placeholders pending Phase D integration)
//
// Phase D's canonical `WireSession` and `WireAudioClip` will live in
// `Networking/Endpoints.swift` once that branch lands the audio-clip
// + session endpoints. Today Phase D only ships `WireSnoreEvent`
// (POST /events shape), so we stand up local mirrors here with the
// same field set + snake_case CodingKeys as the backend's
// `RecordingSession` and `AudioClip` rows in
// backend/internal/store/clips.go on claude/backend-audio-cloud-sync.
//
// On Phase D + E integration, delete these definitions in favor of
// the canonical ones; the call sites in EventStore.upsertRemoteSession
// and EventStore.upsertRemoteClip stay unchanged because the field
// shape is identical.
struct WireSession: Codable, Equatable {
    let id: String?
    let clientSessionID: String
    let startedAt: Date
    let endedAt: Date?
    let deviceName: String?
    let appVersion: String?

    enum CodingKeys: String, CodingKey {
        case id
        case clientSessionID = "client_session_id"
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case deviceName = "device_name"
        case appVersion = "app_version"
    }
}

struct WireAudioClip: Codable, Equatable {
    let id: String?
    let sessionID: String?
    let clientClipID: String
    let clientEventID: String?
    let startedAt: Date
    let durationMS: Int32
    let avgDB: Float?
    let contentType: String
    let sizeBytes: Int64
    let sha256: String
    let uploadedAt: Date?
    let deletedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id
        case sessionID = "session_id"
        case clientClipID = "client_clip_id"
        case clientEventID = "client_event_id"
        case startedAt = "started_at"
        case durationMS = "duration_ms"
        case avgDB = "avg_db"
        case contentType = "content_type"
        case sizeBytes = "size_bytes"
        case sha256
        case uploadedAt = "uploaded_at"
        case deletedAt = "deleted_at"
    }
}

// MARK: - Sync status

/// String values mirror the backend's expectations and the Core Data
/// `syncStatus` attribute defaults. Kept as a namespace of constants
/// (vs an enum stored on the entity) because Core Data's
/// `defaultValueString` only accepts a literal — round-tripping
/// through a Swift enum here would require a transformer for every
/// fetched row.
enum SyncStatus {
    static let pending = "pending"
    static let uploading = "uploading"
    static let synced = "synced"
    static let failed = "failed"
}

@MainActor
final class EventStore: ObservableObject {
    private let controller: PersistenceController
    private let log = Logger(subsystem: "com.snoreguard.app", category: "EventStore")
    private let clipStore: ClipFileStore

    init(_ controller: PersistenceController = .shared,
         clipStore: ClipFileStore = .shared) {
        self.controller = controller
        self.clipStore = clipStore
    }

    /// Persist a finished recording session and its events. Sets
    /// the v2 sync-state defaults (clientSessionID = id.uuidString,
    /// syncStatus = pending) so the SyncManager picks the row up on
    /// its next push.
    func record(_ session: RecordingSession) async {
        let bg = controller.newBackgroundContext()
        await bg.perform {
            let entity = SessionEntity(context: bg)
            entity.id = session.id
            entity.startedAt = session.startedAt
            entity.endedAt = session.endedAt
            // v2 sync-state. clientSessionID doubles as the dedup key
            // the backend upserts on; reusing the local UUID keeps it
            // stable across retries without a separate ID column.
            entity.setValue(session.id.uuidString, forKey: "clientSessionID")
            entity.setValue(SyncStatus.pending, forKey: "syncStatus")
            for ev in session.events {
                let e = EventEntity(context: bg)
                e.id = ev.id
                e.session = entity
                e.startedAt = session.startedAt.addingTimeInterval(TimeInterval(ev.startMs) / 1000)
                e.durationMS = Int32(ev.durationMs)
                e.avgDB = ev.avgDB
                e.setValue(ev.id.uuidString, forKey: "clientEventID")
                e.setValue(Int64(ev.startMs), forKey: "startMs")
                e.setValue(SyncStatus.pending, forKey: "syncStatus")
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
    /// EventSync upload loop. Kept on the v1 syncedAt-nullness check
    /// so the existing SyncManager call site doesn't break — the new
    /// `unsyncedEvents` method below uses the v2 syncStatus column.
    func fetchUnsynced(limit: Int = 500) async throws -> [EventEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<EventEntity>(entityName: "EventEntity")
        req.predicate = NSPredicate(format: "syncedAt == nil")
        req.sortDescriptors = [NSSortDescriptor(key: "startedAt", ascending: true)]
        req.fetchLimit = limit
        return try ctx.fetch(req)
    }

    /// Mark a batch of events synced (v1 syncedAt timestamp path).
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

    // MARK: - Sync state (v2 push side)

    /// Sessions ready to be uploaded — pending or previously failed.
    /// Ordered ascending by startedAt so the oldest unsent night
    /// lands on the server first (cheaper for the backend's compound
    /// (started_at, id) DESC cursor; pulled clients see consistent
    /// history sooner).
    func unsyncedSessions(limit: Int = 200) async throws -> [SessionEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
        req.predicate = NSPredicate(format: "syncStatus == %@ OR syncStatus == %@",
                                    SyncStatus.pending, SyncStatus.failed)
        req.sortDescriptors = [NSSortDescriptor(key: "startedAt", ascending: true)]
        req.fetchLimit = limit
        return try ctx.fetch(req)
    }

    func markSessionSynced(_ id: UUID, remoteID: String) async throws {
        try await updateSession(id) { row in
            row.setValue(SyncStatus.synced, forKey: "syncStatus")
            row.setValue(remoteID, forKey: "remoteID")
        }
    }

    func markSessionFailed(_ id: UUID) async throws {
        try await updateSession(id) { row in
            row.setValue(SyncStatus.failed, forKey: "syncStatus")
        }
    }

    /// Events ready to be uploaded (v2 syncStatus path).
    func unsyncedEvents(limit: Int = 500) async throws -> [EventEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<EventEntity>(entityName: "EventEntity")
        req.predicate = NSPredicate(format: "syncStatus == %@ OR syncStatus == %@",
                                    SyncStatus.pending, SyncStatus.failed)
        req.sortDescriptors = [NSSortDescriptor(key: "startedAt", ascending: true)]
        req.fetchLimit = limit
        return try ctx.fetch(req)
    }

    func markEventSynced(_ id: UUID, remoteID: String) async throws {
        try await updateEvent(id) { row in
            row.setValue(SyncStatus.synced, forKey: "syncStatus")
            row.setValue(remoteID, forKey: "remoteID")
            row.syncedAt = Date()
        }
    }

    func markEventFailed(_ id: UUID) async throws {
        try await updateEvent(id) { row in
            row.setValue(SyncStatus.failed, forKey: "syncStatus")
        }
    }

    /// Audio clips ready to be uploaded. Excludes soft-deleted rows
    /// (deletedAt != nil) and rows that are missing their local file
    /// (uploaded already + cleaned up locally — nothing left to send).
    func unsyncedClips(limit: Int = 200) async throws -> [AudioClipEntity] {
        let ctx = controller.container.viewContext
        let req = NSFetchRequest<AudioClipEntity>(entityName: "AudioClipEntity")
        req.predicate = NSPredicate(format:
            "(syncStatus == %@ OR syncStatus == %@) AND deletedAt == nil AND localFilePath != nil",
            SyncStatus.pending, SyncStatus.failed)
        req.sortDescriptors = [NSSortDescriptor(key: "id", ascending: true)]
        req.fetchLimit = limit
        return try ctx.fetch(req)
    }

    func markClipSynced(_ id: UUID, remoteID: String) async throws {
        try await updateClip(id) { row in
            row.setValue(SyncStatus.synced, forKey: "syncStatus")
            row.setValue(remoteID, forKey: "remoteID")
            row.setValue(Date(), forKey: "uploadedAt")
        }
    }

    func markClipFailed(_ id: UUID) async throws {
        try await updateClip(id) { row in
            row.setValue(SyncStatus.failed, forKey: "syncStatus")
        }
    }

    // MARK: - Audio clip lifecycle

    /// Copy a recorded audio file from `tempFileURL` into the
    /// app-support clips directory and create the AudioClipEntity
    /// row. Computes sha256 + size_bytes from the on-disk copy so the
    /// hash matches what the backend will see on upload (the temp
    /// file may be modified or removed by the caller after this
    /// returns — content-addressing the persisted copy avoids that
    /// race). Caller is responsible for deleting `tempFileURL`.
    @discardableResult
    func recordClip(
        sessionID: UUID,
        eventID: UUID?,
        clientClipID: UUID,
        tempFileURL: URL,
        contentType: String = "audio/m4a"
    ) async throws -> AudioClipEntity {
        // File ops happen on the calling actor (main) — they're a
        // single open + copy per call, well under the 16 ms frame
        // budget for a snore-event-driven write. The CD save then
        // hops to the bg context.
        let dest = try clipStore.copyIn(from: tempFileURL, clientClipID: clientClipID)
        let sha = try ClipFileStore.sha256(of: dest)
        let attrs = try FileManager.default.attributesOfItem(atPath: dest.path)
        let size = (attrs[.size] as? NSNumber)?.int64Value ?? 0

        let bg = controller.newBackgroundContext()
        let objectID: NSManagedObjectID = try await bg.perform {
            // Find the parent session by id (the caller hands us the
            // local UUID, not the NSManagedObjectID, because the
            // detector callsite doesn't carry CD identity).
            let sReq = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
            sReq.predicate = NSPredicate(format: "id == %@", sessionID as CVarArg)
            sReq.fetchLimit = 1
            guard let session = try bg.fetch(sReq).first else {
                throw EventStoreError.sessionNotFound(sessionID)
            }
            let clip = AudioClipEntity(context: bg)
            clip.setValue(UUID(), forKey: "id")
            clip.setValue(clientClipID, forKey: "clientClipID")
            clip.setValue(dest.lastPathComponent, forKey: "localFilePath")
            clip.setValue(contentType, forKey: "contentType")
            clip.setValue(size, forKey: "sizeBytes")
            clip.setValue(sha, forKey: "sha256")
            clip.setValue(SyncStatus.pending, forKey: "syncStatus")
            clip.setValue(session, forKey: "session")
            if let eventID = eventID {
                let eReq = NSFetchRequest<EventEntity>(entityName: "EventEntity")
                eReq.predicate = NSPredicate(format: "id == %@", eventID as CVarArg)
                eReq.fetchLimit = 1
                if let event = try bg.fetch(eReq).first {
                    clip.setValue(event, forKey: "event")
                }
            }
            try bg.save()
            return clip.objectID
        }
        // Hand back a viewContext-resident copy so SwiftUI bindings
        // pick it up. existingObject(with:) is synchronous and safe
        // on the main actor for a row we just wrote.
        let ctx = controller.container.viewContext
        guard let row = try ctx.existingObject(with: objectID) as? AudioClipEntity else {
            throw EventStoreError.clipMaterializationFailed
        }
        return row
    }

    /// Soft-delete a clip: remove the local file, clear
    /// `localFilePath`, set `deletedAt = .now`. Backend cleanup is
    /// done by SyncManager on its next pass — calling this method
    /// alone leaves the remote copy intact.
    func deleteClip(_ id: UUID) async throws {
        let bg = controller.newBackgroundContext()
        let clientClipID: UUID = try await bg.perform {
            let req = NSFetchRequest<AudioClipEntity>(entityName: "AudioClipEntity")
            req.predicate = NSPredicate(format: "id == %@", id as CVarArg)
            req.fetchLimit = 1
            guard let row = try bg.fetch(req).first else {
                throw EventStoreError.clipNotFound(id)
            }
            let cid = (row.value(forKey: "clientClipID") as? UUID) ?? UUID()
            row.setValue(nil, forKey: "localFilePath")
            row.setValue(Date(), forKey: "deletedAt")
            try bg.save()
            return cid
        }
        // File removal is best-effort; if it fails (e.g. the file
        // was already gone after a prior partial-delete), we still
        // want the CD tombstone so the UI reflects the user's
        // intent. Log and continue.
        do {
            try clipStore.remove(clientClipID)
        } catch {
            log.error("deleteClip: file remove failed: \(error.localizedDescription)")
        }
    }

    // MARK: - Pull (server → local)

    /// Match an incoming server session by `clientSessionID` and
    /// upsert. Idempotent: a second call with the same wire row is
    /// a no-op. Sets `syncStatus = synced` and stamps `remoteID`.
    func upsertRemoteSession(_ wire: WireSession) async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let req = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
            req.predicate = NSPredicate(format: "clientSessionID == %@", wire.clientSessionID)
            req.fetchLimit = 1
            let row = try bg.fetch(req).first ?? {
                let s = SessionEntity(context: bg)
                s.id = UUID()
                s.setValue(wire.clientSessionID, forKey: "clientSessionID")
                return s
            }()
            row.startedAt = wire.startedAt
            row.endedAt = wire.endedAt
            row.setValue(wire.deviceName, forKey: "deviceName")
            row.setValue(wire.appVersion, forKey: "appVersion")
            if let remoteID = wire.id { row.setValue(remoteID, forKey: "remoteID") }
            row.setValue(SyncStatus.synced, forKey: "syncStatus")
            try bg.save()
        }
    }

    /// Match an incoming server clip by `clientClipID` and upsert.
    /// Does NOT download the binary — the History tab fetches that
    /// on demand. The local row reflects metadata only until the
    /// user taps "Download".
    func upsertRemoteClip(_ wire: WireAudioClip) async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let req = NSFetchRequest<AudioClipEntity>(entityName: "AudioClipEntity")
            guard let clipUUID = UUID(uuidString: wire.clientClipID) else {
                throw EventStoreError.invalidClientClipID(wire.clientClipID)
            }
            req.predicate = NSPredicate(format: "clientClipID == %@", clipUUID as CVarArg)
            req.fetchLimit = 1
            let row: AudioClipEntity = try bg.fetch(req).first ?? {
                let c = AudioClipEntity(context: bg)
                c.setValue(UUID(), forKey: "id")
                c.setValue(clipUUID, forKey: "clientClipID")
                return c
            }()
            row.setValue(wire.contentType, forKey: "contentType")
            row.setValue(wire.sizeBytes, forKey: "sizeBytes")
            row.setValue(wire.sha256, forKey: "sha256")
            if let remoteID = wire.id { row.setValue(remoteID, forKey: "remoteID") }
            if let uploadedAt = wire.uploadedAt { row.setValue(uploadedAt, forKey: "uploadedAt") }
            if let deletedAt = wire.deletedAt { row.setValue(deletedAt, forKey: "deletedAt") }
            row.setValue(SyncStatus.synced, forKey: "syncStatus")
            try bg.save()
        }
    }

    // MARK: - Migration

    /// Backfill v1 → v2 fields. Safe to call repeatedly: rows that
    /// already have a non-empty `clientSessionID` / `clientEventID`
    /// are skipped, so the cost on a second launch is one indexed
    /// scan and zero writes.
    ///
    /// Backfill policy: `clientSessionID = id.uuidString`. Reusing
    /// the local entity UUID as the dedup key means a v1 row that's
    /// later uploaded matches the same backend row that a hypothetical
    /// re-recording with the same UUID would, so we don't accidentally
    /// duplicate rows server-side.
    func migrateFromV1() async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let sReq = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
            sReq.predicate = NSPredicate(format: "clientSessionID == nil OR clientSessionID == %@", "")
            for s in try bg.fetch(sReq) {
                if let id = s.id {
                    s.setValue(id.uuidString, forKey: "clientSessionID")
                }
                // Don't touch syncStatus here — its default ("pending")
                // is set by Core Data on the new column at migration
                // time, which is the correct state: a pre-v2 row was
                // never synced to a v2-aware backend.
            }
            let eReq = NSFetchRequest<EventEntity>(entityName: "EventEntity")
            eReq.predicate = NSPredicate(format: "clientEventID == nil OR clientEventID == %@", "")
            for e in try bg.fetch(eReq) {
                if let id = e.id {
                    e.setValue(id.uuidString, forKey: "clientEventID")
                }
                // startMs is best-effort: the v1 row only stored an
                // absolute startedAt, so derive the relative ms from
                // the parent session if available. Otherwise leave 0.
                if let session = e.session,
                   let sessStart = session.startedAt,
                   let evStart = e.startedAt {
                    let ms = Int64(evStart.timeIntervalSince(sessStart) * 1000)
                    e.setValue(ms, forKey: "startMs")
                }
            }
            try bg.save()
        }
    }

    // MARK: - Internal helpers

    private func updateSession(_ id: UUID,
                               _ mutate: @escaping (SessionEntity) -> Void) async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let req = NSFetchRequest<SessionEntity>(entityName: "SessionEntity")
            req.predicate = NSPredicate(format: "id == %@", id as CVarArg)
            req.fetchLimit = 1
            guard let row = try bg.fetch(req).first else {
                throw EventStoreError.sessionNotFound(id)
            }
            mutate(row)
            try bg.save()
        }
    }

    private func updateEvent(_ id: UUID,
                             _ mutate: @escaping (EventEntity) -> Void) async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let req = NSFetchRequest<EventEntity>(entityName: "EventEntity")
            req.predicate = NSPredicate(format: "id == %@", id as CVarArg)
            req.fetchLimit = 1
            guard let row = try bg.fetch(req).first else {
                throw EventStoreError.eventNotFound(id)
            }
            mutate(row)
            try bg.save()
        }
    }

    private func updateClip(_ id: UUID,
                            _ mutate: @escaping (AudioClipEntity) -> Void) async throws {
        let bg = controller.newBackgroundContext()
        try await bg.perform {
            let req = NSFetchRequest<AudioClipEntity>(entityName: "AudioClipEntity")
            req.predicate = NSPredicate(format: "id == %@", id as CVarArg)
            req.fetchLimit = 1
            guard let row = try bg.fetch(req).first else {
                throw EventStoreError.clipNotFound(id)
            }
            mutate(row)
            try bg.save()
        }
    }
}

// MARK: - Errors

enum EventStoreError: Error, Equatable {
    case sessionNotFound(UUID)
    case eventNotFound(UUID)
    case clipNotFound(UUID)
    case clipMaterializationFailed
    case invalidClientClipID(String)
}
