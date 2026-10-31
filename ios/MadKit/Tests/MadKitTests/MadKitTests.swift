import XCTest
@testable import MadKit

private let code = "fedcba9876543210fedcba9876543210"

/// The Mac of a #token= link.
private func mac(_ text: String, allowLocalHTTP: Bool = false) throws -> Mac {
    guard case .token(let m) = try Pairing.parse(text, allowLocalHTTP: allowLocalHTTP) else {
        throw PairingError.noToken
    }
    return m
}

private let tok = "0123456789abcdef0123456789abcdef"

final class PairingTests: XCTestCase {
    func testTailscale() throws {
        let m = try mac("https://box.tail1.ts.net:7443/#token=\(tok)")
        XCTAssertEqual(m.name, "box")
        XCTAssertEqual(m.baseURL.absoluteString, "https://box.tail1.ts.net:7443")
        XCTAssertEqual(m.token, tok)
        XCTAssertEqual(m.deckURL.absoluteString, "https://box.tail1.ts.net:7443/#token=\(tok)")
    }

    func testCloudflareAndPastedWhitespace() throws {
        let m = try mac("  https://xyz.trycloudflare.com/#token=\(tok.uppercased())\n")
        XCTAssertEqual(m.name, "xyz")
        XCTAssertEqual(m.baseURL.absoluteString, "https://xyz.trycloudflare.com")
        XCTAssertEqual(m.token, tok)
    }

    func testRejects() {
        let cases: [(String, PairingError)] = [
            ("", .empty),
            ("   ", .empty),
            ("hello world", .notURL),
            ("http://box.ts.net/#token=\(tok)", .notHTTPS),
            ("https:///#token=\(tok)", .noHost),
            ("https://box.ts.net/", .noToken),
            ("https://box.ts.net/?token=\(tok)", .noToken),
            ("https://box.ts.net/#token=abc", .badToken),
            ("https://box.ts.net/#token=\(String(repeating: "g", count: 32))", .badToken),
            ("https://box.ts.net/#token=\(String(repeating: "Ｆ", count: 32))", .badToken),
            ("https://box.ts.net/#token=\(String(repeating: "０", count: 32))", .badToken),
        ]
        for (text, want) in cases {
            XCTAssertThrowsError(try Pairing.parse(text), text) { XCTAssertEqual($0 as? PairingError, want, text) }
        }
    }
}

final class ClientTests: XCTestCase {
    private func response(_ code: Int) -> URLResponse {
        HTTPURLResponse(url: URL(string: "https://x")!, statusCode: code, httpVersion: nil, headerFields: nil)!
    }

    func testRequestCarriesTokenInHeaderOnly() throws {
        let m = try mac("https://box.ts.net:7443/#token=\(tok)")
        let r = MadClient(mac: m).request("/api/deck")
        XCTAssertEqual(r.url?.absoluteString, "https://box.ts.net:7443/api/deck")
        XCTAssertFalse(r.url?.absoluteString.contains(tok) ?? true)
        XCTAssertEqual(r.value(forHTTPHeaderField: "Authorization"), "Bearer \(tok)")
    }

    func testForbiddenIsRefused() {
        XCTAssertThrowsError(try MadClient.decode(Data("forbidden".utf8), response(403))) {
            XCTAssertEqual($0 as? ConnectionError, .refused)
        }
    }

    func testOtherStatusAndGarbage() {
        XCTAssertThrowsError(try MadClient.decode(Data(), response(500))) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("The Mac answered HTTP 500."))
        }
        XCTAssertThrowsError(try MadClient.decode(Data("<html>".utf8), response(200))) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("This does not look like mad's answer."))
        }
    }

    func testOfflineMapping() async throws {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [FailingProtocol.self]
        let m = try mac("https://box.ts.net/#token=\(tok)")
        let client = MadClient(mac: m, session: URLSession(configuration: cfg))
        do {
            _ = try await client.deck()
            XCTFail("expected offline")
        } catch {
            XCTAssertEqual(error as? ConnectionError, .offline)
        }
    }
}

final class FailingProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() { client?.urlProtocol(self, didFailWithError: URLError(.notConnectedToInternet)) }
    override func stopLoading() {}
}

final class SnapshotTests: XCTestCase {
    let sample = """
    {
      "projects": [
        {"name": "mad", "path": "/x/mad", "branch": "main", "changed": 2, "ahead": 0,
         "future": {"a": 1},
         "agents": [
           {"id": "claude-3fe9", "kind": "claude", "name": "ios", "status": "working", "shown": "working", "cost": 1.5},
           {"id": "codex-1", "kind": "codex", "title": "fix tests", "status": "idle", "shown": "done", "done": true}
         ], "rollup": {}},
        {"name": "empty", "path": "/x/e"}
      ],
      "bus": [], "board": [], "spend": {}, "timers": [],
      "inbox": {"items": [
        {"key": "perm:1", "kind": "permission", "title": "kim", "context": "Bash", "text": "go test ./...", "blocks": true, "acts": []},
        {"key": "done:2", "kind": "finished", "title": "gus", "acts": []}
      ], "needs": 1},
      "working": ["claude-3fe9"], "teams": [], "states": [], "at": "2026-10-31T13:00:00Z"
    }
    """

    func testDecodesSubsetAndIgnoresUnknown() throws {
        let s = try JSONDecoder().decode(DeckSnapshot.self, from: Data(sample.utf8))
        XCTAssertEqual(s.projects.map(\.name), ["mad", "empty"])
        XCTAssertEqual(s.agentCount, 2)
        XCTAssertEqual(s.projects[0].agents[0].label, "ios")
        XCTAssertEqual(s.projects[0].agents[1].label, "fix tests")
        XCTAssertEqual(s.projects[0].agents[1].state, "done")
        XCTAssertEqual(s.inbox.count, 2)
        XCTAssertEqual(s.needs, 1)
        XCTAssertEqual(s.waiting.map(\.key), ["perm:1"])
        XCTAssertTrue(s.waiting[0].blocks)
    }

    func testEmptyObject() throws {
        let s = try JSONDecoder().decode(DeckSnapshot.self, from: Data("{}".utf8))
        XCTAssertEqual(s.agentCount, 0)
        XCTAssertEqual(s.needs, 0)
    }

    func testNeedsLargerThanItemsIsClamped() {
        let s = DeckSnapshot(projects: [], inbox: [.init(key: "a", title: "t")], needs: 5)
        XCTAssertEqual(s.waiting.count, 1)
    }
}

final class InboxTests: XCTestCase {
    let sample = """
    {"inbox": {"needs": 2, "items": [
      {"key": "perm:1", "kind": "permission", "agent": "claude-1", "title": "kim", "context": "Bash",
       "text": "go test ./...", "mono": true, "where": "/x", "since": "2026-10-31T13:00:00.123456+08:00",
       "blocks": true, "part": 0.5, "tasks": [{"id": "t1", "title": "x"}],
       "acts": [{"verb": "allow", "label": "Allow", "primary": true, "row": true},
                {"verb": "always", "label": "Always"},
                {"verb": "deny", "label": "Deny", "input": true}],
       "open": {"agent": "claude-1"}},
      {"key": "q:2", "kind": "question", "title": "gus", "body": "pick one", "full": true,
       "since": "2026-10-31T13:01:00Z",
       "acts": [{"verb": "answer", "label": "Yes", "option": "yes"}, {"verb": "open", "label": "Open"}],
       "open": {"channel": "plan"}},
      {"key": "n:3", "kind": "from-the-future", "title": "?", "acts": [], "open": {"page": "board"}, "extra": 1}
    ]},
    "projects": [{"name": "p", "agents": [{"id": "claude-1", "name": "ios", "status": "working"}]}]}
    """

    func testDecodesEveryKindTolerantly() throws {
        let s = try JSONDecoder().decode(DeckSnapshot.self, from: Data(sample.utf8))
        XCTAssertEqual(s.inbox.map(\.kind), ["permission", "question", "from-the-future"])
        XCTAssertEqual(s.waiting.map(\.key), ["perm:1", "q:2"])
        XCTAssertEqual(s.finished.map(\.key), ["n:3"])
        let p = s.inbox[0]
        XCTAssertTrue(p.mono)
        XCTAssertTrue(p.blocks)
        XCTAssertNotNil(p.since)
        XCTAssertEqual(p.acts.map(\.verb), ["allow", "always", "deny"])
        XCTAssertTrue(p.acts[0].primary)
        XCTAssertTrue(p.acts[2].input)
        XCTAssertEqual(p.open.agent, "claude-1")
        XCTAssertEqual(s.agent(id: p.agent)?.label, "ios")
        XCTAssertNil(s.agent(id: "nobody"))
        XCTAssertTrue(s.inbox[1].full)
        XCTAssertNotNil(s.inbox[1].since)
        XCTAssertEqual(s.inbox[1].acts[0].option, "yes")
        XCTAssertTrue(s.inbox[1].acts[1].isOpen)
        XCTAssertEqual(s.inbox[2].open.page, "board")
    }

    func testActRequest() throws {
        let m = try mac("https://box.ts.net/#token=\(tok)")
        let r = MadClient(mac: m).actRequest(key: "perm:1", verb: "deny", option: nil, text: "not now")
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.absoluteString, "https://box.ts.net/api/inbox")
        XCTAssertEqual(r.value(forHTTPHeaderField: "Authorization"), "Bearer \(tok)")
        XCTAssertEqual(r.value(forHTTPHeaderField: "Content-Type"), "application/json")
        XCTAssertEqual(String(data: r.httpBody ?? Data(), encoding: .utf8),
                       #"{"key":"perm:1","text":"not now","verb":"deny"}"#)
    }

    private func response(_ code: Int) -> URLResponse {
        HTTPURLResponse(url: URL(string: "https://x")!, statusCode: code, httpVersion: nil, headerFields: nil)!
    }

    func testActStatusMapping() {
        XCTAssertNoThrow(try MadClient.checkAct(Data(), response(204)))
        XCTAssertThrowsError(try MadClient.checkAct(Data("Already handled\n".utf8), response(409))) {
            XCTAssertEqual($0 as? ConnectionError, .conflict("Already handled"))
        }
        XCTAssertThrowsError(try MadClient.checkAct(Data(), response(403))) {
            XCTAssertEqual($0 as? ConnectionError, .refused)
        }
        XCTAssertThrowsError(try MadClient.checkAct(Data("bad verb".utf8), response(400))) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("bad verb"))
        }
    }
}

final class LocalPairingTests: XCTestCase {
    func testLocalHTTPOnlyWhenAllowed() throws {
        let url = "http://127.0.0.1:8123/#token=\(tok)"
        XCTAssertThrowsError(try Pairing.parse(url)) { XCTAssertEqual($0 as? PairingError, .notHTTPS) }
        let m = try mac(url, allowLocalHTTP: true)
        XCTAssertEqual(m.baseURL.absoluteString, "http://127.0.0.1:8123")
        XCTAssertEqual(m.name, "127.0.0.1")
        XCTAssertEqual(try mac("http://localhost:1/#token=\(tok)", allowLocalHTTP: true).name, "localhost")
    }

    func testAllowedFlagStillRejectsRemoteHTTP() {
        XCTAssertThrowsError(try Pairing.parse("http://box.ts.net/#token=\(tok)", allowLocalHTTP: true)) {
            XCTAssertEqual($0 as? PairingError, .notHTTPS)
        }
        XCTAssertThrowsError(try Pairing.parse("http://127.0.0.1.evil.com/#token=\(tok)", allowLocalHTTP: true))
    }
}

final class PairLinkTests: XCTestCase {
    func testPairLink() throws {
        let link = try Pairing.parse("https://box.tail1.ts.net:7443/#pair=\(code)")
        XCTAssertEqual(link, .pair(base: URL(string: "https://box.tail1.ts.net:7443")!, code: code))
    }

    func testPairCodeIsLowercasedAndASCIIOnly() throws {
        XCTAssertEqual(try Pairing.parse("https://a.b/#pair=\(code.uppercased())"),
                       .pair(base: URL(string: "https://a.b")!, code: code))
        XCTAssertThrowsError(try Pairing.parse("https://a.b/#pair=\(String(repeating: "Ｆ", count: 32))")) {
            XCTAssertEqual($0 as? PairingError, .badCode)
        }
        XCTAssertThrowsError(try Pairing.parse("https://a.b/#pair=abc")) {
            XCTAssertEqual($0 as? PairingError, .badCode)
        }
        XCTAssertThrowsError(try Pairing.parse("https://a.b/#other=\(code)")) {
            XCTAssertEqual($0 as? PairingError, .noToken)
        }
    }

    func testTokenLinkStillParses() throws {
        guard case .token(let m) = try Pairing.parse("https://box.ts.net/#token=\(tok)") else {
            return XCTFail("want a token link")
        }
        XCTAssertEqual(m.token, tok)
        XCTAssertNil(m.deviceID)
        XCTAssertEqual(Pairing.defaultName(for: m.baseURL), "box")
    }

    func testLocalPairLinkNeedsTheFlag() throws {
        XCTAssertThrowsError(try Pairing.parse("http://127.0.0.1:9/#pair=\(code)"))
        XCTAssertEqual(try Pairing.parse("http://127.0.0.1:9/#pair=\(code)", allowLocalHTTP: true),
                       .pair(base: URL(string: "http://127.0.0.1:9")!, code: code))
    }
}

final class PairExchangeTests: XCTestCase {
    private let pending = PendingPairing(base: URL(string: "https://box.ts.net:7443")!,
                                         code: code, name: "Heng's iPhone", token: tok)

    private func response(_ status: Int) -> URLResponse {
        HTTPURLResponse(url: URL(string: "https://x")!, statusCode: status, httpVersion: nil, headerFields: nil)!
    }

    func testRequestBody() throws {
        let r = pending.request
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.absoluteString, "https://box.ts.net:7443/api/pair")
        XCTAssertNil(r.value(forHTTPHeaderField: "Authorization"))
        XCTAssertEqual(r.value(forHTTPHeaderField: "Content-Type"), "application/json")
        XCTAssertEqual(String(data: r.httpBody ?? Data(), encoding: .utf8),
                       #"{"code":"\#(code)","name":"Heng's iPhone","token":"\#(tok)"}"#)
    }

    func testStatusMapping() throws {
        XCTAssertEqual(try PendingPairing.check(Data(#"{"id":"d-1"}"#.utf8), response(200)), "d-1")
        for status in [400, 403] {
            XCTAssertThrowsError(try PendingPairing.check(Data(), response(status))) {
                XCTAssertEqual($0 as? ConnectionError, .pairingRefused)
            }
        }
        XCTAssertThrowsError(try PendingPairing.check(Data(), response(500))) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("The Mac answered HTTP 500."))
        }
        XCTAssertThrowsError(try PendingPairing.check(Data("{}".utf8), response(200))) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("This does not look like mad's answer."))
        }
    }

    func testNetworkErrorIsOffline() async {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [FailingProtocol.self]
        do {
            _ = try await pending.pair(session: URLSession(configuration: cfg))
            XCTFail("expected offline")
        } catch {
            XCTAssertEqual(error as? ConnectionError, .offline)
        }
    }

    /// After a lost answer the retry is the same request, byte for byte: the
    /// Mac then answers 200 with the same id instead of refusing a used code.
    func testRetrySendsTheSameCodeAndToken() {
        let again = pending
        XCTAssertEqual(again.request.httpBody, pending.request.httpBody)
        XCTAssertEqual(again.request.url, pending.request.url)
    }
}
