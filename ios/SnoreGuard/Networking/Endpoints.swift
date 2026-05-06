// Endpoints.swift
//
// Strongly-typed endpoint definitions for the SnoreGuard sync-service.
// Each endpoint declares its method, path, body, and response type so
// `APIClient.request(_:)` can render it into a `URLRequest` without
// duplicating per-call wiring.
//
// Shape mirrors `backend/README.md` (Phase-1 sync-service + Phase-B
// audio cloud sync):
//   POST   /auth/apple             — exchange Apple identity_token for tokens
//   POST   /auth/refresh           — rotate a refresh token; replaced_by is set
//   POST   /auth/logout            — revoke access JTI + refresh family
//   POST   /events                 — bulk upload (≤500), idempotent on client_event_id
//   GET    /events                 — list events, opaque compound cursor
//   POST   /sessions               — open/refresh recording session, idempotent on client_session_id
//   GET    /sessions               — list sessions, newest-first, cursor paged
//   POST   /audio/clips            — upload one audio clip (multipart), idempotent on client_clip_id
//   GET    /audio/clips            — list own clip metadata, newest-first
//   GET    /audio/clips/{id}/download — stream raw clip bytes
//   DELETE /audio/clips/{id}       — soft-delete clip + best-effort blob delete
//
// All bodies and responses use snake_case JSON; `CodingKeys` are
// declared explicitly so we don't need `keyDecodingStrategy` (which
// would round-trip through `convertFromSnakeCase` and lose information
// on already-correct keys).

import Foundation

/// HTTP method enum — Foundation's `URLRequest` takes a `String` for
/// `httpMethod`, but a typed enum is easier to grep for.
enum HTTPMethod: String {
    case get = "GET"
    case post = "POST"
    case delete = "DELETE"
}

/// Base configuration for the API client. The base URL must include
/// the scheme; trailing slash is stripped on use (see APIClient).
struct APIConfig {
    let baseURL: URL
    /// Sent as the User-Agent suffix, e.g. "SnoreGuard/0.1.0". Tracked
    /// so a server-side observability dashboard can break crash rates
    /// down per app version.
    let appVersion: String

    static let `default` = APIConfig(
        baseURL: URL(string: "http://localhost:8080")!,
        appVersion: "0.1.0"
    )
}

/// Endpoint shape — `APIClient` renders an `Endpoint` into a `URLRequest`.
/// `Response` must be `Decodable`; for endpoints that return no body
/// (HTTP 204 or `{}`), use `EmptyResponse`.
protocol Endpoint {
    associatedtype Response: Decodable
    var method: HTTPMethod { get }
    /// Absolute path (with leading slash) — `APIClient` joins this onto
    /// `APIConfig.baseURL`, stripping the leading slash if present so
    /// `URL.appendingPathComponent` doesn't double up the separator.
    var path: String { get }
    /// Pre-encoded body bytes. Endpoints that take an Encodable struct
    /// pre-encode it in their initializer so this protocol stays free
    /// of generics over the request payload.
    var body: Data? { get }
    /// Whether `APIClient` should attach an `Authorization: Bearer …`
    /// header. `false` for the auth endpoints that issue tokens.
    var requiresAuth: Bool { get }
    /// Optional query items appended to the URL. Default `nil`.
    var queryItems: [URLQueryItem]? { get }
}

extension Endpoint {
    var body: Data? { nil }
    var requiresAuth: Bool { true }
    var queryItems: [URLQueryItem]? { nil }
}

// MARK: - Auth endpoints

/// `POST /auth/apple` — exchange the iOS-side Apple identity token for
/// an access + refresh token pair. No bearer required (this is how you
/// *get* the bearer).
struct AuthAppleEndpoint: Endpoint {
    typealias Response = AuthTokensResponse
    let method: HTTPMethod = .post
    let path: String = "/auth/apple"
    let body: Data?
    let requiresAuth: Bool = false

    init(identityToken: String) {
        // Hand-rolled JSON to avoid spinning up an Encoder for one field.
        // `JSONSerialization` produces compact bytes and isn't subject
        // to the snake_case key strategy issues with mixed-case JSON.
        let payload: [String: Any] = ["identity_token": identityToken]
        self.body = try? JSONSerialization.data(withJSONObject: payload, options: [])
    }
}

/// `POST /auth/refresh` — rotate refresh token. Backend sets
/// `replaced_by` on the previous token; reusing the previous token
/// triggers family-wide revocation (refresh-token theft detection).
struct AuthRefreshEndpoint: Endpoint {
    typealias Response = AuthTokensResponse
    let method: HTTPMethod = .post
    let path: String = "/auth/refresh"
    let body: Data?
    let requiresAuth: Bool = false

    init(refreshToken: String) {
        let payload: [String: Any] = ["refresh_token": refreshToken]
        self.body = try? JSONSerialization.data(withJSONObject: payload, options: [])
    }
}

/// `POST /auth/logout` — revokes access JTI + refresh family.
/// Requires the access token in the `Authorization` header (so the
/// backend can pull the JTI) and the refresh token in the body (so it
/// can revoke the matching family).
struct AuthLogoutEndpoint: Endpoint {
    typealias Response = EmptyResponse
    let method: HTTPMethod = .post
    let path: String = "/auth/logout"
    let body: Data?
    let requiresAuth: Bool = true

    init(refreshToken: String) {
        let payload: [String: Any] = ["refresh_token": refreshToken]
        self.body = try? JSONSerialization.data(withJSONObject: payload, options: [])
    }
}

// MARK: - Events endpoints

/// `POST /events` — bulk-upload up to 500 events. Idempotent on
/// `client_event_id`; safe to retry. All-or-nothing on validation
/// failures: a single bad row rejects the whole batch.
struct PostEventsEndpoint: Endpoint {
    typealias Response = PostEventsResponse
    let method: HTTPMethod = .post
    let path: String = "/events"
    let body: Data?
    let requiresAuth: Bool = true

    init(events: [WireSnoreEvent]) {
        let payload = PostEventsRequest(events: events)
        let encoder = JSONEncoder()
        // Match the backend's expected timestamp format (RFC3339Nano).
        // Avg dB is `Float`; backend rejects non-finite, so any NaN/Inf
        // must be filtered upstream — keep the payload faithful here.
        encoder.dateEncodingStrategy = .iso8601
        self.body = try? encoder.encode(payload)
    }
}

/// `GET /events?cursor=…&limit=…` — paginated list of own events,
/// ascending by `(received_at, id)`. The cursor is opaque (base64) and
/// must NOT be parsed by the client; the backend's compound cursor is
/// `base64url(rfc3339nano + "|" + uuid)`. First page uses cursor=nil.
struct ListEventsEndpoint: Endpoint {
    typealias Response = ListEventsResponse
    let method: HTTPMethod = .get
    let path: String = "/events"
    let requiresAuth: Bool = true
    let queryItems: [URLQueryItem]?

    init(cursor: String?, limit: Int? = nil) {
        var items: [URLQueryItem] = []
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        if let limit {
            items.append(URLQueryItem(name: "limit", value: String(limit)))
        }
        self.queryItems = items.isEmpty ? nil : items
    }
}

// MARK: - Wire types

/// Response from `/auth/apple` and `/auth/refresh`. The two timestamps
/// let the client pre-emptively refresh before a 401 even arrives —
/// see `AuthManager.currentAccessToken()`.
struct AuthTokensResponse: Decodable, Equatable {
    let userID: String
    let accessToken: String
    let accessTokenExpiresAt: Date
    let refreshToken: String
    let refreshTokenExpiresAt: Date

    enum CodingKeys: String, CodingKey {
        case userID = "user_id"
        case accessToken = "access_token"
        case accessTokenExpiresAt = "access_token_expires_at"
        case refreshToken = "refresh_token"
        case refreshTokenExpiresAt = "refresh_token_expires_at"
    }
}

/// Used as a marker for endpoints whose body we don't decode (logout).
/// `APIClient.request` short-circuits decoding for this exact type so
/// even an empty `200 OK` body won't fail JSON parsing.
struct EmptyResponse: Decodable, Equatable {}

/// Response shape from `POST /events`. `received` is the input batch
/// length; `inserted` excludes idempotent duplicates. The two values
/// will diverge after a retry — that's expected, not an error.
struct PostEventsResponse: Decodable, Equatable {
    let inserted: Int
    let received: Int
}

/// Response shape from `GET /events`. `nextCursor == nil` indicates
/// the caller has reached the tail; the next pull starts from the same
/// cursor again to pick up newly-uploaded rows.
struct ListEventsResponse: Decodable, Equatable {
    let events: [WireSnoreEvent]
    let nextCursor: String?

    enum CodingKeys: String, CodingKey {
        case events
        case nextCursor = "next_cursor"
    }
}

/// Internal request shape for `POST /events`. Held private because
/// callers should construct `PostEventsEndpoint` directly with an
/// array of `WireSnoreEvent`.
private struct PostEventsRequest: Encodable {
    let events: [WireSnoreEvent]
}

/// On-the-wire form of a `SnoreEvent`. Distinct from the in-memory
/// `SnoreEvent` (which carries `start_ms` relative to the detector's
/// boot time) so we can encode an absolute `started_at` timestamp the
/// backend accepts. Conversion is in `EventSync.upload(events:)`.
struct WireSnoreEvent: Codable, Equatable, Hashable {
    /// Stable per-device UUID; backend dedupes on this. Reusing the
    /// same UUID for the same physical event lets retries be no-ops.
    let clientEventID: String
    /// Absolute event start time. Backend validates RFC3339; our
    /// encoder emits RFC3339 via `dateEncodingStrategy = .iso8601`.
    let startedAt: Date
    /// Event duration in milliseconds. Backend stores as int32.
    let durationMS: Int32
    /// Average uncalibrated dB across the event window. Backend
    /// rejects NaN/Inf and values outside [0, 200] — caller filters.
    let avgDB: Float
    /// Optional client-assigned session identifier. The Phase-2-B
    /// network client always passes `nil`; later phases may stamp a
    /// per-night UUID so cross-device pulls can group nights.
    let sessionID: String?

    enum CodingKeys: String, CodingKey {
        case clientEventID = "client_event_id"
        case startedAt = "started_at"
        case durationMS = "duration_ms"
        case avgDB = "avg_db"
        case sessionID = "session_id"
    }
}

// MARK: - Sessions endpoints (Phase D)

/// `POST /sessions` — record (or refresh) a recording session.
/// Idempotent on `client_session_id`. Re-posting the same id returns
/// the same `session_id` and updates `ended_at`/`device_name`/
/// `app_version` only when the new value is non-null (COALESCE
/// semantics on the backend; see backend/internal/store/clips.go).
struct PostSessionEndpoint: Endpoint {
    typealias Response = PostSessionResponse
    let method: HTTPMethod = .post
    let path: String = "/sessions"
    let body: Data?
    let requiresAuth: Bool = true

    init(clientSessionID: String,
         startedAt: Date,
         endedAt: Date?,
         deviceName: String?,
         appVersion: String?) throws {
        let req = PostSessionRequest(
            clientSessionID: clientSessionID,
            startedAt: startedAt,
            endedAt: endedAt,
            deviceName: deviceName,
            appVersion: appVersion
        )
        self.body = try JSONEncoder.snoreguard.encode(req)
    }
}

/// `GET /sessions?cursor=…&limit=…` — newest-first paginated list of
/// the authenticated user's recording sessions. Cursor is opaque
/// base64; `limit` clamps to `[1, 1000]` (backend default 100).
struct ListSessionsEndpoint: Endpoint {
    typealias Response = ListSessionsResponse
    let method: HTTPMethod = .get
    let path: String = "/sessions"
    let requiresAuth: Bool = true
    let queryItems: [URLQueryItem]?

    init(cursor: String? = nil, limit: Int = 200) {
        var items: [URLQueryItem] = [URLQueryItem(name: "limit", value: String(limit))]
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        self.queryItems = items
    }
}

/// Request body for `POST /sessions`. Mirrors backend
/// `postSessionRequest` (sessions.go) field-for-field.
struct PostSessionRequest: Encodable, Equatable {
    let clientSessionID: String
    let startedAt: Date
    let endedAt: Date?
    let deviceName: String?
    let appVersion: String?

    enum CodingKeys: String, CodingKey {
        case clientSessionID = "client_session_id"
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case deviceName = "device_name"
        case appVersion = "app_version"
    }
}

/// Response shape for `POST /sessions`. `created == false` when this
/// was an idempotent retry — that's a successful no-op, NOT an error.
struct PostSessionResponse: Decodable, Equatable {
    let sessionID: String
    let created: Bool

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id"
        case created
    }
}

/// Response shape for `GET /sessions`. `nextCursor == nil` means the
/// caller has reached the tail; pass it back next pull to pick up
/// rows that landed in the meantime.
struct ListSessionsResponse: Decodable, Equatable {
    let sessions: [WireSession]
    let nextCursor: String?

    enum CodingKeys: String, CodingKey {
        case sessions
        case nextCursor = "next_cursor"
    }
}

/// On-the-wire shape of a recording session (sessionDTO in the
/// backend). The `device_name` / `app_version` / `ended_at` fields
/// are `omitempty` server-side, hence Optional here.
struct WireSession: Codable, Equatable, Hashable {
    let sessionID: String
    let clientSessionID: String
    let startedAt: Date
    let endedAt: Date?
    let deviceName: String?
    let appVersion: String?

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id"
        case clientSessionID = "client_session_id"
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case deviceName = "device_name"
        case appVersion = "app_version"
    }
}

// MARK: - Audio-clip endpoints (Phase D)

/// `GET /audio/clips?cursor=…&limit=…` — newest-first paginated list
/// of the user's clip metadata. Identical query-shape to
/// `ListSessionsEndpoint`. The body bytes are fetched separately via
/// `DownloadAudioClipEndpoint` on demand.
struct ListAudioClipsEndpoint: Endpoint {
    typealias Response = ListAudioClipsResponse
    let method: HTTPMethod = .get
    let path: String = "/audio/clips"
    let requiresAuth: Bool = true
    let queryItems: [URLQueryItem]?

    init(cursor: String? = nil, limit: Int = 200) {
        var items: [URLQueryItem] = [URLQueryItem(name: "limit", value: String(limit))]
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        self.queryItems = items
    }
}

/// Response shape for `GET /audio/clips`. Same cursor convention as
/// the events / sessions list endpoints.
struct ListAudioClipsResponse: Decodable, Equatable {
    let clips: [WireAudioClip]
    let nextCursor: String?

    enum CodingKeys: String, CodingKey {
        case clips
        case nextCursor = "next_cursor"
    }
}

/// On-the-wire shape of an audio clip. Mirrors backend `clipDTO`
/// (clips.go). NOTE: the list endpoint's `clipDTO` does NOT include
/// `object_key` — only the POST-create response surfaces it. We make
/// it optional here so a future backend revision that decides to
/// expose it on list won't break decoding.
struct WireAudioClip: Codable, Equatable, Hashable {
    let clipID: String
    /// Optional remote-session UUID. Empty/missing in the backend's
    /// JSON when the clip wasn't attached to a session.
    let sessionID: String?
    let clientClipID: String
    let clientEventID: String?
    let startedAt: Date
    let durationMS: Int32
    let avgDB: Float?
    let contentType: String
    let sizeBytes: Int64
    let sha256: String
    /// Server-controlled blob key. Optional because list/clipDTO does
    /// NOT include it; only the POST-create response does.
    let objectKey: String?
    let uploadedAt: Date

    enum CodingKeys: String, CodingKey {
        case clipID = "clip_id"
        case sessionID = "session_id"
        case clientClipID = "client_clip_id"
        case clientEventID = "client_event_id"
        case startedAt = "started_at"
        case durationMS = "duration_ms"
        case avgDB = "avg_db"
        case contentType = "content_type"
        case sizeBytes = "size_bytes"
        case sha256
        case objectKey = "object_key"
        case uploadedAt = "uploaded_at"
    }
}

/// `GET /audio/clips/{id}/download` — stream the raw audio bytes.
/// Uses `APIClient.requestData(_:)` rather than the JSON-decoding
/// path because the body is binary, not JSON.
///
/// `Response = Data` is sentinel-typed: `APIClient.request(_:)` would
/// fail to decode raw bytes as JSON, so callers must use
/// `APIClient.requestData(_:)`. The associated type still satisfies
/// `Decodable` because `Data` itself conforms.
struct DownloadAudioClipEndpoint: Endpoint {
    typealias Response = Data
    let method: HTTPMethod = .get
    let path: String
    let body: Data? = nil
    let requiresAuth: Bool = true

    init(clipID: String) {
        self.path = "/audio/clips/\(clipID)/download"
    }
}

/// `DELETE /audio/clips/{id}` — soft-delete the metadata row + best-
/// effort blob delete. Server returns `{"status": "deleted"}` on
/// success; we don't surface that to callers.
struct DeleteAudioClipEndpoint: Endpoint {
    typealias Response = EmptyResponse
    let method: HTTPMethod = .delete
    let path: String
    let body: Data? = nil
    let requiresAuth: Bool = true

    init(clipID: String) {
        self.path = "/audio/clips/\(clipID)"
    }
}

/// Response from `POST /audio/clips`. Decoded directly inside
/// `AudioClipSync.upload(...)` since multipart upload bypasses the
/// stock `Endpoint` flow.
struct PostAudioClipResponse: Decodable, Equatable {
    let clipID: String
    let created: Bool
    let objectKey: String
    let uploadedAt: Date

    enum CodingKeys: String, CodingKey {
        case clipID = "clip_id"
        case created
        case objectKey = "object_key"
        case uploadedAt = "uploaded_at"
    }
}

// MARK: - Shared encoder / decoder

/// Shared `JSONEncoder` for endpoints that pre-encode their bodies.
/// `dateEncodingStrategy = .iso8601` matches the backend's RFC3339
/// expectations. We deliberately do NOT set `keyEncodingStrategy =
/// .convertToSnakeCase` — every Encodable type in this file declares
/// explicit `CodingKeys`, and `convertToSnakeCase` would mangle
/// already-correct keys (e.g. turning `client_session_id` from a
/// CodingKey into a re-snake-cased `clientsessionid`).
extension JSONEncoder {
    static let snoreguard: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()
}

/// Shared `JSONDecoder` matching the backend's mixed-precision
/// timestamp output. Same custom strategy as `APIClient.decoder`
/// (kept in sync); this extension exists so callers outside the
/// actor (e.g. `AudioClipSync`'s manual multipart path) can decode
/// responses without re-instantiating the decoder.
extension JSONDecoder {
    static let snoreguard: JSONDecoder = {
        let d = JSONDecoder()
        let withFraction = ISO8601DateFormatter()
        withFraction.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        d.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            if let date = withFraction.date(from: str) { return date }
            if let date = plain.date(from: str) { return date }
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Expected ISO8601 date, got: \(str)"
            )
        }
        return d
    }()
}

/// Shared ISO8601 formatter used by `AudioClipSync` to stamp the
/// `started_at` field inside multipart metadata (where we hand-build
/// JSON via `JSONSerialization` for forward-compatible part shape).
extension ISO8601DateFormatter {
    static let snoreguard: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
}

/// Backend's standard `{"error": "..."}` error envelope, exposed at
/// file scope so `AudioClipSync` (which bypasses the standard
/// `APIClient.request` path for multipart upload) can decode it.
struct WireErrorBody: Decodable, Equatable {
    let error: String
}
