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
        XCTAssertEqual(ask.title, "想用 Bash")
        XCTAssertEqual(ask.detail, "python3 make_table.py --out report.xlsx")
        XCTAssertTrue(ask.mono)
        XCTAssertEqual(ask.acts.map(\.label), ["Allow", "Deny"])
        XCTAssertEqual(ask.acts.map(\.primary), [true, false])
        XCTAssertEqual(ask.acts.map(\.input), [false, false])
    }

    func testAnActGoesBackAsTheItemsKeyAndVerb() throws {
        let deck = try fixture("deck-edge-home", as: Deck.self)
        let ask = try XCTUnwrap(AgentMapping.ask(for: needs, deck: deck))
        let allow = try XCTUnwrap(AgentMapping.answer(actID: ask.acts[0].id, text: nil, agent: needs, deck: deck))
        XCTAssertEqual(allow.key, "k-need")
        XCTAssertEqual(allow.verb, "allow")
        XCTAssertNil(allow.option)
        let deny = try XCTUnwrap(AgentMapping.answer(actID: ask.acts[1].id, text: "no", agent: needs, deck: deck))
        XCTAssertEqual(deny.verb, "deny")
        XCTAssertEqual(deny.text, "no")
        XCTAssertNil(AgentMapping.answer(actID: ask.acts[0].id, text: nil, agent: "work0001-aaaa", deck: deck), "another agent has no such item")
        XCTAssertNil(AgentMapping.answer(actID: "nope", text: nil, agent: needs, deck: deck))
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
        XCTAssertEqual(try XCTUnwrap(AgentMapping.ask(for: needs, deck: deck)).acts.map(\.label), ["Allow", "Deny"])
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

    func testAHeadlessAgentTakesMessages() throws {
        let c = try conversation()
        XCTAssertTrue(c.canSend)
        XCTAssertEqual(c.sendHint, "")
    }

    func testAnAgentInATerminalDoesNot() throws {
        var deck = try fixture("deck-edge-home", as: Deck.self)
        let i = try XCTUnwrap(deck.projects[0].agents.firstIndex { $0.id == needs })
        deck.projects[0].agents[i].headless = false
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
}
