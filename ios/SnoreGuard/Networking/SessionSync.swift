// SessionSync.swift
//
// Coordinates upload + cursor-paged pull of recording sessions.
// Mirrors the EventSync shape: stateless across calls, the caller
// (today: SyncManager) owns the cursor + persistence.
//
// Upload converts a local `RecordingSession` (id is a UUID, no
// remote-id yet) into a `POST /sessions` round-trip. Backend dedupes
// on `client_session_id`, so a retry of an already-uploaded session
// returns the same `session_id` with `created: false` — that's a
// successful idempotent no-op, not an error.
//
// Pull pages through `GET /sessions?cursor=…` until `next_cursor`
// is nil, with the same 200-page safety bound as EventSync. The
// cursor is opaque base64 (compound `(started_at DESC, id DESC)`);
// we never parse it.

import Foundation
import UIKit
import os.log

/// Result of `pull(since:)`. The `cursor` is what the caller stores
/// to resume from where this pull left off.
struct SessionPullResult: Equatable {
    let sessions: [WireSession]
    let cursor: String?
}

@MainActor
final class SessionSync {
    /// Default page size; backend clamps to `[1, 1000]` (default 100).
    /// 200 balances latency against round-trips, same as EventSync.
    static let defaultPageLimit = 200

    /// Hard cap on pages per pull. 200 × 200 = 40k sessions; in
    /// practice a single user will never exceed this in one pass.
    static let pullPageBudget = 200

    private let client: APIClient
    private let log = Logger(subsystem: "com.snoreguard.app", category: "SessionSync")

    init(client: APIClient) {
        self.client = client
    }

    /// Upload a single completed (or in-progress) session. Idempotent
    /// on `client_session_id`; safe to retry. Returns the server-side
    /// `session_id` UUID string. Caller should persist that id so
    /// subsequent clip uploads can attach via `session_id` in the
    /// metadata part of the multipart body.
    ///
    /// `device_name`: we send `UIDevice.current.model` (e.g. "iPhone")
    /// rather than `UIDevice.current.name` because the latter is
    /// user-customisable and frequently contains PII (e.g. "Sarah's
    /// iPhone"). The model alone is enough for the backend's "what
    /// kind of device produced this" needs without leaking identity.
    @discardableResult
    func upload(session: RecordingSession) async throws -> String {
        let endpoint = try PostSessionEndpoint(
            clientSessionID: session.id.uuidString,
            startedAt: session.startedAt,
            endedAt: session.endedAt,
            deviceName: UIDevice.current.model,
            appVersion: Bundle.main.shortVersionStringForSync
        )
        let response = try await client.request(endpoint)
        if !response.created {
            log.debug("session \(session.id.uuidString, privacy: .public) already on server (idempotent)")
        }
        return response.sessionID
    }

    /// Page through every server-side session newer than `cursor`.
    /// Returns the collected rows + the final cursor; caller persists
    /// the cursor for the next pull.
    func pull(since cursor: String?) async throws -> SessionPullResult {
        var collected: [WireSession] = []
        var current = cursor

        for _ in 0..<Self.pullPageBudget {
            let endpoint = ListSessionsEndpoint(cursor: current, limit: Self.defaultPageLimit)
            let resp = try await client.request(endpoint)
            collected.append(contentsOf: resp.sessions)
            // No more pages, OR the server returned the same cursor
            // (would otherwise spin forever; treat as end-of-stream).
            guard let next = resp.nextCursor, next != current else {
                current = resp.nextCursor ?? current
                break
            }
            current = next
        }
        return SessionPullResult(sessions: collected, cursor: current)
    }
}

// MARK: - Bundle helper

/// Tiny helper so SessionSync + AudioClipSync don't each redeclare a
/// `Bundle.shortVersionString` extension. Distinct method name from
/// `SettingsTabView`'s private extension to avoid a duplicate-symbol
/// collision at link time.
extension Bundle {
    var shortVersionStringForSync: String {
        infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
    }
}
