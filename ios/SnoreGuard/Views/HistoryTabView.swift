// HistoryTabView.swift
//
// Phase-2-C history tab. Backed by Core Data via EventStore;
// reads use the view context so SwiftUI gets change notifications,
// writes happen on a background context inside EventStore.record(_:).

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
                            SessionDetailView(session: session)
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
    private let store: EventStore

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

private struct SessionRow: View {
    let session: SessionEntity

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.startedAt ?? Date(), style: .date)
                    .font(.headline)
                Spacer()
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
}

private struct SessionDetailView: View {
    let session: SessionEntity

    var body: some View {
        List {
            Section("Session") {
                LabeledContent("Started", value: (session.startedAt ?? Date()).formatted(date: .abbreviated, time: .standard))
                if let end = session.endedAt {
                    LabeledContent("Ended", value: end.formatted(date: .abbreviated, time: .standard))
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
        }
        .navigationTitle("Session")
        .navigationBarTitleDisplayMode(.inline)
    }
}

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
