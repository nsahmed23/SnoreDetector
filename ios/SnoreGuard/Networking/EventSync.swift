// EventSync.swift
//
// Coordinates event upload + cross-device pull. Stateless across calls;
// callers (today: `RecorderViewModel`; post-Phase-2-C: an `EventStore`)
// are the data owners. EventSync is just the thin wrapper that knows
// the wire types and the cursor-paging convention.
//
// Upload: convert `[SnoreEvent]` (relative `start_ms`) into
// `[WireSnoreEvent]` (absolute `started_at`) using the recording
// session's start timestamp, then POST in one batch (≤500). Backend
// dedupes on `client_event_id`, so retrying a previously-sent batch
// is safe.
//
// Pull: paginate through `GET /events?cursor=…` until `next_cursor`
// is nil. The cursor is opaque — we never parse it. A safety bound
// of 200 pages × 200-default-limit = 40k events caps the worst case.

import Foundation

/// Result of `pull(since:)`. The `cursor` is what the caller should
/// store and pass back on the next pull to continue from where this
/// one left off.
struct PullResult: Equatable {
    let events: [WireSnoreEvent]
    let cursor: String?
}

@MainActor
final class EventSync {
    /// Maximum events the backend accepts in one POST. Defined here so
    /// callers don't need to know the limit; `upload` chunks longer
    /// inputs implicitly via assertion at the caller for now (Phase 2-B
    /// keeps the bound as documentation; chunking is a follow-up when
    /// we have offline buffers that can exceed 500 events at once).
    static let maxEventsPerBatch = 500

    /// Default page size used by `pull(since:)`. Backend clamps to
    /// `[1, 1000]`; we pick 200 to balance latency against round-trips.
    static let defaultPageLimit = 200

    /// Hard cap on pages per pull, just so a runaway server can't make
    /// the iOS client spin forever. 200 × 200 = 40k events; anything
    /// past that should be a follow-up `pull` call from the caller.
    static let pullPageBudget = 200

    private let client: APIClient

    init(client: APIClient) {
        self.client = client
    }

    /// Upload up to `maxEventsPerBatch` events. Returns the inserted /
    /// received counts the backend produced. `inserted < received`
    /// means some `client_event_id`s were already known to the
    /// backend — that's a successful idempotent retry, not an error.
    ///
    /// `sessionStartedAt` is the wall-clock time the recording session
    /// began; we add `event.startMs` (relative milliseconds since
    /// detector boot) to derive each event's absolute `started_at`.
    @discardableResult
    func upload(events: [SnoreEvent], sessionStartedAt: Date) async throws -> PostEventsResponse {
        precondition(events.count <= Self.maxEventsPerBatch,
                     "EventSync.upload received more events than the backend accepts in one call")

        let wire: [WireSnoreEvent] = events.map { event in
            // Absolute timestamp: session start + relative offset.
            let started = sessionStartedAt.addingTimeInterval(TimeInterval(event.startMs) / 1000.0)
            return WireSnoreEvent(
                clientEventID: event.id.uuidString,
                startedAt: started,
                durationMS: Int32(event.durationMs),
                avgDB: event.avgDB,
                // Phase 2-B doesn't stamp a session id; `RecordingSession.id`
                // is a candidate for later phases when we want cross-device
                // night grouping.
                sessionID: nil
            )
        }

        return try await client.request(PostEventsEndpoint(events: wire))
    }

    /// Pull every event newer than `cursor`, paging until exhausted.
    /// Caller persists the returned cursor opaquely; passing it back
    /// to a subsequent `pull` resumes from where this one stopped.
    ///
    /// Returns the final cursor even on the empty-page boundary, so a
    /// caller that polls every minute can keep using the same cursor
    /// and only get new rows on subsequent calls.
    func pull(since cursor: String?) async throws -> PullResult {
        var collected: [WireSnoreEvent] = []
        var current = cursor

        for _ in 0..<Self.pullPageBudget {
            let endpoint = ListEventsEndpoint(cursor: current, limit: Self.defaultPageLimit)
            let resp = try await client.request(endpoint)
            collected.append(contentsOf: resp.events)
            // No more pages → done. Loop also bails if the server
            // somehow returns the same cursor twice (would otherwise
            // spin forever; treat as an end-of-stream signal).
            guard let next = resp.nextCursor, next != current else {
                current = resp.nextCursor ?? current
                break
            }
            current = next
        }

        return PullResult(events: collected, cursor: current)
    }
}
