import MadKit
import MadUI
import XCTest
@testable import MadShell

/// Real answers of the fixture server, as MadKit decodes them.
private func deck(_ name: String) throws -> Deck {
    let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures"))
    return try JSONDecoder().decode(Deck.self, from: Data(contentsOf: url))
}

private func section(_ m: HomeModel, _ id: String) throws -> HomeSection {
    try XCTUnwrap(m.sections.first { $0.id == id }, "no section \(id) in \(m.sections.map(\.id))")
}

final class HomeMappingTests: XCTestCase {
    func testEdgeHomeSectionsAndTheMachineName() throws {
        let m = HomeMapping.model(from: try deck("deck-edge-home"))
        XCTAssertEqual(m.machine.count, 60)
        XCTAssertEqual(m.sections.count, 2)
        XCTAssertEqual(m.sections[0].id, "chats")
        XCTAssertEqual(m.sections.map(\.title), ["群聊", "mad"])
        XCTAssertEqual(m.sections[0].kind, .chats)
        XCTAssertEqual(m.sections[1].kind, .project(name: "mad"))
        XCTAssertNil(m.usage, "the edge-home deck reports no usage")
    }

    func testTheChatsAreOrderedRedDotsFirstThenNewest() throws {
        let chats = try section(HomeMapping.model(from: try deck("deck-edge-home")), "chats")
        XCTAssertEqual(chats.rows.map(\.id), ["ios", "five", "shots", "pair", "solo"], "what the page lists")
        XCTAssertEqual(chats.rows.map(\.needsYou), [true, true, false, false, false])
    }

    func testAChannelRowCarriesItsMembersForTheTile() throws {
        let chats = try section(HomeMapping.model(from: try deck("deck-edge-home")), "chats")
        let ios = try XCTUnwrap(chats.rows.first { $0.id == "ios" })
        XCTAssertEqual(ios.kind, .channel)
        XCTAssertEqual(ios.name, "ios")
        XCTAssertEqual(ios.members.map(\.name), ["architect", "builder", "reviewer"])
        XCTAssertEqual(ios.members.map(\.state), [.idle, .working, .idle])
        XCTAssertEqual(ios.displayState, .working, "a channel's state is its members'")
        XCTAssertEqual(ios.preview, "architect: 第 3 条也上线了，三条全部完成。")
        let five = try XCTUnwrap(chats.rows.first { $0.id == "five" })
        XCTAssertEqual(five.members.count, 5)
        XCTAssertTrue(five.needsYou, "a member has an item in the inbox")
        XCTAssertEqual(chats.rows.first { $0.id == "solo" }?.members.count, 1)
        XCTAssertEqual(chats.rows.first { $0.id == "pair" }?.members.count, 2)
        XCTAssertEqual(chats.rows.first { $0.id == "shots" }?.preview, "🚀 发布助手: [图片]")
    }

    func testTeamMembersAreNotListedAgainUnderTheirProject() throws {
        let mad = try section(HomeMapping.model(from: try deck("deck-edge-home")), try deckProjectID())
        XCTAssertEqual(mad.rows.count, 7, "the team ios is one row of the chats, not three under the project")
        XCTAssertFalse(mad.rows.contains { ["team0001-arch", "team0001-bld0", "team0001-rev0"].contains($0.id) })
    }

    private func deckProjectID() throws -> String { try deck("deck-edge-home").projects[0].id }

    func testAgentRowsHaveStateDotDimAndTheUntitledName() throws {
        let mad = try section(HomeMapping.model(from: try deck("deck-edge-home")), try deckProjectID())
        let byID = Dictionary(uniqueKeysWithValues: mad.rows.map { ($0.id, $0) })

        let working = try XCTUnwrap(byID["work0001-aaaa"])
        XCTAssertEqual(working.state, .working)
        XCTAssertEqual(working.preview, "正在跑测试…")
        XCTAssertFalse(working.dimmed)

        let need = try XCTUnwrap(byID["need0001-bbbb"])
        XCTAssertEqual(need.state, .needsYou)
        XCTAssertTrue(need.needsYou, "it has an item in the inbox")
        XCTAssertEqual(need.name, "给我绘制一个表格", "a title when there is no name")

        let stopped = try XCTUnwrap(byID["stop0001-cccc"])
        XCTAssertEqual(stopped.state, .stopped)
        XCTAssertTrue(stopped.dimmed)

        let untitled = try XCTUnwrap(byID["unti0001-eeee"])
        XCTAssertEqual(untitled.name, "", "no name: the row reads from its kind and id")
        XCTAssertEqual(untitled.title, "codex unti0001")
        XCTAssertEqual(untitled.agentKind, "codex")

        XCTAssertEqual(byID["idle0001-dddd"]?.preview, "", "no last line, no preview")
        XCTAssertEqual(byID["emoji001-gggg"]?.name, "🚀 发布助手")
    }

    func testAgentRowsMatchMadUIsSampleOfTheSameScenario() throws {
        let mad = try section(HomeMapping.model(from: try deck("deck-edge-home")), try deckProjectID())
        let sample = try XCTUnwrap(SampleHome.model.sections.first { $0.title == "mad" })
        let shared = ["work0001-aaaa", "need0001-bbbb", "stop0001-cccc", "idle0001-dddd", "unti0001-eeee", "cjk00001-ffff", "emoji001-gggg"]
        for id in shared {
            XCTAssertEqual(mad.rows.first { $0.id == id }, sample.rows.first { $0.id == id }, "row \(id) differs from MadUI's sample")
        }
    }

    func testUsageLine() throws {
        let d = try deck("deck-usage")
        XCTAssertEqual(HomeMapping.usageLine(d.usage), "Claude 5 小时 42% · 本周 71%")
        XCTAssertEqual(HomeMapping.model(from: d).usage, "Claude 5 小时 42% · 本周 71%")
        XCTAssertNil(HomeMapping.usageLine([]))
        let onlyWeek = Usage(kind: "codex", fiveHour: UsageWindow(), sevenDay: UsageWindow(used: 10, resetAt: nil))
        XCTAssertEqual(HomeMapping.usageLine([onlyWeek]), "Codex 本周 10%")
    }

    func testAnEmptyDeckIsAnEmptyListWithTheFallbackName() {
        let m = HomeMapping.model(from: Deck(), fallbackName: "Studio")
        XCTAssertEqual(m.machine, "Studio")
        XCTAssertEqual(m.sections, [])
    }

    func testOnlyTheirOwnDMsAreListedAndAnUnreadOneIsADot() {
        let mine = Channel(id: "dm.you.kim", title: "@kim", dm: true, yours: true, members: [.init(id: "k", label: "kim")], last: 3,
                           lastText: "kim: hi", lastAt: Date(), unread: 2)
        let theirs = Channel(id: "dm.a.b", title: "a ↔ b", dm: true, yours: false)
        let quiet = Channel(id: "plain", lastText: "x: y", lastAt: Date().addingTimeInterval(60))
        let m = HomeMapping.model(from: Deck(channels: [theirs, quiet, mine]))
        let rows = m.sections[0].rows
        XCTAssertEqual(rows.map(\.id), ["dm.you.kim", "plain"], "the unread DM first, another pair's DM not at all")
        XCTAssertEqual(rows[0].kind, .dm)
        XCTAssertEqual(rows[0].name, "@kim")
        XCTAssertTrue(rows[0].needsYou)
    }

    func testTiesKeepTheServersOrder() {
        let t = Date()
        let a = Channel(id: "a", lastAt: t), b = Channel(id: "b", lastAt: t), c = Channel(id: "c", lastAt: t)
        XCTAssertEqual(HomeMapping.model(from: Deck(channels: [c, a, b])).sections[0].rows.map(\.id), ["c", "a", "b"])
    }
}

/// Every ⋯ item that is not native yet opens the page on its view; switching Mac is the app's own.
final class WebMenuTests: XCTestCase {
    func testEveryMenuItemButSwitchMacOpensAPageView() {
        for item in HomeMenuItem.allCases {
            if item == .switchMac {
                XCTAssertNil(NativeRoot.webView(for: item))
            } else {
                XCTAssertNotNil(NativeRoot.webView(for: item), "\(item) has no page view")
            }
        }
        XCTAssertEqual(NativeRoot.webView(for: .devices), .phone)
        XCTAssertEqual(NativeRoot.webView(for: .notifications), .notify)
        XCTAssertEqual(NativeRoot.webView(for: .addProject), .addproject)
    }
}
