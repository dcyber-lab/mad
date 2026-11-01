import MadKit
import MadUI
import XCTest
@testable import MadShell

private let tz = TimeZone(secondsFromGMT: 8 * 3600)!
private var cal: Calendar { var c = Calendar(identifier: .gregorian); c.timeZone = tz; return c }
/// "now" for the fixture's messages: 1 November 2026, 09:00 in +08:00.
private let now = ISO8601DateFormatter().date(from: "2026-11-01T09:00:00+08:00")!

private func fixture<T: Decodable>(_ name: String, _ type: T.Type) throws -> T {
    let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures"))
    return try JSONDecoder().decode(T.self, from: Data(contentsOf: url))
}

/// A message by hand, for a rule on its own.
private func msg(_ seq: Int, _ who: String = "architect", _ text: String = "hello", kind: String = "text", at: String = "2026-11-01T08:00:00+08:00",
                 mentions: [String] = [], replyTo: Int = 0, images: [String] = [], handoff: BusMessage.Handoff? = nil,
                 event: String = "") -> BusMessage {
    var m = BusMessage(seq: seq, at: ISO8601DateFormatter().date(from: at), from: .init(id: who == "you" ? "you" : "id-\(who)", label: who),
                       kind: kind, text: text, mentions: mentions, replyTo: replyTo, handoff: handoff, images: images.map { BusImage(id: $0) },
                       event: event)
    m.seq = seq
    return m
}

private func screen(_ messages: [BusMessage], deck: Deck? = nil) -> ChannelScreen {
    ChannelMapping.screen(channel: "fold", messages: messages, deck: deck, calendar: cal, now: now)
}

private func bubbles(_ s: ChannelScreen) -> [ChannelBubble] {
    s.items.flatMap { item -> [ChannelBubble] in
        switch item {
        case .mine(let b): [b]
        case .run(let r): r.bubbles
        default: []
        }
    }
}

final class ChannelMappingTests: XCTestCase {
    private func foldScreen() throws -> ChannelScreen {
        let page = try fixture("messages-channel-fold", MessagesPage.self)
        return screen(page.messages, deck: try fixture("deck-channel-fold", Deck.self))
    }

    // MARK: folding

    func testWhatTheFixtureFoldsAndWhatItShowsWhole() throws {
        let s = try foldScreen()
        let folded = Dictionary(uniqueKeysWithValues: bubbles(s).map { ($0.id, $0.fold) })
        // the handoff (#2) and its result (#3) are not drawn; the rest are
        XCTAssertEqual(folded.keys.sorted(), [1, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13])
        // a long message and a code block fold; the rest are whole
        for seq in [11, 13] { XCTAssertNotNil(folded[seq] ?? nil, "#\(seq) should be folded") }
        for seq in [1, 4, 5, 6, 7, 8, 9, 10, 12] { XCTAssertNil(folded[seq] ?? nil, "#\(seq) should be whole") }
        XCTAssertEqual(try XCTUnwrap(folded[11] ?? nil).summary, "修改在这里：PR #88 看第二处。", "a link is its text and a PR's address its number")
    }

    func testTheRulesOfFolding() {
        let long = String(repeating: "字", count: 281)
        let atLimit = String(repeating: "字", count: 280)
        let lines3 = "a\nb\nc", lines4 = "a\nb\nc\nd"
        func folded(_ m: BusMessage, deck: Deck? = nil) -> Bool { bubbles(screen([m], deck: deck)).first?.fold != nil }
        XCTAssertFalse(folded(msg(1, "builder", atLimit)), "280 characters is not past the limit")
        XCTAssertTrue(folded(msg(1, "builder", long)), "281 is")
        XCTAssertFalse(folded(msg(1, "builder", lines3)), "three lines is not past")
        XCTAssertTrue(folded(msg(1, "builder", lines4)), "four is")
        XCTAssertFalse(folded(msg(1, "builder", "a\n\n\nb\n\nc")), "blank lines do not count")
        // a short message is whole, whatever it is (reports are not drawn at all: see testTheBookkeepingIsNotDrawn)
        XCTAssertFalse(folded(msg(1, "builder", "ok")))
        // the person's own never; one that asks the person never
        XCTAssertFalse(folded(msg(1, "you", long)))
        XCTAssertFalse(folded(msg(1, "builder", "@you " + long)))
        XCTAssertFalse(folded(msg(1, "builder", "please look, @You " + long, kind: "done")), "the case of @you does not matter")
        XCTAssertFalse(folded(msg(1, "builder", long, mentions: ["you"])))
        XCTAssertTrue(folded(msg(1, "builder", "write to me@you.com " + long)), "an address is not an @you")
        XCTAssertTrue(folded(msg(1, "builder", "@younger " + long)), "nor @younger")
    }

    func testAnAgentsMessagesInADMWithThePersonStayWhole() throws {
        let deck = try JSONDecoder().decode(Deck.self, from: Data("""
        {"bus":[{"id":"dm.you.a1","title":"@alice","dm":true,"yours":true,"members":[{"id":"a1","label":"alice","kind":"claude"}]}]}
        """.utf8))
        let s = ChannelMapping.screen(channel: "dm.you.a1", messages: [msg(1, "alice", String(repeating: "x", count: 400))], deck: deck, calendar: cal, now: now)
        XCTAssertNil(bubbles(s).first?.fold)
        XCTAssertEqual(s.title, "alice")
        XCTAssertEqual(s.placeholder, "发消息给 alice")
        XCTAssertFalse(s.readOnly)
    }

    func testTheSummaryIsTheFirstSentenceWithoutMarkdown() {
        XCTAssertEqual(ChannelMapping.firstSentence("## **Done** with `go test`. More follows"), "Done with go test.")
        XCTAssertEqual(ChannelMapping.firstSentence("见 https://example.com/a/b/c 这里。后面"), "见 example.com 这里。")
        XCTAssertEqual(ChannelMapping.firstSentence("[the PR](https://github.com/x/y/pull/5) is up"), "the PR is up")
        XCTAssertEqual(ChannelMapping.firstSentence(String(repeating: "a", count: 300)).count, 120)
        XCTAssertEqual(ChannelMapping.firstSentence("v1.2 is out"), "v1.2 is out", "a dot inside a word is not an end")
    }

    // MARK: the agents' bookkeeping is not drawn

    private func handoffMessage(_ seq: Int, to: String = "builder") -> BusMessage {
        msg(seq, "architect", "Do this.", kind: "handoff", handoff: .init(id: "h-\(seq)", to: to))
    }

    func testTheBookkeepingIsNotDrawn() {
        let kinds = ["handoff", "done", "declined", "timer", "nudge"]
        for kind in kinds {
            let s = screen([msg(1, "architect", "x", kind: kind)])
            XCTAssertFalse(s.items.contains { if case .run = $0 { true } else { false } }, "a \(kind) is not drawn")
        }
        // the lapsed notice of a handoff is a system message that carries the handoff
        XCTAssertTrue(ChannelMapping.isHidden(msg(1, "mad", "h-1 lapsed: builder ended its turn without bus_done.", kind: "system", handoff: .init(id: "h-1"))))
        // the bus's own pause and resume notes stay
        XCTAssertFalse(ChannelMapping.isHidden(msg(1, "mad", "Paused: waiting for you", kind: "system")))
        XCTAssertFalse(ChannelMapping.isHidden(msg(1, "builder", "hello")))
        XCTAssertFalse(ChannelMapping.isHidden(msg(1, "builder", "[a picture]", images: ["p1"])))
    }

    func testAMessageThatMentionsThePersonIsAnOrdinaryBubbleWhateverItsKind() throws {
        for kind in ["handoff", "done", "declined", "timer", "nudge"] {
            let m = msg(1, "builder", "@you 请看一下 #88", kind: kind)
            XCTAssertFalse(ChannelMapping.isHidden(m), "\(kind) with @you stays")
            let b = try XCTUnwrap(bubbles(screen([m])).first, "\(kind) with @you is a bubble")
            XCTAssertNil(b.fold, "never a fold line")
            XCTAssertEqual(b.text, "@you 请看一下 #88")
        }
        XCTAssertFalse(ChannelMapping.isHidden(msg(1, "builder", "look", kind: "done", mentions: ["you"])))
    }

    func testARunStaysOneRunAcrossAHiddenMessageAndItAddsNoTimeLine() {
        let s = screen([
            msg(1, "builder", "one", at: "2026-11-01T08:00:00+08:00"),
            handoffMessage(2), // hidden, from someone else, an hour on
            msg(3, "builder", "two", at: "2026-11-01T08:01:00+08:00"),
        ].map { var m = $0; if m.seq == 2 { m.at = ISO8601DateFormatter().date(from: "2026-11-01T09:30:00+08:00") }; return m })
        let runs = s.items.compactMap { item -> ChannelRun? in if case .run(let r) = item { r } else { nil } }
        XCTAssertEqual(runs.map { $0.bubbles.map(\.id) }, [[1, 3]], "one run: a hidden message between does not split it")
        XCTAssertEqual(s.items.filter { if case .time = $0 { true } else { false } }.count, 1, "and adds no time line")
    }

    func testAReplyKeepsItsQuoteOfAHiddenMessage() throws {
        let s = screen([handoffMessage(5), msg(6, "builder", "got it", replyTo: 5)])
        let b = try XCTUnwrap(bubbles(s).first)
        XCTAssertEqual(b.id, 6)
        XCTAssertEqual(b.quote?.seq, 5)
        XCTAssertEqual(b.quote?.who, "architect")
        XCTAssertEqual(b.quote?.text, "Do this.")
    }

    func testAChannelOfOnlyHiddenMessagesSaysTheAgentsWorkInTheBackground() {
        let s = screen([handoffMessage(1), msg(2, "builder", "ok", kind: "done")])
        XCTAssertEqual(s.items, [.note(id: "quiet", text: "agent 们在后台协作，有事会在这里 @ 你。")])
        XCTAssertEqual(screen([]).items, [.note(id: "empty", text: "还没人说话…")], "a channel nobody has spoken in")
        XCTAssertEqual(ChannelMapping.screen(channel: "fold", messages: [], deck: nil, loaded: false, calendar: cal, now: now).items, [],
                       "before the first look it is not known to be empty")
        // and a hidden message that arrives later changes nothing on screen
        let one = screen([msg(1, "builder", "hi")])
        let two = screen([msg(1, "builder", "hi"), handoffMessage(2), msg(3, "architect", "x", kind: "nudge")])
        XCTAssertEqual(one.items, two.items)
        XCTAssertEqual(one.tail, two.tail, "nothing to follow")
    }

    func testThePauseAndResumeNotesAreInChineseAndOlderOnesKeepTheirText() throws {
        let s = screen([
            msg(1, "mad", "Paused: waiting for you", kind: "system", event: "paused"),
            msg(2, "mad", "Resumed by a person.", kind: "system", event: "resumed"),
            msg(3, "mad", "Paused by an older mad.", kind: "system"),
        ])
        XCTAssertEqual(s.items.compactMap { item -> String? in if case .note(_, let t) = item { t } else { nil } },
                       ["群已暂停", "群已恢复", "Paused by an older mad."])
        // and the field is read from the server's JSON
        let m = try JSONDecoder().decode(BusMessage.self, from: Data(#"{"seq":4,"kind":"system","text":"x","event":"paused"}"#.utf8))
        XCTAssertEqual(m.event, "paused")
        XCTAssertEqual(try JSONDecoder().decode(BusMessage.self, from: Data(#"{"seq":5,"kind":"system","text":"x"}"#.utf8)).event, "")
    }

    func testTheNoteOfDroppedMessagesIsTheFirstItemAndOnlyTheLatestCounts() {
        let s = screen([
            msg(1, "architect", "one", at: "2026-11-01T08:00:00+08:00"),
            msg(2, "mad", "Older messages were dropped…", kind: "system", event: "trimmed"),
            msg(3, "architect", "two", at: "2026-11-01T08:01:00+08:00"),
            msg(4, "mad", "Older messages were dropped…", kind: "system", event: "trimmed"),
            msg(5, "mad", "Paused: x", kind: "system", event: "paused"),
        ])
        guard case .note(let id, let text) = s.items[0] else { return XCTFail("the first item is not a note: \(s.items[0])") }
        XCTAssertEqual(id, "trimmed")
        XCTAssertEqual(text, "更早的消息已清理")
        let notes = s.items.compactMap { item -> String? in if case .note(_, let t) = item { t } else { nil } }
        XCTAssertEqual(notes, ["更早的消息已清理", "群已暂停"], "one trimmed note, first; the pause note stays in place")
        if case .time = s.items[1] {} else { XCTFail("the oldest message's time line comes after the note: \(s.items[1])") }
    }

    /// The table the server, the page and native share (internal/bus/testdata/at-you.json).
    func testTheAtYouRuleIsTheSharedTable() throws {
        struct Table: Decodable { struct Case: Decodable { var text: String; var want: Bool; var note: String? }; var cases: [Case] }
        let table = try fixture("at-you", Table.self)
        XCTAssertGreaterThanOrEqual(table.cases.count, 21)
        for c in table.cases {
            XCTAssertEqual(ChannelMapping.asksYou(msg(1, "builder", c.text)), c.want, "“\(c.text)” \(c.note ?? "")")
        }
        // the cases the review named
        for text in ["@you请看一下", "\u{3000}@you 看", "（@you", "@you.", "@you"] { XCTAssertTrue(ChannelMapping.asksYou(msg(1, "builder", text)), text) }
        for text in ["@you-x", "@young", "x@you"] { XCTAssertFalse(ChannelMapping.asksYou(msg(1, "builder", text)), text) }
    }

    // MARK: runs

    func testOneSendersMessagesInARowAreOneRun() {
        let s = screen([msg(1, "architect", "one", at: "2026-11-01T08:00:00+08:00"), msg(2, "architect", "two", at: "2026-11-01T08:01:00+08:00"),
                        msg(3, "builder", "three", at: "2026-11-01T08:02:00+08:00"), msg(4, "architect", "four", at: "2026-11-01T08:03:00+08:00")])
        let runs = s.items.compactMap { item -> ChannelRun? in if case .run(let r) = item { r } else { nil } }
        XCTAssertEqual(runs.map { $0.sender.name }, ["architect", "builder", "architect"])
        XCTAssertEqual(runs.map { $0.bubbles.map(\.id) }, [[1, 2], [3], [4]])
    }

    func testThePersonsMessageEndsARunAndIsNeverInOne() {
        let s = screen([msg(1, "architect", "one"), msg(2, "you", "mine"), msg(3, "architect", "again"), msg(4, "you", "a"), msg(5, "you", "b")])
        let kinds = s.items.map { item -> String in
            switch item { case .run: "run"; case .mine: "mine"; case .time: "time"; case .note: "note" }
        }
        XCTAssertEqual(kinds, ["time", "run", "mine", "run", "mine", "mine"])
    }

    func testAQuietWhileEndsARun() {
        let s = screen([msg(1, "architect", "one", at: "2026-11-01T08:00:00+08:00"), msg(2, "architect", "two", at: "2026-11-01T08:10:00+08:00")])
        XCTAssertEqual(s.items.count, 4, "time, run, time, run: ten minutes is a quiet")
    }

    // MARK: time lines

    func testTimeLinesComeAfterTenMinutesOrANewDay() {
        let s = screen([
            msg(1, at: "2026-11-01T08:00:00+08:00"),
            msg(2, "you", at: "2026-11-01T08:09:59+08:00"), // under ten minutes: no line
            msg(3, at: "2026-11-01T08:20:00+08:00"),        // ten and more: a line, "08:20"
            msg(4, "you", at: "2026-11-01T08:21:00+08:00"),
        ])
        let lines = s.items.compactMap { item -> String? in if case .time(_, let t) = item { t } else { nil } }
        XCTAssertEqual(lines, ["今天 08:00", "08:20"])
    }

    func testTheFirstLineOfADayNamesTheDay() {
        let s = screen([
            msg(1, at: "2026-10-31T23:58:00+08:00"),        // yesterday
            msg(2, at: "2026-11-01T00:01:00+08:00"),        // a new day, though only 3 minutes on
            msg(3, at: "2026-10-30T08:00:00+08:00"),        // out of order is not expected; sorted by number it is a new day again
        ])
        let lines = s.items.compactMap { item -> String? in if case .time(_, let t) = item { t } else { nil } }
        XCTAssertEqual(lines, ["昨天 23:58", "今天 00:01", "10月30日 08:00"])
    }

    func testSystemLinesAreNotesAndHaveNoCharacter() {
        let s = screen([msg(1, "architect", "one"), msg(2, "mad", "Paused: waiting for you", kind: "system")])
        XCTAssertEqual(s.items.compactMap { item -> String? in if case .note(_, let t) = item { t } else { nil } }, ["Paused: waiting for you"])
        XCTAssertEqual(s.panel.pictureCount, 0)
    }

    // MARK: quotes, pictures, the head and the panel

    func testAReplyQuotesWhatItAnswers() throws {
        let s = try foldScreen()
        let reply = try XCTUnwrap(bubbles(s).first { $0.id == 10 })
        let q = try XCTUnwrap(reply.quote)
        XCTAssertEqual(q.seq, 9)
        XCTAssertEqual(q.who, "你")
        XCTAssertTrue(q.text.hasPrefix("我看了一下这条消息"))
        XCTAssertLessThanOrEqual(q.text.count, 60, "a quote is a line")
        let gone = screen([msg(5, "architect", "re", replyTo: 2)])
        XCTAssertEqual(bubbles(gone).first?.quote, ChannelQuote(seq: 2, who: "#2", text: ""), "an answer to a message that is not held")
        let pics = screen([msg(1, "reviewer", "", images: ["p1"]), msg(2, "you", "ok", replyTo: 1)])
        XCTAssertEqual(bubbles(pics).last?.quote?.text, "[图片]")
    }

    func testPicturesAreCarriedAndCounted() throws {
        let s = try foldScreen()
        XCTAssertEqual(bubbles(s).first { $0.id == 5 }?.images.count, 2)
        XCTAssertEqual(s.panel.pictureCount, 2)
    }

    func testTheHeadAndThePanel() throws {
        var deck = try fixture("deck-channel-fold", Deck.self)
        let s = screen([], deck: deck)
        XCTAssertEqual(s.title, "fold")
        XCTAssertEqual(s.members.count, 8)
        XCTAssertEqual(s.stateLine, "空闲", "nobody works")
        XCTAssertEqual(s.placeholder, "发消息给 fold")
        XCTAssertTrue(s.members.allSatisfy { $0.status == "空闲" })
        // two work, one needs the person; the lead is tagged
        deck.projects[0].agents[0].shown = "running"
        deck.projects[0].agents[0].tool = "Bash"
        deck.projects[0].agents[0].target = "go test"
        deck.projects[0].agents[1].shown = "running"
        deck.projects[0].agents[2].shown = "waiting"
        deck.channels[0].lead = deck.projects[0].agents[0].id
        let t = screen([], deck: deck)
        XCTAssertEqual(t.stateLine, "1 个在等你 · 2 个在工作", "who waits first, then who works")
        XCTAssertEqual(t.members[0].status, "在工作 · Bash go test")
        XCTAssertEqual(t.members[2].status, "在等你")
        XCTAssertEqual(t.members.filter(\.isLead).map(\.name), ["architect"])
        XCTAssertEqual(t.panel.members, t.members)
        XCTAssertFalse(t.panel.paused)
        XCTAssertTrue(ChannelMapping.screen(channel: "fold", messages: [], deck: deck, paused: true).panel.paused)
    }

    func testBeforeTheDeckComesTheChannelIsStillThere() {
        let s = ChannelMapping.screen(channel: "fold", messages: [msg(1, "architect", "hi")], deck: nil, calendar: cal, now: now)
        XCTAssertEqual(s.title, "fold")
        XCTAssertTrue(s.members.isEmpty)
        XCTAssertEqual(s.stateLine, "")
        XCTAssertEqual(bubbles(s).count, 1)
    }

    // MARK: the team and its helpers

    /// A channel of the fixture deck's first seven agents: the 1st, 2nd and 3rd are the team's roles
    /// (listed out of order, the lead last); the 4th is gone from the deck.
    private func teamDeck() throws -> Deck {
        var deck = try fixture("deck-channel-fold", Deck.self)
        let ids = deck.projects[0].agents.map(\.id)
        let members = ids.prefix(7).enumerated().map { Channel.Member(id: $1, label: "m\($0)", kind: "claude") }
        deck.channels[0].members = Array(members)
        deck.channels[0].team = Channel.ChannelTeam(name: "t", roles: [
            .init(role: "审阅", agent: ids[1]), .init(role: "构建", agent: ids[2]), .init(role: "架构", agent: ids[0], lead: true),
        ])
        deck.channels[0].lead = nil
        // the 4th member's agent no longer exists
        deck.projects[0].agents.removeAll { $0.id == ids[3] }
        return deck
    }

    func testTheTeamIsItsRolesLeadFirstAndTheHelpersAreTheRestWhoStillExist() throws {
        let deck = try teamDeck()
        let ids = ["fold0001-aaaa", "fold0002-aaaa", "fold0003-aaaa", "fold0004-aaaa", "fold0005-aaaa", "fold0006-aaaa", "fold0007-aaaa"]
        let split = ChannelMapping.split(channel: deck.channel(id: "fold"), deck: deck)
        XCTAssertTrue(split.sectioned)
        XCTAssertEqual(split.team.map(\.role), ["架构", "审阅", "构建"], "the lead first, then the roles in their order")
        XCTAssertEqual(split.team.map(\.id), [ids[0], ids[1], ids[2]])
        XCTAssertEqual(split.team.map(\.isLead), [true, false, false])
        XCTAssertEqual(split.helpers.count, 3, "7 members - 3 roles - 1 gone")
        XCTAssertFalse(split.helpers.contains { $0.id == ids[3] })
        XCTAssertTrue(split.helpers.allSatisfy { $0.role.isEmpty })
    }

    func testWorkingAndWaitingHelpersComeFirst() throws {
        var deck = try teamDeck()
        // helpers are the 5th, 6th and 7th agents: the 7th works, the 6th waits
        let i = deck.projects[0].agents.firstIndex { $0.id == "fold0007-aaaa" }!
        deck.projects[0].agents[i].shown = "running"
        let j = deck.projects[0].agents.firstIndex { $0.id == "fold0006-aaaa" }!
        deck.projects[0].agents[j].shown = "waiting"
        let split = ChannelMapping.split(channel: deck.channel(id: "fold"), deck: deck)
        XCTAssertEqual(split.helpers.map(\.id), ["fold0006-aaaa", "fold0007-aaaa", "fold0005-aaaa"], "active ones first, each group in the channel's order")
    }

    func testTheHeadAndThePanelCountTheTeamOnly() throws {
        let deck = try teamDeck()
        let s = screen([], deck: deck)
        XCTAssertEqual(s.members.count, 3)
        XCTAssertEqual(s.helpers.count, 3)
        XCTAssertEqual(s.stateLine, "空闲")
        XCTAssertEqual(s.panel.members, s.members)
        XCTAssertEqual(s.panel.helpers, s.helpers)
        XCTAssertTrue(s.panel.sectioned)
    }

    func testATeamWithoutHelpersHasNoHelperHeading() throws {
        var deck = try teamDeck()
        deck.channels[0].members = Array(deck.channels[0].members.prefix(3))
        let s = screen([], deck: deck)
        XCTAssertEqual(s.panel.helpers, [])
        XCTAssertEqual(s.stateLine, "空闲")
        XCTAssertTrue(s.panel.sectioned, "the team still has its heading")
    }

    func testAChannelWithoutATeamIsOneList() throws {
        let deck = try fixture("deck-channel-fold", Deck.self)
        let split = ChannelMapping.split(channel: deck.channel(id: "fold"), deck: deck)
        XCTAssertFalse(split.sectioned)
        XCTAssertEqual(split.team.count, 8)
        XCTAssertEqual(split.helpers, [])
        XCTAssertEqual(split.team, ChannelMapping.members(channel: deck.channel(id: "fold"), deck: deck))
    }

    func testTheHeadLineSaysWhoWaitsAndWhoWorksHelpersIncluded() throws {
        var deck = try teamDeck()
        func set(_ id: String, _ shown: String) { deck.projects[0].agents[deck.projects[0].agents.firstIndex { $0.id == id }!].shown = shown }
        set("fold0006-aaaa", "waiting") // a helper
        XCTAssertEqual(screen([], deck: deck).stateLine, "1 个在等你")
        set("fold0007-aaaa", "running") // another helper
        set("fold0001-aaaa", "running") // the lead
        XCTAssertEqual(screen([], deck: deck).stateLine, "1 个在等你 · 2 个在工作")
        set("fold0006-aaaa", "idle")
        XCTAssertEqual(screen([], deck: deck).stateLine, "2 个在工作")
        deck.channels[0].paused = true
        XCTAssertEqual(screen([], deck: deck).stateLine, "已暂停", "paused says so whoever works")
    }

    func testADirectMessageSaysItInOneWord() throws {
        var deck = try fixture("deck-channel-fold", Deck.self)
        deck.channels[0].dm = true
        XCTAssertEqual(screen([], deck: deck).stateLine, "空闲")
        deck.projects[0].agents[0].shown = "running"
        XCTAssertEqual(screen([], deck: deck).stateLine, "在工作")
        deck.projects[0].agents[1].shown = "waiting"
        XCTAssertEqual(screen([], deck: deck).stateLine, "在等你")
    }

    func testARoleWithoutAnAgentOrWhoseAgentIsGoneStaysAsAFadedRow() throws {
        var deck = try teamDeck()
        // a 4th role not started, and a 5th whose agent left the deck
        deck.channels[0].team?.roles += [.init(role: "测试"), .init(role: "文档", agent: "gone-agent")]
        let split = ChannelMapping.split(channel: deck.channel(id: "fold"), deck: deck)
        XCTAssertEqual(split.team.count, 5, "they count in 小组")
        let notStarted = try XCTUnwrap(split.team.first { $0.role == "测试" })
        XCTAssertEqual(notStarted.status, "未启动")
        XCTAssertTrue(notStarted.dim)
        XCTAssertEqual(notStarted.state, .stopped, "a still, faded character")
        let gone = try XCTUnwrap(split.team.first { $0.role == "文档" })
        XCTAssertEqual(gone.status, "已离开")
        XCTAssertTrue(gone.dim)
        XCTAssertEqual(split.team.filter(\.dim).count, 2)
        XCTAssertEqual(split.team.first?.role, "架构", "the lead is still first")
        let s = screen([], deck: deck)
        XCTAssertEqual(s.stateLine, "空闲", "they neither work nor wait")
    }

    func testAWaitingHelperLightsTheHomeRowAndAWorkingOneDoesNotChangeItsTile() throws {
        var deck = try teamDeck()
        deck.channels[0].mention = false
        deck.channels[0].unread = 0
        let c = try XCTUnwrap(deck.channel(id: "fold"))
        // nobody waits: no dot, idle
        var row = HomeMapping.chatRow(c, dot: HomeMapping.hasDot(c, needs: []), deck: deck)
        XCTAssertFalse(row.needsYou)
        XCTAssertEqual(row.displayState, .idle)
        // a helper (the 6th agent) waits: the dot and the state follow it
        XCTAssertTrue(HomeMapping.hasDot(c, needs: ["fold0006-aaaa"]))
        deck.projects[0].agents[deck.projects[0].agents.firstIndex { $0.id == "fold0006-aaaa" }!].shown = "waiting"
        row = HomeMapping.chatRow(c, dot: true, deck: deck)
        XCTAssertEqual(row.displayState, .needsYou)
        XCTAssertEqual(row.members.map(\.id), ["fold0002-aaaa", "fold0003-aaaa", "fold0001-aaaa"], "the tile's faces are the team's roles")
        XCTAssertEqual(row.helpers.count, 3)
        XCTAssertFalse(row.helpers.contains { $0.id == "fold0004-aaaa" }, "a helper whose agent is gone is out")
        // a working helper shows in the state, never in the tile
        deck.projects[0].agents[deck.projects[0].agents.firstIndex { $0.id == "fold0006-aaaa" }!].shown = "running"
        row = HomeMapping.chatRow(c, dot: false, deck: deck)
        XCTAssertEqual(row.displayState, .working)
        XCTAssertFalse(row.members.contains { $0.id == "fold0006-aaaa" })
        // the screen's tile faces are the team's too
        let s = screen([], deck: deck)
        XCTAssertFalse(s.members.contains { $0.id == "fold0006-aaaa" })
        XCTAssertTrue(s.helpers.contains { $0.id == "fold0006-aaaa" })
    }
}
