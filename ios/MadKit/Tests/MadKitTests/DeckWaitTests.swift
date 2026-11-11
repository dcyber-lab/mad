import XCTest
@testable import MadKit

/// A Mac that long-polls: each look is held until the test answers it, and what it was asked is recorded.
@MainActor
private final class WaitingMac: MadAPI {
    struct Ask: Equatable { var tag: String?; var wait: Int }
    var asks: [Ask] = []
    /// When each look was asked.
    var askedAt: [Date] = []
    private var held: [CheckedContinuation<DeckAnswer, Error>] = []

    func deck(holding tag: String?, wait: Int) async throws -> DeckAnswer {
        asks.append(Ask(tag: tag, wait: wait))
        askedAt.append(Date())
        return try await withCheckedThrowingContinuation { held.append($0) }
    }

    /// Answers the oldest look still held, or the one at `index`.
    func answer(_ a: DeckAnswer, at index: Int = 0) { held.remove(at: index).resume(returning: a) }
    func fail() { held.removeFirst().resume(throwing: ConnectionError.offline) }
    var waiting: Int { held.count }

    func deck() async throws -> Deck { Deck() }
    func messages(channel: String, since: Int) async throws -> MessagesPage { MessagesPage(channel: channel, messages: [], last: since) }
    func post(channel: String, text: String, replyTo: Int?, images: [PostImage]) async throws -> PostResult { PostResult(seq: 1) }
    func dm(to agent: String, text: String, images: [PostImage]) async throws -> DMResult { DMResult(channel: "dm", seq: 1) }
    func markRead(channel: String, seq: Int) async throws {}
    func imageData(id: String) async throws -> Data { Data() }
    func act(key: String, verb: String, option: String?, text: String?) async throws {}
    func chat(agent: String, file: String, from: Int64) async throws -> ChatPage { ChatPage(file: "f", offset: 0, entries: []) }
    func live(agent: String, since: Int64) async throws -> LiveMessage? { nil }
    func send(agent: String, text: String) async throws {}
    func setMode(agent: String, mode: String) async throws {}
    func interrupt(agent: String) async throws {}
}

/// Waits until `cond` holds, for at most a second.
@MainActor
private func eventually(_ cond: () -> Bool, file: StaticString = #filePath, line: UInt = #line) async throws {
    let end = Date().addingTimeInterval(1)
    while !cond() {
        if Date() > end { XCTFail("timed out", file: file, line: line); return }
        try await Task.sleep(for: .milliseconds(5))
    }
}

@MainActor
final class DeckWaitTests: XCTestCase {
    /// The first look is answered at once and says the Mac can wait; from then on each look names the deck held and
    /// waits, and follows the last answer at once.
    func testLooksNameTheDeckHeldAndWait() async throws {
        let mac = WaitingMac()
        let s = DeckStore(api: mac, interval: .seconds(30)) // a sleep would stall the test: there must be none
        let task = Task { await s.run() }
        defer { task.cancel() }
        try await eventually { mac.waiting == 1 }
        XCTAssertEqual(mac.asks, [.init(tag: nil, wait: 0)])
        mac.answer(DeckAnswer(deck: Deck(machine: "A"), tag: "t1", wait: 25))
        try await eventually { mac.waiting == 1 && mac.asks.count == 2 }
        XCTAssertEqual(mac.asks.last, .init(tag: "t1", wait: 25))
        XCTAssertEqual(s.deck?.machine, "A")
    }

    /// A Mac that cannot wait (an older mad) is looked at every `interval`.
    func testAMacThatCannotWaitIsLookedAtEveryInterval() async throws {
        let mac = WaitingMac()
        let s = DeckStore(api: mac, interval: .milliseconds(200))
        let task = Task { await s.run() }
        defer { task.cancel() }
        try await eventually { mac.waiting == 1 }
        mac.answer(DeckAnswer(deck: Deck(machine: "Old"), tag: nil, wait: 0))
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(mac.asks.count, 1, "not asked again at once")
        try await eventually { mac.asks.count == 2 }
        XCTAssertEqual(mac.asks.last, .init(tag: nil, wait: 0))
    }

    func testANewDeckOrNoneIsFollowedAtOnce() async throws {
        let mac = WaitingMac()
        let s = DeckStore(api: mac, interval: .seconds(30))
        // A store that already holds t1 from a Mac that waits.
        let first = Task { await s.refresh() }
        try await eventually { mac.waiting == 1 }
        mac.answer(DeckAnswer(deck: Deck(machine: "A"), tag: "t1", wait: 25))
        await first.value

        let task = Task { await s.run() }
        defer { task.cancel() }
        try await eventually { mac.waiting == 1 }
        XCTAssertEqual(mac.asks.last, .init(tag: "t1", wait: 25))
        mac.answer(DeckAnswer(deck: nil, tag: "t1", wait: 25)) // nothing new
        try await eventually { mac.waiting == 1 && mac.asks.count == 3 }
        XCTAssertEqual(mac.asks.last, .init(tag: "t1", wait: 25))
        mac.answer(DeckAnswer(deck: Deck(machine: "B"), tag: "t2", wait: 25))
        try await eventually { mac.waiting == 1 && mac.asks.count == 4 }
        XCTAssertEqual(mac.asks.last, .init(tag: "t2", wait: 25))
        XCTAssertEqual(s.deck?.machine, "B")
        XCTAssertEqual(s.link, .ok)
    }

    /// A new deck with no tag, or with the tag it was asked with, is followed after `interval`: a proxy that drops
    /// tags must not make the store ask without pause.
    func testANewDeckWithNoTagWaitsAnInterval() async throws {
        let mac = WaitingMac()
        let s = DeckStore(api: mac, interval: .seconds(30))
        let first = Task { await s.refresh() }
        try await eventually { mac.waiting == 1 }
        mac.answer(DeckAnswer(deck: Deck(machine: "A"), tag: "t1", wait: 25))
        await first.value

        let task = Task { await s.run() }
        defer { task.cancel() }
        try await eventually { mac.waiting == 1 }
        mac.answer(DeckAnswer(deck: Deck(machine: "B"), tag: nil, wait: 25))
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(mac.asks.count, 2, "no look right after a deck with no tag")
        XCTAssertEqual(s.deck?.machine, "B")
    }

    /// A refresh and the long poll can cross: the deck of a look asked before the one held is dropped.
    func testAnOlderLookDoesNotReplaceANewerDeck() async throws {
        let mac = WaitingMac()
        let s = DeckStore(api: mac, interval: .seconds(30))
        let long = Task { await s.look(wait: 25) } // asked first
        try await eventually { mac.waiting == 1 }
        let quick = Task { await s.refresh() } // asked second
        try await eventually { mac.waiting == 2 }
        // The refresh is answered first, with the newer deck; the long poll's older one comes after.
        mac.answer(DeckAnswer(deck: Deck(machine: "new"), tag: "t2", wait: 25), at: 1)
        await quick.value
        mac.answer(DeckAnswer(deck: Deck(machine: "old"), tag: "t1", wait: 25))
        let look = await long.value
        XCTAssertEqual(look, .same)
        XCTAssertEqual(s.deck?.machine, "new")
    }

    /// A failure is tried again after `interval`, and the link says the Mac is away meanwhile.
    func testAFailureIsTriedAgainAfterAnInterval() async throws {
        let mac = WaitingMac()
        let s = DeckStore(api: mac, interval: .milliseconds(100))
        let task = Task { await s.run() }
        defer { task.cancel() }
        try await eventually { mac.waiting == 1 }
        let failed = Date()
        mac.fail()
        try await eventually { s.link == .offline }
        try await eventually { mac.asks.count == 2 }
        // Told by when the second look was asked, not by a pause of the test's own and a count after it: a busy
        // machine stretches such a pause past the interval.
        XCTAssertGreaterThanOrEqual(mac.askedAt[1].timeIntervalSince(failed), 0.09, "not tried again at once")
    }
}

/// A URL protocol that records the requests it is given and answers with headers.
private final class HeaderScripted: URLProtocol {
    nonisolated(unsafe) static var answer: (Int, [String: String], Data) = (200, [:], Data())
    nonisolated(unsafe) static var requests: [URLRequest] = []
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        Self.requests.append(request)
        let (code, headers, data) = Self.answer
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: code, httpVersion: nil, headerFields: headers)!,
                            cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

final class DeckWaitClientTests: XCTestCase {
    private func client() -> MadClient {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [HeaderScripted.self]
        HeaderScripted.requests = []
        return MadClient(mac: Mac(name: "m", baseURL: URL(string: "https://box")!, token: "t"), session: URLSession(configuration: config))
    }

    /// The look names the deck held, asks to wait, and waits longer than the Mac may hold it.
    func testTheRequestNamesTheDeckAndWaits() {
        let r = client().deckWaitRequest(holding: "\"t1\"", wait: 25)
        XCTAssertEqual(r.value(forHTTPHeaderField: "If-None-Match"), "\"t1\"")
        XCTAssertEqual(URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.queryItems, [URLQueryItem(name: "wait", value: "25")])
        XCTAssertGreaterThan(r.timeoutInterval, 25)
        XCTAssertEqual(r.value(forHTTPHeaderField: "Authorization"), "Bearer t")

        let once = client().deckWaitRequest(holding: nil, wait: 0)
        XCTAssertNil(once.value(forHTTPHeaderField: "If-None-Match"))
        XCTAssertNil(URLComponents(url: once.url!, resolvingAgainstBaseURL: false)?.queryItems)
        XCTAssertEqual(once.timeoutInterval, 10)
    }

    func testAnswers() async throws {
        let api = client()
        HeaderScripted.answer = (200, ["ETag": "\"t2\"", "X-Mad-Wait": "25"], Data(#"{"machine":"Studio","projects":[]}"#.utf8))
        let fresh = try await api.deck(holding: "\"t1\"", wait: 25)
        XCTAssertEqual(fresh.deck?.machine, "Studio")
        XCTAssertEqual(fresh.tag, "\"t2\"")
        XCTAssertEqual(fresh.wait, 25)

        HeaderScripted.answer = (304, ["ETag": "\"t2\"", "X-Mad-Wait": "25"], Data())
        let same = try await api.deck(holding: "\"t2\"", wait: 25)
        XCTAssertNil(same.deck)
        XCTAssertEqual(same.tag, "\"t2\"")
        XCTAssertEqual(same.wait, 25)

        // An older mad: no tag, no wait.
        HeaderScripted.answer = (200, [:], Data(#"{"machine":"Old","projects":[]}"#.utf8))
        let old = try await api.deck(holding: nil, wait: 25)
        XCTAssertEqual(old.deck?.machine, "Old")
        XCTAssertNil(old.tag)
        XCTAssertEqual(old.wait, 0)

        HeaderScripted.answer = (403, [:], Data())
        do {
            _ = try await api.deck(holding: nil, wait: 0)
            XCTFail("a refused token")
        } catch let e as ConnectionError {
            XCTAssertEqual(e, .refused)
        }
    }
}
