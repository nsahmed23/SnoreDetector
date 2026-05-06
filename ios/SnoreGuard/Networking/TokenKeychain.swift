// TokenKeychain.swift
//
// Direct Security-framework wrapper for SnoreGuard's auth tokens. No
// third-party dependency on purpose: every wrapper layer is one more
// thing that can ship a CVE we'd have to chase across versions, and
// the SecItem* surface we use is small and stable.
//
// Storage shape: four `kSecClassGenericPassword` items keyed on
// `(service: <bundle>.tokens, account: <key>)`. Each value is the
// UTF-8 string. The expiry is stored as an ISO8601 string so we don't
// need a binary plist round-trip.
//
// Accessibility: `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`
//   - Survives backgrounding (unlike WhenUnlocked) so a sync started
//     under the lock screen doesn't fail mid-flight.
//   - "ThisDeviceOnly" prevents iCloud Keychain from syncing tokens
//     across devices — each device has its own refresh family on the
//     backend, and we don't want one device's revocation to nuke
//     another's tokens.
//
// Sharing across the future Watch app is a follow-up: we'll need to
// add `kSecAttrAccessGroup` and an entitlement.

import Foundation
import Security

/// Errors thrown from `TokenKeychain`. The wrapped `OSStatus` is the
/// raw value from the Security framework — `errSecItemNotFound` (-25300),
/// `errSecAuthFailed` (-25293), etc.
enum KeychainError: Error, LocalizedError, Equatable {
    case osStatus(OSStatus)
    /// Bytes stored in Keychain weren't valid UTF-8. Should never
    /// happen for items we wrote ourselves; defensive against external
    /// corruption.
    case invalidData

    var errorDescription: String? {
        switch self {
        case .osStatus(let s):
            return "Keychain error: OSStatus \(s)"
        case .invalidData:
            return "Keychain item was not valid UTF-8."
        }
    }
}

struct TokenKeychain {
    /// `kSecAttrService` value. Includes the bundle ID so a future
    /// "SnoreGuard Lite" wouldn't collide.
    private let service: String

    init(service: String = "com.snoreguard.app.tokens") {
        self.service = service
    }

    /// Account names — internal so only this file controls the
    /// stored-key vocabulary. Renaming a key here is a Keychain-data
    /// migration, so think twice.
    private enum Key {
        static let userID = "user_id"
        static let accessToken = "access_token"
        static let accessExpiry = "access_token_expiry"
        static let refreshToken = "refresh_token"

        static var all: [String] {
            [userID, accessToken, accessExpiry, refreshToken]
        }
    }

    /// Persist a fresh token bundle. Replaces any existing items with
    /// the same accounts (upsert semantics via delete-then-add).
    func store(userID: String,
               accessToken: String,
               accessTokenExpiry: Date,
               refreshToken: String) throws {
        try set(userID, for: Key.userID)
        try set(accessToken, for: Key.accessToken)
        try set(Self.dateFormatter.string(from: accessTokenExpiry), for: Key.accessExpiry)
        try set(refreshToken, for: Key.refreshToken)
    }

    func loadAccessToken() throws -> String? {
        try get(Key.accessToken)
    }

    func loadRefreshToken() throws -> String? {
        try get(Key.refreshToken)
    }

    func loadUserID() throws -> String? {
        try get(Key.userID)
    }

    func loadAccessTokenExpiry() throws -> Date? {
        guard let raw = try get(Key.accessExpiry) else { return nil }
        return Self.dateFormatter.date(from: raw)
    }

    /// Wipe every key this struct manages. Used on sign-out and on
    /// refresh-token revocation responses.
    func deleteAll() throws {
        for key in Key.all {
            try delete(key)
        }
    }

    // MARK: - SecItem CRUD

    /// Common attributes for every item: class + service + account.
    /// Returned untyped because `SecItem*` APIs take a `CFDictionary`.
    private func baseQuery(for account: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    private func set(_ value: String, for account: String) throws {
        // Upsert via delete-then-add. `SecItemUpdate` would also work
        // but requires a separate match-then-update query and more
        // ceremony for first-write.
        try delete(account)

        var attrs = baseQuery(for: account)
        attrs[kSecValueData as String] = Data(value.utf8)
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly

        let status = SecItemAdd(attrs as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw KeychainError.osStatus(status)
        }
    }

    private func get(_ account: String) throws -> String? {
        var query = baseQuery(for: account)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        if status == errSecItemNotFound {
            return nil
        }
        guard status == errSecSuccess else {
            throw KeychainError.osStatus(status)
        }
        guard let data = item as? Data,
              let str = String(data: data, encoding: .utf8) else {
            throw KeychainError.invalidData
        }
        return str
    }

    private func delete(_ account: String) throws {
        let status = SecItemDelete(baseQuery(for: account) as CFDictionary)
        // errSecItemNotFound is success-equivalent for delete.
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainError.osStatus(status)
        }
    }

    /// One formatter, configured once. `ISO8601DateFormatter` is
    /// thread-safe per Apple docs; safe to share across calls.
    private static let dateFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
}
