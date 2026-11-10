import XCTest
@testable import MadKit

final class NewGroupTests: XCTestCase {
    private let client = MadClient(mac: Mac(name: "t", baseURL: URL(string: "https://box.ts.net:7443")!, token: String(repeating: "a", count: 32)))

    private func json(_ r: URLRequest) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(r.httpBody)) as? [String: Any])
    }

    func testANewGroupSendsItsNameTopicAndMembers() throws {
        let r = client.createChannelRequest(name: "api-redesign", topic: "", members: ["a1", "a2"])
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.path, "/api/bus/channel")
        let b = try json(r)
        XCTAssertEqual(b["name"] as? String, "api-redesign")
        XCTAssertEqual(b["topic"] as? String, "")
        XCTAssertEqual(b["members"] as? [String], ["a1", "a2"])
    }

    func testTheCreatedChannelDecodesFromTheChannel() throws {
        let c = try JSONDecoder().decode(CreatedChannel.self, from: Data(##"{"id":"c9","title":"#api","members":[]}"##.utf8))
        XCTAssertEqual(c.id, "c9")
    }

    private func agent(_ id: String, chat: Bool = true, role: String? = nil, team: String? = nil) throws -> Agent {
        var o: [String: Any] = ["id": id, "kind": "claude", "chat": chat]
        if let role { o["role"] = role }
        if let team { o["team"] = team }
        return try JSONDecoder().decode(Agent.self, from: JSONSerialization.data(withJSONObject: o))
    }

    func testAGroupTakesConversationsAndTeamMembersNotStrayRoles() throws {
        let deck = Deck(projects: [Project(name: "mad", path: "/p", agents: [
            try agent("plain"), try agent("terminal", chat: false), try agent("role", role: "reviewer"),
            try agent("member", role: "lead", team: "t1"),
        ])])
        XCTAssertEqual(deck.groupCandidates.map(\.agent.id), ["plain", "member"])
        XCTAssertEqual(deck.groupCandidates.map(\.project), ["mad", "mad"])
    }
}
