import XCTest
@testable import MadKit

/// A Mac that answers as scripted, recording what it was asked.
@MainActor
private final class FakeMac: MadAPI {
    var deckAnswers: [Result<Deck, Error>] = []
    var deckCalls = 0
    var pages: [Result<MessagesPage, Error>] = []
    var sinces: [Int] = []
    var posts: [(channel: String, text: String, replyTo: Int?, images: Int)] = []
    var reads: [Int] = []
    var postGate: CheckedContinuation<Void, Never>?
    var holdPost = false

    func deck() async throws -> Deck {
        deckCalls += 1
        return try deckAnswers.isEmpty ? Deck() : deckAnswers.removeFirst().get()
    }
    func messages(channel: String, since: Int) async throws -> MessagesPage {
        sinces.append(since)
        guard !pages.isEmpty else { return MessagesPage(channel: channel, messages: [], last: since) }
        return try pages.removeFirst().get()
    }
    func post(channel: String, text: String, replyTo: Int?, images: [PostImage]) async throws -> PostResult {
        posts.append((channel, text, replyTo, images.count))
        if holdPost { await withCheckedContinuation { c in self.postGate = c } }
        return PostResult(seq: 99)
    }
    func dm(to agent: String, text: String, images: [PostImage]) async throws -> DMResult { DMResult(channel: "dm", seq: 1) }
    func markRead(channel: String, seq: Int) async throws { reads.append(seq) }
    func imageData(id: String) async throws -> Data { Data() }
    func act(key: String, verb: String, option: String?, text: String?) async throws {}
    func chat(agent: String, file: String, from: Int64) async throws -> ChatPage { ChatPage(file: "f", offset: 0, entries: []) }
    func live(agent: String, since: Int64) async throws -> LiveMessage? { nil }
    func send(agent: String, text: String) async throws {}
    func setMode(agent: String, mode: String) async throws {}
    func interrupt(agent: String) async throws {}
}

private func msg(_ seq: Int, _ text: String = "m", from: String = "kim") -> BusMessage {
    BusMessage(seq: seq, from: .init(id: from, label: from), text: text)
}

@MainActor
final class DeckStoreTests: XCTestCase {
    /// A look that differs from the last only in the time the Mac made it tells nobody: every screen that reads the
    /// deck would be drawn again once a second for nothing.
    func testADeckThatDiffersOnlyInItsTimeIsNotNews() async {
        let mac = FakeMac()
        var a = Deck(machine: "Studio"), same = Deck(machine: "Studio"), changed = Deck(machine: "Studio")
        a.at = Date(timeIntervalSince1970: 100); same.at = Date(timeIntervalSince1970: 101)
        changed.at = Date(timeIntervalSince1970: 102); changed.working = ["x"]
        mac.deckAnswers = [.success(a), .success(same), .success(changed)]
        let s = DeckStore(api: mac)
        await s.refresh()
        var heard = 0
        func watch() { withObservationTracking { _ = s.deck } onChange: { heard += 1 } }
        watch()
        await s.refresh()
        XCTAssertEqual(heard, 0, "the same deck, an instant later, was announced")
        XCTAssertEqual(s.deck?.at, a.at, "the first time stays")
        await s.refresh()
        XCTAssertEqual(heard, 1, "a changed deck is news")
        XCTAssertEqual(s.deck?.working, ["x"])
    }

    func testALookFillsTheDeckAndSetsTheLinkOK() async {
        let mac = FakeMac()
        mac.deckAnswers = [.success(Deck(machine: "Studio", projects: [Project(name: "p", agents: [Agent(id: "a1", name: "ios")])]))]
        let s = DeckStore(api: mac)
        XCTAssertEqual(s.link, .connecting)
        XCTAssertNil(s.deck)
        await s.refresh()
        XCTAssertEqual(s.link, .ok)
        XCTAssertEqual(s.deck?.machine, "Studio")
        XCTAssertEqual(s.agent(id: "a1")?.label, "ios", "a lookup by id")
        XCTAssertNil(s.agent(id: "zzz"))
    }

    func testFailuresSetTheLinkAndKeepTheLastDeck() async {
        let mac = FakeMac()
        mac.deckAnswers = [.success(Deck(machine: "Studio")), .failure(ConnectionError.offline),
                           .failure(ConnectionError.refused), .failure(ConnectionError.unexpected("HTTP 500")), .success(Deck(machine: "Back"))]
        let s = DeckStore(api: mac)
        await s.refresh()
        await s.refresh()
        XCTAssertEqual(s.link, .offline)
        XCTAssertEqual(s.deck?.machine, "Studio", "the last good look stays")
        await s.refresh()
        XCTAssertEqual(s.link, .refused)
        await s.refresh()
        XCTAssertEqual(s.link, .failed("HTTP 500"))
        await s.refresh()
        XCTAssertEqual(s.link, .ok)
        XCTAssertEqual(s.deck?.machine, "Back")
    }

    func testRunPollsUntilCancelled() async throws {
        let mac = FakeMac()
        let s = DeckStore(api: mac, interval: .milliseconds(20))
        let task = Task { await s.run() }
        while mac.deckCalls < 3 { try await Task.sleep(for: .milliseconds(10)) }
        task.cancel()
        await task.value
        let calls = mac.deckCalls
        try await Task.sleep(for: .milliseconds(80))
        XCTAssertEqual(mac.deckCalls, calls, "no look after it was cancelled")
    }
}

@MainActor
final class ChannelStoreTests: XCTestCase {
    /// A look that brings the same receipts and the same pause tells nobody: a view that reads them is not drawn again.
    func testALookThatChangesNothingDoesNotTellTheView() async {
        let mac = FakeMac()
        let r = BusReceipt(seq: 1, targets: [.init(id: "a", label: "a", state: "read")])
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1)], last: 1, receipts: [r])),
                     .success(MessagesPage(channel: "c", messages: [], last: 1, receipts: [r])),
                     .success(MessagesPage(channel: "c", messages: [], last: 1, paused: true, receipts: [r]))]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh()
        var heard = 0
        func watch() { withObservationTracking { _ = s.receipts; _ = s.paused } onChange: { heard += 1 } }
        watch()
        await s.refresh()
        XCTAssertEqual(heard, 0, "the same receipts and pause were told again")
        await s.refresh()
        XCTAssertEqual(heard, 1, "a pause is news")
    }

    func testEachLookAsksForWhatCameAfterTheLastOne() async {
        let mac = FakeMac()
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1), msg(2)], last: 2)),
                     .success(MessagesPage(channel: "c", messages: [msg(3)], last: 3)),
                     .success(MessagesPage(channel: "c", messages: [], last: 3))]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh(); await s.refresh(); await s.refresh()
        XCTAssertEqual(mac.sinces, [0, 2, 3])
        XCTAssertEqual(s.messages.map(\.seq), [1, 2, 3])
        XCTAssertEqual(s.last, 3)
        XCTAssertTrue(s.loaded)
        XCTAssertEqual(s.link, .ok)
    }

    func testANumberAlreadyHeldIsNeverAddedTwiceAndOrderHolds() async {
        let mac = FakeMac()
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(2), msg(1)], last: 2)),
                     .success(MessagesPage(channel: "c", messages: [msg(2, "again"), msg(3)], last: 3))]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh(); await s.refresh()
        XCTAssertEqual(s.messages.map(\.seq), [1, 2, 3])
        XCTAssertEqual(s.messages[1].text, "m", "the first copy stays")
    }

    func testAFullPageIsFollowedAtOnce() async {
        let mac = FakeMac()
        let first = (1...ChannelStore.pageLimit).map { msg($0) }
        mac.pages = [.success(MessagesPage(channel: "c", messages: first, last: 205)),
                     .success(MessagesPage(channel: "c", messages: (201...205).map { msg($0) }, last: 205))]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh()
        XCTAssertEqual(mac.sinces, [0, 200], "a page of 200 may not be all: it asks again from 200 in the same look")
        XCTAssertEqual(s.messages.count, 205)
        XCTAssertEqual(s.last, 205)
    }

    func testPausedAndReceiptsFollowTheLatestAnswer() async {
        let mac = FakeMac()
        let receipt = BusReceipt(seq: 2, targets: [.init(id: "a1", label: "kim", state: "delivered")])
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1)], last: 1, paused: true, receipts: [receipt])),
                     .success(MessagesPage(channel: "c", messages: [], last: 1, paused: false, receipts: []))]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh()
        XCTAssertTrue(s.paused)
        XCTAssertEqual(s.receipts, [receipt])
        await s.refresh()
        XCTAssertFalse(s.paused)
        XCTAssertEqual(s.receipts, [])
    }

    func testAFailedLookKeepsTheMessagesAndSetsTheLink() async {
        let mac = FakeMac()
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1)], last: 1)), .failure(ConnectionError.offline)]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh(); await s.refresh()
        XCTAssertEqual(s.link, .offline)
        XCTAssertEqual(s.messages.map(\.seq), [1])
    }

    func testPostSendsTheReplyAndPicturesThenLooks() async throws {
        let mac = FakeMac()
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1, "answer", from: "you")], last: 1))]
        let s = ChannelStore(id: "c", api: mac)
        let r = try await s.post("answer", images: [PostImage(mime: "image/png", data: Data([1]))], replyTo: 7)
        XCTAssertEqual(r.seq, 99)
        XCTAssertEqual(mac.posts.count, 1)
        XCTAssertEqual(mac.posts[0].replyTo, 7)
        XCTAssertEqual(mac.posts[0].images, 1)
        XCTAssertEqual(s.messages.map(\.text), ["answer"], "it shows at once: the post is followed by a look")
        XCTAssertFalse(s.sending)
    }

    func testOnePostAtATime() async throws {
        let mac = FakeMac()
        mac.holdPost = true
        let s = ChannelStore(id: "c", api: mac)
        let first = Task { try await s.post("one") }
        while mac.postGate == nil { await Task.yield() }
        XCTAssertTrue(s.sending)
        do { try await s.post("two"); XCTFail("a second post should not go") } catch {
            XCTAssertEqual(error as? ChannelStoreError, .busy)
        }
        mac.holdPost = false
        mac.postGate?.resume()
        _ = try await first.value
        XCTAssertEqual(mac.posts.map(\.text), ["one"])
        XCTAssertFalse(s.sending)
    }

    func testMarkReadTellsTheMacOnlyWhenThereIsNews() async {
        let mac = FakeMac()
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1), msg(2)], last: 2)),
                     .success(MessagesPage(channel: "c", messages: [msg(3)], last: 3))]
        let s = ChannelStore(id: "c", api: mac)
        await s.markRead()
        XCTAssertEqual(mac.reads, [], "nothing held, nothing to tell")
        await s.refresh()
        await s.markRead()
        await s.markRead()
        XCTAssertEqual(mac.reads, [2])
        await s.refresh()
        await s.markRead()
        XCTAssertEqual(mac.reads, [2, 3])
    }

    func testAMessageByNumberForAReply() async {
        let mac = FakeMac()
        mac.pages = [.success(MessagesPage(channel: "c", messages: [msg(1, "first"), msg(2, "second")], last: 2))]
        let s = ChannelStore(id: "c", api: mac)
        await s.refresh()
        XCTAssertEqual(s.message(2)?.text, "second")
        XCTAssertNil(s.message(0))
        XCTAssertNil(s.message(9))
    }
}

@MainActor
final class AgentStoreTests: XCTestCase {
    func testItMirrorsItsSessionAndSendsThroughIt() async {
        let mac = FakeMac()
        let s = AgentStore(id: "a1", api: mac)
        XCTAssertFalse(s.loaded)
        await s.look()
        XCTAssertTrue(s.loaded)
        XCTAssertNil(s.problem)
        let sent = await s.send("hello")
        XCTAssertTrue(sent)
        XCTAssertFalse(s.sending)
        s.dismissNotice()
        XCTAssertNil(s.notice)
    }
}
