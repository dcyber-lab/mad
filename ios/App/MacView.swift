import MadKit
import SwiftUI

/// An opened Mac: the native "Needs you" list and the deck page, side by
/// side as two tabs.
struct MacView: View {
    @StateObject private var model: MacModel
    @EnvironmentObject private var store: MacStore
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dismiss) private var dismiss
    @State private var tab: Tab = MacView.firstTab
    @State private var lastTab: Tab = MacView.firstTab

    /// `macs` is not a screen: choosing it goes back to the Macs list, so the
    /// way back stays obvious while the Deck tab hides the navigation bar.
    enum Tab { case needs, agents, deck, macs }

    /// The agent whose conversation is open, if any.
    struct AgentRef: Identifiable, Hashable { var id: String }
    @State private var chatAgent: AgentRef? = MacView.firstAgent

    /// The name as it stands now: the store may have renamed it since this screen opened.
    private var title: String { store.macs.first { $0.id == model.mac.id }?.name ?? model.mac.name }

    /// Debug builds only: `-startTab deck` opens on the Deck tab.
    private static var firstTab: Tab {
        #if DEBUG
        switch UserDefaults.standard.string(forKey: "startTab") {
        case "deck": return .deck
        case "agents": return .agents
        default: break
        }
        #endif
        return .needs
    }

    /// Debug builds only: `-openAgent <id>` opens that agent's conversation.
    private static var firstAgent: AgentRef? {
        #if DEBUG
        return UserDefaults.standard.string(forKey: "openAgent").map(AgentRef.init)
        #else
        return nil
        #endif
    }

    init(mac: Mac) { _model = StateObject(wrappedValue: MacModel(mac: mac)) }

    var body: some View {
        TabView(selection: $tab) {
            NeedsView(model: model, openItem: { item in
                // An item about an agent opens its conversation; the rest, the page.
                if let id = item.open.agent { chatAgent = AgentRef(id: id) } else { tab = .deck }
            })
                .tabItem { Label("Needs you", systemImage: "tray") }
                .badge(model.needs)
                .tag(Tab.needs)
            AgentsView(model: model, open: { chatAgent = AgentRef(id: $0.id) })
                .tabItem { Label("Agents", systemImage: "person.2") }
                .tag(Tab.agents)
            DeckTab(model: model)
                .tabItem { Label("Deck", systemImage: "square.grid.2x2") }
                .tag(Tab.deck)
            Color.clear
                .tabItem { Label("Macs", systemImage: "list.bullet") }
                .tag(Tab.macs)
        }
        .onChange(of: tab) { _, new in
            if new == .macs {
                tab = lastTab
                dismiss()
            } else {
                lastTab = new
            }
        }
        .navigationDestination(item: $chatAgent) { ChatScreen(mac: model, agentID: $0.id) }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        // The page's own header is enough on the Deck tab.
        .toolbar(tab == .deck ? .hidden : .visible, for: .navigationBar)
        .onAppear { model.onMachineName = { [mac = model.mac] in store.adoptMachineName($0, for: mac) } }
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
