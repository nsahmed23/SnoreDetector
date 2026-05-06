// Sha256+File.swift
//
// Shared SHA-256-of-a-file helper used by both the persistence layer
// (EventStore.recordClip → ClipFileStore) and the network layer
// (Phase D's AudioClipSync upload path). Centralizing keeps the chunk
// size + error semantics identical on both sides so a clip's hash
// computed at record-time matches the hash recomputed before upload.
//
// Implementation reads the file in 64 KiB chunks via FileHandle so a
// multi-megabyte clip doesn't materialize the whole thing into memory.
// CryptoKit's SHA256.hash(data:) and Insecure-Hashable updates are
// stable across iOS 13+, so this works on every target deployment we
// care about.

import Foundation
import CryptoKit

extension URL {
    /// Streaming SHA-256 of the file at this URL, hex-encoded
    /// (lowercase, 64 chars). Throws if the file can't be opened or
    /// read. The file is read in 64 KiB chunks so a large audio clip
    /// doesn't blow the heap.
    func sha256Hex() throws -> String {
        let handle = try FileHandle(forReadingFrom: self)
        defer { try? handle.close() }
        var hasher = SHA256()
        let chunkSize = 64 * 1024
        while true {
            let chunk = try handle.read(upToCount: chunkSize) ?? Data()
            if chunk.isEmpty { break }
            hasher.update(data: chunk)
        }
        let digest = hasher.finalize()
        return digest.map { String(format: "%02x", $0) }.joined()
    }
}
