// ClipFileStore.swift
//
// On-disk wrapper for recorded audio clips. Each clip lives at
// `<app-support>/clips/<clientClipID>.<ext>`. The directory is
// created lazily on first use with file protection
// `completeUntilFirstUserAuthentication` so the bytes survive a
// reboot but stay encrypted-at-rest before the user enters the
// passcode for the first time.
//
// Why app-support and not the app-group container that Core Data
// uses? Audio clips can be many MB each; the app-group container is
// shared with extensions (Watch, share) and we don't want a
// runaway-clip extension to eat the shared quota. App-support is
// per-app, included in iCloud + iTunes backups by default (which is
// fine — these are user recordings of their own bedroom), and
// excluded from the system "purgeable" sweep.

import Foundation

/// Manages the on-disk location of recorded audio clips.
struct ClipFileStore {
    static let shared = ClipFileStore()

    /// Returns the `<app-support>/clips` directory, creating it if
    /// missing. The directory inherits the
    /// `completeUntilFirstUserAuthentication` data-protection class
    /// so contents are encrypted at rest and only readable after the
    /// device has been unlocked once since boot.
    func clipsDirectory() throws -> URL {
        let fm = FileManager.default
        let support = try fm.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        let dir = support.appendingPathComponent("clips", isDirectory: true)
        if !fm.fileExists(atPath: dir.path) {
            try fm.createDirectory(
                at: dir,
                withIntermediateDirectories: true,
                attributes: [
                    .protectionKey: FileProtectionType.completeUntilFirstUserAuthentication
                ]
            )
        }
        return dir
    }

    /// Resolves the canonical on-disk URL for a clip. Does NOT check
    /// existence — callers that need that should
    /// `FileManager.default.fileExists(atPath:)`.
    func url(for clientClipID: UUID, ext: String = "m4a") throws -> URL {
        try clipsDirectory().appendingPathComponent("\(clientClipID.uuidString).\(ext)")
    }

    /// Copies a temp file into the clips directory, overwriting any
    /// previous copy at the destination. Returns the destination URL.
    /// Caller is responsible for cleaning up the source.
    func copyIn(from tempURL: URL, clientClipID: UUID, ext: String = "m4a") throws -> URL {
        let dest = try url(for: clientClipID, ext: ext)
        if FileManager.default.fileExists(atPath: dest.path) {
            try FileManager.default.removeItem(at: dest)
        }
        try FileManager.default.copyItem(at: tempURL, to: dest)
        return dest
    }

    /// Deletes a clip from disk. No-op if the file doesn't exist —
    /// callers can invoke this idempotently as part of a soft-delete.
    func remove(_ clientClipID: UUID, ext: String = "m4a") throws {
        let url = try self.url(for: clientClipID, ext: ext)
        if FileManager.default.fileExists(atPath: url.path) {
            try FileManager.default.removeItem(at: url)
        }
    }

    /// Streaming SHA-256 of a file at `url`. Thin re-export of the
    /// `URL.sha256Hex()` extension in `Crypto/Sha256+File.swift` so
    /// existing call sites keep their style.
    static func sha256(of url: URL) throws -> String {
        try url.sha256Hex()
    }
}
