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
    func testReadsTheMachineNameAndIgnoresTheRest() throws {
        let json = """
        {"machine": " Heng’s MacBook Pro ", "projects": [{"name": "mad", "agents": []}],
         "inbox": {"items": [], "needs": 0}, "future": {"a": 1}}
        """
        let s = try JSONDecoder().decode(Deck.self, from: Data(json.utf8))
        XCTAssertEqual(s.machine, "Heng’s MacBook Pro")
    }

    func testNoOrBlankMachineIsNil() throws {
        XCTAssertNil(try JSONDecoder().decode(Deck.self, from: Data("{}".utf8)).machine)
        XCTAssertNil(try JSONDecoder().decode(Deck.self, from: Data(#"{"machine": "  "}"#.utf8)).machine)
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
        XCTAssertEqual(try PendingPairing.check(Data(#"{"id":"d-1"}"#.utf8), response(200)), PairResult(id: "d-1"))
        XCTAssertEqual(try PendingPairing.check(Data(#"{"id":"d-1","machine":"Heng's MacBook"}"#.utf8), response(200)),
                       PairResult(id: "d-1", machine: "Heng's MacBook"))
        XCTAssertEqual(try PendingPairing.check(Data(#"{"id":"d-1","machine":"  "}"#.utf8), response(200)).machine, nil)
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

final class MachineNameTests: XCTestCase {
    func testEmbeddedDeckURL() throws {
        let m = try mac("https://box.ts.net:7443/#token=\(tok)")
        XCTAssertEqual(m.embeddedDeckURL.absoluteString, "https://box.ts.net:7443/#token=\(tok)&embed=app")
        XCTAssertEqual(m.deckURL.absoluteString, "https://box.ts.net:7443/#token=\(tok)")
    }
}

final class WebBridgeTests: XCTestCase {
    func testSwitchMacIsRead() {
        XCTAssertEqual(DeckMessage.parse(["type": "switchMac"]), .switchMac)
        XCTAssertEqual(DeckMessage.parse(["type": "switchMac", "extra": 1] as [String: Any]), .switchMac)
    }

    func testUnknownOrMalformedMessagesAreIgnored() {
        XCTAssertNil(DeckMessage.parse(["type": "openSettings"]))
        XCTAssertNil(DeckMessage.parse(["type": 5]))
        XCTAssertNil(DeckMessage.parse(["kind": "switchMac"]))
        XCTAssertNil(DeckMessage.parse("switchMac"))
        XCTAssertNil(DeckMessage.parse(NSNull()))
        XCTAssertNil(DeckMessage.parse([1, 2]))
    }

    func testOriginComparesSchemeHostAndPort() {
        let base = WebOrigin(URL(string: "https://box.tail1.ts.net:7443")!)
        XCTAssertEqual(base, WebOrigin(scheme: "HTTPS", host: "Box.Tail1.ts.net", port: 7443))
        XCTAssertNotEqual(base, WebOrigin(scheme: "https", host: "box.tail1.ts.net", port: 443))
        XCTAssertNotEqual(base, WebOrigin(scheme: "http", host: "box.tail1.ts.net", port: 7443))
        XCTAssertNotEqual(base, WebOrigin(scheme: "https", host: "evil.example", port: 7443))
        XCTAssertNotEqual(base, WebOrigin(scheme: "https", host: "box.tail1.ts.net.evil.example", port: 7443))
    }

    func testDefaultPortsAreTheSameWhetherNamedOrNot() {
        // WebKit's security origin says port 0 for the scheme's own.
        XCTAssertEqual(WebOrigin(URL(string: "https://xyz.trycloudflare.com")!),
                       WebOrigin(scheme: "https", host: "xyz.trycloudflare.com", port: 0))
        XCTAssertEqual(WebOrigin(URL(string: "https://xyz.trycloudflare.com:443")!),
                       WebOrigin(scheme: "https", host: "xyz.trycloudflare.com", port: 0))
        XCTAssertEqual(WebOrigin(URL(string: "http://127.0.0.1")!), WebOrigin(scheme: "http", host: "127.0.0.1", port: 0))
        XCTAssertNotEqual(WebOrigin(URL(string: "http://127.0.0.1")!), WebOrigin(scheme: "https", host: "127.0.0.1", port: 0))
    }
}
