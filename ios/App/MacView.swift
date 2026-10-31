import MadKit
import SwiftUI

/// An opened Mac: the native "Needs you" list and the deck page, side by
/// side as two tabs.
struct MacView: View {
    @StateObject private var model: MacModel
    @Environment(\.scenePhase) private var scenePhase
    @State private var tab: Tab = .needs

    enum Tab { case needs, deck }

    init(mac: Mac) { _model = StateObject(wrappedValue: MacModel(mac: mac)) }

    var body: some View {
        TabView(selection: $tab) {
            NeedsView(model: model, openDeck: { tab = .deck })
                .tabItem { Label("Needs you", systemImage: "tray") }
                .badge(model.needs)
                .tag(Tab.needs)
            DeckTab(model: model)
                .tabItem { Label("Deck", systemImage: "square.grid.2x2") }
                .tag(Tab.deck)
        }
        .navigationTitle(model.mac.name)
        .navigationBarTitleDisplayMode(.inline)
        // Polls while this screen is shown and the app is active; the task
        // restarts on every return to the foreground, so it looks at once.
        .task(id: scenePhase) {
            guard scenePhase == .active else { return }
            await model.poll()
        }
        .alert("mad", isPresented: Binding(get: { model.notice != nil }, set: { if !$0 { model.notice = nil } })) {
            Button("OK", role: .cancel) {}
        } message: { Text(model.notice ?? "") }
    }
}

/// A thin banner for offline and refused tokens, on both tabs.
struct LinkBanner: View {
    let link: MacModel.Link

    var body: some View {
        switch link {
        case .failed(.refused):
            banner("Token refused, re-pair this Mac.", "key.slash", .red)
        case .failed(.offline):
            banner("Offline: the Mac cannot be reached.", "wifi.slash", .orange)
        case .failed(let e):
            banner(e.localizedDescription, "exclamationmark.triangle", .orange)
        case .connecting, .ok:
            EmptyView()
        }
    }

    private func banner(_ text: String, _ icon: String, _ color: Color) -> some View {
        Label(text, systemImage: icon)
            .font(.footnote)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12).padding(.vertical, 6)
            .background(color.opacity(0.18))
            .foregroundStyle(color)
    }
}

struct DeckTab: View {
    @ObservedObject var model: MacModel

    var body: some View {
        VStack(spacing: 0) {
            LinkBanner(link: model.link)
            // Edge to edge; the keyboard still shrinks the page, so its
            // composer stays in view.
            DeckWebView(host: model.web)
                .ignoresSafeArea(.container, edges: [.horizontal, .top])
        }
    }
}
