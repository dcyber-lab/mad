import XCTest
@testable import MadKit

final class NewChatTests: XCTestCase {
    private let client = MadClient(mac: Mac(name: "t", baseURL: URL(string: "https://box.ts.net:7443")!, token: String(repeating: "a", count: 32)))

    private func body(_ r: URLRequest) throws -> [String: String] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(r.httpBody)) as? [String: String])
    }

    func testANewChatSendsTheProjectKindModelAndFirstMessage() throws {
        let r = client.startRequest(project: "/p/mad", kind: "claude", model: "opus", text: "画个表", session: "", cwd: "")
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.path, "/api/agents")
        XCTAssertEqual(try body(r), ["project": "/p/mad", "kind": "claude", "model": "opus", "text": "画个表"])
    }

    func testResumingSendsTheSessionAndItsDirectoryAndNoMessage() throws {
        let r = client.startRequest(project: "/p/mad", kind: "codex", model: "", text: "", session: "s1", cwd: "/p/mad-wt")
        XCTAssertEqual(try body(r), ["project": "/p/mad", "kind": "codex", "session": "s1", "cwd": "/p/mad-wt"])
    }

    func testPastSessionsAndModelsDecode() throws {
        let past = try JSONDecoder().decode([PastSession].self, from: Data("""
        [{"kind":"claude","id":"s1","title":"修登录","cwd":"/p","origin":"","updated":"2026-11-09T07:12:30.123456+08:00"},
         {"kind":"codex","id":"s2","title":"","cwd":"","updated":"2026-11-08T10:00:00Z"}]
        """.utf8))
        XCTAssertEqual(past.map(\.id), ["s1", "s2"])
        XCTAssertNotNil(past[0].updated, "fractions of a second")
        XCTAssertNotNil(past[1].updated)
        let models = try JSONDecoder().decode([ModelChoice].self, from: Data(#"[{"value":"opus","name":"Opus","description":"best"},{"value":"x"}]"#.utf8))
        XCTAssertEqual(models.map(\.value), ["opus", "x"])
        XCTAssertEqual(models[0].detail, "best")
    }

    /// The server starts the agent even when its first message fails: the reply says why, and the text that did not go is
    /// kept for the person (it is not lost with the screen).
    func testAFirstMessageThatDidNotGoIsKeptAndSaysWhy() throws {
        let failed = try JSONDecoder().decode(StartedAgent.self, from: Data(#"{"id":"a1","error":"host is not up"}"#.utf8))
        XCTAssertEqual(failed.id, "a1")
        XCTAssertEqual(failed.failure, "host is not up")
        XCTAssertEqual(failed.unsent("画个表"), "画个表")
        let sent = try JSONDecoder().decode(StartedAgent.self, from: Data(#"{"id":"a2"}"#.utf8))
        XCTAssertNil(sent.failure)
        XCTAssertEqual(sent.unsent("画个表"), "")
        XCTAssertNil(StartedAgent(id: "a3", error: "").failure, "an empty error is no error")
    }
}
