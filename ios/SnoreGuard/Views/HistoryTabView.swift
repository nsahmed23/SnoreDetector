// HistoryTabView.swift
//
// Phase-2-C history tab. Backed by Core Data via EventStore;
// reads use the view context so SwiftUI gets change notifications,
// writes happen on a background context inside EventStore.record(_:).
//
// Phase E adds a per-row cloud-sync badge (icloud.fill / icloud /
// icloud.slash) plus a "Clips" section in SessionDetailView. Clip
// rows offer play (if a local file exists), download (if only a
// remote copy exists), and swipe-to-delete (soft-deletes via
// EventStore.deleteClip). Actual playback + download are placeholders
// pending the Phase D APIClient — buttons render but only the delete
// affordance does real work in this PR.

import SwiftUI
import CoreData

struct HistoryTabView: View {
    @StateObject private var vm = HistoryViewModel()

    var body: some View {
        NavigationStack {
            Group {
                if vm.sessions.isEmpty {
                    EmptyStateView()
                } else {
                    List(vm.sessions, id: \.objectID) { session in
                        NavigationLink {
                            SessionDetailView(session: session, store: vm.store)
                        } label: {
                            SessionRow(session: session)
                        }
                    }
                }
            }
            .navigationTitle("History")
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    DisclaimerBanner()
                }
            }
            .task { await vm.load() }
            .refreshable { await vm.load() }
        }
    }
}

@MainActor
final class HistoryViewModel: ObservableObject {
    @Published private(set) var sessions: [SessionEntity] = []
    let store: EventStore

    init(store: EventStore = EventStore()) {
        self.store = store
    }

    func load() async {
        do {
            sessions = try await store.allSessions()
        } catch {
            // Surface to a banner in a follow-up; for now, log silently.
        }
    }
}

// MARK: - Session row

private struct SessionRow: View {
    let session: SessionEntity

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.startedAt ?? Date(), style: .date)
                    .font(.headline)
                Spacer()
                SyncBadge(status: syncStatus)
                Text(durationText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Text("\(eventCount) snore-like event\(eventCount == 1 ? "" : "s")")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }

    private var eventCount: Int {
        (session.events as? Set<EventEntity>)?.count ?? 0
    }

    private var durationText: String {
        guard let end = session.endedAt, let start = session.startedAt else { return "—" }
        let secs = Int(end.timeIntervalSince(start))
        let h = secs / 3600
        let m = (secs % 3600) / 60
        return h > 0 ? "\(h)h \(m)m" : "\(m)m"
    }

    private var syncStatus: String {
        // KVC — the v2 attribute exists on the entity but isn't on
        // the v1 generated subclass. Reads the default ("pending")
        // for rows that pre-date v2 once the lightweight migration
        // has populated the column.
        (session.value(forKey: "syncStatus") as? String) ?? SyncStatus.pending
    }
}

// MARK: - Sync badge

private struct SyncBadge: View {
    let status: String

    var body: some View {
        Image(systemName: symbolName)
            .imageScale(.small)
            .foregroundStyle(color)
            .accessibilityLabel("Sync status: \(label)")
    }

    private var symbolName: String {
        switch status {
        case SyncStatus.synced:    return "icloud.fill"
        case SyncStatus.uploading: return "icloud.and.arrow.up"
        case SyncStatus.failed:    return "icloud.slash"
        default:                   return "icloud"
        }
    }

    private var color: Color {
        switch status {
        case SyncStatus.synced:    return .accentColor
        case SyncStatus.uploading: return .accentColor
        case SyncStatus.failed:    return .red
        default:                   return .secondary
        }
    }

    private var label: String {
        switch status {
        case SyncStatus.synced:    return "synced"
        case SyncStatus.uploading: return "uploading"
        case SyncStatus.failed:    return "failed"
        default:                   return "pending"
        }
    }
}

// MARK: - Session detail

private struct SessionDetailView: View {
    let session: SessionEntity
    let store: EventStore

    @State private var clips: [AudioClipEntity] = []

    var body: some View {
        List {
            Section("Session") {
                LabeledContent("Started", value: (session.startedAt ?? Date()).formatted(date: .abbreviated, time: .standard))
                if let end = session.endedAt {
                    LabeledContent("Ended", value: end.formatted(date: .abbreviated, time: .standard))
                }
                HStack {
                    Text("Sync")
                    Spacer()
                    SyncBadge(status: sessionSyncStatus)
                    Text(sessionSyncStatus)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Section("Events") {
                let events = (session.events as? Set<EventEntity>)?
                    .sorted(by: { ($0.startedAt ?? .distantPast) < ($1.startedAt ?? .distantPast) }) ?? []
                ForEach(events, id: \.objectID) { e in
                    HStack {
                        Text((e.startedAt ?? Date()).formatted(date: .omitted, time: .standard))
                        Spacer()
                        Text("\(e.durationMS) ms")
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                        Text(String(format: "%.1f dB", e.avgDB))
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
            }
            if !clips.isEmpty {
                Section("Clips") {
                    ForEach(clips, id: \.objectID) { clip in
                        ClipRow(clip: clip)
                    }
                    .onDelete(perform: deleteClips)
                }
            }
        }
        .navigationTitle("Session")
        .navigationBarTitleDisplayMode(.inline)
        .task { reloadClips() }
    }

    private var sessionSyncStatus: String {
        (session.value(forKey: "syncStatus") as? String) ?? SyncStatus.pending
    }

    private func reloadClips() {
        // Sort by paired event's startedAt so the clip list reads in
        // chronological order; clips with no event (rare; remote pulls
        // before the matching event came down) sink to the bottom.
        let raw = (session.value(forKey: "clips") as? Set<AudioClipEntity>) ?? []
        clips = raw
            .filter { ($0.value(forKey: "deletedAt") as? Date) == nil }
            .sorted { lhs, rhs in
                let l = (lhs.value(forKey: "event") as? EventEntity)?.startedAt ?? .distantFuture
                let r = (rhs.value(forKey: "event") as? EventEntity)?.startedAt ?? .distantFuture
                return l < r
            }
    }

    private func deleteClips(at offsets: IndexSet) {
        let toDelete = offsets.compactMap { idx -> UUID? in
            clips[idx].value(forKey: "id") as? UUID
        }
        Task {
            for id in toDelete {
                try? await store.deleteClip(id)
            }
            reloadClips()
        }
    }
}

// MARK: - Clip row

private struct ClipRow: View {
    let clip: AudioClipEntity

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(timestampText)
                    .font(.subheadline)
                Text(metaText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            SyncBadge(status: clipSyncStatus)
            actionButton
        }
    }

    private var timestampText: String {
        let event = clip.value(forKey: "event") as? EventEntity
        let date = event?.startedAt ?? Date()
        return date.formatted(date: .omitted, time: .standard)
    }

    private var metaText: String {
        let event = clip.value(forKey: "event") as? EventEntity
        let duration = event?.durationMS ?? 0
        let db = event?.avgDB ?? 0
        let size = (clip.value(forKey: "sizeBytes") as? Int64) ?? 0
        let kb = max(1, size / 1024)
        return "\(duration) ms · \(String(format: "%.1f", db)) dB · \(kb) KB"
    }

    private var clipSyncStatus: String {
        (clip.value(forKey: "syncStatus") as? String) ?? SyncStatus.pending
    }

    private var hasLocalFile: Bool {
        (clip.value(forKey: "localFilePath") as? String)?.isEmpty == false
    }

    private var hasRemote: Bool {
        (clip.value(forKey: "remoteID") as? String)?.isEmpty == false
    }

    @ViewBuilder
    private var actionButton: some View {
        if hasLocalFile {
            // Real AVAudioPlayer integration is scoped to v1.1 — this
            // PR ships the affordance + accessibility label so the
            // History UI is feature-complete on the visible surface,
            // and the playback wiring is a localized follow-up.
            Button {
                // intentionally no-op until v1.1 playback lands
            } label: {
                Image(systemName: "play.circle")
                    .imageScale(.large)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("Play clip (v1.1)")
        } else if hasRemote {
            Button {
                // Download-on-demand wires through SyncManager in
                // Phase D's network branch; left as a no-op placeholder
                // here for the same reason as the play button above.
            } label: {
                Image(systemName: "arrow.down.circle")
                    .imageScale(.large)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("Download clip (v1.1)")
        } else {
            Image(systemName: "minus.circle")
                .imageScale(.large)
                .foregroundStyle(.secondary)
                .accessibilityLabel("Clip unavailable")
        }
    }
}

// MARK: - Empty state

private struct EmptyStateView: View {
    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "moon.zzz")
                .font(.system(size: 56))
                .foregroundStyle(.secondary)
            Text("No recordings yet")
                .font(.headline)
            Text("Start a recording on the Record tab. Sessions will appear here when you stop them.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)
        }
    }
}
