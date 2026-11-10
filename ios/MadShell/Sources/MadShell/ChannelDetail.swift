import MadKit
import MadUI
import SwiftUI

/// A channel in the native window: a `ChannelStore` (its messages, polled while the view is there) and the
/// deck's, mapped onto what MadUI's `ChannelView` draws. Sending, reading and the pictures are the store's
/// and the client's; the view knows none of them.
struct ChannelDetail: View {
    let id: String
    let api: MadAPI
    let onBack: (() -> Void)?
    let openAgent: (String) -> Void

    @Environment(DeckStore.self) private var deck
    @Environment(\.scenePhase) private var scenePhase
    @State private var store: ChannelStore
    @State private var pictures = PictureCache()
    @State private var memo = ScreenMemo()

    init(id: String, api: MadAPI, onBack: (() -> Void)?, openAgent: @escaping (String) -> Void) {
        self.id = id
        self.api = api
        self.onBack = onBack
        self.openAgent = openAgent
        // no cache read here: this init runs on every refresh of the parent, the store is kept from the first
        _store = State(initialValue: ChannelStore(id: id, api: api, restore: false))
    }

    var body: some View {
        ChannelView(screen: memo.screen(channel: id, store: store, deck: deck.deck),
                    actions: actions, panelOpen: debugPanel)
            // Polls while the app is active; the task restarts when it comes back, so it looks at once.
            .task(id: scenePhase) {
                store.restore()
                guard scenePhase == .active else { return }
                await store.run()
            }
            // What is on screen is read: when the first look comes and as more arrives.
            .task(id: store.last) { await store.markRead() }
    }

    /// Debug builds: `-nativePanel YES` shows the 群信息 panel open at once.
    private var debugPanel: Bool {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "nativePanel")
        #else
        false
        #endif
    }

    private var actions: ChannelActions {
        let store = store, api = api, cache = pictures, open = openAgent, deck = deck
        return ChannelActions(
            send: { text, images, replyTo in
                do {
                    try await store.post(text, images: images.map { PostImage(mime: $0.mime, data: $0.data) }, replyTo: replyTo)
                    return true
                } catch { return false }
            },
            openAgent: { open($0) },
            loadPicture: { id in await cache.data(for: id) { try await api.imageData(id: id) } },
            back: onBack.map { back in { back() } },
            act: { key, actID, text in
                Task { @MainActor in
                    // the item as the deck says now: a card that is not it any more sends nothing
                    if let item = deck.deck?.inbox.waiting.first(where: { $0.key == key }), let a = item.acts.first(where: { $0.id == actID }) {
                        try? await api.act(key: item.key, verb: a.verb, option: a.option, text: text.flatMap { $0.isEmpty ? nil : $0 })
                    }
                    await deck.refresh()
                }
            },
            openCreated: { target in
                switch target {
                case .channel(let c): open(c)
                case .agent(let a): open(a)
                }
            })
    }
}

/// Pictures fetched once: a message scrolled out of sight and back does not ask the Mac again. Held by
/// bytes (the web page keeps about 40 MB), the least recently used going first; a picture being fetched
/// is fetched once however many views ask for it.
actor PictureCache {
    private var held: [String: Data] = [:]
    /// Least recently used first.
    private var order: [String] = []
    private var bytes = 0
    private var loading: [String: Task<Data?, Never>] = [:]
    private let budget: Int

    init(budget: Int = 40 << 20) { self.budget = budget }

    var count: Int { held.count }
    var totalBytes: Int { bytes }

    func data(for id: String, fetch: @escaping @Sendable () async throws -> Data) async -> Data? {
        if let d = held[id] { touch(id); return d }
        if let running = loading[id] { return await running.value }
        let task = Task { try? await fetch() }
        loading[id] = task
        let d = await task.value
        loading[id] = nil
        if let d { store(d, as: id) }
        return d
    }

    private func touch(_ id: String) {
        if let i = order.firstIndex(of: id) { order.remove(at: i) }
        order.append(id)
    }

    private func store(_ d: Data, as id: String) {
        guard d.count <= budget else { return } // too big to keep; the caller still has it
        if let old = held[id] { bytes -= old.count }
        held[id] = d
        bytes += d.count
        touch(id)
        while bytes > budget, let oldest = order.first {
            order.removeFirst()
            if let gone = held.removeValue(forKey: oldest) { bytes -= gone.count }
        }
    }
}

/// The channel's screen, kept while what it is made of has not changed. The deck is refreshed far more often than
/// a channel's agents or the channel itself change (usage, git counts ...), and the screen is built from every
/// message: without this each refresh rebuilds all of it.
@MainActor
final class ScreenMemo {
    private struct Key: Equatable {
        var count: Int, last: Int, paused: Bool, loaded: Bool, receipts: [BusReceipt]
        var channel: Channel?, agents: [Agent]
        /// the create requests waiting and the ones that were answered: the card and the result lines read them
        var waitingCreates: [InboxItem], creates: [InboxItem]
        /// the minute: the screen reads the clock for its time lines
        var minute: Int
    }
    private var key: Key?
    private var screen: ChannelScreen?

    func screen(channel id: String, store: ChannelStore, deck: Deck?) -> ChannelScreen {
        let k = Key(count: store.messages.count, last: store.last, paused: store.paused, loaded: store.loaded || store.fromCache,
                    receipts: store.receipts, channel: deck?.channel(id: id), agents: deck?.agents ?? [],
                    waitingCreates: deck?.inbox.waiting.filter { $0.kind == "create" } ?? [], creates: deck?.creates ?? [],
                    minute: Int(Date().timeIntervalSince1970 / 60))
        if let screen, k == key { return screen }
        let s = ChannelMapping.screen(channel: id, messages: store.messages, deck: deck, paused: k.paused, loaded: k.loaded, receipts: k.receipts)
        key = k
        screen = s
        return s
    }
}
