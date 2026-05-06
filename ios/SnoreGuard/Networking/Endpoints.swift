// Endpoints.swift
//
// Strongly-typed endpoint definitions for the SnoreGuard sync-service.
// Each endpoint declares its method, path, body, and response type so
// `APIClient.request(_:)` can render it into a `URLRequest` without
// duplicating per-call wiring.
//
// Shape mirrors `backend/README.md` (Phase-1 sync-service):
//   POST /auth/apple        — exchange Apple identity_token for tokens
//   POST /auth/refresh      — rotate a refresh token; replaced_by is set
//   POST /auth/logout       — revoke access JTI + refresh family
//   POST /events            — bulk upload (≤500), idempotent on client_event_id
//   GET  /events            — list events, opaque compound cursor
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
