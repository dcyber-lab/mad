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
    @State private var store: ChannelStore
    @State private var pictures = PictureCache()

    init(id: String, api: MadAPI, onBack: (() -> Void)?, openAgent: @escaping (String) -> Void) {
        self.id = id
        self.api = api
        self.onBack = onBack
        self.openAgent = openAgent
        _store = State(initialValue: ChannelStore(id: id, api: api))
    }

    var body: some View {
        ChannelView(screen: ChannelMapping.screen(channel: id, messages: store.messages, deck: deck.deck, paused: store.paused),
                    actions: actions, panelOpen: debugPanel)
            .task { await store.run() }
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
        let store = store, api = api, cache = pictures, open = openAgent
        return ChannelActions(
            send: { text, images, replyTo in
                do {
                    try await store.post(text, images: images.map { PostImage(mime: $0.mime, data: $0.data) }, replyTo: replyTo)
                    return true
                } catch { return false }
            },
            openAgent: { open($0) },
            loadPicture: { id in await cache.data(for: id) { try await api.imageData(id: id) } },
            back: onBack.map { back in { back() } })
    }
}

/// Pictures fetched once: a message scrolled out of sight and back does not ask the Mac again.
actor PictureCache {
    private var held: [String: Data] = [:]
    private var order: [String] = []
    private let limit = 64

    func data(for id: String, fetch: @Sendable () async throws -> Data) async -> Data? {
        if let d = held[id] { return d }
        guard let d = try? await fetch() else { return nil }
        held[id] = d
        order.append(id)
        if order.count > limit { held[order.removeFirst()] = nil }
        return d
    }
}
