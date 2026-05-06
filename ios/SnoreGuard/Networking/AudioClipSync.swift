// AudioClipSync.swift
//
// Multipart upload + raw-bytes download + delete for audio clips.
// Bypasses the standard `Endpoint` flow on upload because the
// protocol assumes a single `Data` body — multipart needs a hand-
// composed boundary + two parts (metadata JSON + file bytes).
// Download + delete fall through to the regular APIClient code path.
//
// Backend wire shape (see backend/internal/server/clips.go):
//
//   POST /audio/clips
//   Content-Type: multipart/form-data; boundary=<...>
//
//   --<boundary>
//   Content-Disposition: form-data; name="metadata"
//   Content-Type: application/json; charset=utf-8
//
//   {"client_clip_id":"...","client_event_id":"...","session_id":"...",
//    "started_at":"...","duration_ms":1500,"avg_db":62.5,"sha256":"..."}
//   --<boundary>
//   Content-Disposition: form-data; name="file"; filename="...m4a"
//   Content-Type: audio/m4a
//
//   <raw bytes>
//   --<boundary>--
//
// The backend re-computes SHA-256 from the body and compares to the
// metadata `sha256`; mismatch returns HTTP 400.
//
// We compute the hash chunked from the file URL (64 KiB buffers) so
// large clips don't have to be loaded into memory twice. The body
// itself IS loaded into memory in one shot, but the same bytes are
// what `URLSession.uploadTask`/`URLSession.data(for:)` will buffer
// internally anyway — streaming uploads from disk land post-v1
// (would require the URLSession upload-stream API).

import Foundation
import CryptoKit
import os.log

@MainActor
final class AudioClipSync {
    /// The shared API client. `internal` (default visibility) so
    /// `SyncManager` in the same module can reuse it for the
    /// `ListAudioClipsEndpoint` paginated pull without re-injecting
    /// the dependency.
    let client: APIClient
    private let log = Logger(subsystem: "com.snoreguard.app", category: "AudioClipSync")

    init(client: APIClient) {
        self.client = client
    }

    /// Upload a single audio clip via multipart/form-data. Returns
    /// the server-side `clip_id`. Idempotent: server returns the
    /// existing row (with `created: false`) if `client_clip_id` is
    /// already known.
    ///
    /// `contentType` must be on the backend's allowlist (default
    /// `audio/m4a, audio/mp4, audio/wav, audio/aac`); both the
    /// declared type and the sniffed type from `http.DetectContentType`
    /// are checked server-side.
    @discardableResult
    func upload(
        fileURL: URL,
        clientClipID: UUID,
        sessionRemoteID: String?,
        clientEventID: UUID?,
        startedAt: Date,
        durationMS: Int32,
        avgDB: Float?,
        contentType: String
    ) async throws -> PostAudioClipResponse {
        // Hash from disk in chunks; for typical clip sizes (< 5 MiB)
        // the difference is negligible, but keeping the streaming
        // hasher lets a future caller hand us a much bigger file
        // without changing this method's memory profile.
        let sha = try Self.sha256(of: fileURL)
        let data = try Data(contentsOf: fileURL)

        // Build the metadata part as JSON. We use JSONSerialization
        // (not the typed Encodable path) so an absent optional is
        // simply omitted, matching the backend's `omitempty` shape.
        var metadata: [String: Any] = [
            "client_clip_id": clientClipID.uuidString,
            "started_at": ISO8601DateFormatter.snoreguard.string(from: startedAt),
            "duration_ms": Int(durationMS),
            "sha256": sha
        ]
        if let cid = clientEventID { metadata["client_event_id"] = cid.uuidString }
        if let sid = sessionRemoteID { metadata["session_id"] = sid }
        if let db = avgDB { metadata["avg_db"] = db }

        let metadataBytes = try JSONSerialization.data(withJSONObject: metadata, options: [.sortedKeys])

        // Use a UUID-suffixed boundary; collision with arbitrary file
        // bytes is astronomically unlikely. Per RFC 2046 the boundary
        // can be ≤ 70 chars; UUID + prefix = 49.
        let boundary = "snoreguard-\(UUID().uuidString)"
        let body = Self.makeMultipartBody(
            boundary: boundary,
            metadata: metadataBytes,
            fileBytes: data,
            fileContentType: contentType,
            filename: "\(clientClipID.uuidString).m4a"
        )

        let url = client.config.baseURL.appendingPathComponent("audio/clips")
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        req.setValue(String(body.count), forHTTPHeaderField: "Content-Length")
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        req.setValue("SnoreGuard/\(client.config.appVersion)", forHTTPHeaderField: "User-Agent")
        let token = try await client.auth.currentAccessToken()
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.httpBody = body

        return try await performMultipart(req: req, retryAfterRefresh: true)
    }

    /// Download the raw audio bytes for one clip. Returns the body
    /// verbatim (no JSON decoding — the response is binary audio).
    func download(clipID: String) async throws -> Data {
        try await client.requestData(DownloadAudioClipEndpoint(clipID: clipID))
    }

    /// Soft-delete a clip server-side. Server returns `{"status":
    /// "deleted"}`; we discard the body. Subsequent reads return 404.
    func delete(clipID: String) async throws {
        _ = try await client.request(DeleteAudioClipEndpoint(clipID: clipID))
    }

    // MARK: - Internals

    /// Issue a pre-built multipart upload request. On 401, refresh
    /// the access token once and retry exactly once. On 429, surface
    /// `APIError.rateLimited`. On non-2xx, surface `APIError.server`.
    private func performMultipart(req: URLRequest, retryAfterRefresh: Bool) async throws -> PostAudioClipResponse {
        let (data, response) = try await client.urlSession.data(for: req)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        if http.statusCode == 401 && retryAfterRefresh {
            try await client.auth.refresh()
            // Re-attach the freshened token before retry. Mutate a
            // copy so we don't mutate the URLRequest the caller saw.
            var retried = req
            let token = try await client.auth.currentAccessToken()
            retried.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
            return try await performMultipart(req: retried, retryAfterRefresh: false)
        }
        if http.statusCode == 429 {
            let retry = http.value(forHTTPHeaderField: "Retry-After")
                .flatMap { TimeInterval($0) }
            throw APIError.rateLimited(retryAfterSeconds: retry)
        }
        guard (200..<300).contains(http.statusCode) else {
            let msg = (try? JSONDecoder.snoreguard.decode(WireErrorBody.self, from: data))?.error
                ?? "HTTP \(http.statusCode)"
            throw APIError.server(statusCode: http.statusCode, message: msg)
        }
        do {
            return try JSONDecoder.snoreguard.decode(PostAudioClipResponse.self, from: data)
        } catch {
            throw APIError.decoding(message: "\(error)")
        }
    }

    /// Compose a multipart/form-data body with two parts. Pulled out
    /// as a static helper so tests can call it directly without
    /// constructing the full sync stack.
    static func makeMultipartBody(
        boundary: String,
        metadata: Data,
        fileBytes: Data,
        fileContentType: String,
        filename: String
    ) -> Data {
        var body = Data()
        let crlf = "\r\n".data(using: .utf8)!
        let dashdash = "--".data(using: .utf8)!

        // metadata part
        body.append(dashdash)
        body.append(boundary.data(using: .utf8)!)
        body.append(crlf)
        body.append("Content-Disposition: form-data; name=\"metadata\"".data(using: .utf8)!)
        body.append(crlf)
        body.append("Content-Type: application/json; charset=utf-8".data(using: .utf8)!)
        body.append(crlf)
        body.append(crlf)
        body.append(metadata)
        body.append(crlf)

        // file part
        body.append(dashdash)
        body.append(boundary.data(using: .utf8)!)
        body.append(crlf)
        body.append("Content-Disposition: form-data; name=\"file\"; filename=\"\(filename)\"".data(using: .utf8)!)
        body.append(crlf)
        body.append("Content-Type: \(fileContentType)".data(using: .utf8)!)
        body.append(crlf)
        body.append(crlf)
        body.append(fileBytes)
        body.append(crlf)

        // closing delimiter
        body.append(dashdash)
        body.append(boundary.data(using: .utf8)!)
        body.append(dashdash)
        body.append(crlf)

        return body
    }

    /// Streaming SHA-256 of a file at a URL. 64 KiB chunks so a
    /// multi-megabyte clip doesn't have to live twice in memory.
    /// Returns lowercase hex (matches Go's `hex.EncodeToString` from
    /// the backend's verification path).
    static func sha256(of fileURL: URL) throws -> String {
        guard let stream = InputStream(url: fileURL) else {
            throw AudioClipError.fileUnreadable(fileURL)
        }
        stream.open()
        defer { stream.close() }

        var hasher = SHA256()
        let bufferSize = 64 * 1024
        let buf = UnsafeMutablePointer<UInt8>.allocate(capacity: bufferSize)
        defer { buf.deallocate() }

        while stream.hasBytesAvailable {
            let read = stream.read(buf, maxLength: bufferSize)
            if read < 0 {
                throw AudioClipError.fileUnreadable(fileURL)
            }
            if read == 0 { break }
            hasher.update(bufferPointer: UnsafeRawBufferPointer(start: buf, count: read))
        }
        let digest = hasher.finalize()
        return digest.map { String(format: "%02x", $0) }.joined()
    }

    /// Convenience: compute SHA-256 of an in-memory byte buffer.
    /// Used by the test suite to assert the streaming-from-file path
    /// produces the same digest as the one-shot in-memory path.
    static func sha256(of data: Data) -> String {
        let digest = SHA256.hash(data: data)
        return digest.map { String(format: "%02x", $0) }.joined()
    }
}

/// Errors specific to the audio-clip pipeline. Distinct from
/// `APIError` because they're file-IO failures, not transport faults.
enum AudioClipError: Error, LocalizedError, Equatable {
    case fileUnreadable(URL)

    var errorDescription: String? {
        switch self {
        case .fileUnreadable(let url):
            return "Could not read audio clip file at \(url.lastPathComponent)."
        }
    }
}
