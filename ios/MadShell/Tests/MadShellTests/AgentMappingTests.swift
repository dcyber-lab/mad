import MadKit
import MadUI
import XCTest
@testable import MadShell

/// Real answers of the fixture server (edge-home): the deck, and the transcript of the agent that waits on a Bash permission.
private func fixture<T: Decodable>(_ name: String, as: T.Type) throws -> T {
    let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures"))
    return try JSONDecoder().decode(T.self, from: Data(contentsOf: url))
}

private let needs = "need0001-bbbb"

private func transcript() throws -> ChatTranscript {
    var t = ChatTranscript()
    t.apply(try fixture("chat-need0001", as: ChatPage.self))
    return t
}

private func conversation(deck: Deck? = nil, transcript t: ChatTranscript? = nil, live: LiveState = LiveState(), id: String = needs) throws -> AgentConversation {
    AgentMapping.conversation(id: id, transcript: try t ?? transcript(), live: live, deck: try deck ?? fixture("deck-edge-home", as: Deck.self))
}

final class AgentMappingTests: XCTestCase {
    override func setUp() {
        super.setUp()
        MadLocale.language = "zh-Hans" // the words asserted are the Chinese ones, whatever the machine's language
    }

    func testHeaderFromTheAgent() throws {
        let c = try conversation()
        XCTAssertEqual(c.title, "给我绘制一个表格")
        XCTAssertEqual(c.agentID, needs)
        XCTAssertEqual(c.kind, "claude")
        XCTAssertEqual(c.state, .needsYou)
        XCTAssertTrue(c.needsYou)
    }

    func testEntriesPairAToolWithItsResultById() throws {
        let c = try conversation()
        XCTAssertEqual(c.entries.count, 6, "user, text, two tools, text, a tool: each result folds under its call")
        guard case let .tool(read) = c.entries[2].content, case let .tool(test) = c.entries[3].content,
              case let .tool(running) = c.entries[5].content else { return XCTFail("tools at 2, 3, 5: \(c.entries.map(\.content))") }
        XCTAssertEqual(read.summary, "Read · board.json")
        XCTAssertEqual(read.result, "{\n  \"tasks\": 15,\n  \"done\": 12\n}")
        XCTAssertFalse(read.error)
        XCTAssertTrue(test.error, "a failed result marks its call")
        XCTAssertTrue(test.result?.contains("want 15 tasks") == true)
        XCTAssertNil(running.result, "a call with no result yet is still running")
        XCTAssertTrue(running.running)
    }

    func testUserAndAgentTextKeepTheirMarkdown() throws {
        let c = try conversation()
        XCTAssertEqual(c.entries[0].content, .user(text: "给我绘制一个表格，把本周的任务汇总一下", images: []))
        guard case let .text(md) = c.entries[1].content else { return XCTFail() }
        XCTAssertTrue(md.contains("**report.xlsx**"))
        guard case let .text(long) = c.entries[4].content else { return XCTFail() }
        XCTAssertTrue(Markdown.blocks(long).contains { if case .code = $0 { true } else { false } })
    }

    func testEntryIdsAreUniqueAndStayWhileTheTranscriptGrows() throws {
        let t = try transcript()
        let before = try conversation(transcript: t).entries.map(\.id)
        XCTAssertEqual(Set(before).count, before.count)
        var more = t
        more.apply(ChatPage(file: "fixture", offset: 5000, entries: [ChatEntry(kind: "text", text: "done")]))
        let after = try conversation(transcript: more).entries
        XCTAssertEqual(Array(after.map(\.id).prefix(before.count)), before)
        XCTAssertEqual(after.count, before.count + 1)
    }

    func testCommandsThinkingAndOrphanResultsAreLeftOut() throws {
        var t = ChatTranscript()
        t.apply(ChatPage(file: "f", offset: 1, entries: [
            ChatEntry(kind: "command", text: "/model"), ChatEntry(kind: "other"),
            ChatEntry(kind: "result", text: "orphan", id: "zz"), ChatEntry(kind: "text", text: "hi")]))
        XCTAssertEqual(try conversation(transcript: t).entries.map(\.content), [.text(markdown: "hi")])
    }

    func testImagesKeepTheirAddressForTheApp() {
        var t = ChatTranscript()
        t.apply(ChatPage(file: "f", offset: 1, entries: [
            ChatEntry(kind: "user", text: "look", images: [ChatImage(mime: "image/png", url: "/api/image?id=a&file=f&at=0&e=0&n=0")])]))
        let c = AgentConversation(title: "", agentID: "", entries: AgentMapping.entries(t))
        XCTAssertEqual(c.entries[0].content, .user(text: "look", images: [ConvImage(alt: "image", source: "/api/image?id=a&file=f&at=0&e=0&n=0")]))
    }

    func testTheEarlierNoteComesFirstWhenTheMacLeftEntriesOut() throws {
        var t = ChatTranscript()
        t.apply(ChatPage(file: "f", offset: 1, entries: [ChatEntry(kind: "text", text: "x")], reset: true, earlier: true))
        let e = try conversation(transcript: t).entries
        XCTAssertEqual(e.count, 2)
        guard case .earlier = e[0].content else { return XCTFail() }
    }

    // MARK: the ask

    func testTheAskIsTheAgentsBashPermission() throws {
        let ask = try XCTUnwrap(conversation().ask)
        XCTAssertEqual(ask.id, "k-need")
        XCTAssertEqual(ask.title, "想运行一条命令", "a command's card says so in its head")
        XCTAssertEqual(ask.detail, "$ python3 make_table.py --out report.xlsx")
        XCTAssertTrue(ask.mono)
        XCTAssertTrue(ask.permission)
        XCTAssertEqual(ask.acts.map(\.label), ["允许", "拒绝"])
        XCTAssertEqual(ask.acts.map(\.kind), [.allow, .deny])
        XCTAssertEqual(ask.acts.map(\.primary), [true, false])
        XCTAssertEqual(ask.acts.map(\.input), [false, false])
    }

    func testAnActGoesBackAsTheItemsKeyAndVerb() throws {
        let deck = try fixture("deck-edge-home", as: Deck.self)
        let ask = try XCTUnwrap(AgentMapping.ask(for: needs, deck: deck))
        let allow = try XCTUnwrap(AgentMapping.answer(cardID: ask.id, actID: ask.acts[0].id, text: nil, agent: needs, deck: deck))
        XCTAssertEqual(allow.key, "k-need")
        XCTAssertEqual(allow.verb, "allow")
        XCTAssertNil(allow.option)
        let deny = try XCTUnwrap(AgentMapping.answer(cardID: ask.id, actID: ask.acts[1].id, text: "no", agent: needs, deck: deck))
        XCTAssertEqual(deny.verb, "deny")
        XCTAssertEqual(deny.text, "no")
        XCTAssertNil(AgentMapping.answer(cardID: ask.id, actID: ask.acts[0].id, text: nil, agent: "work0001-aaaa", deck: deck), "another agent has no such item")
        XCTAssertNil(AgentMapping.answer(cardID: "k-old", actID: ask.acts[0].id, text: nil, agent: needs, deck: deck), "a stale card's act")
        XCTAssertNil(AgentMapping.answer(cardID: ask.id, actID: "nope", text: nil, agent: needs, deck: deck))
    }

    func testAnAgentWithNothingWaitingHasNoAsk() throws {
        let c = try conversation(id: "work0001-aaaa")
        XCTAssertNil(c.ask)
        XCTAssertEqual(c.state, .working)
        XCTAssertTrue(c.showsInterrupt(draft: ""))
    }

    func testOpenActsAreNotButtons() throws {
        var deck = try fixture("deck-edge-home", as: Deck.self)
        deck.inbox.items[0].acts.append(InboxAct(verb: "open", label: "Open"))
        XCTAssertEqual(try XCTUnwrap(AgentMapping.ask(for: needs, deck: deck)).acts.map(\.label), ["允许", "拒绝"])
    }

    func testTheBlockingItemWinsOverAnEarlierOne() throws {
        var deck = try fixture("deck-edge-home", as: Deck.self)
        var blocking = deck.inbox.items[0]
        blocking.key = "k-block"
        blocking.blocks = true
        deck.inbox.items[0].blocks = false
        deck.inbox.items.append(blocking)
        deck.inbox.needs = 2
        XCTAssertEqual(AgentMapping.ask(for: needs, deck: deck)?.id, "k-block")
    }

    // MARK: sending

    func testAnAgentInWindowModeTakesMessages() throws {
        let c = try conversation()
        XCTAssertTrue(c.canSend)
        XCTAssertEqual(c.sendHint, "")
    }

    func testAnAgentInATerminalDoesNot() throws {
        var deck = try fixture("deck-edge-home", as: Deck.self)
        let i = try XCTUnwrap(deck.projects[0].agents.firstIndex { $0.id == needs })
        // the server marks ordinary terminal agents headless too: only window mode counts
        deck.projects[0].agents[i].headless = true
        deck.projects[0].agents[i].mode = nil
        let c = try conversation(deck: deck)
        XCTAssertFalse(c.canSend)
        XCTAssertEqual(c.sendHint, "这个 agent 在终端里运行，回复请到 Deck 里的终端")
        deck.projects[0].agents[i].mode = "window"
        XCTAssertTrue(try conversation(deck: deck).canSend, "in window mode it can")
    }

    func testAnAgentThatIsGone() throws {
        let c = try conversation(id: "nobody")
        XCTAssertEqual(c.title, "nobody")
        XCTAssertFalse(c.canSend)
        XCTAssertEqual(c.state, .stopped)
    }

    // MARK: live

    func testTheLiveBubbleIsTheTextBeingWritten() throws {
        var live = LiveState()
        live.receive(LiveMessage(id: "m1", blocks: [LiveBlock(kind: "thinking", text: "hm"), LiveBlock(kind: "text", text: "改好了")], version: 1))
        XCTAssertEqual(try conversation(live: live).live, LiveBubble(markdown: "改好了"))
    }

    func testNoLiveBubbleOnceTheTranscriptHasTheMessage() throws {
        var live = LiveState()
        live.receive(LiveMessage(id: "m1", blocks: [LiveBlock(kind: "text", text: "x")], version: 1))
        var t = try transcript()
        t.apply(ChatPage(file: "fixture", offset: 9000, entries: [ChatEntry(kind: "text", text: "x", msg: "m1")]))
        XCTAssertNil(try conversation(transcript: t, live: live).live)
    }

    func testAMessageThatIsOnlyAToolCallIsNoBubble() throws {
        var live = LiveState()
        live.receive(LiveMessage(id: "m2", blocks: [LiveBlock(kind: "tool", tool: "Bash")], done: true, version: 1))
        XCTAssertNil(try conversation(live: live).live)
        live = LiveState()
        live.receive(LiveMessage(id: "m3", blocks: [LiveBlock(kind: "tool", tool: "Bash")], version: 1))
        XCTAssertEqual(try conversation(live: live).live, LiveBubble(markdown: ""), "still writing: the spinner")
    }

    // MARK: details and finishing

    func testFinishButtonsAreInThePagesWords() {
        let menu = FinishMenu(branch: "feat", base: "trunk", actions: [
            FinishAction(name: "push", kind: "push"), FinishAction(name: "push & open PR", kind: "pr"),
            FinishAction(name: "rebase onto trunk", kind: "rebase"), FinishAction(name: "merge into trunk", kind: "merge"),
            FinishAction(name: "deploy"),
        ])
        let b = AgentMapping.finishButtons(menu)
        XCTAssertEqual(b.map(\.label), ["推送", "推送并开 PR", "变基到 trunk", "合并到 trunk", "deploy"])
        XCTAssertEqual(b.map(\.primary), [false, true, false, false, false])
        XCTAssertEqual(b.map(\.index), [0, 1, 2, 3, 4])
        XCTAssertEqual(b[4].name, "deploy", "the name goes back with the index")
        XCTAssertEqual(AgentMapping.finishNote(b[1], FinishResult(ok: true, output: "done\nhttps://x/pull/7\n")), "推送并开 PR · https://x/pull/7")
        XCTAssertEqual(AgentMapping.finishNote(b[0], FinishResult(ok: false, output: "", error: "exit status 1")), "推送: exit status 1")
    }

    func testTokensAreShort() {
        XCTAssertEqual(AgentMapping.count(950), "950")
        XCTAssertEqual(AgentMapping.count(9400), "9.4k")
        XCTAssertEqual(AgentMapping.count(182_000), "182k")
        XCTAssertEqual(AgentMapping.count(1_200_000), "1.2M")
        XCTAssertEqual(AgentMapping.shortPath("/Users/h/src/mad"), "~/src/mad")
    }

    /// A started agent has no transcript for a moment: its page opens on the person's first message and a working state,
    /// not empty; once the transcript has a message of theirs the bubble is the transcript's, never twice.
    func testAFirstMessageIsShownUntilTheTranscriptHasIt() throws {
        let deck = try fixture("deck-edge-home", as: Deck.self)
        let none = AgentMapping.conversation(id: needs, transcript: ChatTranscript(), live: LiveState(), deck: deck)
        XCTAssertTrue(none.entries.isEmpty, "a transcript with nothing shows nothing")
        let shown = AgentMapping.conversation(id: needs, transcript: ChatTranscript(), live: LiveState(), deck: deck, pending: "画个表")
        XCTAssertEqual(shown.entries.count, 1)
        guard case let .user(text, _) = shown.entries[0].content else { return XCTFail("not the person's bubble: \(shown.entries[0])") }
        XCTAssertEqual(text, "画个表")
        // once the transcript has the person's message the pending one goes: one bubble of it
        var t = ChatTranscript()
        t.apply(try JSONDecoder().decode(ChatPage.self, from: Data(#"{"file":"f","offset":10,"reset":true,"entries":[{"kind":"user","text":"画个表"},{"kind":"text","text":"好"}]}"#.utf8)))
        let later = AgentMapping.conversation(id: needs, transcript: t, live: LiveState(), deck: deck, pending: "画个表")
        XCTAssertEqual(later.entries.count, 2)
        XCTAssertEqual(later.entries.filter { if case .user = $0.content { true } else { false } }.count, 1)
    }

    func testAnIdleStartingOrStoppedAgentWithAFirstMessageIsWorking() throws {
        var deck = try fixture("deck-edge-home", as: Deck.self)
        for state in ["idle", "starting", "stopped"] {
            for i in deck.projects.indices { for j in deck.projects[i].agents.indices where deck.projects[i].agents[j].id == needs {
                deck.projects[i].agents[j].shown = state
            } }
            let c = AgentMapping.conversation(id: needs, transcript: ChatTranscript(), live: LiveState(), deck: deck, pending: "hi")
            XCTAssertEqual(c.state, .working, state)
        }
    }
}
