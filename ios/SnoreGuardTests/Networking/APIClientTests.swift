// APIClientTests.swift
//
// URLProtocol-mocked exercises of the APIClient + AuthManager
// integration points. No live network; everything stubbed at the
// URLProtocol layer so we can exercise the 401-then-refresh, 429,
// and concurrent-refresh paths deterministically.
//
// These tests are written blind on Linux. The URLProtocol stub
// pattern is documented at
// https://developer.apple.com/documentation/foundation/urlprotocol .
// First run on macOS may surface mechanical issues — async-test
// `XCTestExpectation` semantics, the exact spelling of
// `URLSessionConfiguration.ephemeral`, etc.

import XCTest
@testable import SnoreGuard

// MARK: - URLProtocol mock

/// Drop-in stub. Tests set `MockURLProtocol.handler` to return either
/// `(Data, HTTPURLResponse)` or throw an `Error`. The protocol is
/// registered on a custom `URLSessionConfiguration`; `URLSession.shared`
/// is never touched.
final class MockURLProtocol: URLProtocol {
    /// Per-test handler. Reset in `setUp`. Static is fine because
    /// `XCTest` runs cases serially within a class.
    static var handler: ((URLRequest) throws -> (HTTPURLResponse, Data))?
    /// Counts every request that reached the protocol. Tests assert
    /// against this for the "single retry" / "single-flight" cases.
    static var requestCount: Int = 0
    /// Captured requests, newest last. Tests inspect this to verify
    /// headers (User-Agent, Authorization, Content-Type) were attached.
    static var observedRequests: [URLRequest] = []

    static func reset() {
        handler = nil
        requestCount = 0
        observedRequests = []
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.requestCount += 1
        Self.observedRequests.append(request)
        guard let handler = Self.handler else {
            client?.urlProtocol(self, didFailWithError: NSError(domain: "MockURLProtocol", code: -1))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() { /* no-op */ }
}

private func makeMockedSession() -> URLSession {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [MockURLProtocol.self]
    return URLSession(configuration: config)
}

private func httpResponse(_ url: URL, status: Int, headers: [String: String] = [:]) -> HTTPURLResponse {
    HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
}

// MARK: - Tests

@MainActor
final class APIClientTests: XCTestCase {
    private let baseURL = URL(string: "https://test.local")!

    override func setUp() {
        super.setUp()
        MockURLProtocol.reset()
    }

    /// Auth-Apple round-trip: returns a token bundle; AuthManager
    /// persists it; `isSignedIn` flips true.
    func testAuthApple_HappyPath() async throws {
        let body = """
        {
          "user_id": "u-1",
          "access_token": "atok",
          "access_token_expires_at": "2030-01-01T00:00:00Z",
          "refresh_token": "rtok",
          "refresh_token_expires_at": "2030-03-01T00:00:00Z"
        }
        """.data(using: .utf8)!
        MockURLProtocol.handler = { req in
            (httpResponse(req.url!, status: 200, headers: ["Content-Type": "application/json"]), body)
        }

        let auth = AuthManager(keychain: TokenKeychain(service: "com.snoreguard.tests.\(UUID())"))
        let client = APIClient(
            config: APIConfig(baseURL: baseURL, appVersion: "test"),
            session: makeMockedSession(),
            auth: auth
        )
        auth.setAPIClient(client)

        let resp = try await client.request(AuthAppleEndpoint(identityToken: "fake-apple-jwt"))

        XCTAssertEqual(resp.userID, "u-1")
        XCTAssertEqual(resp.accessToken, "atok")
        // User-Agent and Content-Type both applied.
        let captured = MockURLProtocol.observedRequests.last!
        XCTAssertEqual(captured.value(forHTTPHeaderField: "Content-Type"), "application/json")
        XCTAssertTrue(captured.value(forHTTPHeaderField: "User-Agent")?.hasPrefix("SnoreGuard/") ?? false)
    }

    /// 401 on a Bearer-required call → refresh fires once → original
    /// request is replayed → 200 returns. Total request count = 3
    /// (original 401, refresh 200, replay 200).
    func test401_TriggersRefreshOnce() async throws {
        let kc = TokenKeychain(service: "com.snoreguard.tests.\(UUID())")
        try kc.store(
            userID: "u-1",
            accessToken: "old",
            accessTokenExpiry: Date().addingTimeInterval(3600),
            refreshToken: "rtok"
        )
        let auth = AuthManager(keychain: kc)
        let client = APIClient(
            config: APIConfig(baseURL: baseURL, appVersion: "test"),
            session: makeMockedSession(),
            auth: auth
        )
        auth.setAPIClient(client)

        let listOK = """
        {"events": [], "next_cursor": null}
        """.data(using: .utf8)!
        let refreshOK = """
        {
          "user_id": "u-1",
          "access_token": "newtok",
          "access_token_expires_at": "2030-01-01T00:00:00Z",
          "refresh_token": "newrtok",
          "refresh_token_expires_at": "2030-03-01T00:00:00Z"
        }
        """.data(using: .utf8)!

        var hits = 0
        MockURLProtocol.handler = { req in
            hits += 1
            switch (hits, req.url?.path) {
            case (1, "/events"):
                return (httpResponse(req.url!, status: 401), Data())
            case (2, "/auth/refresh"):
                return (httpResponse(req.url!, status: 200, headers: ["Content-Type": "application/json"]), refreshOK)
            case (3, "/events"):
                return (httpResponse(req.url!, status: 200, headers: ["Content-Type": "application/json"]), listOK)
            default:
                XCTFail("unexpected request \(hits) to \(req.url?.path ?? "?")")
                return (httpResponse(req.url!, status: 500), Data())
            }
        }

        let resp = try await client.request(ListEventsEndpoint(cursor: nil, limit: 200))
        XCTAssertEqual(resp.events, [])
        XCTAssertEqual(MockURLProtocol.requestCount, 3)
    }

    /// 429 surfaces as APIError.rateLimited with the parsed Retry-After.
    func test429_ThrowsRateLimited() async throws {
        let auth = AuthManager(keychain: TokenKeychain(service: "com.snoreguard.tests.\(UUID())"))
        let client = APIClient(
            config: APIConfig(baseURL: baseURL, appVersion: "test"),
            session: makeMockedSession(),
            auth: auth
        )
        auth.setAPIClient(client)

        MockURLProtocol.handler = { req in
            (httpResponse(req.url!, status: 429, headers: ["Retry-After": "30"]), Data())
        }

        do {
            _ = try await client.request(AuthAppleEndpoint(identityToken: "x"))
            XCTFail("expected rateLimited")
        } catch let APIError.rateLimited(retryAfter) {
            XCTAssertEqual(retryAfter, 30)
        } catch {
            XCTFail("wrong error: \(error)")
        }
    }

    /// Two concurrent calls that both 401 should produce ONE refresh
    /// round-trip, not two. Asserts via the request counter.
    func testConcurrentRefresh_IsSingleFlight() async throws {
        let kc = TokenKeychain(service: "com.snoreguard.tests.\(UUID())")
        try kc.store(
            userID: "u-1",
            accessToken: "old",
            accessTokenExpiry: Date().addingTimeInterval(3600),
            refreshToken: "rtok"
        )
        let auth = AuthManager(keychain: kc)
        let client = APIClient(
            config: APIConfig(baseURL: baseURL, appVersion: "test"),
            session: makeMockedSession(),
            auth: auth
        )
        auth.setAPIClient(client)

        let refreshOK = """
        {
          "user_id": "u-1",
          "access_token": "newtok",
          "access_token_expires_at": "2030-01-01T00:00:00Z",
          "refresh_token": "newrtok",
          "refresh_token_expires_at": "2030-03-01T00:00:00Z"
        }
        """.data(using: .utf8)!
        let listOK = """
        {"events": [], "next_cursor": null}
        """.data(using: .utf8)!

        var refreshCalls = 0
        MockURLProtocol.handler = { req in
            switch req.url?.path {
            case "/auth/refresh":
                refreshCalls += 1
                return (httpResponse(req.url!, status: 200, headers: ["Content-Type": "application/json"]), refreshOK)
            case "/events":
                // First call from each task: 401. Second call: 200.
                let auth = req.value(forHTTPHeaderField: "Authorization") ?? ""
                if auth.contains("newtok") {
                    return (httpResponse(req.url!, status: 200, headers: ["Content-Type": "application/json"]), listOK)
                }
                return (httpResponse(req.url!, status: 401), Data())
            default:
                return (httpResponse(req.url!, status: 500), Data())
            }
        }

        async let r1 = client.request(ListEventsEndpoint(cursor: nil, limit: 200))
        async let r2 = client.request(ListEventsEndpoint(cursor: nil, limit: 200))
        _ = try await (r1, r2)

        // The exact value depends on actor-scheduling — on macOS this
        // should be 1 in the common case. Assert ≤ 1 to keep the test
        // robust against scheduling differences while still catching
        // a regression that would burn N refreshes for N requests.
        XCTAssertLessThanOrEqual(refreshCalls, 1, "single-flight refresh fired \(refreshCalls)x")
    }

    /// Server error envelope (`{"error": "..."}`) is decoded and
    /// surfaced in `APIError.server.message`.
    func testServerErrorEnvelope_IsSurfaced() async throws {
        let auth = AuthManager(keychain: TokenKeychain(service: "com.snoreguard.tests.\(UUID())"))
        let client = APIClient(
            config: APIConfig(baseURL: baseURL, appVersion: "test"),
            session: makeMockedSession(),
            auth: auth
        )
        auth.setAPIClient(client)

        let errBody = #"{"error":"avg_db out of range"}"#.data(using: .utf8)!
        MockURLProtocol.handler = { req in
            (httpResponse(req.url!, status: 400, headers: ["Content-Type": "application/json"]), errBody)
        }

        do {
            _ = try await client.request(AuthAppleEndpoint(identityToken: "x"))
            XCTFail("expected error")
        } catch let APIError.server(code, msg) {
            XCTAssertEqual(code, 400)
            XCTAssertEqual(msg, "avg_db out of range")
        } catch {
            XCTFail("wrong error: \(error)")
        }
    }
}

// MARK: - EventSync wire-conversion test

/// Sanity check that relative `start_ms` becomes an absolute
/// `started_at` correctly. Doesn't hit the network.
@MainActor
final class EventSyncConversionTests: XCTestCase {
    func testUploadConversion_ProducesAbsoluteTimestamps() async throws {
        let kc = TokenKeychain(service: "com.snoreguard.tests.\(UUID())")
        try kc.store(
            userID: "u-1",
            accessToken: "atok",
            accessTokenExpiry: Date().addingTimeInterval(3600),
            refreshToken: "rtok"
        )
        let auth = AuthManager(keychain: kc)
        let client = APIClient(
            config: APIConfig(baseURL: URL(string: "https://test.local")!, appVersion: "test"),
            session: makeMockedSession(),
            auth: auth
        )
        auth.setAPIClient(client)

        // Capture the body the endpoint sent so we can assert on the
        // absolute timestamp it produced.
        var capturedBody: Data?
        MockURLProtocol.handler = { req in
            // URLProtocol receives `httpBody` separately on iOS for
            // some configurations; fall back to bodyStream if needed.
            capturedBody = req.httpBody ?? req.httpBodyStream.flatMap {
                stream in
                stream.open()
                defer { stream.close() }
                var data = Data()
                let buf = UnsafeMutablePointer<UInt8>.allocate(capacity: 4096)
                defer { buf.deallocate() }
                while stream.hasBytesAvailable {
                    let n = stream.read(buf, maxLength: 4096)
                    if n <= 0 { break }
                    data.append(buf, count: n)
                }
                return data
            }
            let body = #"{"inserted":1,"received":1}"#.data(using: .utf8)!
            return (httpResponse(req.url!, status: 200, headers: ["Content-Type": "application/json"]), body)
        }

        let sync = EventSync(client: client)
        let sessionStart = Date(timeIntervalSince1970: 1_700_000_000)
        let event = SnoreEvent(startMs: 5_000, durationMs: 1_500, avgDB: 60.0)
        let resp = try await sync.upload(events: [event], sessionStartedAt: sessionStart)

        XCTAssertEqual(resp.inserted, 1)
        XCTAssertNotNil(capturedBody)
        // Absolute ts should be sessionStart + 5s.
        if let body = capturedBody,
           let json = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
           let events = json["events"] as? [[String: Any]],
           let started = events.first?["started_at"] as? String {
            XCTAssertTrue(started.hasPrefix("2023-11-14T22:13:25"),
                          "expected 5s after sessionStart, got \(started)")
        } else {
            XCTFail("malformed body")
        }
    }
}
