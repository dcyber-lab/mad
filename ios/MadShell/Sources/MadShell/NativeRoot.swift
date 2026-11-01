import MadKit
import MadUI
import SwiftUI

/// The native window of mad, on the iPhone and on the Mac: the home list of MadUI over a `DeckStore`,
/// and a detail for what a row opens. The detail is a placeholder for now: the channel and agent views
/// are built into `Detail` from `ChannelStore` and `AgentStore` by the agents who own them.
///
/// It owns the `DeckStore` for `api`, polls it while the app is active, and offers it to the views below
/// through the environment (`@Environment(DeckStore.self)`). `onSwitchMac` is the way back to the list
/// of Macs (the iPhone's; the Mac app has no such list). `openWeb` shows the page on a view that has no
/// native screen yet (the ⋯ menu's usage, board ...; an agent's terminal and changes).
public struct NativeRoot: View {
    @State private var store: DeckStore
    @State private var selection: HomeRow.ID?
    @Environment(\.scenePhase) private var scenePhase

    private let api: MadAPI
    private let title: String
    private let onSwitchMac: (() -> Void)?
    private let openWeb: ((WebTarget) -> Void)?

    public init(api: MadAPI, title: String = "mad", onSwitchMac: (() -> Void)? = nil,
                openWeb: ((WebTarget) -> Void)? = nil) {
        self.api = api
        self.title = title
        self.onSwitchMac = onSwitchMac
        self.openWeb = openWeb
        _store = State(initialValue: DeckStore(api: api))
        #if DEBUG
        // Debug builds: `-nativeOpen <row id>` opens a row at once (a channel or an agent), to look at it
        // without a tap (the simulator cannot be tapped from a script).
        if let row = UserDefaults.standard.string(forKey: "nativeOpen"), !row.isEmpty { _selection = State(initialValue: row) }
        #endif
    }

    private var model: HomeModel {
        guard let deck = store.deck else { return HomeModel(machine: title, sections: []) }
        return HomeMapping.model(from: deck, fallbackName: title)
    }

    private var actions: HomeActions {
        HomeActions(
            open: { selection = $0 },
            switchMac: { onSwitchMac?() },
            menu: { item in
                if item == .switchMac { onSwitchMac?() } else if let v = Self.webView(for: item) { openWeb?(.view(v)) }
            })
    }

    /// The page's view for a ⋯ menu item that is not native yet.
    static func webView(for item: HomeMenuItem) -> WebTarget.View? {
        switch item {
        case .usage: .usage
        case .board: .board
        case .skills: .skills
        case .devices: .phone
        case .notifications: .notify
        case .addProject: .addproject
        case .theme: .theme
        case .switchMac: nil
        }
    }

    public var body: some View {
        content
            .environment(store)
            // Polls while the app is active; the task restarts when it comes back, so it looks at once.
            .task(id: scenePhase) {
                guard scenePhase == .active else { return }
                await store.run()
            }
            .overlay(alignment: .top) { LinkNote(link: store.link) }
            #if DEBUG && os(macOS)
            .task { await DebugWindow.runIfAsked() }
            #endif
    }

    /// Whether the row's own view draws its head and way back (a channel, an agent): the iPhone's bar stays out of it.
    private func drawsOwnHead(_ id: HomeRow.ID) -> Bool { store.channel(id: id) != nil || store.agent(id: id) != nil }

    @ViewBuilder private var content: some View {
        #if os(macOS)
        MacShell(model: model, selection: $selection, actions: actions) {
            Detail(selection: selection, api: api, onBack: nil, open: { selection = $0 }, openWeb: openWeb)
        }
        #else
        // The home list has its own navigation; a row opens the detail over it, with a way back.
        if let id = selection {
            NavigationStack {
                Detail(selection: id, api: api, onBack: { selection = nil }, open: { selection = $0 }, openWeb: openWeb)
                    // a channel and an agent draw their own head, with their own way back
                    .toolbar(drawsOwnHead(id) ? .hidden : .automatic, for: .navigationBar)
                    .toolbar { ToolbarItem(placement: .topBarLeading) { if !drawsOwnHead(id) { Button { selection = nil } label: { Image(systemName: "chevron.backward") } } } }
            }
        } else {
            HomeView(model: model, actions: actions)
        }
        #endif
    }
}

/// What a row opens: a channel is the native channel view; an agent is still a placeholder.
struct Detail: View {
    @Environment(DeckStore.self) private var store
    let selection: HomeRow.ID?
    let api: MadAPI
    /// The iPhone's way back to the list; the Mac's detail pane has none.
    var onBack: (() -> Void)?
    /// Opens another row (a member of the channel).
    let open: (HomeRow.ID) -> Void
    var openWeb: ((WebTarget) -> Void)?

    var body: some View {
        if let id = selection {
            if store.channel(id: id) != nil {
                ChannelDetail(id: id, api: api, onBack: onBack, openAgent: open)
                    .id(id) // another channel is another store
            } else if let agent = store.agent(id: id) {
                AgentDetail(api: api, id: agent.id, onBack: onBack, openWeb: openWeb).id(agent.id)
            } else {
                placeholder(id, "已经不在了")
            }
        } else {
            placeholder("mad", "选一个群聊或 agent")
        }
    }

    private func placeholder(_ title: String, _ note: String) -> some View {
        VStack(spacing: 8) {
            Text(title).font(.title2.weight(.semibold))
            Text(note).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// A thin note at the top when the Mac cannot be reached or refuses the token.
public struct LinkNote: View {
    let link: LinkState
    public init(link: LinkState) { self.link = link }

    public var body: some View {
        switch link {
        case .refused: note("这台 Mac 不认这个令牌了：请重新配对。", "key.slash")
        case .offline: note("连不上这台 Mac。", "wifi.slash")
        case .failed(let why): note(why, "exclamationmark.triangle")
        case .connecting, .ok: EmptyView()
        }
    }

    private func note(_ text: String, _ icon: String) -> some View {
        Label(text, systemImage: icon)
            .font(.subheadline)
            .foregroundStyle(.orange)
            .padding(.horizontal, 12).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial)
    }
}

/// An agent's conversation: an `AgentStore` that runs while this is on screen, drawn by `ConversationView`.
struct AgentDetail: View {
    @Environment(DeckStore.self) private var deck
    @Environment(\.scenePhase) private var scenePhase
    @State private var store: AgentStore
    @State private var failure: String?
    private let api: MadAPI
    private let id: String
    private let onBack: (() -> Void)?
    private let openWeb: ((WebTarget) -> Void)?

    init(api: MadAPI, id: String, onBack: (() -> Void)?, openWeb: ((WebTarget) -> Void)? = nil) {
        self.api = api
        self.id = id
        self.onBack = onBack
        self.openWeb = openWeb
        _store = State(initialValue: AgentStore(id: id, api: api))
    }

    var body: some View {
        let model = deck.deck.map { AgentMapping.conversation(store: store, deck: $0) }
            ?? AgentConversation(title: id, agentID: id, canSend: false, sendHint: "")
        ConversationView(model: model, actions: actions, showsBack: onBack != nil)
            #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            #endif
            .task(id: scenePhase) {
                guard scenePhase == .active else { return }
                await store.run()
            }
            .overlay(alignment: .top) { NoticeBar(text: store.notice ?? failure) { store.dismissNotice(); failure = nil } }
    }

    private var actions: ConversationActions {
        ConversationActions(
            send: { text in Task { _ = await store.send(text) } },
            act: { cardID, actID, text in Task { await answer(cardID, actID, text) } },
            interrupt: { Task { await store.interrupt() } },
            loadImage: { image in try? await api.pictureData(url: image.source) },
            back: { onBack?() },
            openWeb: openWeb.map { open in { open(.agent(id)) } })
    }

    private func answer(_ cardID: String, _ actID: String, _ text: String?) async {
        // the item as the deck says now: a card that is not it any more sends nothing
        guard let d = deck.deck, let a = AgentMapping.answer(cardID: cardID, actID: actID, text: text, agent: id, deck: d) else {
            failure = "它现在在问别的了"
            await deck.refresh()
            return
        }
        do {
            try await api.act(key: a.key, verb: a.verb, option: a.option, text: a.text)
            failure = nil
            await deck.refresh()
            await store.look()
        } catch {
            failure = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
    }
}

/// A line under the top that says what the Mac refused, with a way to dismiss it.
struct NoticeBar: View {
    let text: String?
    let dismiss: () -> Void

    var body: some View {
        if let text {
            Button(action: dismiss) {
                Label(text, systemImage: "exclamationmark.triangle")
                    .font(.subheadline)
                    .foregroundStyle(.orange)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.regularMaterial)
            }
            .buttonStyle(.plain)
            .accessibilityHint("dismisses")
        }
    }
}
