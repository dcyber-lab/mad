import XCTest
@testable import MadKit

private let tok = "0123456789abcdef0123456789abcdef"

final class PairingTests: XCTestCase {
    func testTailscale() throws {
        let m = try Pairing.parse("https://box.tail1.ts.net:7443/#token=\(tok)")
        XCTAssertEqual(m.name, "box")
        XCTAssertEqual(m.baseURL.absoluteString, "https://box.tail1.ts.net:7443")
        XCTAssertEqual(m.token, tok)
        XCTAssertEqual(m.deckURL.absoluteString, "https://box.tail1.ts.net:7443/#token=\(tok)")
    }

    func testCloudflareAndPastedWhitespace() throws {
        let m = try Pairing.parse("  https://xyz.trycloudflare.com/#token=\(tok.uppercased())\n")
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
        let mac = try Pairing.parse("https://box.ts.net:7443/#token=\(tok)")
        let r = MadClient(mac: mac).request("/api/deck")
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
        let mac = try Pairing.parse("https://box.ts.net/#token=\(tok)")
        let client = MadClient(mac: mac, session: URLSession(configuration: cfg))
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
