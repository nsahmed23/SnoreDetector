// ContentView.swift
//
// The three-tab top-level container. Mirrors `App.tsx` from the React
// prototype: Record / History / Settings.

import SwiftUI

struct ContentView: View {
    @EnvironmentObject var store: SettingsStore

    var body: some View {
        TabView {
            NavigationStack { RecordTabView(settings: store) }
                .tabItem {
                    Label("Record", systemImage: "mic.circle")
                }

            NavigationStack { HistoryTabView() }
                .tabItem {
                    Label("History", systemImage: "moon.zzz")
                }

            NavigationStack { SettingsTabView() }
                .tabItem {
                    Label("Settings", systemImage: "gearshape")
                }
        }
        .sheet(isPresented: .constant(!store.hasAcceptedDisclaimer)) {
            ConsentSheet()
        }
    }
}
