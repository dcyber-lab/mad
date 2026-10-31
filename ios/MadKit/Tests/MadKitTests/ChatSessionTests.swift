import XCTest
@testable import MadKit

/// A conversation's Mac, scripted. `chatReplies` are answered in order; a
/// reply may wait on a continuation so a test can interleave.
@MainActor
private final class FakeAPI: ChatAPI {
    var calls: [String] = []
    var chatCursors: [(String, Int64)] = []
    var chatHandler: (Int) async throws -> ChatPage = { _ in ChatPage(file: "f", offset: 0, entries: []) }
    var modeError: Error?
    var sendError: Error?
    var sendGate: CheckedContinuation<Void, Never>?
    var holdSend = false

    nonisolated func chat(agent: String, file: String, from: Int64) async throws -> ChatPage {
        let n = await MainActor.run { () -> Int in
            self.calls.append("chat")
            self.chatCursors.append((file, from))
            return self.chatCursors.count - 1
        }
        return try await chatHandler(n)
    }
    nonisolated func live(agent: String, since: Int64) async throws -> LiveMessage? { nil }
    nonisolated func send(agent: String, text: String) async throws {
        await MainActor.run { self.calls.append("send:\(text)") }
        if await MainActor.run(body: { self.holdSend }) {
            await withCheckedContinuation { c in Task { @MainActor in self.sendGate = c } }
        }
        if let e = await MainActor.run(body: { self.sendError }) { throw e }
    }
    nonisolated func setMode(agent: String, mode: String) async throws {
        await MainActor.run { self.calls.append("mode:\(mode)") }
        if let e = await MainActor.run(body: { self.modeError }) { throw e }
    }
    nonisolated func interrupt(agent: String) async throws { await MainActor.run { self.calls.append("interrupt") } }
}

private func entry(_ text: String) -> ChatEntry { ChatEntry(kind: "text", text: text) }

@MainActor
final class ChatSessionTests: XCTestCase {
    func testOverlappingLooksAreSerialisedAndTheSecondStartsWhereTheFirstGotTo() async {
        let api = FakeAPI()
        var release: CheckedContinuation<Void, Never>?
        api.chatHandler = { n in
            if n == 0 { await withCheckedContinuation { c in Task { @MainActor in release = c } } }
            return n == 0 ? ChatPage(file: "f", offset: 10, entries: [entry("one")], reset: true)
                          : ChatPage(file: "f", offset: 20, entries: [entry("two")])
        }
        let s = ChatSession(agentID: "a", api: api)
        let first = Task { await s.look() }
        while release == nil { await Task.yield() }
        await s.look() // asked while the first is in flight: it only notes it wants another look
        XCTAssertEqual(api.chatCursors.count, 1, "no second request while one is in flight")
        release?.resume()
        await first.value
        XCTAssertEqual(api.chatCursors.count, 2, "the look asked for ran after the first")
        XCTAssertEqual(api.chatCursors[1].0, "f")
        XCTAssertEqual(api.chatCursors[1].1, 10, "from where the first look got to")
        XCTAssertEqual(s.transcript.entries.compactMap(\.text), ["one", "two"], "each page applied once")
        XCTAssertEqual(s.transcript.offset, 20)
    }

    func testAResultFromAStaleCursorIsDropped() async {
        let api = FakeAPI()
        let s = ChatSession(agentID: "a", api: api)
        // The look starts from ("", 0); while it is out the cursor moves.
        var release: CheckedContinuation<Void, Never>?
        api.chatHandler = { _ in
            await withCheckedContinuation { c in Task { @MainActor in release = c } }
            return ChatPage(file: "f", offset: 5, entries: [entry("stale")], reset: true)
        }
        let look = Task { await s.look() }
        while release == nil { await Task.yield() }
        s.applyForTest(ChatPage(file: "g", offset: 9, entries: [entry("fresh")], reset: true))
        release?.resume()
        await look.value
        XCTAssertEqual(s.transcript.entries.compactMap(\.text), ["fresh"])
        XCTAssertEqual(s.transcript.file, "g")
    }

    func testALookErrorShowsAsTheChatsOwnProblemAndClearsOnTheNextGoodLook() async {
        let api = FakeAPI()
        api.chatHandler = { n in
            if n == 0 { throw ConnectionError.unexpected("no such agent") }
            return ChatPage(file: "f", offset: 1, entries: [entry("hi")], reset: true)
        }
        let s = ChatSession(agentID: "a", api: api)
        await s.look()
        XCTAssertEqual(s.problem, "no such agent")
        XCTAssertFalse(s.loaded)
        await s.look() // Retry
        XCTAssertNil(s.problem)
        XCTAssertTrue(s.loaded)
    }

    func testEverySendAsksForWindowModeFirst() async {
        let api = FakeAPI()
        let s = ChatSession(agentID: "a", api: api)
        let ok = await s.send("  hello  ")
        XCTAssertTrue(ok)
        XCTAssertEqual(Array(api.calls.prefix(2)), ["mode:window", "send:hello"], "mode is not trusted from the snapshot")
        _ = await s.send("again")
        XCTAssertEqual(api.calls.filter { $0 == "mode:window" }.count, 2)
    }

    func testARefusedModeSwitchShowsTheServersMessageAndSendsNothing() async {
        let api = FakeAPI()
        api.modeError = ConnectionError.conflict("it is working: wait for it to finish")
        let s = ChatSession(agentID: "a", api: api)
        let ok = await s.send("hello")
        XCTAssertFalse(ok)
        XCTAssertEqual(s.notice, "it is working: wait for it to finish")
        XCTAssertFalse(api.calls.contains { $0.hasPrefix("send") })
        XCTAssertFalse(s.sending)
    }

    func testASecondSendWhileOneIsInFlightDoesNothing() async {
        let api = FakeAPI()
        api.holdSend = true
        let s = ChatSession(agentID: "a", api: api)
        let first = Task { await s.send("one") }
        while api.sendGate == nil { await Task.yield() }
        XCTAssertTrue(s.sending)
        let second = await s.send("two")
        XCTAssertFalse(second)
        api.holdSend = false
        api.sendGate?.resume()
        let firstOK = await first.value
        XCTAssertTrue(firstOK)
        XCTAssertEqual(api.calls.filter { $0.hasPrefix("send") }, ["send:one"])
        XCTAssertFalse(s.sending)
    }

    func testAFailedSendKeepsTheDraftAndSaysWhy() async {
        let api = FakeAPI()
        api.sendError = ConnectionError.conflict("its host is not running")
        let s = ChatSession(agentID: "a", api: api)
        let ok = await s.send("keep me")
        XCTAssertFalse(ok)
        XCTAssertEqual(s.notice, "its host is not running")
    }

    func testTheDraftIsClearedOnlyIfItIsStillWhatWasSent() {
        XCTAssertEqual(ChatSession.draftAfterSend(sent: "hello", current: "hello"), "")
        XCTAssertEqual(ChatSession.draftAfterSend(sent: "hello", current: "hello, and then"), "hello, and then",
                       "typing the next message while sending keeps it")
        XCTAssertEqual(ChatSession.draftAfterSend(sent: "hello", current: ""), "")
    }

    func testTheSampleReadsAndSendsNothing() async {
        let api = FakeAPI()
        let page = ChatPage(file: "s", offset: 1, entries: [entry("sample")], reset: true)
        let s = ChatSession(agentID: "a", api: api, sample: (page, nil))
        XCTAssertEqual(s.transcript.entries.count, 1)
        let sent = await s.send("x")
        XCTAssertFalse(sent)
        await s.interrupt()
        XCTAssertEqual(api.calls, [])
        XCTAssertNotNil(s.notice)
    }
}
