// AuthManager.swift
//
// Sign in with Apple, Keychain-backed token storage, and single-flight
// refresh. Owns the only @Published state the UI needs to observe for
// signed-in/signed-out transitions.
//
// Threading: `@MainActor` so the @Published properties can be assigned
// safely from sync code without a hop. The Keychain wrapper itself is
// thread-safe (Security-framework `SecItem*` calls are), so calling
// the synchronous accessors from inside an async-on-MainActor method
// is fine. Network calls go via `APIClient.request` which is
// actor-isolated.
//
// Why Apple's identity-token flow only:
//   The `ASAuthorizationAppleIDCredential.identityToken` is a JWT signed
//   by Apple. The backend's `internal/apple` package verifies it
//   against Apple's JWKS (audience = bundle ID, issuer =
//   `appleid.apple.com`). On success the backend upserts the user row
//   keyed on the JWT's `sub`, returns access + refresh tokens, and the
//   iOS client never needs to talk to Apple again — refresh tokens are
//   ours, not Apple's.

import Foundation
import AuthenticationServices
import Security
import os.log

/// Errors thrown from `AuthManager`. `APIError` covers transport-level
/// failures; this enum is the local-state vocabulary.
enum AuthError: Error, LocalizedError, Equatable {
    /// `currentAccessToken()` was called with no token in Keychain —
    /// caller should route the user back to sign-in.
    case notSignedIn
    /// `setAPIClient(_:)` was never called. A programming error;
    /// surfaces with a clear message rather than a crash.
    case notConfigured
    /// `ASAuthorizationAppleIDCredential.identityToken` was nil. Apple
    /// only returns this on the first sign-in for a given bundle ID,
    /// and the user must complete the dialog.
    case missingIdentityToken

    var errorDescription: String? {
        switch self {
        case .notSignedIn: return "Sign in to continue."
        case .notConfigured: return "API client not initialized."
        case .missingIdentityToken: return "Apple did not return an identity token."
        }
    }
}

@MainActor
final class AuthManager: NSObject, ObservableObject {
    /// Mirrored from Keychain on init; flipped on sign-in/sign-out.
    @Published private(set) var isSignedIn: Bool = false
    /// The backend-assigned UUID for this user. Stable across sign-ins.
    @Published private(set) var userID: String?

    private let keychain: TokenKeychain
    private let log = Logger(subsystem: "com.snoreguard.app", category: "AuthManager")

    /// Single-flight refresh: if a refresh task is already running, all
    /// new callers `await` the same task instead of starting another.
    /// Cleared in the task's `defer` so a subsequent call after success
    /// (or failure) starts a fresh task.
    private var inflightRefresh: Task<AuthTokensResponse, Error>?

    /// Set after construction (avoids an init cycle: APIClient has the
    /// AuthManager as a dependency so AuthManager can't have APIClient
    /// in its initializer).
    private var apiClient: APIClient?

    init(keychain: TokenKeychain = TokenKeychain()) {
        self.keychain = keychain
        super.init()
        // Hydrate signed-in state from Keychain on construction so a
        // relaunch starts in the right state without a network round-trip.
        if (try? keychain.loadAccessToken()) != nil {
            self.isSignedIn = true
            self.userID = (try? keychain.loadUserID()) ?? nil
        }
    }

    /// Wires the API client. Must be called once before any auth-
    /// requiring call. Done this way to break the circular dep:
    /// AuthManager <- APIClient.auth, APIClient <- AuthManager.apiClient.
    func setAPIClient(_ client: APIClient) {
        self.apiClient = client
    }

    // MARK: - Sign in with Apple

    /// Exchange an `ASAuthorizationAppleIDCredential.identityToken` for
    /// our backend's session + refresh tokens. Persists both in Keychain.
    /// Caller is the `ASAuthorizationControllerDelegate` (typically the
    /// sign-in view's coordinator).
    func signIn(with credential: ASAuthorizationAppleIDCredential) async throws {
        guard let tokenData = credential.identityToken,
              let token = String(data: tokenData, encoding: .utf8) else {
            log.error("Apple credential missing identityToken")
            throw AuthError.missingIdentityToken
        }
        guard let client = apiClient else { throw AuthError.notConfigured }
        let response = try await client.request(AuthAppleEndpoint(identityToken: token))
        try persist(response)
        log.info("Signed in with Apple, user_id=\(response.userID, privacy: .public)")
    }

    // MARK: - Token access (called by APIClient)

    /// Returns a usable access token. If the cached token expires within
    /// the next 60 seconds, this triggers a refresh first so the caller
    /// doesn't have to absorb a 401-and-retry round-trip on the common
    /// case. Throws `AuthError.notSignedIn` if no token is in Keychain.
    func currentAccessToken() async throws -> String {
        guard let token = try keychain.loadAccessToken() else {
            throw AuthError.notSignedIn
        }
        if let exp = try keychain.loadAccessTokenExpiry(),
           exp.timeIntervalSinceNow < 60 {
            // Pre-emptive refresh. The single-flight logic in `refresh`
            // means concurrent callers all benefit from one refresh.
            let refreshed = try await refresh()
            return refreshed.accessToken
        }
        return token
    }

    /// Run a refresh round-trip. Single-flight: if a refresh is already
    /// in progress, the second caller awaits the same task. This avoids
    /// burning two refresh tokens (and triggering theft detection on
    /// the loser) when, e.g., a list-events and a post-events fire
    /// concurrently and both see a 401.
    @discardableResult
    func refresh() async throws -> AuthTokensResponse {
        if let inflight = inflightRefresh {
            return try await inflight.value
        }
        // The Task body runs on @MainActor (this method is MainActor-
        // isolated, so the unstructured Task inherits the actor).
        let task = Task<AuthTokensResponse, Error> {
            try await self.doRefresh()
        }
        inflightRefresh = task
        defer { inflightRefresh = nil }
        // `await task.value` returns when doRefresh resolves; clearing
        // inflightRefresh in `defer` ensures the next caller starts a
        // fresh task. No risk of dropping a concurrent caller's
        // reference: they captured the same `task` value above before
        // we cleared the slot.
        return try await task.value
    }

    private func doRefresh() async throws -> AuthTokensResponse {
        guard let client = apiClient else { throw AuthError.notConfigured }
        guard let refreshToken = try keychain.loadRefreshToken() else {
            // No refresh token to use means we're effectively signed out.
            // Surface as `notSignedIn` so callers get a clean signal to
            // route back to the sign-in view.
            throw AuthError.notSignedIn
        }
        do {
            let response = try await client.request(AuthRefreshEndpoint(refreshToken: refreshToken))
            try persist(response)
            return response
        } catch let APIError.server(statusCode, _) where statusCode == 401 {
            // 401 on refresh = refresh token revoked / family compromised.
            // Wipe Keychain and surface as notSignedIn so the UI routes
            // the user back to sign-in.
            try? keychain.deleteAll()
            isSignedIn = false
            userID = nil
            throw AuthError.notSignedIn
        }
    }

    /// Best-effort logout: notifies the backend so the access JTI and
    /// refresh family are revoked, then deletes Keychain regardless.
    /// Local state is cleared even if the network call fails — the
    /// alternative leaves a "stuck signed-in" UX after a flaky network.
    func signOut() async {
        if let client = apiClient,
           let refreshToken = try? keychain.loadRefreshToken() {
            _ = try? await client.request(AuthLogoutEndpoint(refreshToken: refreshToken))
        }
        try? keychain.deleteAll()
        isSignedIn = false
        userID = nil
        log.info("Signed out")
    }

    // MARK: - Persistence

    private func persist(_ response: AuthTokensResponse) throws {
        try keychain.store(
            userID: response.userID,
            accessToken: response.accessToken,
            accessTokenExpiry: response.accessTokenExpiresAt,
            refreshToken: response.refreshToken
        )
        isSignedIn = true
        userID = response.userID
    }
}
