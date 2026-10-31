import XCTest
@testable import MadKit

private let token = "0123456789abcdef0123456789abcdef"

private func client() throws -> MadClient {
    guard case .token(let m) = try Pairing.parse("https://box.ts.net:7443/#token=\(token)") else {
        throw PairingError.noToken
    }
    return MadClient(mac: m)
}

private func http(_ code: Int) -> URLResponse {
    HTTPURLResponse(url: URL(string: "https://x")!, statusCode: code, httpVersion: nil, headerFields: nil)!
}

private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
    try JSONDecoder().decode(type, from: Data(json.utf8))
}

final class ChatDecodingTests: XCTestCase {
    func testEntriesTolerateUnknownKindsAndFields() throws {
        let page = try decode(ChatPage.self, """
        {"file": "ab12", "offset": 1234, "entries": [
          {"kind": "user", "text": "run the tests", "at": "2026-10-31T13:00:00.5Z"},
          {"kind": "text", "text": "On it, with **bold**.", "msg": "m1", "part": "p1"},
          {"kind": "tool", "tool": "Bash", "label": "go test ./...", "input": "go test ./...", "id": "c1",
           "args": {"command": "go test ./..."}, "msg": "m1"},
          {"kind": "result", "id": "c1", "text": "ok", "error": false},
          {"kind": "result", "id": "c2", "text": "boom", "error": true},
          {"kind": "other", "msg": "m1"},
          {"kind": "from-the-future", "novel": [1, 2]},
          {"kind": "user", "text": "look", "images": [{"mime": "image/png", "url": "/api/image?id=x"}]}
        ], "earlier": true}
        """)
        XCTAssertEqual(page.file, "ab12")
        XCTAssertEqual(page.offset, 1234)
        XCTAssertTrue(page.earlier)
        XCTAssertFalse(page.reset)
        XCTAssertEqual(page.entries.count, 8)
        XCTAssertNotNil(page.entries[0].at)
        XCTAssertEqual(page.entries[2].tool, "Bash")
        XCTAssertEqual(page.entries[7].images.count, 1)
        XCTAssertEqual(page.entries[6].kind, "from-the-future")
    }

    func testEmptyAnswer() throws {
        let page = try decode(ChatPage.self, #"{"file": "", "offset": 0, "entries": []}"#)
        XCTAssertEqual(page.entries, [])
        XCTAssertEqual(try decode(ChatPage.self, "{}").entries, [])
    }

    func testLiveMessage() throws {
        let m = try decode(LiveMessage.self, """
        {"id": "m2", "blocks": [{"kind": "thinking", "text": "hm"}, {"kind": "text", "text": "Hello"},
        {"kind": "tool", "tool": "Read", "id": "c9", "done": true}], "session": "s", "done": false, "version": 7}
        """)
        XCTAssertEqual(m.id, "m2")
        XCTAssertEqual(m.version, 7)
        XCTAssertEqual(m.blocks.map(\.kind), ["thinking", "text", "tool"])
        XCTAssertTrue(m.blocks[2].done)
    }

    func testAgentFieldsAndWording() throws {
        let s = try decode(DeckSnapshot.self, """
        {"projects": [{"name": "p", "agents": [
          {"id": "a1", "kind": "claude", "name": "ios", "status": "running", "shown": "running", "mode": "window", "headless": true},
          {"id": "a2", "kind": "codex", "title": "fix tests", "status": "idle", "shown": "idle", "team": "pay"},
          {"id": "a3", "kind": "pi", "status": "waiting", "shown": "waiting"},
          {"id": "a4", "status": "off", "shown": "asleep"}]}],
         "teams": [{"name": "pay", "project": "p", "members": ["a2"]}],
         "states": [{"state": "waiting", "word": "Needs you", "tone": "needs"},
                    {"state": "running", "word": "Working", "tone": "working"},
                    {"state": "idle", "word": "Idle", "tone": "quiet"},
                    {"state": "asleep", "word": "Asleep", "tone": "quiet"}]}
        """)
        let a = s.projects[0].agents
        XCTAssertEqual(a.map(\.label), ["ios", "fix tests", "pi a3", "a4"], "name, else title, else kind and the id's first 8, else the id")
        XCTAssertTrue(a[0].inWindowMode)
        XCTAssertTrue(a[0].headless)
        XCTAssertEqual(a[1].team, "pay")
        XCTAssertEqual(s.teams.map(\.members), [["a2"]])
        XCTAssertEqual(s.word(for: "running"), "Working")
        XCTAssertEqual(s.word(for: "waiting"), "Needs you")
        XCTAssertEqual(s.word(for: "mystery"), "Mystery", "an unknown state is shown as it is")
        let order = a.sorted { s.rank(of: $0) < s.rank(of: $1) }.map(\.id)
        XCTAssertEqual(order, ["a1", "a3", "a2", "a4"], "working first, then needs you, then the deck's order")
    }
}

final class AgentLabelTests: XCTestCase {
    func testUntitledAgentsAreToldApartByTheirIdLikeThePage() {
        let a = DeckSnapshot.Agent(id: "6625f15d-ed6b-4dc7-94ff-e8eb501a6c99", kind: "claude", status: "asleep")
        let b = DeckSnapshot.Agent(id: "39a28bf3-f828-4ac9-9ceb-72341a6e8e38", kind: "claude", status: "stopped")
        XCTAssertEqual(a.label, "claude 6625f15d")
        XCTAssertEqual(b.label, "claude 39a28bf3")
        XCTAssertFalse(a.isNamed)
    }

    func testANameOrTitleWinsOverTheKindAndId() {
        XCTAssertEqual(DeckSnapshot.Agent(id: "abcdef0123456789", kind: "codex", name: "ada", title: "t", status: "idle").label, "ada")
        XCTAssertEqual(DeckSnapshot.Agent(id: "abcdef0123456789", kind: "codex", title: "fix tests", status: "idle").label, "fix tests")
        XCTAssertTrue(DeckSnapshot.Agent(id: "abcdef0123456789", kind: "codex", title: "fix tests", status: "idle").isNamed)
        XCTAssertEqual(DeckSnapshot.Agent(id: "abc", kind: "", status: "idle").label, "abc", "no kind: the id")
    }
}

final class ChatTranscriptTests: XCTestCase {
    private func e(_ kind: String, text: String? = nil, id: String? = nil, msg: String? = nil, error: Bool = false) -> ChatEntry {
        ChatEntry(kind: kind, text: text, id: id, error: error, msg: msg)
    }

    func testFirstLookThenAppendUsingFileAndOffset() {
        var t = ChatTranscript()
        XCTAssertEqual(t.file, "")
        XCTAssertEqual(t.offset, 0)
        t.apply(ChatPage(file: "f1", offset: 100, entries: [e("user", text: "hi")], reset: true, earlier: true))
        XCTAssertEqual(t.entries.count, 1)
        XCTAssertTrue(t.earlier)
        t.apply(ChatPage(file: "f1", offset: 180, entries: [e("text", text: "hello")]))
        XCTAssertEqual(t.entries.count, 2, "later looks add")
        XCTAssertEqual(t.file, "f1")
        XCTAssertEqual(t.offset, 180)
        t.apply(ChatPage(file: "f1", offset: 180, entries: []))
        XCTAssertEqual(t.entries.count, 2)
    }

    func testResetReplacesEverything() {
        var t = ChatTranscript()
        t.apply(ChatPage(file: "f1", offset: 100, entries: [e("user", text: "old"), e("text", text: "old")], reset: true, earlier: true))
        t.apply(ChatPage(file: "f2", offset: 40, entries: [e("user", text: "new")], reset: true))
        XCTAssertEqual(t.entries.map(\.text), ["new"])
        XCTAssertEqual(t.file, "f2")
        XCTAssertEqual(t.offset, 40)
        XCTAssertFalse(t.earlier, "the new start has nothing before it")
    }

    func testRowsFoldResultsAndSkipUnknownKinds() {
        var t = ChatTranscript()
        t.apply(ChatPage(file: "f", offset: 1, entries: [
            e("user", text: "go"),
            ChatEntry(kind: "tool", tool: "Bash", label: "ls", input: "ls", id: "c1"),
            e("result", text: "a b", id: "c1"),
            ChatEntry(kind: "tool", tool: "Bash", label: "boom", id: "c2"),
            e("result", text: "no", id: "c2", error: true),
            e("result", text: "orphan", id: "zzz"),
            e("command", text: "/model"),
            e("other", msg: "m1"),
            e("from-the-future"),
            ChatEntry(kind: "tool", tool: "Read", label: "x", id: "c3"),
            e("text", text: "done"),
        ], reset: true))
        let rows = t.rows
        XCTAssertEqual(rows.count, 5)
        guard case .user = rows[0], case .tool(let c1, let r1) = rows[1], case .tool(_, let r2) = rows[2],
              case .tool(_, let r3) = rows[3], case .text = rows[4] else { return XCTFail("rows: \(rows)") }
        XCTAssertEqual(c1.label, "ls")
        XCTAssertEqual(r1?.text, "a b")
        XCTAssertEqual(r2?.error, true)
        XCTAssertNil(r3, "a call with no result yet")
    }

    func testLiveShowsUntilTheTranscriptHasItsMessage() {
        var t = ChatTranscript()
        var live = LiveState()
        let now = Date(timeIntervalSince1970: 1_000)
        live.receive(LiveMessage(id: "m1", blocks: [LiveBlock(kind: "text", text: "Hel")], version: 1), at: now)
        XCTAssertEqual(live.visible(in: t, now: now)?.id, "m1")
        live.receive(LiveMessage(id: "m1", blocks: [LiveBlock(kind: "text", text: "Hello")], version: 2), at: now)
        XCTAssertEqual(live.version, 2)
        t.apply(ChatPage(file: "f", offset: 5, entries: [e("text", text: "Hello", msg: "m1")], reset: true))
        XCTAssertNil(live.visible(in: t, now: now), "the transcript's entry takes its place")
    }

    func testOlderVersionsAreIgnoredAndEndedMessagesFade() {
        var live = LiveState()
        let t = ChatTranscript()
        let now = Date(timeIntervalSince1970: 1_000)
        live.receive(LiveMessage(id: "m1", blocks: [LiveBlock(kind: "text", text: "b")], version: 5), at: now)
        live.receive(LiveMessage(id: "m0", blocks: [LiveBlock(kind: "text", text: "a")], version: 3), at: now)
        XCTAssertEqual(live.message?.id, "m1")
        live.receive(LiveMessage(id: "m1", blocks: [LiveBlock(kind: "text", text: "b")], done: true, version: 6), at: now)
        XCTAssertNotNil(live.visible(in: t, now: now.addingTimeInterval(4)))
        XCTAssertNil(live.visible(in: t, now: now.addingTimeInterval(6)), "ended a while ago: the transcript is the whole of it")
        XCTAssertNil(LiveState().visible(in: t))
    }
}

final class ChatClientTests: XCTestCase {
    func testChatRequestFirstLookAndNext() throws {
        let c = try client()
        let first = c.chatRequest(agent: "claude-1", file: "", from: 0)
        XCTAssertEqual(first.httpMethod ?? "GET", "GET")
        XCTAssertEqual(first.value(forHTTPHeaderField: "Authorization"), "Bearer \(token)")
        let q = URLComponents(url: first.url!, resolvingAgainstBaseURL: false)!
        XCTAssertEqual(q.path, "/api/chat")
        XCTAssertEqual(q.queryItems?.first { $0.name == "id" }?.value, "claude-1")
        XCTAssertEqual(q.queryItems?.first { $0.name == "file" }?.value, "")
        XCTAssertEqual(q.queryItems?.first { $0.name == "from" }?.value, "0")
        XCTAssertNil(q.queryItems?.first { $0.name == "token" }, "the token is in the header only")
        let next = URLComponents(url: c.chatRequest(agent: "claude-1", file: "ab12", from: 1234).url!, resolvingAgainstBaseURL: false)!
        XCTAssertEqual(next.queryItems?.first { $0.name == "file" }?.value, "ab12")
        XCTAssertEqual(next.queryItems?.first { $0.name == "from" }?.value, "1234")
    }

    func testLiveRequestAndAnswers() throws {
        let c = try client()
        let r = c.liveRequest(agent: "a", since: 7)
        XCTAssertEqual(URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "since" }?.value, "7")
        XCTAssertEqual(URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.path, "/api/live")
        XCTAssertGreaterThan(r.timeoutInterval, 20, "the server holds it open")
        XCTAssertNil(try MadClient.decodeLive(Data(), http(204)), "204: nothing streams")
        let m = try MadClient.decodeLive(Data(#"{"id":"m","blocks":[],"version":3}"#.utf8), http(200))
        XCTAssertEqual(m?.version, 3)
        XCTAssertThrowsError(try MadClient.decodeLive(Data("nope".utf8), http(200)))
        XCTAssertThrowsError(try MadClient.decodeLive(Data(), http(403))) { XCTAssertEqual($0 as? ConnectionError, .refused) }
    }

    func testSendBody() throws {
        let r = try client().sendRequest(agent: "claude-1", text: "hello \"there\"")
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.path, "/api/send")
        XCTAssertEqual(URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "id" }?.value, "claude-1")
        XCTAssertEqual(r.value(forHTTPHeaderField: "Content-Type"), "application/json")
        XCTAssertEqual(String(data: r.httpBody ?? Data(), encoding: .utf8), #"{"text":"hello \"there\""}"#)
    }

    func testModeAndInterruptRequests() throws {
        let c = try client()
        let m = c.modeRequest(agent: "a1")
        XCTAssertEqual(m.httpMethod, "POST")
        let mq = URLComponents(url: m.url!, resolvingAgainstBaseURL: false)!
        XCTAssertEqual(mq.path, "/api/mode")
        XCTAssertEqual(mq.queryItems?.first { $0.name == "mode" }?.value, "window")
        XCTAssertEqual(mq.queryItems?.first { $0.name == "id" }?.value, "a1")
        let i = c.interruptRequest(agent: "a1")
        XCTAssertEqual(i.httpMethod, "POST")
        XCTAssertNil(i.httpBody, "interrupt takes the id in the query, no body")
        XCTAssertEqual(URLComponents(url: i.url!, resolvingAgainstBaseURL: false)?.path, "/api/interrupt")
        XCTAssertEqual(URLComponents(url: i.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "id" }?.value, "a1")
    }

    func testErrorMapping() {
        XCTAssertNoThrow(try MadClient.check(Data(), http(204), [204, 200]))
        XCTAssertThrowsError(try MadClient.check(Data("it is working".utf8), http(409), [204])) {
            XCTAssertEqual($0 as? ConnectionError, .conflict("it is working"))
        }
        XCTAssertThrowsError(try MadClient.check(Data(), http(403), [204])) { XCTAssertEqual($0 as? ConnectionError, .refused) }
        XCTAssertThrowsError(try MadClient.check(Data("no such agent\n".utf8), http(404), [204])) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("no such agent"))
        }
        XCTAssertThrowsError(try MadClient.check(Data("no such mode for shell".utf8), http(400), [204])) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("no such mode for shell"))
        }
    }

    func testNetworkErrorsAreOffline() async throws {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [FailingProtocol.self]
        let c = MadClient(mac: try { guard case .token(let m) = try Pairing.parse("https://box.ts.net/#token=\(token)") else { throw PairingError.noToken }; return m }(),
                          session: URLSession(configuration: cfg))
        do { _ = try await c.chat(agent: "a"); XCTFail() } catch { XCTAssertEqual(error as? ConnectionError, .offline) }
        do { _ = try await c.live(agent: "a", since: 0); XCTFail() } catch { XCTAssertEqual(error as? ConnectionError, .offline) }
        do { try await c.send(agent: "a", text: "x"); XCTFail() } catch { XCTAssertEqual(error as? ConnectionError, .offline) }
    }
}
