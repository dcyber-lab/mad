import XCTest
@testable import MadKit

/// Real answers of the fixture server (internal/desktop/fixture), kept in Tests/MadKitTests/Fixtures.
func fixtureData(_ name: String) throws -> Data {
    let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures"), "no fixture \(name)")
    return try Data(contentsOf: url)
}

func decodeFixture<T: Decodable>(_ type: T.Type, _ name: String) throws -> T {
    try JSONDecoder().decode(type, from: fixtureData(name))
}

final class DeckDecodingTests: XCTestCase {
    func testEdgeHomeDeck() throws {
        let d = try decodeFixture(Deck.self, "deck-edge-home")
        XCTAssertEqual(d.machine?.count, 60, "the 60-character Mac name")
        XCTAssertEqual(d.projects.map(\.name), ["mad"])
        XCTAssertEqual(d.agents.count, 10)

        let working = try XCTUnwrap(d.agent(id: "work0001-aaaa"))
        XCTAssertEqual(working.label, "Sidebar token 统计")
        XCTAssertEqual(working.state, "running")
        XCTAssertEqual(working.tool, "Bash")
        XCTAssertEqual(working.target, "go test ./...")
        XCTAssertEqual(working.last, "正在跑测试…")
        XCTAssertTrue(working.chat)

        XCTAssertEqual(d.agent(id: "need0001-bbbb")?.label, "给我绘制一个表格", "a title when there is no name")
        XCTAssertEqual(d.agent(id: "unti0001-eeee")?.label, "codex unti0001", "an untitled agent: kind and the id's first 8")
        XCTAssertEqual(d.agent(id: "emoji001-gggg")?.label, "🚀 发布助手")
        let lead = try XCTUnwrap(d.agent(id: "team0001-arch"))
        XCTAssertEqual(lead.team, "ios")
        XCTAssertEqual(lead.role, "lead")
        XCTAssertNil(d.agent(id: "nobody"))
        XCTAssertNil(d.agent(id: nil))
    }

    func testEdgeHomeTeamsChannelsInboxAndStates() throws {
        let d = try decodeFixture(Deck.self, "deck-edge-home")
        XCTAssertEqual(d.teams.map(\.name), ["ios"])
        XCTAssertEqual(d.teams[0].members, ["team0001-arch", "team0001-bld0", "team0001-rev0"])

        XCTAssertEqual(d.channels.map(\.id), ["ios", "solo", "pair", "five", "shots"])
        let ios = try XCTUnwrap(d.channel(id: "ios"))
        XCTAssertTrue(ios.mention)
        XCTAssertEqual(ios.members.map(\.label), ["architect", "builder", "reviewer"])
        XCTAssertEqual(ios.team?.name, "ios")
        XCTAssertEqual(ios.team?.roles.first { $0.lead }?.agent, "team0001-arch")
        XCTAssertEqual(ios.last, 3)
        XCTAssertEqual(ios.lastText, "architect: 第 3 条也上线了，三条全部完成。")
        XCTAssertNotNil(ios.lastAt)
        XCTAssertTrue(ios.listed)
        XCTAssertEqual(d.channel(id: "five")?.members.count, 5)
        XCTAssertEqual(d.channel(id: "shots")?.lastText, "🚀 发布助手: [图片]")

        XCTAssertEqual(d.inbox.needs, 1)
        let item = try XCTUnwrap(d.inbox.waiting.first)
        XCTAssertEqual(item.kind, "permission")
        XCTAssertEqual(item.agent, "need0001-bbbb")
        XCTAssertTrue(item.mono)
        XCTAssertEqual(item.acts.map(\.verb), ["allow", "deny"])
        XCTAssertTrue(item.acts[0].primary)
        XCTAssertEqual(d.inbox.finished, [])

        XCTAssertEqual(d.states.map(\.state), ["waiting", "running", "finished", "idle", "asleep", "starting", "stopped"])
        XCTAssertEqual(d.word(for: "running"), "在工作")
        XCTAssertEqual(d.tone(for: "waiting"), "needs")
        XCTAssertNotNil(d.at)
        XCTAssertEqual(d.working, ["work0001-aaaa", "team0001-bld0"], "the agents at work")
    }

    func testUsageDeckKeepsTheOverAndTheZeroTime() throws {
        let d = try decodeFixture(Deck.self, "deck-usage")
        XCTAssertEqual(d.usage.map(\.kind), ["claude", "codex"])
        XCTAssertEqual(d.usage[0].fiveHour.used, 42)
        XCTAssertEqual(d.usage[0].sevenDay.used, 71)
        XCTAssertEqual(d.usage[1].fiveHour.used, 104)
        XCTAssertEqual(d.usage[1].fiveHour.leftText, "over the limit")
        XCTAssertEqual(d.usage[1].sevenDay.used, 10)
        XCTAssertNil(d.usage[1].sevenDay.resetAt, "Go's zero time is no reset time")
    }

    func testAnEmptyOrFutureDeckDecodes() throws {
        XCTAssertEqual(try JSONDecoder().decode(Deck.self, from: Data("{}".utf8)), Deck())
        let d = try JSONDecoder().decode(Deck.self, from: Data(#"{"machine":" ","novel":{"a":[1]},"bus":[{"id":"x"}],"projects":[{"name":"p"}]}"#.utf8))
        XCTAssertNil(d.machine)
        XCTAssertEqual(d.channels.first?.title, "#x")
        XCTAssertEqual(d.projects.first?.agents, [])
    }
}

final class BusMessageDecodingTests: XCTestCase {
    func testTheFoldChannel() throws {
        let page = try decodeFixture(MessagesPage.self, "messages-fold")
        XCTAssertEqual(page.channel, "fold")
        XCTAssertEqual(page.last, 13)
        XCTAssertFalse(page.paused)
        XCTAssertEqual(page.receipts, [])
        XCTAssertEqual(page.messages.map(\.seq), Array(1...13))

        let handoff = page.messages[1]
        XCTAssertEqual(handoff.kind, "handoff")
        XCTAssertEqual(handoff.handoff, BusMessage.Handoff(id: "h-1234abcd", to: "fold0002-aaaa", status: "open"))
        XCTAssertEqual(page.messages[2].kind, "done")
        XCTAssertEqual(page.messages[2].handoff?.status, "done")

        XCTAssertTrue(page.messages[0].from.isYou)
        XCTAssertFalse(page.messages[1].from.isYou)
        XCTAssertEqual(page.messages[3].mentions, ["you"])
        XCTAssertTrue(page.messages[3].asksYou)
        XCTAssertFalse(page.messages[7].asksYou)

        let pictures = page.messages[4]
        XCTAssertEqual(pictures.images.map(\.id), ["fx-before", "fx-after"])
        XCTAssertEqual(pictures.images[0].w, 200)
        XCTAssertEqual(pictures.images[0].h, 120)
        XCTAssertEqual(pictures.images[0].mime, "image/png")

        XCTAssertEqual(page.messages[9].replyTo, 9, "message 10 answers message 9")
        XCTAssertEqual(page.messages[0].replyTo, 0)
        XCTAssertNotNil(page.messages[0].at)
        XCTAssertTrue(page.messages[12].text.contains("```"), "the code block comes through whole")
    }

    func testReceiptsAndResults() throws {
        let page = try JSONDecoder().decode(MessagesPage.self, from: Data("""
        {"channel":"c","messages":[],"last":4,"paused":true,
         "receipts":[{"seq":4,"targets":[{"id":"a1","label":"kim","state":"read"}],"paused":true}]}
        """.utf8))
        XCTAssertTrue(page.paused)
        XCTAssertEqual(page.receipts, [BusReceipt(seq: 4, targets: [.init(id: "a1", label: "kim", state: "read")], paused: true)])
        XCTAssertEqual(try JSONDecoder().decode(PostResult.self, from: Data(#"{"seq":5,"woke":["kim"],"warning":"20 in a row"}"#.utf8)),
                       PostResult(seq: 5, woke: ["kim"], warning: "20 in a row"))
        XCTAssertEqual(try JSONDecoder().decode(DMResult.self, from: Data(#"{"channel":"dm.you.kim","seq":1,"woke":[]}"#.utf8)),
                       DMResult(channel: "dm.you.kim", seq: 1))
    }
}
