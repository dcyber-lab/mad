import MadKit
import MadUI
import SwiftUI

/// The native window of mad, on the iPhone and on the Mac: the home list of MadUI over a `DeckStore`, and a detail
/// for what a row opens (a channel, an agent, the Usage screen; on the Mac, Today when nothing is open).
///
/// It owns the `DeckStore` for `api`, polls it while the app is active, and offers it to the views below
/// through the environment (`@Environment(DeckStore.self)`). `onSwitchMac` is the way back to the list
/// of Macs (the iPhone's; the Mac app has no such list). `openWeb` shows the page on a view that has no
/// native screen yet (the ⋯ menu's usage, board ...; an agent's terminal and changes).
public struct NativeRoot: View {
    @State private var store: DeckStore
    @State private var selection: HomeRow.ID?
    /// What the Mac refused when the person acted on something that waits, until dismissed.
    @State private var notice: String?
    #if os(macOS)
    @State private var showPhone = false
    #endif
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
        #if os(macOS)
        // `-nativePhone YES` shows the 手机设备 sheet at once
        if UserDefaults.standard.bool(forKey: "nativePhone") { _showPhone = State(initialValue: true) }
        #endif
        #endif
    }

    private var model: HomeModel {
        guard let deck = store.deck else { return HomeModel(machine: title, sections: []) }
        return HomeMapping.model(from: deck, fallbackName: title)
    }

    private var actions: HomeActions {
        let act: (String, String, String?) -> Void = { item, act, text in Task { await self.act(item: item, act: act, text: text) } }
        let interrupt: (String) -> Void = { id in Task { try? await api.interrupt(agent: id); await store.refresh() } }
        #if os(macOS)
        // 手机设备 is native on the Mac: a sheet over the window, not the page in a window of its own
        return Self.homeActions(open: { selection = $0 }, switchMac: onSwitchMac, openWeb: openWeb, openPhone: { showPhone = true },
                                act: act, interrupt: interrupt)
        #else
        return Self.homeActions(open: { selection = $0 }, switchMac: onSwitchMac, openWeb: openWeb, act: act, interrupt: interrupt)
        #endif
    }

    /// What the home's controls do: a row opens, the title's ▾ and ⋯'s 切换 Mac switch Mac, 用量 opens the Usage screen,
    /// and everything that is not native yet (the other ⋯ items, the + items) opens the page on its view. The search
    /// needs nothing of the app: the list filters itself.
    static func homeActions(open: @escaping (HomeRow.ID) -> Void, switchMac: (() -> Void)?,
                            openWeb: ((WebTarget) -> Void)?, openPhone: (() -> Void)? = nil,
                            act: @escaping (String, String, String?) -> Void = { _, _, _ in },
                            interrupt: @escaping (String) -> Void = { _ in }) -> HomeActions {
        HomeActions(
            open: open,
            switchMac: { switchMac?() },
            create: { item in openWeb?(.view(Self.webView(forNew: item))) },
            menu: { item in
                if item == .switchMac { switchMac?() }
                else if item == .usage { open(HomeRow.usageID) }
                else if item == .devices, let openPhone { openPhone() }
                else if let v = Self.webView(for: item) { openWeb?(.view(v)) }
            },
            act: act,
            interrupt: interrupt)
    }

    /// An act on what waits, from the list or Today: the item as the deck says now (one answered meanwhile sends
    /// nothing), then a fresh look.
    @MainActor
    private func act(item key: String, act id: String, text: String?) async {
        guard let item = store.deck?.inbox.items.first(where: { $0.key == key }), let a = item.acts.first(where: { $0.id == id }) else {
            notice = L("它现在在问别的了")
            await store.refresh()
            return
        }
        do {
            try await api.act(key: item.key, verb: a.verb, option: a.option, text: text.flatMap { $0.isEmpty ? nil : $0 })
            notice = nil
        } catch {
            notice = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
        }
        await store.refresh()
    }

    /// The page's dialog for a + item.
    static func webView(forNew item: HomeNewItem) -> WebTarget.View {
        switch item {
        case .chat: .newchat
        case .channel: .newchannel
        case .team: .newteam
        }
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
                // leaving the foreground: what the device keeps of the Mac is written now, not after its pause
                if scenePhase != .active { SnapshotCache.flushAll() }
                guard scenePhase == .active else { return }
                await store.run()
            }
            #if os(iOS)
            // a thin bar across the top; the Mac's is a capsule in the detail pane (MacShell)
            .overlay(alignment: .top) { LinkNote(link: store.link) }
            #endif
            .overlay(alignment: .bottom) { NoticeBar(text: notice) { notice = nil } }
            #if DEBUG && os(macOS)
            .task { await DebugWindow.runIfAsked() }
            #endif
    }

    /// Whether the row's own view draws its head and way back (a channel, an agent, Usage): the iPhone's bar stays out of it.
    private func drawsOwnHead(_ id: HomeRow.ID) -> Bool {
        id == HomeRow.usageID || store.channel(id: id) != nil || store.agent(id: id) != nil
    }

    /// On the Mac: whether a channel's or an agent's head is on screen (the link's note then goes in it).
    private func rowOpen(_ id: HomeRow.ID) -> Bool { store.channel(id: id) != nil || store.agent(id: id) != nil }

    @ViewBuilder private var content: some View {
        #if os(macOS)
        MacShell(model: model, selection: $selection, actions: actions, link: store.link,
                 rowOpen: selection.map { rowOpen($0) } ?? false) {
            Detail(selection: selection, api: api, onBack: nil, open: { selection = $0 }, openWeb: openWeb, home: model, homeActions: actions)
        }
        // a channel's or an agent's name pill says it in place of its state line (see MacShell for no row open)
        .environment(\.madLinkNote, LinkNote.message(for: store.link))
        .sheet(isPresented: $showPhone) {
            if let phone = api as? PhoneAPI { PhoneSheet(api: phone) { showPhone = false } }
        }
        // the menu bar's 前往 does what the sidebar's menu does
        .focusedSceneValue(\.madHomeMenu, actions.menu)
        #else
        // The home list has its own navigation; a row opens the detail over it, with a way back.
        if let id = selection {
            NavigationStack {
                Detail(selection: id, api: api, onBack: { selection = nil }, open: { selection = $0 }, openWeb: openWeb, home: model,
                       homeActions: actions)
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

/// What a row opens: a channel, an agent, the Usage screen; on the Mac, Today when nothing (or 今日) is chosen.
struct Detail: View {
    @Environment(DeckStore.self) private var store
    let selection: HomeRow.ID?
    let api: MadAPI
    /// The iPhone's way back to the list; the Mac's detail pane has none.
    var onBack: (() -> Void)?
    /// Opens another row (a member of the channel).
    let open: (HomeRow.ID) -> Void
    var openWeb: ((WebTarget) -> Void)?
    /// The list's model and actions: Today is drawn from them.
    var home = HomeModel(machine: "", sections: [])
    var homeActions = HomeActions()

    var body: some View {
        if let id = selection, id != HomeRow.todayID {
            if id == HomeRow.usageID {
                UsageDetail(api: api, onBack: onBack, open: open)
            } else if store.channel(id: id) != nil {
                ChannelDetail(id: id, api: api, onBack: onBack, openAgent: open)
                    .id(id) // another channel is another store
            } else if let agent = store.agent(id: id) {
                AgentDetail(api: api, id: agent.id, onBack: onBack, openWeb: openWeb).id(agent.id)
            } else {
                placeholder(id, L("已经不在了"))
            }
        } else {
            TodayView(model: home, actions: homeActions)
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

    /// What the link says when it is not good, and its symbol; nil when all is well.
    public static func message(for link: LinkState) -> MadLinkNote? {
        switch link {
        case .refused: MadLinkNote(text: L("这台 Mac 不认这个令牌了：请重新配对。"), symbol: "key.slash")
        case .offline: MadLinkNote(text: L("连不上这台 Mac。"), symbol: "wifi.slash")
        case .failed(let why): MadLinkNote(text: why, symbol: "exclamationmark.triangle")
        case .connecting, .ok: nil
        }
    }

    public var body: some View {
        if let m = Self.message(for: link) { note(m.text, m.symbol) }
    }

    private func note(_ text: String, _ icon: String) -> some View {
        #if os(macOS)
        // a small floating capsule, like the name pill: it covers no more than itself, and a long text is cut at the end
        HStack(spacing: 6) {
            Image(systemName: icon)
            Text(text).lineLimit(1).truncationMode(.tail)
        }
        .font(.system(size: 13)) // MadType.label
        .foregroundStyle(.orange)
        .padding(.horizontal, 14).padding(.vertical, 7)
        // opaque, so it reads the same over anything
        .background(Color.chatComposer, in: Capsule())
        .overlay(Capsule().strokeBorder(Color.chatLine.opacity(0.6)))
        .shadow(color: .black.opacity(0.08), radius: 8, y: 2)
        #else
        Label(text, systemImage: icon)
            .font(.subheadline)
            .foregroundStyle(.orange)
            .padding(.horizontal, 12).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial)
        #endif
    }
}

/// An agent's conversation: an `AgentStore` that runs while this is on screen, drawn by `ConversationView`.
struct AgentDetail: View {
    @Environment(DeckStore.self) private var deck
    @Environment(\.scenePhase) private var scenePhase
    @State private var store: AgentStore
    @State private var failure: String?
    /// How the last finish run went, until dismissed (a note, not a failure, when it worked).
    @State private var done: String?
    /// The ways to finish the agent's branch, as the Mac last listed them.
    @State private var finish: [FinishButton] = []
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
        var model = deck.deck.map { AgentMapping.conversation(store: store, deck: $0) }
            ?? AgentConversation(title: id, agentID: id, canSend: false, sendHint: "")
        model.details?.finish = finish
        return ConversationView(model: model, actions: actions, showsBack: onBack != nil)
            #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            #endif
            .task(id: scenePhase) {
                guard scenePhase == .active else { return }
                await store.run()
            }
            // the finish menu: looked at when the page opens and again when the project's git changes
            .task(id: gitKey) { await loadFinish() }
            .overlay(alignment: .top) {
                if let note = store.notice ?? failure {
                    NoticeBar(text: note) { store.dismissNotice(); failure = nil }
                } else {
                    NoticeBar(text: done, ok: true) { done = nil }
                }
            }
    }

    /// What the finish menu depends on: the branch, the changes and the commits not pushed of the agent's project.
    private var gitKey: String {
        let p = deck.deck?.projects.first { $0.agents.contains { $0.id == id } }
        return "\(p?.branch ?? "")|\(p?.changed ?? 0)|\(p?.ahead ?? 0)"
    }

    private func loadFinish() async {
        guard let menu = try? await api.finishActions(agent: id) else { return }
        finish = AgentMapping.finishButtons(menu)
    }

    /// Runs a finish action, says how it went (its output's last line), and looks at the deck and the menu again.
    private func runFinish(_ b: FinishButton) async {
        do {
            let r = try await api.runFinish(agent: id, index: b.index, name: b.name)
            if r.ok { done = AgentMapping.finishNote(b, r); failure = nil } else { failure = AgentMapping.finishNote(b, r) }
        } catch {
            failure = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
        await deck.refresh()
        await loadFinish()
    }

    private var actions: ConversationActions {
        ConversationActions(
            send: { text in Task { _ = await store.send(text) } },
            act: { cardID, actID, text in Task { await answer(cardID, actID, text) } },
            interrupt: { Task { await store.interrupt() } },
            loadImage: { image in try? await api.pictureData(url: image.source) },
            back: { onBack?() },
            openWeb: openWeb.map { open in { open(.agent(id)) } },
            openTerminal: { Task { await run { try await api.openTerminal(agent: id) } } },
            finish: { b in Task { await runFinish(b) } },
            rename: { name in Task { await run { try await api.renameAgent(id: id, name: name) } } },
            sleep: { Task { await run { try await api.sleepAgent(id: id) } } })
    }

    /// Runs a call to the Mac for this agent: what it refuses shows in the notice; the deck is looked at again after.
    private func run(_ call: () async throws -> Void) async {
        do {
            try await call()
            failure = nil
        } catch {
            failure = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
        await deck.refresh()
    }

    private func answer(_ cardID: String, _ actID: String, _ text: String?) async {
        // the item as the deck says now: a card that is not it any more sends nothing
        guard let d = deck.deck, let a = AgentMapping.answer(cardID: cardID, actID: actID, text: text, agent: id, deck: d) else {
            failure = L("它现在在问别的了")
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

/// A line under the top that says what the Mac refused (or, `ok`, how something went), with a way to dismiss it.
struct NoticeBar: View {
    let text: String?
    var ok = false
    let dismiss: () -> Void

    var body: some View {
        if let text {
            Button(action: dismiss) {
                Label(text, systemImage: ok ? "checkmark.circle" : "exclamationmark.triangle")
                    .font(.subheadline)
                    .foregroundStyle(ok ? Color.madWork : Color.orange)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.regularMaterial)
            }
            .buttonStyle(.plain)
            .accessibilityHint("dismisses")
        }
    }
}

/// The Usage screen over the deck: its quotas and spend as `UsageMapping` says them, 今天不限 sent to the Mac.
struct UsageDetail: View {
    @Environment(DeckStore.self) private var store
    let api: MadAPI
    var onBack: (() -> Void)?
    let open: (HomeRow.ID) -> Void
    @State private var failure: String?

    var body: some View {
        UsageView(screen: store.deck.map { UsageMapping.screen(from: $0) } ?? UsageScreen(),
                  actions: UsageActions(
                    liftToday: { Task { await lift() } },
                    openAgent: { open($0) },
                    back: onBack))
            #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            #endif
            .overlay(alignment: .top) { NoticeBar(text: failure) { failure = nil } }
    }

    private func lift() async {
        do {
            try await api.budget(op: "open", usd: 0)
            failure = nil
        } catch {
            failure = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
        await store.refresh()
    }
}
