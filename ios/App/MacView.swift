import MadKit
import SwiftUI

struct MacView: View {
    let mac: Mac
    @State private var phase: Phase = .loading
    @State private var showDeck = false

    enum Phase {
        case loading
        case ok(DeckSnapshot)
        case failed(ConnectionError)
    }

    var body: some View {
        List {
            Section("Connection") { connection }
            if case .ok(let snap) = phase {
                Section("Waiting for you (\(snap.waiting.count))") {
                    if snap.waiting.isEmpty {
                        Text("Nothing waits on you.").foregroundStyle(.secondary)
                    }
                    ForEach(snap.waiting) { item in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.title).font(.headline)
                            if let c = item.context { Text(c).font(.caption).foregroundStyle(.secondary) }
                            if let t = item.text { Text(t).font(.callout).lineLimit(3) }
                        }
                    }
                }
                ForEach(snap.projects) { p in
                    Section(p.name) {
                        ForEach(p.agents) { a in
                            HStack {
                                Text(a.label)
                                Spacer()
                                Text(a.state).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
            Section {
                Button("Open deck", systemImage: "safari") { showDeck = true }
            }
        }
        .navigationTitle(mac.name)
        .refreshable { await load() }
        .task { await load() }
        .fullScreenCover(isPresented: $showDeck) {
            NavigationStack {
                DeckWebView(url: mac.deckURL)
                    .ignoresSafeArea(edges: .bottom)
                    .navigationTitle(mac.name)
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Done") { showDeck = false } } }
            }
        }
    }

    @ViewBuilder private var connection: some View {
        switch phase {
        case .loading:
            HStack { ProgressView(); Text("Connecting…") }
        case .ok(let snap):
            Label("Reachable", systemImage: "checkmark.circle.fill").foregroundStyle(.green)
            Text("\(snap.agentCount) agents in \(snap.projects.count) projects")
        case .failed(.refused):
            Label("Token refused", systemImage: "key.slash").foregroundStyle(.red)
            Text("Run `mad phone` on the Mac and pair again.").font(.caption)
        case .failed(.offline):
            Label("Offline", systemImage: "wifi.slash").foregroundStyle(.orange)
            Text("The Mac cannot be reached. Is it awake and `mad phone` on?").font(.caption)
        case .failed(let e):
            Label("Unexpected answer", systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
            Text(e.localizedDescription).font(.caption)
        }
    }

    private func load() async {
        do { phase = .ok(try await MadClient(mac: mac).deck()) }
        catch let e as ConnectionError { phase = .failed(e) }
        catch { phase = .failed(.offline) }
    }
}
