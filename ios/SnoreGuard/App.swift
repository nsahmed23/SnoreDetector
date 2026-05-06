// App.swift
//
// `@main` entrypoint. Owns the SettingsStore and injects it into the
// view hierarchy as an EnvironmentObject. ContentView decides whether
// to show the consent sheet on launch.

import SwiftUI

@main
struct SnoreGuardApp: App {
    @StateObject private var settings = SettingsStore()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(settings)
        }
    }
}
