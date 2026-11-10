import XCTest
@testable import MadKit

final class NewTeamTests: XCTestCase {
    private let client = MadClient(mac: Mac(name: "t", baseURL: URL(string: "https://box.ts.net:7443")!, token: String(repeating: "a", count: 32)))

    func testTemplatesAskForTheProjectAndDecode() throws {
        let r = client.request("/api/team/templates", query: [URLQueryItem(name: "project", value: "/p/mad")])
        XCTAssertEqual(r.url?.query, "project=/p/mad")
        let info = try JSONDecoder().decode(TeamTemplates.self, from: Data("""
        {"templates":[{"name":"review","description":"two eyes","source":"built in",
          "roles":[{"name":"lead","agent":"claude:opus@high","lead":true,"brief":"plan"},{"name":"checker","agent":"codex","read_only":true}]}],
         "choices":[{"agent":"claude:opus","efforts":["low","high"]},{"agent":"codex","efforts":[]}],
         "permissions":["auto","allowlist","bypass"],"max_teams":3}
        """.utf8))
        XCTAssertEqual(info.templates[0].label, "review: two eyes")
        XCTAssertEqual(info.templates[0].roles.map(\.lead), [true, false])
        XCTAssertTrue(info.templates[0].roles[1].readOnly)
        XCTAssertEqual(info.permissions, ["auto", "allowlist", "bypass"])
        XCTAssertEqual(info.maxTeams, 3)
        XCTAssertNil(info.error)
        XCTAssertEqual(info.efforts(of: "claude:opus"), ["low", "high"])
        XCTAssertEqual(info.efforts(of: "gone"), [])
    }

    func testARoleOffersItsOwnAgentWhenItIsNotAChoice() {
        let info = TeamTemplates(choices: [TeamChoice(agent: "claude"), TeamChoice(agent: "codex")])
        XCTAssertEqual(info.agents(including: "pi"), ["pi", "claude", "codex"])
        XCTAssertEqual(info.agents(including: "codex"), ["claude", "codex"])
    }

    func testAnAgentAndItsEffortSplitAndJoin() {
        XCTAssertEqual(TeamAgent.split("claude:opus@high").agent, "claude:opus")
        XCTAssertEqual(TeamAgent.split("claude:opus@high").effort, "high")
        XCTAssertEqual(TeamAgent.split("codex").effort, "")
        XCTAssertEqual(TeamAgent.join(agent: "codex", effort: ""), "codex")
        XCTAssertEqual(TeamAgent.join(agent: "claude", effort: "low"), "claude@low")
    }

    func testStartingSendsEverythingTheFormHas() throws {
        let r = client.startTeamRequest(TeamStart(template: "review", project: "/p/mad", name: "", goal: "ship it", permission: "auto",
                                                  worktree: "team/api", agents: ["lead": "claude:opus@high", "checker": "codex"]))
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.path, "/api/team")
        let b = try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(r.httpBody)) as? [String: Any])
        XCTAssertEqual(b["op"] as? String, "start")
        XCTAssertEqual(b["template"] as? String, "review")
        XCTAssertEqual(b["project"] as? String, "/p/mad")
        XCTAssertEqual(b["name"] as? String, "")
        XCTAssertEqual(b["goal"] as? String, "ship it")
        XCTAssertEqual(b["permission"] as? String, "auto")
        XCTAssertEqual(b["worktree"] as? String, "team/api")
        XCTAssertEqual(b["agents"] as? [String: String], ["lead": "claude:opus@high", "checker": "codex"])
        XCTAssertGreaterThan(r.timeoutInterval, 60, "the agents come up one by one")
    }

    func testAStartedTeamDecodesWithAndWithoutNotes() throws {
        let a = try JSONDecoder().decode(StartedTeam.self, from: Data(#"{"name":"api","channel":"c1","notes":["pi is not installed"]}"#.utf8))
        XCTAssertEqual(a.channel, "c1")
        XCTAssertEqual(a.notes, ["pi is not installed"])
        let b = try JSONDecoder().decode(StartedTeam.self, from: Data(#"{"name":"api","channel":"c1","notes":null}"#.utf8))
        XCTAssertEqual(b.notes, [])
    }
}
