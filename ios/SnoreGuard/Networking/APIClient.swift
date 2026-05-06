// APIClient.swift
//
// Generic URLSession-backed HTTP client. Renders an `Endpoint` into a
// `URLRequest`, performs the request via `URLSession.data(for:)`
// (iOS 15+ async overload), and decodes the response.
//
// Cross-cutting concerns handled here:
//   - JSON decoding with ISO8601-with-fractional-seconds dates
//   - Bearer auth from `AuthManager` for `requiresAuth` endpoints
//   - Single auto-retry on `401` after `AuthManager.refresh()`
//   - 429 surfacing as `APIError.rateLimited(retryAfter:)`
//   - `{"error": "..."}` body decoding for non-2xx responses
//
// `actor`-isolated so the auto-retry path can't race itself; concurrent
// callers from different tasks are serialized through the actor mailbox
// only at the boundary, not during the network I/O itself (the actor
// re-enters cooperatively at each `await`).

import Foundation

/// Errors surfaced from the API client. Never thrown by `AuthManager`
/// directly — callers can match on `.server(401, _)` to decide whether
/// to push the user back to sign-in.
enum APIError: Error, LocalizedError, Equatable {
    /// `URLResponse` wasn't an `HTTPURLResponse` — should be unreachable
    /// for HTTP/HTTPS URLs, kept for completeness.
    case invalidResponse
    /// Non-2xx, non-429 response. `message` is decoded from the
    /// backend's `{"error": "..."}` body when present, else `HTTP <n>`.
    case server(statusCode: Int, message: String)
    /// HTTP 429. `retryAfterSeconds` is parsed from `Retry-After`; nil
    /// when the header is missing or unparseable.
    case rateLimited(retryAfterSeconds: TimeInterval?)
    /// Body decoded as 2xx but JSON parsing failed. The underlying
    /// error is wrapped so debug builds can log it.
    case decoding(message: String)

    var errorDescription: String? {
        switch self {
        case .invalidResponse:
            return "Server returned a non-HTTP response."
        case .server(let code, let msg):
            return "Server error \(code): \(msg)"
        case .rateLimited(let secs):
            if let s = secs {
                return "Rate-limited; retry in \(Int(s))s"
            }
            return "Rate-limited; please try again shortly."
        case .decoding(let msg):
            return "Decoding failed: \(msg)"
        }
    }

    static func == (lhs: APIError, rhs: APIError) -> Bool {
        switch (lhs, rhs) {
        case (.invalidResponse, .invalidResponse): return true
        case (.server(let a, let am), .server(let b, let bm)): return a == b && am == bm
        case (.rateLimited(let a), .rateLimited(let b)):
            // TimeInterval == ; nil-vs-nil also equates.
            return a == b
        case (.decoding(let a), .decoding(let b)): return a == b
        default: return false
        }
    }
}

/// Backend's standard error envelope. Made internal so test fixtures
/// can produce equivalent payloads without a duplicate definition.
struct ErrorBody: Decodable {
    let error: String
}

actor APIClient {
    private let config: APIConfig
    private let session: URLSession
    private let auth: AuthManager

    /// Decoder shared across all requests. Snake-case keys are handled
    /// per-type via explicit `CodingKeys`, so no global key strategy.
    /// The custom date strategy accepts ISO8601 with OR without
    /// fractional seconds — Go's `time.Time` JSON marshaller emits
    /// `2026-05-06T13:00:00Z` for whole-second timestamps and
    /// `…13:00:00.123456789Z` for sub-second ones; both must parse.
    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        let withFraction = ISO8601DateFormatter()
        withFraction.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]

        d.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            if let date = withFraction.date(from: str) {
                return date
            }
            if let date = plain.date(from: str) {
                return date
            }
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Expected ISO8601 date, got: \(str)"
            )
        }
        return d
    }()

    /// Encoder for endpoints that delegate to `APIClient.encode(_:)`.
    /// Today the only such caller path is unused — endpoints pre-encode
    /// their own bodies — but it's exposed so future callers don't
    /// reinvent the snake_case + ISO8601 combo.
    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()

    init(config: APIConfig = .default,
         session: URLSession = .shared,
         auth: AuthManager) {
        self.config = config
        self.session = session
        self.auth = auth
    }

    /// Encode a payload with the client's standard encoder. Useful for
    /// callers that want to construct a body without depending on
    /// `JSONEncoder` themselves.
    func encode<T: Encodable>(_ payload: T) throws -> Data {
        try encoder.encode(payload)
    }

    /// Render an endpoint and execute the request. On HTTP 401 (and only
    /// when the endpoint requires auth), runs `AuthManager.refresh()`
    /// once and retries. The retry is NOT recursive — `_perform` skips
    /// the refresh path on the second attempt so a stuck 401 surfaces
    /// to the caller as `.server(401, _)`. That's the signal to push
    /// the user back to sign-in.
    func request<E: Endpoint>(_ endpoint: E) async throws -> E.Response {
        try await _perform(endpoint, allowRefreshRetry: true)
    }

    /// Internal helper so the retry path can disable further retries.
    private func _perform<E: Endpoint>(_ endpoint: E, allowRefreshRetry: Bool) async throws -> E.Response {
        let req = try buildRequest(for: endpoint)

        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }

        // 401 → single-flight refresh, then retry exactly once.
        // Coalescing of concurrent refresh attempts is in AuthManager.
        if http.statusCode == 401 && endpoint.requiresAuth && allowRefreshRetry {
            try await auth.refresh()
            return try await _perform(endpoint, allowRefreshRetry: false)
        }

        if http.statusCode == 429 {
            // `Retry-After` may be either a delta in seconds or an
            // HTTP-date. The backend emits the integer-seconds form;
            // we don't bother parsing the date variant.
            let retry = http.value(forHTTPHeaderField: "Retry-After")
                .flatMap { TimeInterval($0) }
            throw APIError.rateLimited(retryAfterSeconds: retry)
        }

        guard (200..<300).contains(http.statusCode) else {
            // Try to decode the standard `{"error": "..."}` envelope.
            // Falls back to a generic message if the body is empty or
            // not JSON (e.g. an upstream proxy returning HTML).
            let msg = (try? decoder.decode(ErrorBody.self, from: data))?.error
                ?? "HTTP \(http.statusCode)"
            throw APIError.server(statusCode: http.statusCode, message: msg)
        }

        // Short-circuit decoding for endpoints with no response body.
        // Cast is safe: the protocol associated type IS `EmptyResponse`.
        if E.Response.self == EmptyResponse.self {
            // swiftlint:disable:next force_cast
            return EmptyResponse() as! E.Response
        }

        do {
            return try decoder.decode(E.Response.self, from: data)
        } catch {
            throw APIError.decoding(message: "\(error)")
        }
    }

    /// Compose the URL + headers + body for an endpoint.
    private func buildRequest<E: Endpoint>(for endpoint: E) async throws -> URLRequest {
        // Strip a leading slash on the path so `appendingPathComponent`
        // doesn't produce `//auth/apple`.
        let pathPart = endpoint.path.hasPrefix("/") ? String(endpoint.path.dropFirst()) : endpoint.path
        var url = config.baseURL.appendingPathComponent(pathPart)

        if let items = endpoint.queryItems, !items.isEmpty {
            // Round-trip through URLComponents to attach query items.
            // `appendingPathComponent` doesn't support a `query` arg.
            if var comps = URLComponents(url: url, resolvingAgainstBaseURL: false) {
                comps.queryItems = items
                if let composed = comps.url {
                    url = composed
                }
            }
        }

        var req = URLRequest(url: url)
        req.httpMethod = endpoint.method.rawValue
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if endpoint.body != nil {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        req.setValue("SnoreGuard/\(config.appVersion)", forHTTPHeaderField: "User-Agent")
        req.httpBody = endpoint.body

        if endpoint.requiresAuth {
            // currentAccessToken() may itself trigger a pre-emptive
            // refresh if the access token's expiry is within 60 s.
            // Either way it returns a usable token or throws.
            let token = try await auth.currentAccessToken()
            req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }

        return req
    }
}
