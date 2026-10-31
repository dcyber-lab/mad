import Combine
import Foundation
import Observation

// The stores the views read. Each is @MainActor and @Observable: a view reads its properties and
// redraws when they change. Each has a `run()` that polls until the calling task is cancelled, so a
// view starts it with `.task { await store.run() }` and it stops with the view (or, with
// `.task(id: scenePhase)`, while the app is not active). `refresh()` is one look, for a pull to
// refresh or right after an action.

/// How the Mac answers.
public enum LinkState: Equatable, Sendable {
    /// Nothing has been heard yet.
    case connecting
    case ok
    /// The token is not the Mac's: pair again.
    case refused
    /// The Mac cannot be reached.
    case offline
    /// It answered, but not as mad does, or with an error; the text says how.
    case failed(String)

    init(_ error: Error) {
        switch error as? ConnectionError {
        case .refused?: self = .refused
        case .offline?: self = .offline
        case .some(let e): self = .failed(e.localizedDescription)
        case nil: self = .failed(error.localizedDescription)
        }
    }
}

/// The deck: polled about once a second while it runs. `deck` keeps the last good look, so the
/// screen does not empty when the Mac drops away; `link` says how the last look went.
@MainActor @Observable
public final class DeckStore {
    public private(set) var deck: Deck?
    public private(set) var link: LinkState = .connecting

    @ObservationIgnored public let api: MadAPI
    @ObservationIgnored public let interval: Duration

    public init(api: MadAPI, interval: Duration = .seconds(1)) {
        self.api = api
        self.interval = interval
    }

    /// One look at /api/deck.
    public func refresh() async {
        do {
            let d = try await api.deck()
            deck = d
            link = .ok
        } catch {
            if Task.isCancelled { return }
            link = LinkState(error)
        }
    }

    /// Looks, waits `interval`, looks again, until the task is cancelled.
    public func run() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: interval)
        }
    }

    // MARK: Lookups in the last look

    public func agent(id: String?) -> Agent? { deck?.agent(id: id) }
    public func channel(id: String?) -> Channel? { deck?.channel(id: id) }
}

/// One channel's messages, polled by number: each look asks for what came after the last one it
/// has, so it carries only news. A page of 200 means there may be more, and the next look follows
/// at once. Messages only grow, so a number already held is never added twice.
@MainActor @Observable
public final class ChannelStore {
    /// The server answers at most this many messages at a time.
    public static let pageLimit = 200

    public let id: String
    /// The channel's messages in number order.
    public private(set) var messages: [BusMessage] = []
    /// The number of the last message held (0 for none).
    public private(set) var last = 0
    public private(set) var paused = false
    /// How the person's last messages stand: whom they woke and how far each got.
    public private(set) var receipts: [BusReceipt] = []
    public private(set) var link: LinkState = .connecting
    /// The first look has come.
    public private(set) var loaded = false
    /// A post is on its way; one at a time.
    public private(set) var sending = false

    @ObservationIgnored private let api: MadAPI
    @ObservationIgnored private let interval: Duration
    @ObservationIgnored private var lastReadSent = 0
    @ObservationIgnored private var looking = false

    public init(id: String, api: MadAPI, interval: Duration = .milliseconds(1500)) {
        self.id = id
        self.api = api
        self.interval = interval
    }

    /// One look: asks for what came after `last`, and goes on asking while the pages are full.
    /// A look asked for while another runs is left to it.
    public func refresh() async {
        guard !looking else { return }
        looking = true
        defer { looking = false }
        var rounds = 0
        while rounds < 50 && !Task.isCancelled {
            rounds += 1
            do {
                let page = try await api.messages(channel: id, since: last)
                apply(page)
                link = .ok
                loaded = true
                if page.messages.count < Self.pageLimit { return }
            } catch {
                if !Task.isCancelled { link = LinkState(error) }
                return
            }
        }
    }

    func apply(_ page: MessagesPage) {
        for m in page.messages.sorted(by: { $0.seq < $1.seq }) where m.seq > last {
            messages.append(m)
            last = m.seq
        }
        paused = page.paused
        receipts = page.receipts
    }

    public func run() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: interval)
        }
    }

    /// Posts the person's message (with pictures, as a reply to message `replyTo`) and looks, so it
    /// shows at once. Throws what the Mac says when it refuses; a second post while one is on its
    /// way throws `ChannelStoreError.busy`.
    @discardableResult
    public func post(_ text: String, images: [PostImage] = [], replyTo: Int? = nil) async throws -> PostResult {
        guard !sending else { throw ChannelStoreError.busy }
        sending = true
        defer { sending = false }
        let result = try await api.post(channel: id, text: text, replyTo: replyTo, images: images)
        await refresh()
        return result
    }

    /// Tells the Mac the person has read the channel up to the last message held; nothing is sent
    /// when it was already told.
    public func markRead() async {
        guard last > lastReadSent else { return }
        let seq = last
        do {
            try await api.markRead(channel: id, seq: seq)
            lastReadSent = seq
        } catch {}
    }

    /// The message with number `seq`, for showing what a reply answers.
    public func message(_ seq: Int) -> BusMessage? {
        seq > 0 ? messages.first { $0.seq == seq } : nil
    }
}

public enum ChannelStoreError: Error, Equatable, LocalizedError {
    case busy
    public var errorDescription: String? { "A message is already on its way." }
}

/// One agent's conversation: its transcript (/api/chat, read by file and offset), the message being
/// written (/api/live), and writing to it. It wraps `ChatSession`, whose rules (one look at a time,
/// window mode asked for before every send) it keeps.
@MainActor @Observable
public final class AgentStore {
    public let id: String
    public private(set) var transcript = ChatTranscript()
    public private(set) var live = LiveState()
    public private(set) var loaded = false
    /// Why the transcript cannot be read now (offline, token refused, no such agent); nil when it can.
    public private(set) var problem: String?
    public private(set) var sending = false
    /// What the Mac said when a send or an interrupt failed; `dismissNotice()` clears it.
    public private(set) var notice: String?

    @ObservationIgnored private let session: ChatSession
    @ObservationIgnored private var subscriptions: Set<AnyCancellable> = []

    public init(id: String, api: ChatAPI) {
        self.id = id
        session = ChatSession(agentID: id, api: api)
        session.$transcript.sink { [weak self] in self?.transcript = $0 }.store(in: &subscriptions)
        session.$live.sink { [weak self] in self?.live = $0 }.store(in: &subscriptions)
        session.$loaded.sink { [weak self] in self?.loaded = $0 }.store(in: &subscriptions)
        session.$problem.sink { [weak self] in self?.problem = $0 }.store(in: &subscriptions)
        session.$sending.sink { [weak self] in self?.sending = $0 }.store(in: &subscriptions)
        session.$notice.sink { [weak self] in self?.notice = $0 }.store(in: &subscriptions)
    }

    public func dismissNotice() { session.notice = nil }

    /// Reads the transcript every ~2 s and follows the live message, until the task is cancelled.
    public func run() async { await session.run() }
    /// One look at the transcript.
    public func look() async { await session.look() }
    /// Sends `text`; true when the Mac took it.
    public func send(_ text: String) async -> Bool { await session.send(text) }
    public func interrupt() async { await session.interrupt() }
}
