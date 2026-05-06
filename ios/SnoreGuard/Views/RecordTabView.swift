// RecordTabView.swift
//
// Mirrors the React `RecordTab` component layout: meter + start/stop
// + live event count + duration. Phase 2 keeps history off-screen.

import SwiftUI

struct RecordTabView: View {
    @EnvironmentObject var settings: SettingsStore
    @StateObject private var vm: RecorderViewModel

    init(settings: SettingsStore) {
        _vm = StateObject(wrappedValue: RecorderViewModel(settingsStore: settings))
    }

    var body: some View {
        VStack(spacing: 16) {
            DisclaimerBanner()

            VStack(spacing: 4) {
                Text(formattedDuration)
                    .font(.system(size: 48, weight: .bold, design: .rounded))
                    .monospacedDigit()
                Text("\(vm.eventCount) snore-like event\(vm.eventCount == 1 ? "" : "s")")
                    .foregroundStyle(.secondary)
            }
            .padding(.top, 16)

            VStack(alignment: .leading, spacing: 6) {
                Text("Live level vs. threshold (\(Int(settings.settings.thresholdDB)) dB)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                LevelMeterView(liveDB: vm.liveLevelDB, thresholdDB: settings.settings.thresholdDB)
            }
            .padding(.horizontal)

            Spacer()

            recordButton
                .padding(.horizontal)

            if case .error(let msg) = vm.state {
                Text(msg)
                    .font(.footnote)
                    .foregroundStyle(.red)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal)
            }

            Spacer()
        }
        .navigationTitle("Record")
    }

    @ViewBuilder
    private var recordButton: some View {
        let isActive: Bool = {
            switch vm.state {
            case .recording, .starting: return true
            default: return false
            }
        }()

        Button {
            isActive ? vm.stop() : vm.start()
        } label: {
            HStack {
                Image(systemName: isActive ? "stop.fill" : "mic.fill")
                Text(isActive ? "Stop" : "Start listening")
                    .fontWeight(.semibold)
            }
            .frame(maxWidth: .infinity, minHeight: 50)
            .background(isActive ? Color.red : Color.accentColor, in: Capsule())
            .foregroundStyle(.white)
        }
        .accessibilityIdentifier(isActive ? "stopButton" : "startButton")
    }

    private var formattedDuration: String {
        let total = Int(vm.session?.duration ?? 0)
        let h = total / 3600
        let m = (total % 3600) / 60
        let s = total % 60
        if h > 0 {
            return String(format: "%d:%02d:%02d", h, m, s)
        }
        return String(format: "%02d:%02d", m, s)
    }
}
