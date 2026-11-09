import Foundation

/// Everything the stores ask of the Mac. `MadClient` is it; a test's fake can be, so the stores'
/// polling and `since` logic run without a server.
public protocol MadAPI: ChatAPI {
    /// GET /api/deck.
    func deck() async throws -> Deck
    /// GET /api/deck naming the deck held (`tag`, its ETag). With `wait` > 0 the Mac holds the answer until the deck
    /// changes or `wait` seconds pass (a long poll); a Mac that cannot wait answers at once.
    func deck(holding tag: String?, wait: Int) async throws -> DeckAnswer
    /// GET /api/bus/messages?channel&since.
    func messages(channel: String, since: Int) async throws -> MessagesPage
    /// POST /api/bus/post.
    func post(channel: String, text: String, replyTo: Int?, images: [PostImage]) async throws -> PostResult
    /// POST /api/bus/dm.
    func dm(to agent: String, text: String, images: [PostImage]) async throws -> DMResult
    /// POST /api/bus/read.
    func markRead(channel: String, seq: Int) async throws
    /// GET /api/bus/image?id: a picture's bytes.
    func imageData(id: String) async throws -> Data
    /// GET of an image address a chat entry carries (`ChatImage.url`).
    func pictureData(url: String) async throws -> Data
    /// POST /api/inbox: the person acts on an inbox item.
    func act(key: String, verb: String, option: String?, text: String?) async throws
    /// POST /api/budget {op, usd}: "open" lifts mad's own budget for the day, "raise" adds `usd` to it.
    func budget(op: String, usd: Double) async throws
    /// POST /api/agents/stop?id: puts the agent to sleep.
    func sleepAgent(id: String) async throws
    /// PATCH /api/agents?id {name}: the person's name for the agent.
    func renameAgent(id: String, name: String) async throws
    /// POST /api/terminal?id: opens the agent in mad's terminal deck on the Mac.
    func openTerminal(agent id: String) async throws
}

extension MadClient: MadAPI {}

/// An answer to a look at the deck that names the deck held.
public struct DeckAnswer: Sendable {
    /// The Mac's deck now; nil when the one held still is (304).
    public var deck: Deck?
    /// The deck's tag (ETag), to name it by on the next look; nil when the Mac sent none.
    public var tag: String?
    /// How long the Mac lets a look wait for news, in seconds (X-Mad-Wait); 0 for a Mac that answers at once (an
    /// older mad), which is looked at every `interval` instead.
    public var wait: Int

    public init(deck: Deck?, tag: String?, wait: Int) {
        self.deck = deck; self.tag = tag; self.wait = wait
    }
}

extension MadAPI {
    /// A fake of the Mac that knows no long poll answers each look at once with its deck, and cannot wait.
    public func deck(holding tag: String?, wait: Int) async throws -> DeckAnswer {
        DeckAnswer(deck: try await deck(), tag: nil, wait: 0)
    }

    /// A fake of the Mac that has no pictures need not say so.
    public func pictureData(url: String) async throws -> Data { throw ConnectionError.unexpected("No picture.") }

    /// Nor a budget, a way to sleep or rename an agent, or a terminal.
    public func budget(op: String, usd: Double) async throws { throw ConnectionError.unexpected("No budget.") }
    public func sleepAgent(id: String) async throws { throw ConnectionError.unexpected("No sleep.") }
    public func renameAgent(id: String, name: String) async throws { throw ConnectionError.unexpected("No rename.") }
    public func openTerminal(agent id: String) async throws { throw ConnectionError.unexpected("No terminal.") }
}
