// PersistenceController.swift
//
// Single Core Data stack for the app. The app-group container
// (group.com.snoreguard.shared) is provisioned in
// SnoreGuard.entitlements so a future Watch extension or share
// extension can read the same DB.
//
// Phase-2-C ships only the iPhone target reading + writing; the
// Watch path is a follow-up but the storage is already in the
// right place.

import CoreData
import OSLog

@MainActor
final class PersistenceController: ObservableObject {
    static let shared = PersistenceController()
    static let preview: PersistenceController = {
        let c = PersistenceController(inMemory: true)
        // Seed a few preview sessions so SwiftUI previews render.
        let ctx = c.container.viewContext
        for i in 0..<3 {
            let s = SessionEntity(context: ctx)
            s.id = UUID()
            s.startedAt = Date(timeIntervalSinceNow: TimeInterval(-i * 86400))
            s.endedAt = s.startedAt!.addingTimeInterval(7 * 3600)
            for j in 0..<5 {
                let e = EventEntity(context: ctx)
                e.id = UUID()
                e.session = s
                e.startedAt = s.startedAt!.addingTimeInterval(TimeInterval(j * 1200))
                e.durationMS = Int32(800 + j * 200)
                e.avgDB = 60.0 + Float(j)
                e.syncedAt = nil
            }
        }
        try? ctx.save()
        return c
    }()

    let container: NSPersistentContainer
    private let log = Logger(subsystem: "com.snoreguard.app", category: "Persistence")

    init(inMemory: Bool = false) {
        container = NSPersistentContainer(name: "SessionEntity")
        if inMemory {
            container.persistentStoreDescriptions.first?.url = URL(fileURLWithPath: "/dev/null")
        } else if let groupURL = FileManager.default.containerURL(
            forSecurityApplicationGroupIdentifier: "group.com.snoreguard.shared"
        ) {
            // Use app-group storage in production so a Watch
            // extension can read.
            let storeURL = groupURL.appendingPathComponent("SnoreGuard.sqlite")
            container.persistentStoreDescriptions.first?.url = storeURL
        }
        // Lightweight migration is a sane default; explicit migration
        // models land if the schema gains breaking changes.
        container.persistentStoreDescriptions.forEach {
            $0.shouldMigrateStoreAutomatically = true
            $0.shouldInferMappingModelAutomatically = true
        }
        container.loadPersistentStores { [weak self] _, err in
            if let err = err {
                self?.log.error("loadPersistentStores: \(err.localizedDescription)")
                fatalError("Core Data load failed: \(err)")
            }
        }
        container.viewContext.mergePolicy = NSMergeByPropertyObjectTrumpMergePolicy
        container.viewContext.automaticallyMergesChangesFromParent = true

        // Phase E v1 → v2 backfill. Detached so the launch path isn't
        // blocked on a (typically empty) row scan; running off a
        // background context inside EventStore.migrateFromV1.
        // Idempotent — second-launch cost is one indexed predicate
        // hit, no writes.
        Task.detached { [weak self] in
            guard let self = self else { return }
            let store = await EventStore(self)
            do {
                try await store.migrateFromV1()
            } catch {
                let log = Logger(subsystem: "com.snoreguard.app", category: "Persistence")
                log.error("migrateFromV1: \(error.localizedDescription)")
            }
        }
    }

    /// Background context for writes that shouldn't block the UI.
    func newBackgroundContext() -> NSManagedObjectContext {
        let ctx = container.newBackgroundContext()
        ctx.mergePolicy = NSMergeByPropertyObjectTrumpMergePolicy
        return ctx
    }
}
