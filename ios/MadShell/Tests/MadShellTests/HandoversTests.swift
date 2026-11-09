import XCTest
@testable import MadShell

final class HandoversTests: XCTestCase {
    /// A first message that did not go is handed to the agent's page once: its draft and why; reopening gets neither.
    func testADraftAndItsNoteAreTakenOnce() {
        var h = Handovers()
        h.add(StartedChat(id: "a1", text: "画个表", failure: "host is not up"))
        XCTAssertEqual(h.drafts["a1"], "画个表")
        XCTAssertEqual(h.failures["a1"], "host is not up")
        XCTAssertNil(h.firsts["a1"], "a message that did not go is not a bubble")
        h.took("a1")
        XCTAssertNil(h.drafts["a1"], "the draft came back after the page took it")
        XCTAssertNil(h.failures["a1"])
    }

    /// A first message that went is the page's bubble until its transcript has it: taking the handover keeps it, or the
    /// bubble would go before the transcript came.
    func testAFirstMessageThatWentStaysAsTheBubble() {
        var h = Handovers()
        let at = Date()
        h.add(StartedChat(id: "a2", text: "画个表", failure: nil, at: at))
        XCTAssertEqual(h.firsts["a2"], PendingFirst(text: "画个表", since: at))
        XCTAssertNil(h.drafts["a2"])
        XCTAssertNil(h.failures["a2"])
        h.took("a2")
        XCTAssertEqual(h.firsts["a2"], PendingFirst(text: "画个表", since: at))
    }

    func testResumingAPastSessionHandsNothing() {
        var h = Handovers()
        h.add(StartedChat(id: "a3", text: "", failure: nil))
        XCTAssertEqual(h, Handovers())
    }
}
