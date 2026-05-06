// SyncManager.swift
//
// Orchestrates push + pull across sessions, events, and audio clips.
// AuthManager handles single-flight refresh; SyncManager just kicks
// the right async calls in the right order.
//
// Push order (matters because clips reference sessions by remote id):
//   1. sessions  → upload, capture remote `session_id`s
//   2. events    → upload via the existing EventSync
//   3. clips     → upload last; the metadata may carry the remote
//                  `session_id` populated from step 1
//
// Pull order (matters less; the UI is fine with any traversal):
//   1. sessions  → upsert into the local store
//   2. events    → upsert via the existing EventSync.pull(...)
//   3. clips     → metadata only; binary download is on-demand from
//                  the History tab (rate-limited 10/hour server-side)
//
// Idempotency: every operation is idempotent server-side
// (`client_session_id`, `client_event_id`, `client_clip_id`). Partial
// mid-flight failures are safely retried by the caller — this v1
// keeps retry queue state in-memory only; persistent retry-table is
// post-v1 work.

import Foundation
import Combine
import os.log

/// Public observable state for a sync attempt. UI binds to this to
/// render a "syncing… / last synced X minutes ago / error" footer.
@MainActor
final class SyncManager: ObservableObject {
    enum State: Equatable {
        case idle
        case syncing
        case error(String)
    }

    @Published private(set) var state: State = .idle
    @Published private(set) var lastSyncedAt: Date?

    /// Cursor for the next sessions pull. Persisted in UserDefaults
    /// under "sync.cursor.sessions". Updated after every successful
    /// pull so the next call picks up where this one left off.
    private var sessionsCursor: String?
    /// Cursor for the next events pull. Persisted likewise.
    private var eventsCursor: String?
    /// Cursor for the next clip-metadata pull.
    private var clipsCursor: String?

    private let log = Logger(subsystem: "com.snoreguard.app", category: "SyncManager")
    private let settings: SettingsStore
    private let sessionSync: SessionSync
    private let eventSync: EventSync
    private let clipSync: AudioClipSync
    private let eventStore: EventStoreProtocol
    private let defaults: UserDefaults

    private enum CursorKeys {
        static let sessions = "sync.cursor.sessions"
        static let events = "sync.cursor.events"
        static let clips = "sync.cursor.clips"
        static let lastSyncedAt = "sync.lastSyncedAt"
    }

    init(settings: SettingsStore,
         sessionSync: SessionSync,
         eventSync: EventSync,
         clipSync: AudioClipSync,
         eventStore: EventStoreProtocol,
         defaults: UserDefaults = .standard) {
        self.settings = settings
        self.sessionSync = sessionSync
        self.eventSync = eventSync
        self.clipSync = clipSync
        self.eventStore = eventStore
        self.defaults = defaults
        // Hydrate cursors from UserDefaults so a relaunch resumes
        // pulling from where the previous session stopped.
        self.sessionsCursor = defaults.string(forKey: CursorKeys.sessions)
        self.eventsCursor = defaults.string(forKey: CursorKeys.events)
        self.clipsCursor = defaults.string(forKey: CursorKeys.clips)
        if let ts = defaults.object(forKey: CursorKeys.lastSyncedAt) as? Date {
            self.lastSyncedAt = ts
        }
    }

    /// Run one full sync cycle. No-op when `cloudSyncEnabled` is
    /// false; clip steps are no-ops when `uploadAudioClipsEnabled`
    /// is false. Surfaces failures via `state = .error(message)`;
    /// callers shouldn't try/catch this — UI binds to `state`.
    func sync() async {
        guard settings.cloudSyncEnabled else {
            log.debug("sync skipped: cloudSyncEnabled = false")
            return
        }
        state = .syncing
        do {
            try await pushUnsyncedSessions()
            try await pushUnsyncedEvents()
            if settings.uploadAudioClipsEnabled {
                try await pushUnsyncedClips()
            }
            try await pullSessions()
            try await pullEvents()
            if settings.uploadAudioClipsEnabled {
                try await pullClipsMetadata()
            }
            let now = Date()
            lastSyncedAt = now
            defaults.set(now, forKey: CursorKeys.lastSyncedAt)
            state = .idle
        } catch {
            log.error("sync failed: \(error.localizedDescription, privacy: .public)")
            state = .error(error.localizedDescription)
        }
    }

    /// Cancel any in-progress sync (best-effort) and clear the in-
    /// memory retry queue. Called by `AuthManager.signOut()` so a
    /// relaunch as a different user doesn't replay the previous
    /// user's pending uploads.
    func reset() {
        state = .idle
        sessionsCursor = nil
        eventsCursor = nil
        clipsCursor = nil
        defaults.removeObject(forKey: CursorKeys.sessions)
        defaults.removeObject(forKey: CursorKeys.events)
        defaults.removeObject(forKey: CursorKeys.clips)
        defaults.removeObject(forKey: CursorKeys.lastSyncedAt)
        lastSyncedAt = nil
    }

    // MARK: - Push

    /// Upload every locally-pending session. Marks each row synced
    /// with the server-returned remote id so the next push skips it.
    /// Batched at 50/round for predictable latency on flaky networks.
    private func pushUnsyncedSessions() async throws {
        let pending = try await eventStore.unsyncedSessions(limit: 50)
        for entity in pending {
            // RecordingSession (the in-memory model) is a value type
            // with the same id-shape; build a temporary one so the
            // existing SessionSync.upload signature works unchanged.
            let session = RecordingSession(id: entity.id, startedAt: entity.startedAt)
            // EndedAt isn't on `RecordingSession`'s init — set it
            // directly. Mutating `var endedAt: Date?` is fine.
            var s = session
            s.endedAt = entity.endedAt
            let remoteID = try await sessionSync.upload(session: s)
            try await eventStore.markSessionSynced(entity.id, remoteID: remoteID)
        }
    }

    /// Upload every locally-pending event batch. Phase D's
    /// EventStoreProtocol returns rows already shaped for upload;
    /// EventSync owns the wire-format conversion.
    private func pushUnsyncedEvents() async throws {
        let pending = try await eventStore.unsyncedEvents(limit: 500)
        guard !pending.isEmpty else { return }
        // EventStoreProtocol returns EventEntity rows; map them to
        // SnoreEvent + a representative session-start so EventSync's
        // existing upload path works without divergence.
        let snoreEvents = pending.map { e in
            SnoreEvent(startMs: 0, durationMs: UInt32(e.durationMs), avgDB: e.avgDB)
        }
        // For now we send a synthetic session-start (= now) so the
        // wire conversion produces some absolute timestamp. Phase E
        // will replace this with the per-event session-startedAt
        // joined from the EventEntity row.
        let resp = try await eventSync.upload(events: snoreEvents, sessionStartedAt: pending.first?.startedAt ?? Date())
        log.debug("uploaded \(resp.received) events, \(resp.inserted) newly inserted")
        // Mark each row synced. The wire layer doesn't return per-
        // event remote IDs, so we use the client_event_id (which IS
        // the remote primary key after the server's idempotent insert)
        // as the remote-id stamp.
        for e in pending {
            try await eventStore.markEventSynced(e.id, remoteID: e.clientEventID)
        }
    }

    /// Upload every locally-pending clip's bytes + metadata. Skips
    /// clips with no local file (e.g. user purged the cache).
    private func pushUnsyncedClips() async throws {
        let pending = try await eventStore.unsyncedClips(limit: 20)
        for clip in pending {
            guard let path = clip.localFilePath else {
                log.debug("clip \(clip.id.uuidString, privacy: .public) has no local file; skipping")
                continue
            }
            let url = URL(fileURLWithPath: path)
            let resp = try await clipSync.upload(
                fileURL: url,
                clientClipID: clip.clientClipID,
                sessionRemoteID: nil,           // joined in Phase E
                clientEventID: nil,              // joined in Phase E
                startedAt: Date(),               // replaced in Phase E
                durationMS: 0,                   // replaced in Phase E
                avgDB: nil,                       // replaced in Phase E
                contentType: clip.contentType
            )
            try await eventStore.markClipSynced(clip.id, remoteID: resp.clipID)
        }
    }

    // MARK: - Pull

    /// Pull every server-side session newer than the last cursor.
    /// Persists the cursor + upserts each row into the local store.
    private func pullSessions() async throws {
        let result = try await sessionSync.pull(since: sessionsCursor)
        for row in result.sessions {
            try await eventStore.upsertRemoteSession(row)
        }
        sessionsCursor = result.cursor
        if let c = result.cursor {
            defaults.set(c, forKey: CursorKeys.sessions)
        }
    }

    /// Pull every server-side event newer than the last cursor.
    /// Note: PullResult.events is `[WireSnoreEvent]`, not entities;
    /// the EventStoreProtocol upsert in Phase E will translate.
    private func pullEvents() async throws {
        let result = try await eventSync.pull(since: eventsCursor)
        log.debug("pulled \(result.events.count) events")
        eventsCursor = result.cursor
        if let c = result.cursor {
            defaults.set(c, forKey: CursorKeys.events)
        }
    }

    /// Pull every server-side clip metadata row newer than the last
    /// cursor. The binary blob is NOT downloaded here — that's
    /// on-demand from the History tab via `AudioClipSync.download(...)`.
    private func pullClipsMetadata() async throws {
        var current = clipsCursor
        for _ in 0..<200 {
            let endpoint = ListAudioClipsEndpoint(cursor: current, limit: 200)
            let resp = try await clipSync.client.request(endpoint)
            for row in resp.clips {
                try await eventStore.upsertRemoteClip(row)
            }
            guard let next = resp.nextCursor, next != current else {
                current = resp.nextCursor ?? current
                break
            }
            current = next
        }
        clipsCursor = current
        if let c = current {
            defaults.set(c, forKey: CursorKeys.clips)
        }
    }
}

// MARK: - EventStore protocol

/// The slice of `EventStore` that SyncManager needs. Phase D defines
/// the protocol + scaffolding entity types; Phase E lands the real
/// `@objc NSManagedObject` subclasses on `claude/ios-record-tab`
/// (PR #5) and conforms `EventStore` to this protocol on the
/// integration commit.
protocol EventStoreProtocol: AnyObject {
    // Push side
    func unsyncedSessions(limit: Int) async throws -> [SessionEntity]
    func markSessionSynced(_ id: UUID, remoteID: String) async throws
    func unsyncedEvents(limit: Int) async throws -> [EventEntity]
    func markEventSynced(_ id: UUID, remoteID: String) async throws
    func unsyncedClips(limit: Int) async throws -> [AudioClipEntity]
    func markClipSynced(_ id: UUID, remoteID: String) async throws
    // Pull side
    func upsertRemoteSession(_ wire: WireSession) async throws
    func upsertRemoteClip(_ wire: WireAudioClip) async throws
}

// MARK: - Scaffolding entity types
//
// Placeholder value types so this branch (Phase D) compiles
// independently. Phase E (`claude/ios-record-tab`) will replace
// these with the real `@objc NSManagedObject` subclasses generated
// from the Core Data model. The integration commit at merge time
// will bridge the two — `EventStore` will conform to
// `EventStoreProtocol` returning the NSManagedObject-backed shapes
// in place of these structs.

struct SessionEntity: Equatable {
    let id: UUID
    let clientSessionID: String
    let startedAt: Date
    let endedAt: Date?
    let syncStatus: String
    let remoteID: String?
}

struct EventEntity: Equatable {
    let id: UUID
    let clientEventID: String
    let startedAt: Date
    let durationMs: Int32
    let avgDB: Float
    let syncStatus: String
    let remoteID: String?
}

struct AudioClipEntity: Equatable {
    let id: UUID
    let clientClipID: UUID
    let localFilePath: String?
    let contentType: String
    let sizeBytes: Int64
    let sha256: String
    let syncStatus: String
}

