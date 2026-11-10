import MadKit
import MadUI
import XCTest
@testable import MadShell

/// The fixture `create-confirm`, as MadKit decodes it.
private func deck() throws -> Deck {
    let url = try XCTUnwrap(Bundle.module.url(forResource: "deck-create-confirm", withExtension: "json", subdirectory: "Fixtures"))
    return try JSONDecoder().decode(Deck.self, from: Data(contentsOf: url))
}

final class CreateMappingTests: XCTestCase {
    override func setUp() {
        super.setUp()
        MadLocale.language = "zh-Hans"
    }

    func testAnAgentCardIsWordedByItsTypeNotByTheServersEnglish() throws {
        let d = try deck()
        let item = try XCTUnwrap(d.inbox.waiting.first { $0.key == "create:c-00000001" })
        let card = try XCTUnwrap(CreateMapping.card(item, deck: d))
        XCTAssertEqual(card.head, "想创建一个 agent")
        XCTAssertEqual(card.name, "scout")
        XCTAssertEqual(card.project, "项目 mad")
        XCTAssertEqual(card.rows.map(\.left), ["模型", "加入群聊"])
        XCTAssertEqual(card.rows.map(\.right), ["Claude Sonnet", "api"])
        XCTAssertEqual(card.firstLabel, "第一句话")
        XCTAssertTrue(card.first.hasPrefix("Find the flaky test"))
        XCTAssertNotNil(card.allow)
        XCTAssertNotNil(card.deny)
        XCTAssertNotNil(card.change, "deny with words, labelled 改一下…, never by the English label")
        XCTAssertNotEqual(card.allow, card.deny)
    }

    func testATeamCardHasARowPerRoleAndTheLeadIsTagged() throws {
        let d = try deck()
        let item = try XCTUnwrap(d.inbox.waiting.first { $0.key == "create:c-00000002" })
        let card = try XCTUnwrap(CreateMapping.card(item, deck: d, inChannel: true))
        XCTAssertEqual(card.head, "architect 想创建一个团队", "in a channel the asker is named")
        XCTAssertEqual(card.rows.map(\.left), ["architect", "builder", "reviewer"])
        XCTAssertEqual(card.rows.map(\.tag), ["负责人", "", ""])
        XCTAssertEqual(card.rows.map(\.right), ["Claude Opus", "Claude Sonnet", "Codex"])
        XCTAssertEqual(card.firstLabel, "第一件事")
        XCTAssertEqual(card.first, "stop the follow-up test from flaking")
    }

    func testModelWords() {
        XCTAssertEqual(CreateMapping.modelWords(kind: "codex", model: ""), "Codex")
        XCTAssertEqual(CreateMapping.modelWords(kind: "claude", model: "sonnet"), "Claude Sonnet")
        XCTAssertEqual(CreateMapping.modelWords(kind: "codex", model: "gpt-5.5"), "Codex gpt-5.5")
    }

    func testEachOutcomeIsAGreyLineOrTheResultLine() throws {
        let d = try deck()
        func line(_ key: String) throws -> CreateLine {
            try XCTUnwrap(CreateMapping.line(try XCTUnwrap(d.creates.first { $0.key == key }), deck: d), key)
        }
        XCTAssertEqual(try line("create:c-00000004").title, "你拒绝了，没有创建「scout2」")
        XCTAssertEqual(try line("create:c-00000008").title, "你要改一下：use sonnet, not opus")
        XCTAssertEqual(try line("create:c-00000005").title, "没等到你确认，没有创建「scout3」")
        XCTAssertEqual(try line("create:c-00000006").title, "没建成「scout4」：not a directory: /fixture/mad")
        for k in ["create:c-00000004", "create:c-00000008", "create:c-00000005", "create:c-00000006"] {
            XCTAssertEqual(try line(k).phase, .note)
        }
    }

    func testTheMacSidebarSaysWhatIsAskedWithoutTheProject() throws {
        let d = try deck()
        func card(_ key: String) throws -> HomeWaiting {
            let item = try XCTUnwrap(d.inbox.waiting.first { $0.key == key })
            return HomeMapping.card(item, agent: d.agent(id: item.agent), deck: d, now: Date())
        }
        let agent = try card("create:c-00000001")
        XCTAssertEqual(agent.macLine, "想创建一个 agent · scout · Claude Sonnet")
        let team = try card("create:c-00000002")
        XCTAssertEqual(team.macLine, "想创建一个团队 · fix-flaky · 3 个角色")
        XCTAssertFalse(agent.macLine.contains("mad"), "the project is in the Mac's line")
        XCTAssertTrue(agent.detail.contains("mad"), "the iPhone's row lost its project")
    }

    func testACreatedTeamsLineHasItsTileSubAndTarget() throws {
        let d = try deck()
        let made = try XCTUnwrap(CreateMapping.line(try XCTUnwrap(d.creates.first { $0.key == "create:c-00000003" }), deck: d))
        // the fixture's channel "ship-it" is not in its deck: the line stays, says so and has no 打开
        XCTAssertEqual(made.title, "团队「ship-it」建好了")
        XCTAssertEqual(made.phase, .created)
        XCTAssertEqual(made.sub, "已经不在了")
        XCTAssertNil(made.target)
        // a gone team is drawn from the request's roles, stopped: never the empty tile (a "#")
        guard case .team(let members)? = made.mark else { return XCTFail("no tile for a gone team") }
        XCTAssertFalse(members.isEmpty)
        XCTAssertTrue(members.allSatisfy { $0.state == .stopped })
    }

    func testAnEventLineOfAChannelDrawsFromItsData() throws {
        let d = try deck()
        let m = BusMessage(seq: 4, from: .init(id: "mad", label: "mad", kind: "system"), kind: "system", text: "Team fold was created.",
                           event: "created", data: ["type": "team", "name": "fold", "team": "fold", "channel": "fold"], ref: "create:c-00000003")
        let line = try XCTUnwrap(CreateMapping.line(event: m, deck: d))
        XCTAssertEqual(line.title, "团队「fold」建好了")
        XCTAssertEqual(line.target, .channel("fold"))
        XCTAssertEqual(line.id, "create:c-00000003")
        XCTAssertNotNil(line.sub.firstRange(of: "成员"))
        XCTAssertNil(CreateMapping.line(event: BusMessage(seq: 5, from: .init(id: "mad", label: "mad"), kind: "system", event: "paused"), deck: d))
    }

    func testAChannelDrawsCreateEventsAsLinesNotNotes() throws {
        let d = try deck()
        let events = [
            BusMessage(seq: 1, from: .init(id: "mad", label: "mad", kind: "system"), kind: "system", text: "You declined; nothing was created.",
                       event: "create_declined", data: ["type": "agent", "name": "scout2"], ref: "create:c-4"),
        ]
        let s = ChannelMapping.screen(channel: "fold", messages: events, deck: d)
        guard case .create(let line)? = s.items.last else { return XCTFail("\(s.items)") }
        XCTAssertEqual(line.title, "你拒绝了，没有创建「scout2」")
    }

    func testTheToolStepOfAnAskedRequestIsDrawnFromTheItemsOutcomeOrNotAtAll() throws {
        let d = try deck()
        let done = #"{"status":"asked","request":"c-00000004","item":"create:c-00000004"}"#
        XCTAssertEqual(CreateMapping.line(toolResult: done, step: "r1", deck: d)?.title, "你拒绝了，没有创建「scout2」")
        let waiting = #"{"status":"asked","request":"c-00000001","item":"create:c-00000001"}"#
        XCTAssertNil(CreateMapping.line(toolResult: waiting, step: "r2", deck: d), "no outcome yet: the card is the place")
        let gone = #"{"status":"asked","request":"c-9","item":"create:c-9"}"#
        XCTAssertNil(CreateMapping.line(toolResult: gone, step: "r3", deck: d), "gone with no outcome ever seen")
    }

    func testMadsOutcomeMessageToTheBotIsPlumbing() {
        XCTAssertTrue(CreateMapping.isOutcomeDM("[mad] your request c-00000003 to create team ship-it: it is ready\n{}"))
        XCTAssertFalse(CreateMapping.isOutcomeDM("hello"))
        XCTAssertTrue(CreateMapping.isCreateTool("mcp__mad__agent_new"))
        XCTAssertTrue(CreateMapping.isCreateTool("team_start"))
        XCTAssertFalse(CreateMapping.isCreateTool("Bash"))
    }

    func testTheConversationShowsTheCardInsteadOfTheAsk() throws {
        let d = try deck()
        let c = AgentMapping.conversation(id: "fold0001-aaaa", transcript: ChatTranscript(), live: LiveState(), deck: d)
        XCTAssertNotNil(c.create)
        XCTAssertNil(c.ask, "the create request has its own card")
        XCTAssertTrue(c.needsYou)
    }
}
