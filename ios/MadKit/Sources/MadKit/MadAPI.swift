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
    /// GET /api/finish?id: what finishing the agent's branch offers (push, a PR, rebase, merge, the person's own).
    func finishActions(agent id: String) async throws -> FinishMenu
    /// POST /api/finish?id&i&name: runs the action at `index` (named `name`, so a changed list is refused).
    func runFinish(agent id: String, index: Int, name: String) async throws -> FinishResult
}

/// What finishing an agent's branch offers (the deck's f menu): its branch, the default branch, the actions.
public struct FinishMenu: Equatable, Decodable, Sendable {
    public var branch: String
    public var base: String
    public var actions: [FinishAction]
    /// Why there is nothing to offer ("not on a branch"); nil when there is a list.
    public var error: String?

    enum CodingKeys: String, CodingKey { case branch, base, actions, error }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        branch = try c.decodeIfPresent(String.self, forKey: .branch) ?? ""
        base = try c.decodeIfPresent(String.self, forKey: .base) ?? ""
        actions = try c.decodeIfPresent([FinishAction].self, forKey: .actions) ?? []
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
    public init(branch: String = "", base: String = "", actions: [FinishAction] = [], error: String? = nil) {
        self.branch = branch; self.base = base; self.actions = actions; self.error = error
    }
}

/// One way to finish a branch: its name (what is sent back) and, for a built-in one, its kind: "push", "pr",
/// "rebase" or "merge" (empty for the person's own, which shows its name).
public struct FinishAction: Equatable, Decodable, Sendable {
    public var name: String
    public var kind: String

    enum CodingKeys: String, CodingKey { case name, kind }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
    }
    public init(name: String, kind: String = "") { self.name = name; self.kind = kind }
}

/// How a finish action went: whether it worked, what it printed (its tail), and the error when it did not.
public struct FinishResult: Equatable, Decodable, Sendable {
    public var ok: Bool
    public var output: String
    public var error: String?

    enum CodingKeys: String, CodingKey { case ok, output, error }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        ok = try c.decodeIfPresent(Bool.self, forKey: .ok) ?? false
        output = try c.decodeIfPresent(String.self, forKey: .output) ?? ""
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
    public init(ok: Bool, output: String = "", error: String? = nil) { self.ok = ok; self.output = output; self.error = error }

    /// The last line it printed that is not empty, else the error.
    public var lastLine: String {
        output.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespaces) }.last { !$0.isEmpty } ?? (error ?? "")
    }
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
    public func finishActions(agent id: String) async throws -> FinishMenu { FinishMenu() }
    public func runFinish(agent id: String, index: Int, name: String) async throws -> FinishResult {
        throw ConnectionError.unexpected("No finish.")
    }
}
