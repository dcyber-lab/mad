import MadKit
import SwiftUI

/// How much of each subscription is used, and when it resets: per kind, the
/// five-hour window and the weekly one.
struct UsageView: View {
    @ObservedObject var model: MacModel

    var body: some View {
        Group {
            if let usage = model.snapshot?.usage.filter(\.known), !usage.isEmpty {
                TimelineView(.periodic(from: .now, by: 30)) { ctx in
                    List {
                        ForEach(usage, id: \.kind) { u in
                            Section {
                                WindowRow(title: "5 hours", window: u.fiveHour, now: ctx.date)
                                WindowRow(title: "7 days", window: u.sevenDay, now: ctx.date)
                            } header: {
                                Text(u.title).font(.headline).textCase(nil)
                            } footer: {
                                if let r = UsageWording().reported(u, now: ctx.date) { Text(r) }
                            }
                        }
                    }
                }
            } else if model.snapshot == nil {
                ProgressView()
            } else {
                ContentUnavailableView("No usage yet", systemImage: "gauge.with.dots.needle.33percent",
                    description: Text("Claude and Codex report it after a response."))
            }
        }
        .navigationTitle("Usage")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await model.refresh() }
    }
}

/// One window: the percent used large, a bar, what is left and when it resets.
struct WindowRow: View {
    let title: String
    let window: UsageWindow
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).font(.body).foregroundStyle(.secondary)
                Spacer()
                if window.known {
                    Text("\(Int(window.used.rounded()))%").font(.largeTitle.weight(.semibold)).monospacedDigit()
                }
            }
            if window.known {
                ProgressView(value: min(max(window.used, 0), 100), total: 100)
                    .tint(window.used >= 90 ? .red : .accentColor)
                VStack(alignment: .leading, spacing: 2) {
                    Text(window.leftText).font(.body)
                    if let reset = UsageWording().reset(window, now: now) {
                        Text(reset).font(.subheadline).foregroundStyle(.secondary)
                    }
                }
            } else {
                Text("Nothing used in this window yet.").font(.body)
            }
        }
        .padding(.vertical, 8)
    }
}
